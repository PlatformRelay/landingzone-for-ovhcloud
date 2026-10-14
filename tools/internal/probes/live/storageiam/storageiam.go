// Package storageiam is the storage-iam probe's check (spec 005 T100): what the probe identities
// the root creates may and may not do, observed by lz-live after the companion's apply, so that an
// answer other than the expected one fails the run. Before T100 the companion read the buckets and
// GET /me in OpenTofu check blocks, where a refused read is only a warning: T010 run
// 20261012T072729Z-0646 printed `pass` while the P25 identity was refused its own bucket.
//
// lz-live runs it on tests/live/probes/storage-iam (live.ProbeCheck), after the companion:
//
//   - Observe, five observations in order: the P25 identity reads bucket A and bucket B, both
//     refused; the P9 tenant identity calls GET /me (refused, P26) and attempts one IAM write,
//     POST /v2/iam/policy (refused, P9); the P1 S3 key reads the companion's state object back (an
//     OpenTofu encryption envelope, not plaintext, P3; the x-amz-server-side-encryption header
//     recorded when present). Bucket A is refused although the P25 policy allows
//     region/storage/get where lz:tenant = lzprobe-a: T010 run 20261012T072729Z-0646 saw it refused
//     403 naming publicCloudProject:apiovh:region/storage/get, P25 is refuted, and coordinator
//     decision D107 (2026-10-12, T101) makes that refusal the expected answer; an allowed bucket-A
//     read is a finding saying P25 holds after all (re-open tag-scoped envelopes). A refused
//     bucket-A read is also what a P25 policy not yet in effect would answer (IAM propagation
//     delay UNVERIFIED), so a `pass` does not show the tag condition was evaluated: before the
//     read, the sandbox admin lists the P25 policy (lzprobe-p25-<run id>) and the observation
//     records the seconds from its createdAt to the read (policy_age_seconds; local clock against
//     OVHcloud's) with a note saying so; an age the listing cannot tell is said, never a finding
//     (T113, T101 gap 6). A refusal is as expected when it is a 403 that names the step's IAM action or none (bucket
//     A's must name it: D107 names the action); one naming another action is not the denial the
//     step observes. Every answer is recorded; an answer
//     other than the expected one is a finding: listed, printed as `LZ-LIVE finding`, and Observe's
//     error, so the run fails naming the step and the refused actions. Each OAuth2 identity is bound
//     to the run's account (G13) before its first call.
//   - Undo, with the sandbox admin's credential: deletes every IAM policy named
//     lzprobe-p9-write-<run id> and every policy id an accepted write returned to this check's
//     Observe (T101: a listing that lags behind the create, or a name OVHcloud normalised, would
//     miss it by name), each once, and lists again, keeping the name and those ids (review r2: a
//     delete acknowledged but not effective under another name); anything left, or a delete
//     refused (the other deletes are still tried), is an error, and lz-live keeps the root for
//     `--cleanup`. Undo also runs after an interrupted
//     Observe and on a cleanup; there, a fresh process, Recorded reads every policy_id from the
//     run's observations.json first, so the undo deletes those ids as well as the name (T113, T101
//     gap 2). No observations.json: by name only, said on the terminal; a malformed one, a
//     policy_id that is not a policy uuid or not on the accepted IAM write, fails the undo step
//     before any request. A recorded id comes from a file: it is deleted only when the first
//     listing does not show it under another name (one shown as, say, the admin's own policy is
//     refused and fails the undo; a name differing in case only counts as the run's).
//
// The credentials come from the root's sensitive output iam_check; lz-live has added every value
// whose key is not public to its redactor. Nothing here prints or records a credential, the
// object or the server's text: of an API answer the status and the action-shaped tokens a refusal
// names (live.APIChecker), of an S3 answer the status, the error code and the SSE header
// (live.S3Checker).
package storageiam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// Output is the probe root's sensitive output that publishes the check's variables.
const Output = "iam_check"

// StateKey is the companion's state object in the P1 bucket (companion/probe.tf, backend key).
const StateKey = "storage-iam-companion.tfstate"

// Public are the keys of Output whose values are no secret; lz-live redacts every other value.
var Public = []string{"bucket_a", "bucket_b", "project_id", "project_urn", "region", "run_id", "state_bucket", "state_key", "tenant_identity"}

// envKeys are the keys Output must publish, and the only ones: the public ones and the three
// credentials (P25 identity, P9 tenant identity, P1 S3 user).
var envKeys = slices.Sorted(slices.Values(append(slices.Clone(Public),
	"p25_client_id", "p25_client_secret", "s3_key_id", "s3_secret", "tenant_client_id", "tenant_client_secret")))

