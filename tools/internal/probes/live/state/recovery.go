package state

// The locked-recovery check (spec 003 T035, T036): whether a stale S3-lockfile lock on an
// Object-Locked (governance) state bucket can be recovered with the state credential alone
// (FR-005 C005.4, C005.8). lz-live runs it on tests/live/probes/locked-recovery, whose sensitive
// output recovery_check publishes the variables (data-model.md *Probe checks*):
//
//   - Observe, through the tofu seam on the disposable backend root (tests/live/probes/
//     locked-recovery/backend, S3 backend with use_lockfile on the probe bucket; the seam's child
//     holds the state key pair only): `init` names the run's bucket; writer A's `apply` waits at its
//     approval prompt holding the lock and is SIGKILLed; only when the seam reports that A exited on
//     that signal (the owner-death proof) does writer B `plan -lock-timeout=0s`, whose refusal is
//     `refused (locked)` only on the lock-held diagnostic naming this run's bucket and state key, and
//     gives the lock id; then `force-unlock -force <id>` and B's plan again. Beside it, with the state
//     user's S3 key: a conditional PUT (`If-None-Match: *`) of the held lock object, sent only while
//     B's refusal shows the lock held, whose body is lock info carrying the held lock's own id, so an
//     ignored condition (a finding: C005.3/C005.4 refuted) leaves force-unlock's id check as it was
//     (OpenTofu 1.13.0 internal/backend/remote-state/s3/client.go:524-557 compares the object's id);
//     and the version restore, which has no tofu command: two versions of a probe key (never the
//     lock object or the backend's live state key), a copy of the first over it by version id, and a
//     GET whose digest is compared with the first's (never recorded or printed).
//   - Each step is an observation (expected, observed, status, code, reason, lock_id); a step
//     observed otherwise is a finding, except B's first plan when the lock was not established
//     (`blocked`, never a refutation). The tofu diagnostics are classified against the fixtures of
//     OpenTofu 1.13.0 in tests/fixtures/tofu-probes/locked-recovery/; anything else is `blocked`
//     with a reason, never `refused`. Observe fails only on an S3 step without an answer (a 5xx or
//     none); a refusal, a blocked step or a finding is an observation.
//   - Undo is state's undo with the janitor (bucket policy, versioning, every version and delete
//     marker with the governance bypass, list again; no tofu), and converges: a second Undo
//     passes, and so does an Undo after a Sweep removed the bucket (404 NoSuchBucket) or both
//     users (403 InvalidAccessKeyId for the janitor, then for the state user: undoGone).
//   - Sweep deletes the bucket and the two users by their exact run-derived names with the sandbox
//     admin, listing every region, then lists again. The bucket is empty when only a lost create
//     answer left it (the check writes only after the outputs exist); a bucket Undo did not empty
//     cannot be deleted while its versions are retained, and the error says so. The janitor is
//     kept while the bucket may still exist (a failed listing or bucket delete).
//
// Nothing here records or prints a key, an object body, a tofu output or the server's text: of a
// tofu run only the classification and a lock id shaped like OpenTofu's (a UUID,
// internal/states/statemgr/locker.go:163-174), of an answer only the status and a well-formed S3
// error code (live.S3Checker).

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// RecoveryOptions are the locked-recovery check's seams; lz-live fills them from the run.
type RecoveryOptions struct {
	HTTP *http.Client     // supporting S3 controls; nil: the S3 client's default
	Now  func() time.Time // nil: time.Now
	// Tofu runs the pinned OpenTofu on the disposable backend root; env holds the state key
	// pair only (no OVH_*, no janitor key). lz-live supplies it (scratch HOME, own TF_DATA_DIR,
	// redacted output). kill: start the command, wait until it waits at its approval prompt
	// (holding the lock), SIGKILL it and report how it exited.
	Tofu  func(ctx context.Context, env map[string]string, kill bool, args ...string) (TofuRun, error)
	API   live.API        // Sweep's listings and deletes
	Admin live.Credential // Sweep: the sandbox admin
	// Project is the run's project (lz-live: the one probe.env records). Undo lists it with the
	// admin to confirm the bucket is gone when neither user's key answers any more (review r2);
	// "": Undo cannot confirm it and fails.
	Project string
}