// The IAM actions the steps observe (kb/api/v1/cloud.json, me.json; kb/api/v2/iam.json).
const (
	readAction  = "publicCloudProject:apiovh:region/storage/get"
	meAction    = "account:apiovh:me/get"
	writeAction = "account:apiovh:iam/policy/create"
)

// WriteName is the name of the policy the IAM-write step asks for; Undo deletes every policy of
// that name.
func WriteName(runID string) string { return "lzprobe-p9-write-" + runID }

var (
	projectID   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	regionName  = regexp.MustCompile(`^[A-Z]+$`)
	identityURN = regexp.MustCompile(`^urn:v1:(eu|ca):identity:[\x21-\x7e]{1,200}$`)
)

// Options are the check's seams; lz-live fills them from the run.
type Options struct {
	API   live.API        // the OVHcloud API of the run's endpoint
	Admin live.Credential // the sandbox admin: the P25 policy's listing, Undo's listing and delete; its endpoint is the identities'
	// Bind binds a probe identity to the run's account before its first call (G13); nil binds nothing.
	Bind func(context.Context, live.Credential) error
	HTTP *http.Client     // the S3 client; nil: its default
	Now  func() time.Time // nil: time.Now
}

// Observation is one step's answer as the run record keeps it.
type Observation struct {
	Step       string   `json:"step"`
	Operation  string   `json:"operation"`
	Credential string   `json:"credential"`
	Expected   string   `json:"expected"`
	Observed   string   `json:"observed"`
	Status     int      `json:"status,omitempty"`
	Actions    []string `json:"actions,omitempty"`   // the IAM actions a refusal named
	Code       string   `json:"code,omitempty"`      // an S3 refusal's error code
	SSE        string   `json:"sse,omitempty"`       // the state object's x-amz-server-side-encryption
	Detail     string   `json:"detail,omitempty"`    // the state object's classification
	PolicyID   string   `json:"policy_id,omitempty"` // the policy an accepted IAM write created
	// PolicyAge (p25-read-bucket-a): seconds from the P25 policy's createdAt to the read (T113).
	PolicyAge  *int64 `json:"policy_age_seconds,omitempty"`
	Note       string `json:"note,omitempty"` // what the answer does not show (T113)
	AsExpected bool   `json:"as_expected"`
}

// Record is observations.json.
type Record struct {
	Check        string        `json:"check"`
	Observations []Observation `json:"observations"`
	Findings     []string      `json:"findings"`
}

// Observed values.
const (
	allowed    = "allowed"
	refused    = "refused"
	encrypted  = "encrypted"
	plaintext  = "plaintext"
	unreadable = "unreadable"
	notRun     = "not-run"
	noAnswer   = "no-answer"
)

// Check is the probe check lz-live runs on the storage-iam root. Its Observe hands the Undo of the
// same check the ids of the policies an accepted IAM write created.
func Check(o Options) *live.ProbeCheck {
	var mu sync.Mutex
	var written, recorded []string // ids this process's Observe saw; ids read back from observations.json
	return &live.ProbeCheck{
		Output:   Output,
		Public:   Public,
		Validate: validate,
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			if err := validate(env); err != nil {
				return nil, err
			}
			rec, err := o.observe(ctx, env, term)
			mu.Lock()
			for _, ob := range rec.Observations {
				if ob.PolicyID != "" && !slices.Contains(written, ob.PolicyID) {
					written = append(written, ob.PolicyID)
				}
			}
			mu.Unlock()
			raw, jerr := json.Marshal(rec)
			return raw, errors.Join(err, jerr)
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			if err := validate(env); err != nil {
				return err
			}
			mu.Lock()
			ids, back := slices.Clone(written), slices.Clone(recorded)
			mu.Unlock()
			return o.undo(ctx, env, ids, back, term)
		},
		// A cleanup in a fresh process: the ids are only in the run's observations.json (T113).
		Recorded: func(record []byte, term io.Writer) error {
			if record == nil {
				fmt.Fprintf(term, "LZ-LIVE undo no observations.json: deleting by name only (lzprobe-p9-write-<run id>)\n")
				return nil
			}
			ids, accepted, err := recordedIDs(record)
			if err != nil {
				return err
			}
			if accepted && len(ids) == 0 {
				fmt.Fprintf(term, "LZ-LIVE undo observations.json records an accepted write without its id: deleting by name only (lzprobe-p9-write-<run id>)\n")
			}
			mu.Lock()
			for _, id := range ids {
				if !slices.Contains(recorded, id) {
					recorded = append(recorded, id)
				}
			}
			mu.Unlock()
			return nil
		},
	}
}

// policyUUID is a policy id as DELETE /v2/iam/policy/{policyId} takes it (kb/api/v2/iam.json:
// policyId is a uuid; lower case, as the API answers it and live.APIChecker admits it).
var policyUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// recordedIDs reads the policy ids an earlier process's Observe recorded in observations.json: the
// record must be this check's, strict JSON of Record and nothing after it, and every policy_id a
// policy uuid; anything else is refused whole, before any request (T113: never a silent name-only
// undo, never an id of another shape in a request path).
//
// Only the IAM-write step observed `allowed` may carry a policy_id (review r1: an id on another
// step is not the run's write); accepted says whether that step was recorded allowed. Refusals are
// fixed texts with the observation's index, never the record's content (review r1).
func recordedIDs(record []byte) (ids []string, accepted bool, err error) {
	const bad = "observations.json is not a storage-iam record; nothing deleted"
	var rec *Record
	dec := json.NewDecoder(bytes.NewReader(record))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return nil, false, errors.New(bad)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false, errors.New("observations.json: data after the record; nothing deleted")
	}
	if rec == nil || rec.Check != "storage-iam" {
		return nil, false, errors.New(bad)
	}
	for i, ob := range rec.Observations {
		write := ob.Step == "p9-iam-write" && ob.Observed == allowed
		accepted = accepted || write
		switch {
		case ob.PolicyID == "":
		case !write:
			return nil, false, fmt.Errorf("observations.json: observation %d carries a policy_id but is not an accepted IAM write; nothing deleted", i)
		case !policyUUID.MatchString(ob.PolicyID):
			return nil, false, fmt.Errorf("observations.json: the policy_id of observation %d is not a policy uuid; nothing deleted", i)
		default:
			ids = append(ids, ob.PolicyID)
		}
	}
	return ids, accepted, nil
}