// TofuRun is how one tofu invocation ended.
type TofuRun struct {
	Exit     int
	Signaled bool   // exited on the SIGKILL the seam sent: the owner-death proof
	Output   []byte // redacted; classified against the fixtures of OpenTofu 1.13.0's diagnostics
}

// RecoveryOutput is the locked-recovery root's sensitive output that publishes the check's
// variables.
const RecoveryOutput = "recovery_check"

const (
	// recPrefix names everything the root creates (the bucket lowercased).
	recPrefix = "lzprobe-locked-recovery-"
	// recStateKey is the backend root's state key (backend/main.tf); OpenTofu's lock object is
	// <key>.tflock (client.go:609-611).
	recStateKey = "lzprobe-locked-recovery/terraform.tfstate"
	recLockKey  = recStateKey + ".tflock"
	// recRestoreKey is the probe key the version restore writes: never the live state key.
	recRestoreKey = "lzprobe-locked-recovery/restore"
)

// recEnvKeys are the keys RecoveryOutput must publish, and the only ones.
var recEnvKeys = []string{"bucket", "janitor_key_id", "janitor_secret", "region", "run_id", "state_key_id", "state_secret"}

var (
	// recLockID is OpenTofu's lock id: a UUID (statemgr.NewLockInfo).
	recLockID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// recProject is a public cloud project id.
	recProject = regexp.MustCompile(`^[0-9a-f]{32}$`)
	// recANSI is a terminal colour sequence (the check passes -no-color; belt and braces).
	recANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")
	// recServerError is an SDK error line's 5xx status.
	recServerError = regexp.MustCompile(`StatusCode: 5[0-9][0-9]\b`)
	// The lock info lines of a diagnostic (OpenTofu's LockInfo.String, statemgr/locker.go).
	recIDLine   = regexp.MustCompile(`^ID:\s+(\S+)$`)
	recPathLine = regexp.MustCompile(`^Path:\s+(\S+)$`)
)

// recSteps are the record's steps, in order, with their operations and expected values.
var recSteps = []struct{ name, op, expected string }{
	{"writer-a-apply", "tofu apply", "killed"},
	{"writer-b-plan", "tofu plan", refused},
	{"lock-conditional-put", "PutObject If-None-Match", refused},
	{"force-unlock", "tofu force-unlock", allowed},
	{"writer-b-plan-again", "tofu plan", allowed},
	{"restore-put-v1", "PutObject", allowed},
	{"restore-put-v2", "PutObject", allowed},
	{"restore-copy", "CopyObject", allowed},
	{"restore-verify", "GetObject", "match"},
}

// Observed values beyond state.go's.
const (
	blocked          = "blocked"
	conditionIgnored = "condition-ignored"
)

// RecoveryObservation is one step's observation as the run record keeps it.
type RecoveryObservation struct {
	Step       string `json:"step"`
	Operation  string `json:"operation"`
	Credential string `json:"credential"`
	Expected   string `json:"expected"`
	Observed   string `json:"observed"`
	Status     int    `json:"status,omitempty"`
	Code       string `json:"code,omitempty"`
	Reason     string `json:"reason,omitempty"`
	LockID     string `json:"lock_id,omitempty"`
	AsExpected bool   `json:"as_expected"`
}

// RecoveryRecord is observations.json: observed is "recovered" (B acquired the lock after the
// unlock), "refused" (the unlock was denied) or "blocked" (with its reason).
type RecoveryRecord struct {
	Check        string                `json:"check"`
	Observed     string                `json:"observed"`
	Reason       string                `json:"reason,omitempty"`
	Observations []RecoveryObservation `json:"observations"`
	Findings     []string              `json:"findings"`
}