// validate refuses published variables that are not the storage-iam root's of one run: the keys,
// none empty, the run id, the names derived from it, the project and its URN, the region, the
// tenant identity's URN. Refusals name keys and the expected public values, never a secret.
func validate(env map[string]string) error {
	if got := slices.Sorted(maps.Keys(env)); !slices.Equal(got, envKeys) {
		return fmt.Errorf("output %s publishes the keys %s, want %s", Output, strings.Join(got, ", "), strings.Join(envKeys, ", "))
	}
	for _, k := range envKeys {
		if env[k] == "" {
			return fmt.Errorf("output %s publishes an empty %s", Output, k)
		}
	}
	if !live.RunIDPattern.MatchString(env["run_id"]) {
		return fmt.Errorf("output %s: run_id is not a run id", Output)
	}
	run := strings.ToLower(env["run_id"])
	for _, k := range []string{"bucket_a", "bucket_b", "state_bucket", "state_key"} {
		want := map[string]string{"bucket_a": "lzprobe-p25a-" + run, "bucket_b": "lzprobe-p25b-" + run, "state_bucket": "lzprobe-p1-" + run, "state_key": StateKey}[k]
		if env[k] != want {
			return fmt.Errorf("output %s: %s is not %s", Output, k, want)
		}
	}
	switch {
	case !projectID.MatchString(env["project_id"]):
		return fmt.Errorf("output %s: project_id is not a project id", Output)
	case !regexp.MustCompile(`^urn:v1:(eu|ca):resource:publicCloudProject:` + env["project_id"] + `$`).MatchString(env["project_urn"]):
		return fmt.Errorf("output %s: project_urn is not the URN of project_id", Output)
	case !regionName.MatchString(env["region"]):
		return fmt.Errorf("output %s: region is not a region name", Output)
	case !identityURN.MatchString(env["tenant_identity"]):
		return fmt.Errorf("output %s: tenant_identity is not an identity URN", Output)
	}
	return nil
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// step is one API observation: by whom (p25, tenant), which call, what is expected and which IAM
// action a refusal of it names. A refusal is the step's when it is a 403 naming its action, or —
// unless named — no action at all.
type step struct {
	name, who string
	call      live.APICall
	expected  string
	action    string
	named     bool // the refusal must name action (D107 for bucket A)
}

// observe makes the five observations in order; every step runs (unless the context ended or its
// identity could not be bound or authenticated), so one finding does not hide the others.
func (o Options) observe(ctx context.Context, env map[string]string, term io.Writer) (Record, error) {
	rec := Record{Check: "storage-iam", Observations: []Observation{}, Findings: []string{}}
	var errs []error
	creds := map[string]live.Credential{
		"p25":    {Endpoint: o.Admin.Endpoint, ClientID: env["p25_client_id"], ClientSecret: env["p25_client_secret"]},
		"tenant": {Endpoint: o.Admin.Endpoint, ClientID: env["tenant_client_id"], ClientSecret: env["tenant_client_secret"]},
	}
	clients := map[string]*live.APIChecker{}
	failed := map[string]error{}
	// client binds an identity and fetches its token before its first call, once.
	client := func(who string) (*live.APIChecker, error) {
		if c, ok := clients[who]; ok {
			return c, nil
		}
		if err, ok := failed[who]; ok {
			return nil, err
		}
		var c *live.APIChecker
		err := error(nil)
		if o.Bind != nil {
			err = o.Bind(ctx, creds[who])
		}
		if err == nil {
			c, err = live.NewAPIChecker(ctx, o.API, creds[who])
		}
		if err != nil {
			failed[who] = fmt.Errorf("%s identity: %w", who, err)
			return nil, failed[who]
		}
		clients[who] = c
		return c, nil
	}
	storage := func(name string) live.APICall {
		return live.APICall{Op: live.APIGetStorage, Project: env["project_id"], Region: env["region"], Name: name}
	}
	write, err := writeBody(env, o.now())
	if err != nil {
		return rec, err
	}
	for _, s := range []step{
		// D107: refused naming region/storage/get, as T010 run 20261012T072729Z-0646 saw (P25
		// refuted); a 403 naming no action is not that refutation (review r1).
		{"p25-read-bucket-a", "p25", storage(env["bucket_a"]), refused, readAction, true},
		{"p25-read-bucket-b", "p25", storage(env["bucket_b"]), refused, readAction, false},
		{"p26-get-me", "tenant", live.APICall{Op: live.APIGetMe}, refused, meAction, false},
		{"p9-iam-write", "tenant", live.APICall{Op: live.APICreatePolicy, Body: write}, refused, writeAction, false},
	} {
		ob := Observation{Step: s.name, Operation: string(s.call.Op), Credential: s.who, Expected: s.expected, Observed: notRun}
		c, err := (*live.APIChecker)(nil), ctx.Err()
		if err == nil {
			c, err = client(s.who)
		}
		if err == nil && s.name == "p25-read-bucket-a" {
			// T113 (T101 gap 6): a refusal is also what a policy not yet in effect answers, so the
			// record keeps how old the P25 policy was at the read.
			created, why := o.p25Created(ctx, env)
			ob.Note = "the P25 policy's age unknown (" + why + ")"
			if why == "" {
				age := int64(o.now().Sub(created) / time.Second)
				ob.PolicyAge = &age
				ob.Note = fmt.Sprintf("the P25 policy was created %d s before the read", age)
				if age < 0 {
					ob.Note += " (negative: the local clock is behind OVHcloud's)"
				}
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: not run: %w", s.name, err))
		} else if a, err := c.Do(ctx, s.call); err != nil {
			ob.Observed = noAnswer
			errs = append(errs, fmt.Errorf("%s: %w", s.name, err))
		} else {
			ob.Status, ob.Actions, ob.Observed = a.Status, a.Actions, refused
			if a.Status >= 200 && a.Status <= 299 {
				ob.Observed, ob.PolicyID = allowed, a.ID
			}
			ob.AsExpected = ob.Observed == s.expected && (s.expected != refused || a.Status == http.StatusForbidden && (len(a.Actions) == 0 && !s.named || slices.Contains(a.Actions, s.action)))
		}
		if ob.Note != "" && ob.Observed == refused {
			ob.Note += "; " + notInEffect + ", so a refusal does not show the tag condition was evaluated (IAM propagation delay UNVERIFIED)"
		}
		errs = append(errs, rec.add(ob, term))
	}
	errs = append(errs, rec.add(o.stateObject(ctx, env, &errs), term))
	return rec, errors.Join(errs...)
}

// notInEffect is what a refused bucket-A read does not rule out (strong review leg, T101 r2).
const notInEffect = "a refusal is also what a P25 policy not yet in effect would answer"

// P25Name is the P25 policy the probe root creates (tests/live/probes/storage-iam/main.tf).
func P25Name(runID string) string { return "lzprobe-p25-" + runID }

// p25Created reads the P25 policy's createdAt from the v2 policy listing, with the sandbox admin's
// credential; why says, when it cannot, why not (no request text, no secret). The age is context
// for the bucket-A read, not an observation: an unknown one is no finding.
func (o Options) p25Created(ctx context.Context, env map[string]string) (created time.Time, why string) {
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return time.Time{}, "admin identity: " + err.Error()
	}
	name := P25Name(env["run_id"])
	a, err := admin.Do(ctx, live.APICall{Op: live.APIListPolicies, Name: name})
	switch {
	case err != nil:
		return time.Time{}, err.Error()
	case a.Status < 200 || a.Status > 299:
		return time.Time{}, fmt.Sprintf("ListPolicies answered %d", a.Status)
	case len(a.Policies) != 1:
		return time.Time{}, fmt.Sprintf("%d policies named %s listed", len(a.Policies), name)
	}
	at, ok := a.CreatedAt[a.Policies[0].ID]
	if !ok {
		return time.Time{}, "no createdAt listed"
	}
	return at, ""
}

// stateObject reads the companion's state object back with the P1 S3 key and classifies it.
func (o Options) stateObject(ctx context.Context, env map[string]string, errs *[]error) Observation {
	ob := Observation{Step: "p3-state-object", Operation: string(live.S3GetObject), Credential: "s3", Expected: encrypted, Observed: notRun}
	if ctx.Err() != nil {
		*errs = append(*errs, fmt.Errorf("%s: not run: %w", ob.Step, ctx.Err()))
		return ob
	}
	s3, err := live.NewS3Checker(live.S3Options{Region: env["region"], HTTP: o.HTTP, Now: o.Now,
		Keys: map[string]string{"AWS_ACCESS_KEY_ID": env["s3_key_id"], "AWS_SECRET_ACCESS_KEY": env["s3_secret"]}})
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: not run: %w", ob.Step, err))
		return ob
	}
	a, err := s3.Do(ctx, live.S3Call{Op: live.S3GetObject, Bucket: env["state_bucket"], Key: env["state_key"]})
	if err != nil {
		ob.Observed = noAnswer
		*errs = append(*errs, fmt.Errorf("%s: %w", ob.Step, err))
		return ob
	}
	ob.Status, ob.SSE = a.Status, a.SSE
	switch ok, detail := live.StateObjectEncrypted(a.Body); {
	case a.Status < 200 || a.Status > 299:
		ob.Observed, ob.Code = refused, a.Code
	case ok:
		ob.Observed, ob.Detail = encrypted, detail
	case !json.Valid(a.Body):
		ob.Observed, ob.Detail = unreadable, detail
	default:
		ob.Observed, ob.Detail = plaintext, detail
	}
	ob.AsExpected = ob.Observed == encrypted
	return ob
}