// LockedRecovery is the probe check lz-live runs on the locked-recovery root.
func LockedRecovery(o RecoveryOptions) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output: RecoveryOutput,
		Public: []string{"bucket", "region", "run_id"},
		Validate: func(env map[string]string) error {
			_, _, err := o.open(env)
			return err
		},
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			st, _, err := o.open(env)
			if err != nil {
				return nil, err
			}
			if o.Tofu == nil {
				return nil, errors.New("locked-recovery: no tofu seam")
			}
			rec, err := o.observe(ctx, env, st, term)
			raw, jerr := json.Marshal(rec)
			return raw, errors.Join(err, jerr)
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			st, jan, err := o.open(env)
			if err != nil {
				return err
			}
			return undoGone(ctx, st, jan, env["bucket"], term, func() (bool, error) { return o.bucketListed(ctx, env["bucket"]) })
		},
		Sweep: o.sweep,
	}
}

// open checks the published variables and opens the state user's and the janitor's clients.
// Refusals name keys, never values.
func (o RecoveryOptions) open(env map[string]string) (state, janitor s3, err error) {
	if got := slices.Sorted(maps.Keys(env)); !slices.Equal(got, recEnvKeys) {
		return nil, nil, fmt.Errorf("output %s publishes the keys %s, want %s", RecoveryOutput, strings.Join(got, ", "), strings.Join(recEnvKeys, ", "))
	}
	for _, k := range recEnvKeys {
		if env[k] == "" {
			return nil, nil, fmt.Errorf("output %s publishes an empty %s", RecoveryOutput, k)
		}
	}
	if !live.RunIDPattern.MatchString(env["run_id"]) {
		return nil, nil, fmt.Errorf("output %s: run_id is not a run id", RecoveryOutput)
	}
	if env["bucket"] != RecoveryBucket(env["run_id"]) {
		return nil, nil, fmt.Errorf("output %s names bucket %q, not the run's %s", RecoveryOutput, env["bucket"], RecoveryBucket(env["run_id"]))
	}
	client := func(who string) (s3, error) {
		c, err := live.NewS3Checker(live.S3Options{Region: env["region"], HTTP: o.HTTP, Now: o.Now,
			Keys: map[string]string{"AWS_ACCESS_KEY_ID": env[who+"_key_id"], "AWS_SECRET_ACCESS_KEY": env[who+"_secret"]}})
		if err != nil {
			return nil, fmt.Errorf("%s credential: %w", who, err)
		}
		return c, nil
	}
	if state, err = client("state"); err != nil {
		return nil, nil, err
	}
	if janitor, err = client("janitor"); err != nil {
		return nil, nil, err
	}
	return state, janitor, nil
}

// RecoveryBucket is the run's probe bucket: lzprobe-locked-recovery-<lower(run id)>.
func RecoveryBucket(runID string) string { return recPrefix + strings.ToLower(runID) }

// RecoveryUsers are the descriptions of the root's state user and janitor.
func RecoveryUsers(runID string) []string {
	return []string{recPrefix + runID, recPrefix + "janitor-" + runID}
}

// recLines are a tofu output's lines without colours and the diagnostic box's edges, trimmed.
func recLines(out []byte) []string {
	var ls []string
	for _, l := range strings.Split(recANSI.ReplaceAllString(string(out), ""), "\n") {
		l = strings.TrimSpace(l)
		for _, edge := range []string{"│", "╷", "╵"} {
			l = strings.TrimSpace(strings.TrimPrefix(l, edge))
		}
		ls = append(ls, l)
	}
	return ls
}

// recText is a tofu output's words, single-spaced: OpenTofu wraps a diagnostic's detail, so a
// phrase may span lines.
func recText(out []byte) string {
	return strings.Join(strings.Fields(strings.Join(recLines(out), " ")), " ")
}

// recBlocked names why a failed tofu step is blocked, from the fixtures' diagnostics.
func recBlocked(r TofuRun) string {
	t := recText(r.Output)
	switch {
	case strings.Contains(t, "request send failed"):
		return "transport"
	case strings.Contains(t, "StatusCode: 404"):
		return "not found"
	case recServerError.MatchString(t):
		return "server error"
	case strings.Contains(t, "Backend initialization required"):
		return "configuration"
	case strings.Contains(t, "does not match existing lock"):
		return "lock id mismatch"
	}
	return "unrecognised diagnostic"
}

// recPlan classifies a plan: allowed (exit 0), refused (locked) with the lock id when the
// lock-held diagnostic names this run's bucket and state key and a lock id of OpenTofu's shape,
// else blocked with a reason.
func recPlan(r TofuRun, err error, bucket string) (observed, reason, lockID string) {
	switch {
	case err != nil:
		return blocked, "seam error", ""
	case r.Exit == 0:
		return allowed, "", ""
	case !strings.Contains(recText(r.Output), "Error acquiring the state lock"):
		return blocked, recBlocked(r), ""
	case !strings.Contains(recText(r.Output), "StatusCode: 412") || !strings.Contains(recText(r.Output), "api error PreconditionFailed"):
		// OpenTofu prints the lock info it finds after any failed lock PUT (client.go:336-345):
		// only the conditional PUT's own 412 shows a held lock refused B; a 5xx or a transport
		// error beside lock info is an outage (review r1).
		return blocked, recBlocked(r), ""
	}
	var id, path string
	for _, l := range recLines(r.Output) {
		if m := recIDLine.FindStringSubmatch(l); m != nil {
			id = m[1]
		}
		if m := recPathLine.FindStringSubmatch(l); m != nil {
			path = m[1]
		}
	}
	switch {
	case !recLockID.MatchString(id):
		return blocked, "lock id unrecognised", ""
	case path != bucket+"/"+recStateKey:
		return blocked, "lock elsewhere", ""
	}
	return refused, "locked", id
}

// recUnlock classifies force-unlock: allowed on OpenTofu's success message, refused only on an
// attributable denial (an unlock failure carrying `api error AccessDenied`, from the GET or the
// DELETE of the lock object), else blocked with a reason.
func recUnlock(r TofuRun, err error) (observed, reason string) {
	if err != nil {
		return blocked, "seam error"
	}
	t := recText(r.Output)
	switch {
	case r.Exit == 0 && strings.Contains(t, "OpenTofu state has been successfully unlocked!"):
		return allowed, ""
	case r.Exit == 0:
		return blocked, "unrecognised diagnostic"
	case strings.Contains(t, "Failed to unlock state:") && strings.Contains(t, "api error AccessDenied"):
		return refused, "AccessDenied"
	}
	return blocked, recBlocked(r)
}

// recLockInfo is the conditional PUT's body: lock info shaped like OpenTofu's (statemgr.LockInfo)
// carrying the held lock's id, so that if the condition is ignored, the object force-unlock reads
// still names the lock it is told to break.
type recLockInfo struct {
	ID        string
	Operation string
	Info      string
	Who       string
	Version   string
	Created   time.Time
	Path      string
}