// add records an observation, prints it, and returns its finding as an error: an observed answer
// other than the expected one fails the run (unlike the state-backend check, T096 design 6: the
// task makes a refused expected read a failed run).
func (r *Record) add(ob Observation, term io.Writer) error {
	r.Observations = append(r.Observations, ob)
	answer := strings.TrimSpace(fmt.Sprintf("%s %d %s%s", ob.Observed, ob.Status, strings.Join(ob.Actions, ","), ob.Code))
	if ob.Status == 0 {
		answer = ob.Observed
	}
	fmt.Fprintf(term, "LZ-LIVE observe %s %s\n", ob.Step, answer)
	if ob.Note != "" {
		fmt.Fprintf(term, "LZ-LIVE observe %s note: %s\n", ob.Step, ob.Note)
	}
	if ob.Observed == notRun || ob.Observed == noAnswer || ob.AsExpected {
		return nil
	}
	finding := fmt.Sprintf("%s: expected %s, observed %s", ob.Step, ob.Expected, answer)
	if ob.Detail != "" {
		finding += " (" + ob.Detail + ")"
	}
	switch {
	case ob.Step == "p25-read-bucket-a" && ob.Observed == allowed:
		finding += "; P25 holds after all: re-open tag-scoped envelopes (D107)"
	case ob.Step == "p25-read-bucket-a" && ob.Status == http.StatusForbidden && len(ob.Actions) == 0:
		finding += "; D107 expects a 403 naming " + readAction
	case ob.Step == "p9-iam-write" && ob.Observed == allowed:
		finding += "; the undo deletes the policy before the root's destroy"
	}
	if ob.Step == "p25-read-bucket-a" && ob.Observed == refused {
		finding += "; " + notInEffect
	}
	r.Findings = append(r.Findings, ob.Step)
	fmt.Fprintf(term, "LZ-LIVE finding %s\n", finding)
	return errors.New(finding)
}