// observe runs the steps; the record is complete on every path.
func (o RecoveryOptions) observe(ctx context.Context, env map[string]string, st s3, term io.Writer) (RecoveryRecord, error) {
	bucket := env["bucket"]
	now := o.Now
	if now == nil {
		now = time.Now
	}
	rec := RecoveryRecord{Check: "locked-recovery", Observed: blocked, Observations: []RecoveryObservation{}, Findings: []string{}}
	obs := map[string]*RecoveryObservation{}
	for _, s := range recSteps {
		obs[s.name] = &RecoveryObservation{Step: s.name, Operation: s.op, Credential: "state", Expected: s.expected, Observed: notRun}
	}
	// The seam's child gets the state key pair and nothing else.
	keys := map[string]string{"AWS_ACCESS_KEY_ID": env["state_key_id"], "AWS_SECRET_ACCESS_KEY": env["state_secret"]}
	tofu := func(kill bool, args ...string) (TofuRun, error) { return o.Tofu(ctx, maps.Clone(keys), kill, args...) }
	var errs []error
	// call sends one S3 step; a 5xx or no answer makes it no-answer and fails Observe.
	call := func(ob *RecoveryObservation, c live.S3Call) (live.S3Answer, bool) {
		a, err := st.Do(ctx, c)
		if err != nil {
			ob.Observed = noAnswer
			errs = append(errs, fmt.Errorf("%s: %w", ob.Step, err))
			return a, false
		}
		ob.Status, ob.Code = a.Status, a.Code
		if a.Status >= 500 {
			ob.Observed = noAnswer
			errs = append(errs, fmt.Errorf("%s: answered %d %s", ob.Step, a.Status, a.Code))
			return a, false
		}
		return a, true
	}
	ok2xx := func(a live.S3Answer) bool { return a.Status >= 200 && a.Status <= 299 }

	notEstablished := false
	func() {
		a := obs["writer-a-apply"]
		endpoint, err := live.ObjectStorageEndpoint(env["region"])
		if err != nil {
			a.Observed, a.Reason, rec.Reason = blocked, "region", "region"
			return
		}
		if r, err := tofu(false, "init", "-input=false", "-no-color", "-lockfile=readonly",
			"-backend-config=bucket="+bucket,
			"-backend-config=region="+strings.ToLower(env["region"]),
			`-backend-config=endpoints={s3="`+endpoint+`"}`); err != nil || r.Exit != 0 {
			a.Observed, a.Reason, rec.Reason = blocked, "init failed", "init failed"
			return
		}
		// Writer A: no -auto-approve, no -input=false, so it waits at its prompt holding the lock.
		r, err := tofu(true, "apply", "-no-color")
		switch {
		case err == nil && !r.Signaled && r.Exit != 0 && strings.Contains(recText(r.Output), "Error acquiring the state lock"):
			// A exited before its prompt because it could not take the lock (review r2): not an
			// owner that may still hold it, a lock never taken.
			why := "lock not taken (" + recBlocked(r) + ")"
			a.Observed, a.Reason, rec.Reason = blocked, why, why
			return
		case err != nil || !r.Signaled:
			a.Observed, a.Reason, rec.Reason = blocked, "owner alive", "owner alive"
			return
		}
		a.Observed = "killed"

		b := obs["writer-b-plan"]
		var id string
		r, err = tofu(false, "plan", "-no-color", "-input=false", "-lock-timeout=0s")
		b.Observed, b.Reason, id = recPlan(r, err, bucket)
		switch b.Observed {
		case allowed:
			notEstablished, rec.Reason = true, "lock not established"
			return
		case blocked:
			rec.Reason = b.Reason
			return
		}
		b.LockID = id

		c := obs["lock-conditional-put"]
		body, err := json.Marshal(recLockInfo{ID: id, Operation: "OperationTypeInvalid", Info: "lzprobe-locked-recovery conditional PUT",
			Who: "lz-live", Created: now().UTC(), Path: bucket + "/" + recStateKey})
		if err != nil {
			c.Observed, c.Reason = blocked, "lock info"
		} else if ans, ok := call(c, live.S3Call{Op: live.S3PutObject, Bucket: bucket, Key: recLockKey, Body: body, IfNoneMatch: true}); ok {
			c.Observed = refused
			switch {
			case ok2xx(ans):
				c.Observed = conditionIgnored
			case ans.Status != http.StatusPreconditionFailed:
				// Refused, but not by the condition (403, 400 …): not as expected, no refutation of
				// C005.3/C005.4 either (review r1); the code says why.
				c.Reason = "not a precondition failure"
			}
		}

		u := obs["force-unlock"]
		r, err = tofu(false, "force-unlock", "-no-color", "-force", id)
		u.Observed, u.Reason = recUnlock(r, err)
		switch u.Observed {
		case refused:
			rec.Observed, rec.Reason = refused, u.Reason
			return
		case blocked:
			rec.Reason = u.Reason
			return
		}

		p := obs["writer-b-plan-again"]
		r, err = tofu(false, "plan", "-no-color", "-input=false", "-lock-timeout=0s")
		p.Observed, p.Reason, p.LockID = recPlan(r, err, bucket)
		switch p.Observed {
		case allowed:
			rec.Observed, rec.Reason = "recovered", ""
		case refused:
			rec.Reason = "lock still held"
		default:
			rec.Reason = p.Reason
		}
	}()

	// The version restore, whatever the tofu steps observed.
	run := env["run_id"]
	v1, v2 := []byte("lz-locked-recovery v1 "+run+"\n"), []byte("lz-locked-recovery v2 "+run+"\n")
	put := func(name string, body []byte) string {
		ob := obs[name]
		a, ok := call(ob, live.S3Call{Op: live.S3PutObject, Bucket: bucket, Key: recRestoreKey, Body: body})
		switch {
		case !ok:
			return ""
		case !ok2xx(a):
			ob.Observed = refused
			return ""
		}
		ob.Observed = allowed
		if a.VersionID == "" {
			ob.Reason = "no version id"
		}
		return a.VersionID
	}
	if id1 := put("restore-put-v1", v1); id1 != "" {
		id2 := put("restore-put-v2", v2)
		switch {
		case id2 == id1:
			obs["restore-put-v2"].Reason = "the version id of v1"
		case id2 != "":
			cp := obs["restore-copy"]
			if a, ok := call(cp, live.S3Call{Op: live.S3CopyObject, Bucket: bucket, Key: recRestoreKey, CopyFrom: recRestoreKey, CopyVersion: id1}); ok {
				cp.Observed = refused
				if ok2xx(a) {
					cp.Observed = allowed
					vf := obs["restore-verify"]
					if g, ok := call(vf, live.S3Call{Op: live.S3GetObject, Bucket: bucket, Key: recRestoreKey}); ok {
						vf.Observed = "mismatch"
						if g.Status == http.StatusOK && sha256.Sum256(g.Body) == sha256.Sum256(v1) {
							vf.Observed = "match"
						}
					}
				}
			}
		}
	}

	for _, s := range recSteps {
		ob := obs[s.name]
		ob.AsExpected = ob.Observed == ob.Expected
		switch s.name {
		case "writer-b-plan":
			ob.AsExpected = ob.AsExpected && ob.Reason == "locked"
		case "lock-conditional-put":
			ob.AsExpected = ob.AsExpected && ob.Status == http.StatusPreconditionFailed
		case "restore-put-v1", "restore-put-v2":
			// Allowed but without a version id to restore by: the restore was not shown (review r2).
			ob.AsExpected = ob.AsExpected && ob.Reason == ""
		}
		rec.Observations = append(rec.Observations, *ob)
		fmt.Fprintf(term, "LZ-LIVE observe %s %s %d %s %s\n", ob.Step, ob.Observed, ob.Status, ob.Code, ob.Reason)
		judged := slices.Contains([]string{allowed, refused, conditionIgnored, "mismatch"}, ob.Observed)
		// A step observed as expected but not as_expected (a refusal not caused by the condition)
		// carries its reason and refutes nothing (review r1).
		if ob.AsExpected || ob.Observed == ob.Expected || !judged || s.name == "writer-b-plan" && notEstablished {
			continue
		}
		f := fmt.Sprintf("%s: expected %s, observed %s", ob.Step, ob.Expected, ob.Observed)
		if ob.Observed == conditionIgnored {
			f += " (refutes C005.3/C005.4: a conditional write does not exclude a second writer)"
		}
		rec.Findings = append(rec.Findings, f)
		fmt.Fprintf(term, "LZ-LIVE finding %s\n", f)
	}
	fmt.Fprintf(term, "LZ-LIVE observe locked-recovery %s %s\n", rec.Observed, rec.Reason)
	return rec, errors.Join(errs...)
}

// undoGone is state's undo, except when a first listing as the janitor shows the run's
// resources already removed: 404 NoSuchBucket (no bucket, so no retained version), or 403
// InvalidAccessKeyId for the janitor and then for the state user too, and the admin's listing of
// the project no longer shows the bucket (listed; review r2: invalid keys alone prove nothing
// about the bucket, they may have been revoked by hand). An invalid janitor key beside a state key
// that still answers (a key not yet in effect) fails Undo too, and the root is kept (review r1).
func undoGone(ctx context.Context, st, jan s3, bucket string, term io.Writer, listed func() (bool, error)) error {
	a, err := jan.Do(ctx, live.S3Call{Op: live.S3ListObjectVersions, Bucket: bucket})
	switch {
	case err != nil:
	case a.Status == http.StatusNotFound && a.Code == "NoSuchBucket":
		fmt.Fprintf(term, "LZ-LIVE undo %s already removed (no such bucket)\n", bucket)
		return nil
	case a.Status == http.StatusForbidden && a.Code == "InvalidAccessKeyId":
		b, err := st.Do(ctx, live.S3Call{Op: live.S3ListObjectVersions, Bucket: bucket})
		if err == nil && b.Status == http.StatusForbidden && b.Code == "InvalidAccessKeyId" {
			still, err := listed()
			switch {
			case err != nil:
				return fmt.Errorf("undo: both users' keys are refused and the bucket's absence could not be confirmed: %w", err)
			case still:
				return errors.New("undo: both users' keys are refused but the bucket is still listed: it cannot be emptied; the root is kept for --cleanup")
			}
			fmt.Fprintf(term, "LZ-LIVE undo %s already removed (both users' keys gone, bucket not listed)\n", bucket)
			return nil
		}
		return errors.New("undo: the janitor's key is refused (InvalidAccessKeyId) while the state user's is not: the bucket cannot be emptied; the root is kept for --cleanup")
	}
	return undo(ctx, jan, bucket, term)
}