// writeBody is the IAM write the tenant identity attempts: a policy that grants nothing it does not
// already hold (region/storage/get on the project, for the tenant identity itself), named for the
// run so that Undo finds it, and expiring an hour later should the undo not run.
func writeBody(env map[string]string, now time.Time) ([]byte, error) {
	return json.Marshal(map[string]any{
		"name":        WriteName(env["run_id"]),
		"description": WriteName(env["run_id"]) + ": P9 IAM-write probe, expected to be refused; deleted by the run otherwise",
		"identities":  []string{env["tenant_identity"]},
		"resources":   []map[string]string{{"urn": env["project_urn"]}},
		"permissions": map[string]any{"allow": []map[string]string{{"action": readAction}}},
		"expiredAt":   now.Add(time.Hour).UTC().Format(time.RFC3339),
	})
}

// undo deletes, with the admin's credential, every policy named for the run's IAM write and every
// id in written (the policies an accepted write created), each once, then lists again: one left of
// the name or of a written id, under any name, is an error. A failed listing or delete does not stop
// the other deletes (review r1, r2); every failure is in the error.
//
// recorded are ids read back from observations.json on a cleanup (T113). They come from a file, not
// from OVHcloud's answer to this process, so the admin deletes one only when the first listing
// does not show it under another name (a lagging listing; a name differing in case only counts as
// the run's: a normalised name, UNVERIFIED): an id the listing shows as another policy, such as
// the admin's own, is refused and the undo fails (T113 review r1). Without a first listing no
// recorded id is deleted.
func (o Options) undo(ctx context.Context, env map[string]string, written, recorded []string, term io.Writer) error {
	name := WriteName(env["run_id"])
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return fmt.Errorf("undo: admin identity: %w", err)
	}
	// mine lists the policies of the name and those of the ids in keep, whatever their name.
	mine := func(keep []string) ([]live.APIPolicy, error) {
		a, err := admin.Do(ctx, live.APICall{Op: live.APIListPolicies, Name: name, Keep: keep})
		if err != nil {
			return nil, fmt.Errorf("undo: %w", err)
		}
		if a.Status < 200 || a.Status > 299 {
			return nil, fmt.Errorf("undo: ListPolicies answered %d %s", a.Status, strings.Join(a.Actions, ","))
		}
		return slices.DeleteFunc(a.Policies, func(p live.APIPolicy) bool { return p.Name != name && !slices.Contains(keep, p.ID) }), nil
	}
	found, listErr := mine(slices.Concat(written, recorded))
	var errs []error
	ids := slices.Clone(written)
	for _, id := range recorded {
		i := slices.IndexFunc(found, func(p live.APIPolicy) bool { return p.ID == id })
		switch {
		case slices.Contains(ids, id):
		case listErr != nil:
			errs = append(errs, errors.New("undo: a recorded policy id is not deleted without a listing"))
		case i >= 0 && !strings.EqualFold(found[i].Name, name):
			errs = append(errs, errors.New("undo: a recorded policy id is listed under another name than the run's write; not deleted"))
		default:
			ids = append(ids, id)
		}
	}
	for _, p := range found {
		if p.Name == name && !slices.Contains(ids, p.ID) {
			ids = append(ids, p.ID)
		}
	}
	removed := 0
	for _, id := range ids {
		d, err := admin.Do(ctx, live.APICall{Op: live.APIDeletePolicy, ID: id})
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("undo: %w", err))
		case d.Status == http.StatusNotFound:
			// Expired, or gone already: removed nothing.
		case d.Status < 200 || d.Status > 299:
			errs = append(errs, fmt.Errorf("undo: DeletePolicy answered %d %s", d.Status, strings.Join(d.Actions, ",")))
		default:
			removed++
		}
	}
	// The second listing decides what is left, also after a failed first one.
	left, err := mine(ids)
	if err != nil {
		return errors.Join(append(errs, listErr, err)...)
	}
	if len(left) > 0 {
		errs = append(errs, fmt.Errorf("undo: %d policies named %s remain after deleting %d", len(left), name, removed))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	fmt.Fprintf(term, "LZ-LIVE undo removed %d IAM policies named %s or created by the write\n", removed, name)
	return nil
}