// bucketListed reports whether any region of the run's project lists bucket, with the sandbox
// admin (Undo's confirmation that a swept bucket is gone).
func (o RecoveryOptions) bucketListed(ctx context.Context, bucket string) (bool, error) {
	if !recProject.MatchString(o.Project) {
		return false, errors.New("no project to list")
	}
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return false, fmt.Errorf("admin identity: %w", err)
	}
	do := func(call live.APICall) (live.APIAnswer, error) {
		call.Project = o.Project
		a, err := admin.Do(ctx, call)
		if err == nil && (a.Status < 200 || a.Status > 299) && !(a.Status == http.StatusNotFound && a.NoBucketService) {
			err = fmt.Errorf("%s answered %d", call.Op, a.Status)
		}
		return a, err
	}
	a, err := do(live.APICall{Op: live.APIListRegions})
	if err != nil {
		return false, err
	}
	for _, r := range a.Names {
		b, err := do(live.APICall{Op: live.APIListBuckets, Region: r})
		if err != nil {
			return false, fmt.Errorf("region %s: %w", r, err)
		}
		if slices.Contains(b.Names, bucket) {
			return true, nil
		}
	}
	return false, nil
}

// sweep is the check's Sweep (data-model.md *Probe checks*): it deletes, by listing the run's
// exact names with the sandbox admin, the bucket lzprobe-locked-recovery-<lower(run id)> in
// whichever region lists it and the users lzprobe-locked-recovery-<run id> and
// lzprobe-locked-recovery-janitor-<run id>, whether or not an output or a state entry exists, then
// lists again; anything still listed is an error.
func (o RecoveryOptions) sweep(ctx context.Context, runID, project string, term io.Writer) error {
	if !live.RunIDPattern.MatchString(runID) {
		return errors.New("sweep locked-recovery: the run id is not a run id")
	}
	if !recProject.MatchString(project) {
		return errors.New("sweep locked-recovery: the project is not a project id")
	}
	bucket, users := RecoveryBucket(runID), RecoveryUsers(runID)
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return fmt.Errorf("sweep locked-recovery: admin identity: %w", err)
	}
	// do sends a call on the project; a non-2xx answer is an error, but a 404 to a delete (gone
	// already) or to a region's bucket listing where the region offers no bucket service.
	do := func(call live.APICall) (live.APIAnswer, error) {
		call.Project = project
		a, err := admin.Do(ctx, call)
		absent := a.Status == http.StatusNotFound && (a.NoBucketService || call.Op == live.APIDeleteBucket || call.Op == live.APIDeleteUser)
		if err == nil && (a.Status < 200 || a.Status > 299) && !absent {
			err = fmt.Errorf("%s answered %d %s", call.Op, a.Status, strings.Join(a.Actions, ","))
		}
		return a, err
	}
	// list finds the regions that list the run's bucket and the run's users (id → description).
	list := func() ([]string, map[string]string, []error) {
		var errs []error
		var regions []string
		people := map[string]string{}
		if a, err := do(live.APICall{Op: live.APIListRegions}); err != nil {
			errs = append(errs, err)
		} else {
			for _, r := range a.Names {
				b, err := do(live.APICall{Op: live.APIListBuckets, Region: r})
				if err != nil {
					errs = append(errs, fmt.Errorf("region %s: %w", r, err))
				}
				if slices.Contains(b.Names, bucket) {
					regions = append(regions, r)
				}
			}
		}
		if a, err := do(live.APICall{Op: live.APIListUsers}); err != nil {
			errs = append(errs, err)
		} else {
			for _, u := range a.Users {
				if slices.Contains(users, u.Description) {
					people[u.ID] = u.Description
				}
			}
		}
		return regions, people, errs
	}
	regions, people, errs := list()
	// The janitor is the only identity that can empty the bucket (the governance bypass): kept
	// while the bucket may still exist, that is when a listing failed or its delete did not
	// succeed (review r1).
	keepJanitor := len(errs) > 0
	removed := 0
	for _, r := range regions {
		if _, err := do(live.APICall{Op: live.APIDeleteBucket, Region: r, Name: bucket}); err != nil {
			errs = append(errs, fmt.Errorf("bucket %s (%s): %w (a bucket holding retained versions is deleted only after Undo empties it)", bucket, r, err))
			keepJanitor = true
			continue
		}
		removed++
	}
	for _, id := range slices.Sorted(maps.Keys(people)) {
		if keepJanitor && people[id] == users[1] {
			errs = append(errs, fmt.Errorf("user %s kept: the bucket may still hold versions only it can delete", people[id]))
			continue
		}
		if _, err := do(live.APICall{Op: live.APIDeleteUser, ID: id}); err != nil {
			errs = append(errs, fmt.Errorf("user %s: %w", people[id], err))
			continue
		}
		removed++
	}
	regions, people, lerrs := list()
	errs = append(errs, lerrs...)
	for _, r := range regions {
		errs = append(errs, fmt.Errorf("bucket %s (%s) still listed", bucket, r))
	}
	for _, id := range slices.Sorted(maps.Keys(people)) {
		errs = append(errs, fmt.Errorf("user %s still listed", people[id]))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("sweep locked-recovery %s: %w", runID, err)
	}
	fmt.Fprintf(term, "LZ-LIVE sweep locked-recovery removed %d buckets and users of run %s\n", removed, runID)
	return nil
}
