package state

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T035: the locked-recovery check (T036) against a fake tofu runner (RecoveryOptions.Tofu), a fake
// S3 endpoint behind one TLS server that the client's dialer reaches for every host
// (RecoveryOptions.HTTP), and a fake OVHcloud API for Sweep. The fake tofu and the fake S3 share
// one bucket: writer A's apply writes the lock object `<state key>.tflock` (OpenTofu's S3 lockfile,
// internal/backend/remote-state/s3/client.go:313-345, :609-611) and keeps it until a
// force-unlock with its id removes it; B's plan is refused while it is there. The bucket is
// Object-Locked in governance mode as the probe root creates it (D104, 005/T099): a versioned
// DELETE of a version is refused unless it asks for the bypass and its sender is the janitor
// (docs.ovhcloud.com s3-managing-object-lock.mdx, see state_test.go). The tofu answers are the
// fixtures in tests/fixtures/tofu-probes/locked-recovery/, composed from OpenTofu 1.13.0's source
// (fixtures.json cites each file and line). Whether OVHcloud honours If-None-Match, a copy by
// version id or the lockfile at all is UNVERIFIED (research.md); these fakes answer the worlds T028
// may meet. No test calls OVHcloud, runs tofu or reads ~/.config/ovh-lz/.
//
// What the tests fix for T036, beyond data-model.md and tasks.md:
//   - The check first runs `tofu init` with `-backend-config=bucket=<bucket>` (the backend root
//     names no bucket); the backend root's state key is lzprobe-locked-recovery/terraform.tfstate,
//     so the lock object is lzprobe-locked-recovery/terraform.tfstate.tflock. Then `apply` with
//     kill (writer A; no -auto-approve, -input=false or -lock=false), `plan -lock-timeout=0s`
//     (writer B; no -lock=false), `force-unlock -force <lock id>` and B's `plan` again. A tofu
//     command is the first argument that is not a flag.
//   - B's refusal is `refused (locked)` only on the lock-held diagnostic whose Path names this
//     run's bucket and state key; the lock id is the diagnostic's `ID:`. A force-unlock refusal is
//     `refused` only on an attributable denial (`api error AccessDenied`), with reason
//     `AccessDenied`. Anything else, including a seam error, is `blocked` with a reason.
//   - The conditional PUT runs with the state credential on the lock object, while the lock is
//     held: after B's refused plan, before the force-unlock; it runs only when B's refusal
//     established the lock (`not-run` otherwise: without a held lock it observes nothing).
//   - The restore writes two versions of one object other than the lock object and the backend's
//     state object (bodies of at
//     least 16 bytes, never recorded), copies the first over it by version id (x-amz-copy-source,
//     URL-encoded), then GETs it and compares digests.
//   - The record is observations.json: check "locked-recovery", top-level observed
//     ("recovered", "refused" or "blocked") and reason, findings, and one observation per step in
//     rcSteps, in that order (step, operation, credential "state", expected, observed, status, code,
//     reason, lock_id, as_expected). Step values: writer-a-apply killed/blocked; writer-b-plan,
//     force-unlock, writer-b-plan-again allowed/refused/blocked/not-run;
//     lock-conditional-put refused/condition-ignored/no-answer/not-run; restore-put-v1,
//     restore-put-v2, restore-copy allowed/refused/no-answer/not-run; restore-verify
//     match/mismatch/no-answer/not-run. A step observed other than expected is a finding (the
//     step's name in a findings entry), except writer-b-plan when the lock was not established
//     (blocked, never a refutation). Observe fails only on an S3 step without an answer (a 5xx or
//     none); a refusal, a blocked tofu step or a finding is an observation. The restore runs
//     whatever the tofu steps observed.
//   - Undo is state's undo with the janitor (bucket policy, versioning, every version and delete
//     marker with the bypass, list again); it runs no tofu and sends nothing as the state user.
//   - The root's users carry the descriptions lzprobe-locked-recovery-<run id> (the state user)
//     and lzprobe-locked-recovery-janitor-<run id>; Sweep deletes them and the bucket by listing.

const (
	rcRun      = "20261016T111500Z-7c3d"
	rcRunLower = "20261016t111500z-7c3d"
	rcOtherRun = "20261015T090000Z-9f8e"
	rcBucket   = "lzprobe-locked-recovery-" + rcRunLower
	rcStateKey = "lzprobe-locked-recovery/terraform.tfstate"
	rcLockKey  = rcStateKey + ".tflock"
	rcStateAK  = "AKRCSTATE0001"
	rcStateSK  = "Wq3eRt6yUi9oPa2sDf5gHj7k"
	rcJanAK    = "AKRCJANITOR02"
	rcJanSK    = "Lk8jHg4fDs1aZx7cVb3nMq6w"
	rcAdminID  = "EU.rcsweepadmin"
	rcAdminSK  = "Mn5bVc2xZa8sQw4eRt7yUp1o"
	rcProject  = "0123456789abcdef0123456789abcdef"
	rcEcho     = "RCSERVERTEXT"
)

var (
	rcFixtureDir = filepath.Join("..", "..", "..", "..", "..", "tests", "fixtures", "tofu-probes", "locked-recovery")
	rcSecrets    = []string{rcStateSK, rcJanSK, rcAdminSK, rcStateAK, rcJanAK, "tok-rcadmin"}
	rcNow        = time.Date(2026, 10, 16, 11, 20, 0, 0, time.UTC)
	rcSteps      = []string{"writer-a-apply", "writer-b-plan", "lock-conditional-put", "force-unlock", "writer-b-plan-again",
		"restore-put-v1", "restore-put-v2", "restore-copy", "restore-verify"}
	rcExpected = map[string]string{"writer-a-apply": "killed", "writer-b-plan": "refused", "lock-conditional-put": "refused",
		"force-unlock": "allowed", "writer-b-plan-again": "allowed", "restore-put-v1": "allowed", "restore-put-v2": "allowed",
		"restore-copy": "allowed", "restore-verify": "match"}
	rcLockSeq atomic.Int64
)

func rcEnv() map[string]string {
	return map[string]string{"run_id": rcRun, "bucket": rcBucket, "region": "GRA", "state_key_id": rcStateAK, "state_secret": rcStateSK,
		"janitor_key_id": rcJanAK, "janitor_secret": rcJanSK}
}

// rcStateEnv is the only environment a tofu child may receive: the state user's key pair.
func rcStateEnv() map[string]string {
	return map[string]string{"AWS_ACCESS_KEY_ID": rcStateAK, "AWS_SECRET_ACCESS_KEY": rcStateSK}
}

// rcUsers are the descriptions of the users the root creates for a run.
func rcUsers(run string) []string {
	return []string{"lzprobe-locked-recovery-" + run, "lzprobe-locked-recovery-janitor-" + run}
}

// rcFixture is one entry of fixtures.json with its text.
type rcFixture struct {
	File    string   `json:"file"`
	Command string   `json:"command"`
	Exit    int      `json:"exit"`
	Class   string   `json:"class"`
	Sources []string `json:"sources"`
	text    string
}

func rcLoadFixtures(t *testing.T) map[string]rcFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(rcFixtureDir, "fixtures.json"))
	if err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	var doc struct {
		Version  string      `json:"version"`
		Fixtures []rcFixture `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Version != "1.13.0" {
		t.Fatalf("fixtures.json: version %q, %v", doc.Version, err)
	}
	out := map[string]rcFixture{}
	for _, f := range doc.Fixtures {
		b, err := os.ReadFile(filepath.Join(rcFixtureDir, f.File))
		if err != nil {
			t.Fatalf("fixture %s: %v", f.File, err)
		}
		f.text = string(b)
		out[strings.TrimSuffix(f.File, ".txt")] = f
	}
	return out
}

// rcEvent is one tofu invocation or one S3 request, in the order the fakes saw them.
type rcEvent struct {
	tofu     bool
	cmd      string // tofu: the command; S3: put, cond-put, copy, get, delete, list, policy-get, versioning-get, …
	args     []string
	env      map[string]string
	kill     bool
	who      string // S3: state, janitor or ?
	key      string
	version  string // S3: the versionId a DELETE addressed, or the copy source's
	copyFrom string // S3 copy: the source key
	cond     string // S3: If-None-Match
	bypass   bool
	status   int    // S3: the status answered; 0: hung up
	answered string // S3: the version id answered
	body     []byte
}

func (e rcEvent) String() string {
	if e.tofu {
		return fmt.Sprintf("tofu %s kill=%v", strings.Join(e.args, " "), e.kill)
	}
	return fmt.Sprintf("%s %s %s v=%s %d", e.who, e.cmd, e.key, e.version, e.status)
}

type rcVersion struct {
	key, id string
	body    []byte
	marker  bool
}

// rcReply is one scripted S3 answer: a status with an error code, whether a write is stored
// first, and whether the connection is then closed without an answer. The zero reply is the
// default answer.
type rcReply struct {
	status int
	code   string
	store  bool
	hangup bool
}

// rcWorld is the fakes. Tofu knobs: apply replaces A's report (after A took the lock); noLock: A
// takes no lock; tofuScript: per command, fixture names consumed in order before the defaults
// ("": the default, "!error": the seam fails); unlockInert: force-unlock reports success and the
// lock stays; lockPath: the Path the lock-held diagnostic names; tofuEcho: every failing output
// ends in the server-text marker and the secrets. S3 knobs: s3Script per operation; condIgnored:
// a conditional PUT on a held object answers 200 (and stores); copyInert: a copy answers 200 and
// changes nothing; keepVersions: a versioned DELETE answers 204 and removes nothing. API knobs as
// swift-reach's.
type rcWorld struct {
	t        *testing.T
	srv      *httptest.Server
	api      *httptest.Server
	mu       sync.Mutex
	fx       map[string]rcFixture
	log      []rcEvent
	versions []rcVersion
	seq      int
	backend  string
	lockID   string
	lastLock string

	apply       func() (TofuRun, error)
	noLock      bool
	tofuScript  map[string][]string
	unlockInert bool
	lockPath    string
	tofuEcho    bool

	s3Script     map[string][]rcReply
	condIgnored  bool
	copyInert    bool
	copyWrong    bool // a copy stores the newest version's bytes, not the source's, under the id it answers
	keepVersions bool

	buckets map[string][]string
	users   []rcAPIUser
	apiKeep bool
}

type rcAPIUser struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
	Username    string `json:"username"`
}

func newRcWorld(t *testing.T) *rcWorld {
	t.Helper()
	w := &rcWorld{t: t, fx: rcLoadFixtures(t), tofuScript: map[string][]string{}, s3Script: map[string][]rcReply{},
		buckets: map[string][]string{"GRA": {}, "SBG": {}}}
	w.srv = httptest.NewTLSServer(http.HandlerFunc(w.serveS3))
	t.Cleanup(w.srv.Close)
	w.api = httptest.NewServer(http.HandlerFunc(w.serveAPI))
	t.Cleanup(w.api.Close)
	return w
}

func (w *rcWorld) client() *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(w.srv.Certificate())
	addr := w.srv.Listener.Addr().String()
	// No keep-alive: net/http resends an idempotent request once when a reused connection closes
	// without an answer, which would turn a scripted hangup into the default answer.
	return &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig:   &tls.Config{RootCAs: pool, ServerName: "example.com"},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}}
}

func (w *rcWorld) options() RecoveryOptions {
	return RecoveryOptions{
		HTTP:  w.client(),
		Now:   func() time.Time { return rcNow },
		Tofu:  w.tofu,
		API:   live.API{TokenURL: w.api.URL + "/token", BaseURL: w.api.URL + "/v1"},
		Admin: live.Credential{Endpoint: "ovh-eu", ClientID: rcAdminID, ClientSecret: rcAdminSK},
	}
}

func (w *rcWorld) events() []rcEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.log)
}

func (w *rcWorld) held() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lockID
}

func (w *rcWorld) left() []rcVersion {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.versions)
}

// rcCommand is a tofu invocation's command: its first argument that is not a flag.
func rcCommand(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// putVersion stores a new version of key and returns its id; ids carry `+`, `/` and `=`, which
// a version id in a query or a copy source must escape.
func (w *rcWorld) putVersion(key string, body []byte) string {
	w.seq++
	id := fmt.Sprintf("v%03d.Qx+y/z=", w.seq)
	w.versions = append(w.versions, rcVersion{key: key, id: id, body: slices.Clone(body)})
	return id
}

func (w *rcWorld) addMarker(key string) {
	w.seq++
	w.versions = append(w.versions, rcVersion{key: key, id: fmt.Sprintf("m%03d", w.seq), marker: true})
}

// current is key's newest version unless that is a delete marker.
func (w *rcWorld) current(key string) (rcVersion, bool) {
	for i := len(w.versions) - 1; i >= 0; i-- {
		if v := w.versions[i]; v.key == key {
			return v, !v.marker
		}
	}
	return rcVersion{}, false
}

func rcLockInfo(id string) []byte {
	return fmt.Appendf(nil, `{"ID":%q,"Operation":"OperationTypeApply","Info":"","Who":"runner@lz-live","Version":"1.13.0","Created":"2026-10-16T11:15:03.482913107Z","Path":%q}`,
		id, rcBucket+"/"+rcStateKey)
}

// takeLock is writer A's lock: a fresh id, its info written as the lock object.
func (w *rcWorld) takeLock() {
	id := fmt.Sprintf("5f1c0a2e-7b3d-4c8e-9a61-%012x", rcLockSeq.Add(1))
	w.lockID, w.lastLock = id, id
	w.putVersion(rcLockKey, rcLockInfo(id))
}

// answer renders a fixture: the held (or last held) lock's id and path, the id force-unlock got.
func (w *rcWorld) answer(name, given string) TofuRun {
	f, ok := w.fx[name]
	if !ok {
		w.t.Errorf("no fixture %s", name)
		return TofuRun{Exit: 1}
	}
	path := w.lockPath
	if path == "" {
		path = rcBucket + "/" + rcStateKey
	}
	text := strings.NewReplacer("@LOCK_ID@", w.lastLock, "@PATH@", path, "@GIVEN_ID@", given).Replace(f.text)
	if w.tofuEcho && f.Exit != 0 {
		text += rcEcho + " " + rcStateSK + " " + rcJanSK + "\n"
	}
	return TofuRun{Exit: f.Exit, Output: []byte(text)}
}

// tofu is the fake seam: it records each invocation with its arguments and environment and
// answers from the shared bucket.
func (w *rcWorld) tofu(ctx context.Context, env map[string]string, kill bool, args ...string) (TofuRun, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	cmd := rcCommand(args)
	w.log = append(w.log, rcEvent{tofu: true, cmd: cmd, args: slices.Clone(args), env: maps.Clone(env), kill: kill})
	if !maps.Equal(env, rcStateEnv()) {
		// Names only: a value may be a secret.
		w.t.Errorf("tofu %s ran with the environment keys %q (or other values), want the state user's key pair only", cmd, slices.Sorted(maps.Keys(env)))
	}
	if kill != (cmd == "apply") {
		w.t.Errorf("tofu %s ran with kill %v, want kill for writer A's apply only", cmd, kill)
	}
	if cmd == "init" {
		for _, a := range args {
			if b, ok := strings.CutPrefix(a, "-backend-config=bucket="); ok {
				w.backend = b
			}
		}
		return TofuRun{Output: []byte("\nOpenTofu has been successfully initialized!\n")}, nil
	}
	if w.backend != rcBucket {
		return w.answer("plan-init-required", ""), nil
	}
	given := ""
	if cmd == "force-unlock" && len(args) > 0 {
		given = args[len(args)-1]
	}
	if q := w.tofuScript[cmd]; len(q) > 0 {
		w.tofuScript[cmd] = q[1:]
		switch q[0] {
		case "":
		case "!error":
			return TofuRun{}, errors.New("tofu seam: the command could not be run")
		default:
			return w.answer(q[0], given), nil
		}
	}
	switch cmd {
	case "apply":
		if !w.noLock {
			w.takeLock()
		}
		if w.apply != nil {
			return w.apply()
		}
		return TofuRun{Exit: -1, Signaled: true}, nil
	case "plan":
		if w.lockID != "" {
			return w.answer("lock-held", ""), nil
		}
		// B takes the lock and releases it.
		w.putVersion(rcLockKey, rcLockInfo("b-plan"))
		w.addMarker(rcLockKey)
		return w.answer("plan-ok", ""), nil
	case "force-unlock":
		switch {
		case w.lockID == "":
			return w.answer("unlock-no-lock", given), nil
		case given != w.lockID:
			return w.answer("unlock-id-mismatch", given), nil
		}
		if !w.unlockInert {
			w.lockID = ""
			w.addMarker(rcLockKey)
		}
		return w.answer("unlock-ok", given), nil
	}
	return w.answer("unmatched", ""), nil
}

func rcErr(rw http.ResponseWriter, status int, code string) int {
	rw.WriteHeader(status)
	// Error answers echo what a server might: text and the request's credentials.
	fmt.Fprintf(rw, "<?xml version=\"1.0\"?><Error><Code>%s</Code><Message>%s %s %s</Message></Error>", code, rcEcho, rcStateSK, rcJanSK)
	return status
}

// serveS3 is the fake S3 endpoint for the probe bucket: path-style, the caller told apart by the
// access key in its signature.
func (w *rcWorld) serveS3(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	auth := r.Header.Get("Authorization")
	who := "?"
	switch {
	case strings.Contains(auth, "Credential="+rcStateAK+"/"):
		who = "state"
	case strings.Contains(auth, "Credential="+rcJanAK+"/"):
		who = "janitor"
	}
	q := r.URL.Query()
	b, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	op := ""
	switch {
	case key != "" && r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") != "":
		op = "copy"
	case key != "" && r.Method == http.MethodPut && r.Header.Get("If-None-Match") != "":
		op = "cond-put"
	case key != "" && r.Method == http.MethodPut:
		op = "put"
	case key != "" && r.Method == http.MethodGet:
		op = "get"
	case key != "" && r.Method == http.MethodDelete:
		op = "delete"
	case key == "" && r.Method == http.MethodGet && q.Has("versions"):
		op = "list"
	case key == "" && q.Has("policy"):
		op = "policy-" + strings.ToLower(r.Method)
	case key == "" && q.Has("versioning"):
		op = "versioning-" + strings.ToLower(r.Method)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	ev := rcEvent{cmd: op, who: who, key: key, version: q.Get("versionId"), cond: r.Header.Get("If-None-Match"),
		bypass: r.Header.Get("X-Amz-Bypass-Governance-Retention") == "true", body: body}
	ev.status = w.s3(rw, r, &ev, b, key, op, body, who)
	w.log = append(w.log, ev)
}

func (w *rcWorld) s3(rw http.ResponseWriter, r *http.Request, ev *rcEvent, b, key, op string, body []byte, who string) int {
	if who == "?" || b != rcBucket || op == "" {
		return rcErr(rw, http.StatusForbidden, "SignatureDoesNotMatch")
	}
	// Scripts address the restore object for put, copy and get, the lock object for cond-put.
	if q := w.s3Script[op]; len(q) > 0 && (op == "cond-put") == (key == rcLockKey) {
		rep := q[0]
		w.s3Script[op] = q[1:]
		if rep.store && (op == "put" || op == "cond-put") {
			ev.answered = w.putVersion(key, body)
		}
		if rep.hangup {
			if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return 0
		}
		if rep.status != 0 {
			return rcErr(rw, rep.status, rep.code)
		}
	}
	if who == "state" {
		switch {
		case op == "delete" && ev.version != "", op == "list", strings.HasPrefix(op, "policy-"), strings.HasPrefix(op, "versioning-"):
			return rcErr(rw, http.StatusForbidden, "AccessDenied")
		}
	}
	switch op {
	case "cond-put":
		if ev.cond != "*" {
			return rcErr(rw, http.StatusNotImplemented, "NotImplemented")
		}
		if _, ok := w.current(key); ok && !w.condIgnored {
			return rcErr(rw, http.StatusPreconditionFailed, "PreconditionFailed")
		}
		ev.answered = w.putVersion(key, body)
		rw.Header().Set("X-Amz-Version-Id", ev.answered)
		rw.WriteHeader(http.StatusOK)
		return http.StatusOK
	case "put":
		ev.answered = w.putVersion(key, body)
		rw.Header().Set("X-Amz-Version-Id", ev.answered)
		rw.WriteHeader(http.StatusOK)
		return http.StatusOK
	case "copy":
		srcPath, srcQuery, _ := strings.Cut(strings.TrimPrefix(r.Header.Get("X-Amz-Copy-Source"), "/"), "?")
		p, err := url.PathUnescape(srcPath)
		vq, qerr := url.ParseQuery(srcQuery)
		sb, sk, _ := strings.Cut(p, "/")
		ev.copyFrom, ev.version = sk, vq.Get("versionId")
		i := slices.IndexFunc(w.versions, func(v rcVersion) bool { return v.key == sk && v.id == ev.version && !v.marker })
		if err != nil || qerr != nil || sb != rcBucket || i < 0 {
			return rcErr(rw, http.StatusNotFound, "NoSuchVersion")
		}
		if w.copyInert {
			// Answered as a real copy (a fresh version id), stored nowhere: only the GET body tells.
			w.seq++
			ev.answered = fmt.Sprintf("v%03d.Qx+y/z=", w.seq)
		} else if v, ok := w.current(key); w.copyWrong && ok {
			ev.answered = w.putVersion(key, v.body)
		} else {
			ev.answered = w.putVersion(key, w.versions[i].body)
		}
		rw.Header().Set("X-Amz-Version-Id", ev.answered)
		rw.Header().Set("X-Amz-Copy-Source-Version-Id", ev.version)
		rw.WriteHeader(http.StatusOK)
		fmt.Fprint(rw, `<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult><ETag>"e"</ETag><LastModified>2026-10-16T11:20:00.000Z</LastModified></CopyObjectResult>`)
		return http.StatusOK
	case "get":
		v, ok := w.current(key)
		if !ok {
			return rcErr(rw, http.StatusNotFound, "NoSuchKey")
		}
		rw.Header().Set("X-Amz-Version-Id", v.id)
		rw.WriteHeader(http.StatusOK)
		rw.Write(v.body)
		return http.StatusOK
	case "delete":
		if v := ev.version; v != "" {
			locked := slices.ContainsFunc(w.versions, func(x rcVersion) bool { return x.key == key && x.id == v && !x.marker })
			if locked && !(ev.bypass && who == "janitor") {
				return rcErr(rw, http.StatusForbidden, "AccessDenied")
			}
			if !w.keepVersions {
				w.versions = slices.DeleteFunc(w.versions, func(x rcVersion) bool { return x.key == key && x.id == v })
			}
		} else {
			w.addMarker(key)
		}
		rw.WriteHeader(http.StatusNoContent)
		return http.StatusNoContent
	case "list":
		var sb strings.Builder
		fmt.Fprintf(&sb, `<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>%s</Name><IsTruncated>false</IsTruncated>`, rcBucket)
		for _, v := range w.versions {
			tag := "Version"
			if v.marker {
				tag = "DeleteMarker"
			}
			fmt.Fprintf(&sb, `<%s><Key>%s</Key><VersionId>%s</VersionId></%s>`, tag, v.key, v.id, tag)
		}
		sb.WriteString(`</ListVersionsResult>`)
		rw.WriteHeader(http.StatusOK)
		fmt.Fprint(rw, sb.String())
		return http.StatusOK
	case "policy-get":
		return rcErr(rw, http.StatusNotFound, "NoSuchBucketPolicy")
	case "versioning-get":
		rw.WriteHeader(http.StatusOK)
		fmt.Fprint(rw, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
		return http.StatusOK
	}
	return rcErr(rw, http.StatusForbidden, "AccessDenied")
}

// serveAPI is the fake OVHcloud API for Sweep: regions, their bucket listings and deletes, the
// project's users and their deletes, for the sandbox admin only.
func (w *rcWorld) serveAPI(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.mu.Lock()
	defer w.mu.Unlock()
	if r.URL.Path == "/token" {
		if strings.Contains(string(body), "client_id="+rcAdminID) && strings.Contains(string(body), "client_secret="+rcAdminSK) {
			fmt.Fprint(rw, `{"access_token":"tok-rcadmin"}`)
			return
		}
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	who := "?"
	if r.Header.Get("Authorization") == "Bearer tok-rcadmin" {
		who = "admin"
	}
	w.log = append(w.log, rcEvent{cmd: "api " + r.Method + " " + r.URL.Path, who: who, key: r.URL.Path})
	if who != "admin" {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	base := "/v1/cloud/project/" + rcProject
	rw.Header().Set("Content-Type", "application/json")
	rest, ok := strings.CutPrefix(r.URL.Path, base)
	if !ok {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"This service does not exist"}`)
		return
	}
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	switch {
	case r.Method == http.MethodGet && rest == "/region":
		json.NewEncoder(rw).Encode(slices.Sorted(maps.Keys(w.buckets)))
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "region":
		json.NewEncoder(rw).Encode(map[string]any{"name": parts[1], "status": "UP",
			"services": []map[string]string{{"name": "storage-s3-standard", "status": "UP"}, {"name": "storage", "status": "UP"}}})
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "region" && parts[2] == "storage":
		out := []map[string]any{}
		for _, b := range w.buckets[parts[1]] {
			out = append(out, map[string]any{"name": b, "region": parts[1]})
		}
		json.NewEncoder(rw).Encode(out)
	case r.Method == http.MethodDelete && len(parts) == 4 && parts[0] == "region" && parts[2] == "storage":
		if !slices.Contains(w.buckets[parts[1]], parts[3]) {
			rw.WriteHeader(http.StatusNotFound)
			fmt.Fprint(rw, `{"message":"not found"}`)
			return
		}
		if !w.apiKeep {
			w.buckets[parts[1]] = slices.DeleteFunc(w.buckets[parts[1]], func(b string) bool { return b == parts[3] })
		}
		fmt.Fprint(rw, `null`)
	case r.Method == http.MethodGet && rest == "/user":
		json.NewEncoder(rw).Encode(append([]rcAPIUser{}, w.users...))
	case r.Method == http.MethodDelete && len(parts) == 2 && parts[0] == "user":
		i := slices.IndexFunc(w.users, func(u rcAPIUser) bool { return fmt.Sprint(u.ID) == parts[1] })
		if i < 0 {
			rw.WriteHeader(http.StatusNotFound)
			fmt.Fprint(rw, `{"message":"not found"}`)
			return
		}
		if !w.apiKeep {
			w.users = slices.Delete(w.users, i, i+1)
		}
		fmt.Fprint(rw, `null`)
	default:
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"This call has not been granted"}`)
	}
}

// rcObs and rcRecord read observations.json as T036 writes it (the JSON names are the contract;
// fields T036 adds are ignored).
type rcObs struct {
	Step       string `json:"step"`
	Operation  string `json:"operation"`
	Credential string `json:"credential"`
	Expected   string `json:"expected"`
	Observed   string `json:"observed"`
	Status     int    `json:"status"`
	Code       string `json:"code"`
	Reason     string `json:"reason"`
	LockID     string `json:"lock_id"`
	AsExpected bool   `json:"as_expected"`
}

type rcRecord struct {
	Check        string   `json:"check"`
	Observed     string   `json:"observed"`
	Reason       string   `json:"reason"`
	Observations []rcObs  `json:"observations"`
	Findings     []string `json:"findings"`
}

// rcLeaks returns the secrets (whole, or either half of a secret key) that texts carry.
func rcLeaks(texts ...string) []string {
	bad := slices.Clone(rcSecrets)
	for _, s := range []string{rcStateSK, rcJanSK, rcAdminSK} {
		bad = append(bad, s[:12], s[12:])
	}
	var out []string
	for _, s := range texts {
		for _, b := range bad {
			if strings.Contains(s, b) && !slices.Contains(out, b) {
				out = append(out, b)
			}
		}
	}
	return out
}

// rcObserve runs Observe in a fresh check; the case checks the invocations and requests first,
// then reads the record with rcParse, so a check that does nothing fails on its first missing
// invocation.
func rcObserve(t *testing.T, w *rcWorld) (raw []byte, term string, err error) {
	t.Helper()
	var buf bytes.Buffer
	raw, err = LockedRecovery(w.options()).Observe(context.Background(), rcEnv(), &buf)
	if leaks := rcLeaks(string(raw), buf.String(), fmt.Sprint(err)); len(leaks) > 0 {
		t.Errorf("record, terminal or error carry %q", leaks)
	}
	if strings.Contains(string(raw), rcEcho) {
		t.Errorf("the record carries the server's text")
	}
	return raw, buf.String(), err
}

func rcParse(t *testing.T, raw []byte, err error) rcRecord {
	t.Helper()
	var rec rcRecord
	if raw == nil {
		t.Fatalf("Observe returned no record (error %v)", err)
	}
	if jerr := json.Unmarshal(raw, &rec); jerr != nil {
		t.Fatalf("record %s: %v", raw, jerr)
	}
	if rec.Check != "locked-recovery" {
		t.Errorf("record check = %q, want locked-recovery", rec.Check)
	}
	return rec
}

// rcByStep indexes the record's observations; it fails unless they are exactly rcSteps, in order,
// each with the state credential, its expected value and an as_expected that agrees with it.
func rcByStep(t *testing.T, rec rcRecord) map[string]rcObs {
	t.Helper()
	var got []string
	m := map[string]rcObs{}
	for _, ob := range rec.Observations {
		got = append(got, ob.Step)
		m[ob.Step] = ob
	}
	if !slices.Equal(got, rcSteps) {
		t.Fatalf("observations %q, want %q", got, rcSteps)
	}
	for _, s := range rcSteps {
		ob := m[s]
		if ob.Credential != "state" || ob.Expected != rcExpected[s] || ob.AsExpected && ob.Observed != ob.Expected {
			t.Errorf("observation %+v: want credential state, expected %s, as_expected only when observed as expected", ob, rcExpected[s])
		}
		if ob.Operation == "" {
			t.Errorf("observation %s names no operation", s)
		}
	}
	return m
}

// rcWant checks one observation: observed, status (-1: any), code and reason ("": any); a refused
// or blocked step must carry a reason, a not-run step no status.
func rcWant(t *testing.T, ob rcObs, observed string, status int, code, reason string) {
	t.Helper()
	if ob.Observed != observed || status >= 0 && ob.Status != status || code != "" && ob.Code != code || reason != "" && ob.Reason != reason {
		t.Errorf("observation %+v, want observed %s, status %d, code %q, reason %q", ob, observed, status, code, reason)
	}
	tofuStep := ob.Step == "writer-b-plan" || ob.Step == "force-unlock" || ob.Step == "writer-b-plan-again"
	if (observed == "blocked" || observed == "refused" && tofuStep) && ob.Reason == "" {
		t.Errorf("observation %s is %s without a reason", ob.Step, observed)
	}
	if observed == "not-run" && ob.Status != 0 {
		t.Errorf("observation %s is not-run with status %d", ob.Step, ob.Status)
	}
}

// rcTofu is the tofu invocations other than init.
func rcTofu(evs []rcEvent) []rcEvent {
	var out []rcEvent
	for _, e := range evs {
		if e.tofu && e.cmd != "init" {
			out = append(out, e)
		}
	}
	return out
}

func rcCommands(evs []rcEvent) []string {
	var out []string
	for _, e := range rcTofu(evs) {
		out = append(out, e.cmd)
	}
	return out
}

// rcWantFlow checks the tofu invocations are exactly want (init aside), after an init naming the
// run's bucket, with the flags tasks.md fixes and force-unlock given lockID.
func rcWantFlow(t *testing.T, evs []rcEvent, want []string, lockID string) {
	t.Helper()
	if got := rcCommands(evs); !slices.Equal(got, want) {
		t.Errorf("tofu commands %q, want %q (init aside): %v", got, want, evs)
		return
	}
	first := slices.IndexFunc(evs, func(e rcEvent) bool { return e.tofu && e.cmd != "init" })
	init := slices.IndexFunc(evs, func(e rcEvent) bool {
		return e.tofu && e.cmd == "init" && slices.Contains(e.args, "-backend-config=bucket="+rcBucket)
	})
	if first >= 0 && (init < 0 || init > first) {
		t.Errorf("no tofu init with -backend-config=bucket=%s before the first command: %v", rcBucket, evs)
	}
	for _, e := range rcTofu(evs) {
		if slices.Contains(e.args, "-lock=false") {
			t.Errorf("tofu %s ran with -lock=false: %q", e.cmd, e.args)
		}
		switch e.cmd {
		case "apply":
			if !e.kill || slices.Contains(e.args, "-auto-approve") || slices.Contains(e.args, "-input=false") {
				t.Errorf("writer A ran %q kill %v: want an apply that waits at its prompt, killed", e.args, e.kill)
			}
		case "plan":
			if !slices.Contains(e.args, "-lock-timeout=0s") {
				t.Errorf("writer B ran %q, want -lock-timeout=0s", e.args)
			}
		case "force-unlock":
			if !slices.Contains(e.args, "-force") || e.args[len(e.args)-1] != lockID {
				t.Errorf("force-unlock ran %q, want -force and the lock id %s read from B's refusal", e.args, lockID)
			}
		}
	}
}

// rcS3 is the S3 requests of one operation (any operation when op is "").
func rcS3(evs []rcEvent, op string) []rcEvent {
	var out []rcEvent
	for _, e := range evs {
		if !e.tofu && !strings.HasPrefix(e.cmd, "api ") && (op == "" || e.cmd == op) {
			out = append(out, e)
		}
	}
	return out
}

func rcIndex(evs []rcEvent, f func(rcEvent) bool) int { return slices.IndexFunc(evs, f) }

// rcFinding reports whether a findings entry names step.
func rcFinding(rec rcRecord, step string) bool {
	return slices.ContainsFunc(rec.Findings, func(f string) bool { return strings.Contains(f, step) })
}

// TestRecoveryLock: the primary cases against the fake tofu runner.
func TestRecoveryLock(t *testing.T) {
	t.Run("recovered", func(t *testing.T) {
		// A killed holding the lock, B refused, the unlock with the lock id from B's refusal, B
		// allowed: exposes an unlock not followed by a real acquisition, or a lock never held.
		w := newRcWorld(t)
		raw, term, err := rcObserve(t, w)
		evs := w.events()
		lockID := w.lastLock
		rcWantFlow(t, evs, []string{"apply", "plan", "force-unlock", "plan"}, lockID)
		if w.held() != "" {
			t.Errorf("the lock %s is still held at the end", w.held())
		}
		plan := rcIndex(evs, func(e rcEvent) bool { return e.tofu && e.cmd == "plan" })
		unlock := rcIndex(evs, func(e rcEvent) bool { return e.tofu && e.cmd == "force-unlock" })
		cond := rcIndex(evs, func(e rcEvent) bool { return e.cmd == "cond-put" })
		if cond < 0 || !(plan < cond && cond < unlock) {
			t.Errorf("conditional PUT at %d, want one after B's refused plan (%d) and before the force-unlock (%d): %v", cond, plan, unlock, evs)
		} else if c := evs[cond]; c.who != "state" || c.key != rcLockKey || c.cond != "*" || c.status != 412 {
			t.Errorf("conditional PUT %v (If-None-Match %q), want the state user's PUT of %s with If-None-Match * answered 412", c, c.cond, rcLockKey)
		}
		puts := rcS3(evs, "put")
		copies := rcS3(evs, "copy")
		var v1, v2 []byte
		if len(puts) != 2 || puts[0].key != puts[1].key || puts[0].key == rcLockKey || puts[0].key == rcStateKey || puts[0].who != "state" || puts[1].who != "state" ||
			puts[0].status != 200 || puts[1].status != 200 {
			t.Errorf("restore PUTs %v, want two by the state user of one object other than the lock and state objects, both 200", puts)
		} else {
			v1, v2 = puts[0].body, puts[1].body
			if len(copies) != 1 || copies[0].who != "state" || copies[0].key != puts[0].key || copies[0].copyFrom != puts[0].key ||
				copies[0].version != puts[0].answered || copies[0].status != 200 {
				t.Errorf("copies %v, want one by the state user of %s over itself from v1 %s", copies, puts[0].key, puts[0].answered)
			} else {
				ci := slices.IndexFunc(evs, func(e rcEvent) bool { return e.cmd == "copy" })
				if !slices.ContainsFunc(evs[ci+1:], func(e rcEvent) bool {
					return e.cmd == "get" && e.key == puts[0].key && e.who == "state" && e.status == 200
				}) {
					t.Errorf("no GET of %s by the state user after the copy: %v", puts[0].key, evs)
				}
			}
		}
		rec := rcParse(t, raw, err)
		m := rcByStep(t, rec)
		rcWant(t, m["writer-a-apply"], "killed", -1, "", "")
		rcWant(t, m["writer-b-plan"], "refused", -1, "", "locked")
		if m["writer-b-plan"].LockID != lockID {
			t.Errorf("writer-b-plan lock_id = %q, want %s from B's output", m["writer-b-plan"].LockID, lockID)
		}
		rcWant(t, m["lock-conditional-put"], "refused", 412, "PreconditionFailed", "")
		rcWant(t, m["force-unlock"], "allowed", -1, "", "")
		rcWant(t, m["writer-b-plan-again"], "allowed", -1, "", "")
		rcWant(t, m["restore-put-v1"], "allowed", 200, "", "")
		rcWant(t, m["restore-put-v2"], "allowed", 200, "", "")
		rcWant(t, m["restore-copy"], "allowed", 200, "", "")
		rcWant(t, m["restore-verify"], "match", -1, "", "")
		for _, s := range rcSteps {
			if !m[s].AsExpected {
				t.Errorf("step %s not as expected: %+v", s, m[s])
			}
		}
		if rec.Observed != "recovered" || len(rec.Findings) != 0 || err != nil {
			t.Errorf("record observed %q findings %q, Observe = %v; want recovered, none, nil", rec.Observed, rec.Findings, err)
		}
		var decoded any
		json.Unmarshal(raw, &decoded)
		flat := fmt.Sprint(decoded)
		for i, b := range [][]byte{v1, v2} {
			body := strings.TrimSpace(string(b))
			if len(body) < 16 {
				t.Errorf("restore body v%d is %d bytes, want at least 16 (so a leak of it is detectable)", i+1, len(body))
				continue
			}
			if strings.Contains(flat, body) || strings.Contains(string(raw), body) || strings.Contains(term, body) {
				t.Errorf("the record or terminal carries v%d's body", i+1)
			}
		}
	})

	for _, v := range []struct {
		name string
		run  TofuRun
		err  error
	}{
		{"exit-0-unsignalled", TofuRun{Exit: 0}, nil},
		{"exit-1-unsignalled", TofuRun{Exit: 1}, nil},
		{"seam-error", TofuRun{}, errors.New("tofu seam: no exit observed after SIGKILL")},
		// A report that comes with an error is no report.
		{"signalled-with-seam-error", TofuRun{Exit: -1, Signaled: true}, errors.New("tofu seam: wait failed")},
	} {
		t.Run("owner-alive/"+v.name, func(t *testing.T) {
			// The seam does not report that A died on the SIGKILL: exposes a lock broken while its
			// writer may still hold it (V005's owner-death proof).
			w := newRcWorld(t)
			w.apply = func() (TofuRun, error) { return v.run, v.err }
			raw, _, err := rcObserve(t, w)
			evs := w.events()
			if a := rcTofu(evs); len(a) == 0 || a[0].cmd != "apply" || !a[0].kill {
				t.Fatalf("tofu commands %v, want writer A's apply with kill first", a)
			}
			if slices.ContainsFunc(evs, func(e rcEvent) bool { return e.tofu && e.cmd == "force-unlock" }) {
				t.Errorf("force-unlock ran although A's death was not reported: %v", evs)
			}
			if n := len(slices.DeleteFunc(rcCommands(evs), func(c string) bool { return c != "plan" })); n > 1 {
				t.Errorf("B planned %d times, want no second plan", n)
			}
			if w.held() == "" {
				t.Errorf("the lock was removed")
			}
			rec := rcParse(t, raw, err)
			m := rcByStep(t, rec)
			rcWant(t, m["writer-a-apply"], "blocked", -1, "", "")
			rcWant(t, m["force-unlock"], "not-run", -1, "", "")
			rcWant(t, m["writer-b-plan-again"], "not-run", -1, "", "")
			if rec.Observed != "blocked" || rec.Reason != "owner alive" || err != nil {
				t.Errorf("record observed %q reason %q, Observe = %v; want blocked (owner alive) and nil", rec.Observed, rec.Reason, err)
			}
		})
	}

	t.Run("lock-not-established", func(t *testing.T) {
		// A took no lock (released it, or never took it), so B's plan is allowed: exposes an A
		// that released or never took the lock read as a backend fault.
		w := newRcWorld(t)
		w.noLock = true
		raw, _, err := rcObserve(t, w)
		evs := w.events()
		rcWantFlow(t, evs, []string{"apply", "plan"}, "")
		if len(rcS3(evs, "cond-put")) != 0 {
			t.Errorf("a conditional PUT ran without a held lock: %v", evs)
		}
		rec := rcParse(t, raw, err)
		m := rcByStep(t, rec)
		rcWant(t, m["writer-b-plan"], "allowed", -1, "", "")
		rcWant(t, m["lock-conditional-put"], "not-run", -1, "", "")
		rcWant(t, m["force-unlock"], "not-run", -1, "", "")
		rcWant(t, m["writer-b-plan-again"], "not-run", -1, "", "")
		if err != nil {
			t.Errorf("Observe = %v on a blocked observation, want nil", err)
		}
		if rec.Observed != "blocked" || rec.Reason != "lock not established" || rcFinding(rec, "writer-b-plan") {
			t.Errorf("record observed %q reason %q findings %q, want blocked (lock not established) and no refutation", rec.Observed, rec.Reason, rec.Findings)
		}
	})

	t.Run("unlock-not-effective", func(t *testing.T) {
		// force-unlock reports success, the lock stays, B is refused again: exposes an unlock
		// reported from its exit status alone.
		w := newRcWorld(t)
		w.unlockInert = true
		raw, _, err := rcObserve(t, w)
		evs := w.events()
		rcWantFlow(t, evs, []string{"apply", "plan", "force-unlock", "plan"}, w.lastLock)
		rec := rcParse(t, raw, err)
		m := rcByStep(t, rec)
		rcWant(t, m["writer-b-plan-again"], "refused", -1, "", "locked")
		if rec.Observed == "recovered" || !rcFinding(rec, "writer-b-plan-again") || err != nil {
			t.Errorf("record observed %q findings %q, want no recovery, a finding for writer-b-plan-again and Observe nil (%v)", rec.Observed, rec.Findings, err)
		}
	})

	t.Run("lock-elsewhere", func(t *testing.T) {
		// B's refusal names a lock on another bucket: not attributable to A's lock here; exposes a
		// refusal taken from the diagnostic's summary alone.
		w := newRcWorld(t)
		w.lockPath = "lzprobe-locked-recovery-" + strings.ToLower(rcOtherRun) + "/" + rcStateKey
		raw, _, err := rcObserve(t, w)
		evs := w.events()
		rcWantFlow(t, evs, []string{"apply", "plan"}, "")
		rec := rcParse(t, raw, err)
		m := rcByStep(t, rec)
		rcWant(t, m["writer-b-plan"], "blocked", -1, "", "")
		rcWant(t, m["lock-conditional-put"], "not-run", -1, "", "")
		rcWant(t, m["force-unlock"], "not-run", -1, "", "")
		if len(rcS3(evs, "cond-put")) != 0 {
			t.Errorf("a conditional PUT ran without an attributable lock: %v", evs)
		}
		if rec.Observed != "blocked" || err != nil {
			t.Errorf("record observed %q, Observe = %v; want blocked and nil", rec.Observed, err)
		}
	})
}

// TestRecoveryUnlockRefused: force-unlock exits non-zero on an authorisation or retention denial
// (403 AccessDenied): recorded refused with that reason, not retried, B's second plan not-run.
// Exposes a retry loop or a refusal reported as success.
func TestRecoveryUnlockRefused(t *testing.T) {
	for _, fx := range []string{"unlock-denied", "unlock-denied-read"} {
		t.Run(fx, func(t *testing.T) {
			w := newRcWorld(t)
			w.tofuScript["force-unlock"] = []string{fx}
			raw, _, err := rcObserve(t, w)
			evs := w.events()
			rcWantFlow(t, evs, []string{"apply", "plan", "force-unlock"}, w.lastLock)
			if w.lastLock != "" && w.held() == "" {
				t.Errorf("the lock was removed")
			}
			rec := rcParse(t, raw, err)
			m := rcByStep(t, rec)
			rcWant(t, m["force-unlock"], "refused", -1, "", "AccessDenied")
			rcWant(t, m["writer-b-plan-again"], "not-run", -1, "", "")
			if rec.Observed != "refused" || !rcFinding(rec, "force-unlock") || err != nil {
				t.Errorf("record observed %q findings %q, Observe = %v; want refused, a force-unlock finding, nil", rec.Observed, rec.Findings, err)
			}
		})
	}
}

// TestRecoveryBlocked: a plan or the force-unlock fails with a transport error, a 404, a 5xx, a
// configuration error, a diagnostic matching no fixture or a seam error: the step is blocked with
// its reason, never refused, and the tofu steps after it do not run. Exposes an outage or a broken
// backend root counted as a denial.
func TestRecoveryBlocked(t *testing.T) {
	type c struct {
		step   string
		cmd    string
		script []string
		flow   []string
		after  []string
	}
	var cases []c
	for _, fx := range []string{"plan-transport", "plan-no-bucket", "plan-init-required", "unmatched", "!error"} {
		cases = append(cases, c{"writer-b-plan", "plan", []string{fx}, []string{"apply", "plan"}, []string{"force-unlock", "writer-b-plan-again"}})
	}
	for _, fx := range []string{"unlock-5xx", "unlock-no-lock", "unlock-id-mismatch", "unmatched", "!error"} {
		cases = append(cases, c{"force-unlock", "force-unlock", []string{fx}, []string{"apply", "plan", "force-unlock"}, []string{"writer-b-plan-again"}})
	}
	for _, fx := range []string{"plan-transport", "plan-no-bucket", "unmatched", "!error"} {
		cases = append(cases, c{"writer-b-plan-again", "plan", []string{"", fx}, []string{"apply", "plan", "force-unlock", "plan"}, nil})
	}
	for _, k := range cases {
		t.Run(k.step+"/"+strings.TrimPrefix(k.script[len(k.script)-1], "!"), func(t *testing.T) {
			w := newRcWorld(t)
			w.tofuScript[k.cmd] = slices.Clone(k.script)
			raw, _, err := rcObserve(t, w)
			evs := w.events()
			rcWantFlow(t, evs, k.flow, w.lastLock)
			rec := rcParse(t, raw, err)
			m := rcByStep(t, rec)
			rcWant(t, m[k.step], "blocked", -1, "", "")
			for _, s := range k.after {
				rcWant(t, m[s], "not-run", -1, "", "")
			}
			if rec.Observed != "blocked" || err != nil {
				t.Errorf("record observed %q, Observe = %v; want blocked and nil (a blocked tofu step is an observation)", rec.Observed, err)
			}
		})
	}
}

// TestRecoveryTofuEnv: every tofu invocation's environment is the state key pair only: no OVH_*
// variable, no janitor key, no admin credential. Exposes an unlock that works only with broader
// rights, or a credential leaked into a child. (The fake checks each invocation; this test
// requires that invocations happened on each path.)
func TestRecoveryTofuEnv(t *testing.T) {
	for _, v := range []struct {
		name   string
		script map[string][]string
		min    int
	}{
		{"recovered", nil, 5},
		{"refused", map[string][]string{"force-unlock": {"unlock-denied"}}, 4},
		{"blocked", map[string][]string{"plan": {"plan-transport"}}, 3},
	} {
		t.Run(v.name, func(t *testing.T) {
			w := newRcWorld(t)
			maps.Copy(w.tofuScript, v.script)
			rcObserve(t, w)
			n := 0
			for _, e := range w.events() {
				if e.tofu {
					n++
					if !maps.Equal(e.env, rcStateEnv()) {
						t.Errorf("tofu %s environment keys %q, want the state key pair only", e.cmd, slices.Sorted(maps.Keys(e.env)))
					}
				}
			}
			if n < v.min {
				t.Errorf("%d tofu invocations, want at least %d (init and the writers)", n, v.min)
			}
		})
	}
}

// TestRecoveryConditionalPut: the supporting control on the held lock object.
func TestRecoveryConditionalPut(t *testing.T) {
	t.Run("honoured", func(t *testing.T) {
		// 412: refused, as expected. Exposes a condition never sent.
		w := newRcWorld(t)
		raw, _, err := rcObserve(t, w)
		conds := rcS3(w.events(), "cond-put")
		if len(conds) != 1 || conds[0].cond != "*" || conds[0].key != rcLockKey || conds[0].who != "state" {
			t.Errorf("conditional PUTs %v, want one by the state user on %s with If-None-Match *", conds, rcLockKey)
		}
		if n := len(rcS3(w.events(), "put")); slices.ContainsFunc(rcS3(w.events(), "put"), func(e rcEvent) bool { return e.key == rcLockKey }) {
			t.Errorf("an unconditional PUT of the lock object among %d PUTs", n)
		}
		rec := rcParse(t, raw, err)
		m := rcByStep(t, rec)
		rcWant(t, m["lock-conditional-put"], "refused", 412, "PreconditionFailed", "")
		if !m["lock-conditional-put"].AsExpected {
			t.Errorf("a 412 is not as expected: %+v", m["lock-conditional-put"])
		}
	})

	t.Run("ignored", func(t *testing.T) {
		// 2xx: condition-ignored, a refutation of C005.3/C005.4 in the findings; Observe does not
		// fail on it. Exposes a check that treats a lost lock guarantee as a test failure instead
		// of an observation.
		w := newRcWorld(t)
		w.condIgnored = true
		raw, _, err := rcObserve(t, w)
		if conds := rcS3(w.events(), "cond-put"); len(conds) != 1 || conds[0].status != 200 {
			t.Errorf("conditional PUTs %v, want one answered 200", conds)
		}
		rec := rcParse(t, raw, err)
		m := rcByStep(t, rec)
		rcWant(t, m["lock-conditional-put"], "condition-ignored", 200, "", "")
		if m["lock-conditional-put"].AsExpected || !rcFinding(rec, "lock-conditional-put") || err != nil {
			t.Errorf("observation %+v findings %q, Observe = %v; want not as expected, a finding, nil", m["lock-conditional-put"], rec.Findings, err)
		}
	})
}

// TestRecoveryRestore: the supporting restore of an earlier version by copy.
func TestRecoveryRestore(t *testing.T) {
	for _, v := range []struct{ name, why string }{
		{"copy-not-effective", "the copy answers 200 and changes nothing: the GET reads v2"},
		{"copy-wrong-content", "the copy stores v2's bytes under the version id it answers, which the GET then names"},
	} {
		t.Run(v.name, func(t *testing.T) {
			// Exposes a restore reported from the copy's status or a version id alone, not v1's digest.
			w := newRcWorld(t)
			w.copyInert, w.copyWrong = v.name == "copy-not-effective", v.name == "copy-wrong-content"
			raw, _, err := rcObserve(t, w)
			evs := w.events()
			ci := rcIndex(evs, func(e rcEvent) bool { return e.cmd == "copy" })
			if ci < 0 || !slices.ContainsFunc(evs[ci+1:], func(e rcEvent) bool { return e.cmd == "get" && e.status == 200 }) {
				t.Errorf("no GET after the copy: %v", evs)
			}
			rec := rcParse(t, raw, err)
			m := rcByStep(t, rec)
			rcWant(t, m["restore-copy"], "allowed", 200, "", "")
			rcWant(t, m["restore-verify"], "mismatch", -1, "", "")
			if !rcFinding(rec, "restore-verify") || err != nil {
				t.Errorf("%s: findings %q, Observe = %v; want a restore-verify finding and nil", v.why, rec.Findings, err)
			}
		})
	}

	t.Run("copy-refused", func(t *testing.T) {
		// The restore is refused: recorded with its code, the step after it not-run. Exposes a
		// refused restore counted as recovery.
		w := newRcWorld(t)
		w.s3Script["copy"] = []rcReply{{status: 403, code: "AccessDenied"}}
		raw, _, err := rcObserve(t, w)
		evs := w.events()
		ci := rcIndex(evs, func(e rcEvent) bool { return e.cmd == "copy" })
		if ci < 0 {
			t.Fatalf("no copy: %v", evs)
		}
		if slices.ContainsFunc(evs[ci+1:], func(e rcEvent) bool { return e.cmd == "get" }) {
			t.Errorf("a GET after the refused copy: %v", evs)
		}
		rec := rcParse(t, raw, err)
		m := rcByStep(t, rec)
		rcWant(t, m["restore-copy"], "refused", 403, "AccessDenied", "")
		rcWant(t, m["restore-verify"], "not-run", -1, "", "")
		if !rcFinding(rec, "restore-copy") || err != nil {
			t.Errorf("findings %q, Observe = %v; want a restore-copy finding and nil", rec.Findings, err)
		}
	})
}

// TestRecoveryNoAnswer: a 5xx or no answer on any S3 step: that step is no-answer and Observe
// fails. Exposes an outage counted as either outcome.
func TestRecoveryNoAnswer(t *testing.T) {
	for _, s := range []struct {
		step, op string
		before   int // default answers of op before the failing one
	}{
		{"lock-conditional-put", "cond-put", 0}, {"restore-put-v1", "put", 0}, {"restore-put-v2", "put", 1},
		{"restore-copy", "copy", 0}, {"restore-verify", "get", 0},
	} {
		for _, f := range []struct {
			name string
			r    rcReply
		}{
			{"500", rcReply{status: 500, code: "InternalError"}},
			{"503", rcReply{status: 503, code: "SlowDown"}},
			{"hangup", rcReply{hangup: true, store: s.op == "put"}},
		} {
			t.Run(s.step+"/"+f.name, func(t *testing.T) {
				w := newRcWorld(t)
				w.s3Script[s.op] = append(make([]rcReply, s.before), f.r)
				raw, _, err := rcObserve(t, w)
				got := rcS3(w.events(), s.op)
				if len(got) <= s.before {
					t.Fatalf("%d %s requests, want the failing one (number %d)", len(got), s.op, s.before+1)
				}
				if e := got[s.before]; e.who != "state" || e.status != f.r.status || (e.key == rcLockKey) != (s.op == "cond-put") {
					t.Errorf("failing request %v, want the state user's on the step's object answered %d (0: no answer)", e, f.r.status)
				}
				if w.mu.Lock(); len(w.s3Script[s.op]) != 0 {
					t.Errorf("the scripted answer was not consumed")
				}
				w.mu.Unlock()
				rec := rcParse(t, raw, err)
				m := rcByStep(t, rec)
				rcWant(t, m[s.step], "no-answer", -1, "", "")
				if err == nil {
					t.Errorf("Observe = nil after no answer on %s", s.step)
				}
			})
		}
	}
}

// TestRecoveryUndo: a fresh-process Undo after each partial step empties the bucket with the
// janitor's governance bypass, delete markers included, lists again and fails on anything left
// (state's undo). Exposes locked versions left to their retention unrecorded.
func TestRecoveryUndo(t *testing.T) {
	k := "lzprobe-locked-recovery/restore"
	for _, v := range []struct {
		name string
		seed func(w *rcWorld)
	}{
		{"lock-left", func(w *rcWorld) { w.takeLock() }},
		{"lost-put", func(w *rcWorld) {
			w.takeLock()
			w.addMarker(rcLockKey)
			w.putVersion(k, []byte("lz-locked-recovery v1 0123456789\n"))
			w.putVersion(k, []byte("lz-locked-recovery v2 0123456789\n")) // its answer was lost
		}},
		{"restore-made", func(w *rcWorld) {
			w.takeLock()
			w.addMarker(rcLockKey)
			w.putVersion(k, []byte("lz-locked-recovery v1 0123456789\n"))
			w.putVersion(k, []byte("lz-locked-recovery v2 0123456789\n"))
			w.putVersion(k, []byte("lz-locked-recovery v1 0123456789\n"))
		}},
	} {
		t.Run(v.name, func(t *testing.T) {
			w := newRcWorld(t)
			v.seed(w)
			seeded := w.left()
			var term bytes.Buffer
			err := LockedRecovery(w.options()).Undo(context.Background(), rcEnv(), &term)
			evs := w.events()
			deleted := map[string]bool{}
			last := -1
			for i, e := range evs {
				switch {
				case e.tofu:
					t.Errorf("Undo ran tofu: %v", e)
				case e.who != "janitor":
					t.Errorf("Undo sent %v, want the janitor only", e)
				case e.cmd == "delete" && e.version != "":
					deleted[e.version], last = true, i
					if !slices.ContainsFunc(seeded, func(x rcVersion) bool { return x.id == e.version && x.marker }) && !e.bypass {
						t.Errorf("DELETE of version %s without the governance bypass", e.version)
					}
				}
			}
			for _, x := range seeded {
				if !deleted[x.id] {
					t.Errorf("Undo did not delete %s %s (marker %v) by its version id", x.key, x.id, x.marker)
				}
			}
			if last < 0 || !slices.ContainsFunc(evs[last+1:], func(e rcEvent) bool { return e.cmd == "list" }) {
				t.Errorf("Undo did not list again after its deletes: %v", evs)
			}
			if left := w.left(); len(left) != 0 || err != nil {
				t.Errorf("Undo = %v, left %d versions; want nil and an empty bucket", err, len(left))
			}
			if leaks := rcLeaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 {
				t.Errorf("terminal or error carry %q", leaks)
			}
		})
	}

	t.Run("still-listed", func(t *testing.T) {
		// Exposes an Undo that trusts an acknowledged delete.
		w := newRcWorld(t)
		w.takeLock()
		w.keepVersions = true
		err := LockedRecovery(w.options()).Undo(context.Background(), rcEnv(), io.Discard)
		if len(rcS3(w.events(), "delete")) == 0 || err == nil {
			t.Errorf("Undo = %v after %v, want deletes and then an error naming what is left", err, w.events())
		}
	})
}

// TestRecoverySweep: Sweep in a fresh process, with the run id and the project only, after creates
// whose answers were lost (no output, no state entry): the bucket and the two users, found by
// listing their exact names, deleted with the sandbox admin, listed again. Exposes an orphan
// locked bucket.
func TestRecoverySweep(t *testing.T) {
	// Sweep has the signature of ProbeCheck's Sweep field (data-model.md; the field lands in T038).
	var _ func(context.Context, string, string, io.Writer) error = RecoveryOptions{}.sweep

	base := "/v1/cloud/project/" + rcProject
	setup := func(t *testing.T) *rcWorld {
		w := newRcWorld(t)
		otherLower := strings.ToLower(rcOtherRun)
		w.buckets["GRA"] = []string{"lzprobe-locked-recovery-" + otherLower, rcBucket + "-x", "lzprobe-state-" + rcRunLower}
		w.buckets["SBG"] = []string{rcBucket} // Sweep is not told the region: it must list them all.
		for i, d := range append(rcUsers(rcRun), rcUsers(rcOtherRun)[0], "lzprobe-locked-recovery-"+rcRun+"-x", "lzprobe-state-"+rcRun) {
			w.users = append(w.users, rcAPIUser{ID: 201 + i, Description: d, Username: fmt.Sprintf("user-%04x", 201+i)})
		}
		return w
	}
	ours := []string{"DELETE " + base + "/region/SBG/storage/" + rcBucket, "DELETE " + base + "/user/201", "DELETE " + base + "/user/202"}
	slices.Sort(ours)
	deletes := func(t *testing.T, evs []rcEvent) ([]string, int) {
		var out []string
		last := -1
		for i, e := range evs {
			if !strings.HasPrefix(e.cmd, "api ") || e.who != "admin" {
				t.Errorf("sweep sent %v: want the sandbox admin on the OVHcloud API only", e)
				continue
			}
			if m := strings.TrimPrefix(e.cmd, "api "); strings.HasPrefix(m, http.MethodDelete+" ") {
				out, last = append(out, m), i
			}
		}
		slices.Sort(out)
		return out, last
	}
	listed := func(evs []rcEvent, from int, path string) bool {
		return from >= 0 && slices.ContainsFunc(evs[from:], func(e rcEvent) bool { return e.cmd == "api GET "+path })
	}

	t.Run("lost-creates", func(t *testing.T) {
		w := setup(t)
		var term bytes.Buffer
		err := w.options().sweep(context.Background(), rcRun, rcProject, &term)
		evs := w.events()
		got, last := deletes(t, evs)
		if !slices.Equal(got, ours) {
			t.Errorf("sweep deleted %q, want exactly %q", got, ours)
		}
		for _, p := range []string{base + "/region/GRA/storage", base + "/region/SBG/storage", base + "/user"} {
			if !listed(evs, last+1, p) {
				t.Errorf("sweep did not list %s again after its deletes: %v", p, evs)
			}
			first := slices.IndexFunc(evs, func(e rcEvent) bool { return e.cmd == "api GET "+p })
			del := slices.IndexFunc(evs, func(e rcEvent) bool { return strings.HasPrefix(e.cmd, "api DELETE "+p+"/") })
			if first < 0 || del >= 0 && first > del {
				t.Errorf("sweep deleted under %s before listing it: %v", p, evs)
			}
		}
		regions := slices.IndexFunc(evs, func(e rcEvent) bool { return e.cmd == "api GET "+base+"/region" })
		storage := slices.IndexFunc(evs, func(e rcEvent) bool { return strings.HasSuffix(e.cmd, "/storage") })
		if regions < 0 || storage >= 0 && regions > storage {
			t.Errorf("sweep listed storage before the project's regions: %v", evs)
		}
		if err != nil {
			t.Errorf("sweep = %v, want nil", err)
		}
		if leaks := rcLeaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 {
			t.Errorf("terminal or error carry %q", leaks)
		}
	})

	t.Run("partial", func(t *testing.T) {
		// Only the bucket and the janitor were created before the apply failed.
		w := newRcWorld(t)
		w.buckets["GRA"] = []string{rcBucket}
		w.users = []rcAPIUser{{ID: 302, Description: rcUsers(rcRun)[1], Username: "user-012e"}}
		err := w.options().sweep(context.Background(), rcRun, rcProject, io.Discard)
		want := []string{"DELETE " + base + "/region/GRA/storage/" + rcBucket, "DELETE " + base + "/user/302"}
		if got, _ := deletes(t, w.events()); !slices.Equal(got, want) || err != nil {
			t.Errorf("sweep = %v, deleted %q, want nil and %q", err, got, want)
		}
	})

	t.Run("still-listed", func(t *testing.T) {
		// Exposes a sweep that trusts an acknowledged delete.
		w := setup(t)
		w.apiKeep = true
		err := w.options().sweep(context.Background(), rcRun, rcProject, io.Discard)
		if got, _ := deletes(t, w.events()); !slices.Equal(got, ours) {
			t.Errorf("sweep deleted %q, want %q", got, ours)
		}
		if err == nil || !strings.Contains(err.Error(), rcBucket) {
			t.Errorf("sweep = %v, want an error naming what is left (%s)", err, rcBucket)
		}
	})

	for _, c := range [][2]string{{"lzprobe", rcProject}, {strings.ToLower(rcRun), rcProject}, {rcRun, "../" + rcProject}} {
		t.Run("bad-run-id/"+c[0]+"/"+c[1], func(t *testing.T) {
			// Control (passes on the stub too): a refusal before any request.
			w := newRcWorld(t)
			if err := w.options().sweep(context.Background(), c[0], c[1], io.Discard); err == nil || len(w.events()) != 0 {
				t.Errorf("sweep(%q, %q) = %v after %v, want a refusal before any request", c[0], c[1], err, w.events())
			}
		})
	}
}

// TestRecoveryValidate: Validate refuses published variables the check cannot use, before any
// request or tofu run, and the check publishes the data-model's output and public keys. Exposes an
// Undo emptying a bucket the run does not own.
func TestRecoveryValidate(t *testing.T) {
	c := LockedRecovery(RecoveryOptions{})
	if c.Output != "recovery_check" {
		t.Errorf("Output = %q, want recovery_check", c.Output)
	}
	if want := []string{"bucket", "region", "run_id"}; !slices.Equal(slices.Sorted(slices.Values(c.Public)), want) {
		t.Errorf("Public = %q, want %q", c.Public, want)
	}
	if err := c.Validate(rcEnv()); err != nil {
		t.Errorf("Validate refused the run's own variables: %v", err)
	}
	edit := func(k, v string) map[string]string {
		e := rcEnv()
		if v == "" {
			delete(e, k)
		} else {
			e[k] = v
		}
		return e
	}
	type badEnv struct {
		name string
		env  map[string]string
	}
	var bad []badEnv
	for _, k := range slices.Sorted(maps.Keys(rcEnv())) {
		bad = append(bad, badEnv{"missing " + k, edit(k, "")})
		e := rcEnv()
		e[k] = ""
		bad = append(bad, badEnv{"empty " + k, e})
	}
	bad = append(bad, []badEnv{
		{"run_id not a run id", edit("run_id", "lzprobe-locked-recovery")},
		{"run_id lower case", edit("run_id", rcRunLower)},
		{"run_id of another run", edit("run_id", rcOtherRun)},
		{"bucket of another run", edit("bucket", "lzprobe-locked-recovery-"+strings.ToLower(rcOtherRun))},
		{"bucket upper case", edit("bucket", "lzprobe-locked-recovery-"+rcRun)},
		{"bucket another root's", edit("bucket", "lzprobe-state-"+rcRunLower)},
		{"bucket longer", edit("bucket", rcBucket+"-x")},
		{"an extra key", edit("admin_secret", rcAdminSK)},
	}...)
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			w := newRcWorld(t)
			c := LockedRecovery(w.options())
			err := c.Validate(b.env)
			if err == nil {
				t.Errorf("Validate accepted it")
			} else if leaks := rcLeaks(err.Error()); len(leaks) > 0 {
				t.Errorf("refusal carries %q", leaks)
			}
			if _, err := c.Observe(context.Background(), b.env, io.Discard); err == nil || len(w.events()) != 0 {
				t.Errorf("Observe = %v after %v, want a refusal before any request or tofu run", err, w.events())
			}
			if err := c.Undo(context.Background(), b.env, io.Discard); err == nil || len(w.events()) != 0 {
				t.Errorf("Undo = %v after %v, want a refusal before any request", err, w.events())
			}
		})
	}
}

// TestRecoveryLeak: S3 error bodies and a tofu diagnostic that echo the published secrets reach
// neither the record nor the terminal. The guard is shown first on a leaking mutant, then
// trusted on the check.
func TestRecoveryLeak(t *testing.T) {
	w := newRcWorld(t)
	// The leaking mutant: a check that copies the server's text into its record and terminal.
	mutant := &live.ProbeCheck{Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://s3.gra.io.cloud.ovh.net/"+env["bucket"]+"/missing", nil)
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+env["state_key_id"]+"/20261016/gra/s3/aws4_request, SignedHeaders=host, Signature=0")
		resp, err := w.client().Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		text, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(term, "LZ-LIVE observe get refused: %s\n", text)
		return json.Marshal(map[string]any{"check": "locked-recovery", "reason": string(text)})
	}}
	var mterm bytes.Buffer
	mraw, _ := mutant.Observe(context.Background(), rcEnv(), &mterm)
	if leaks := rcLeaks(string(mraw)); !slices.Contains(leaks, rcStateSK) {
		t.Fatalf("the leak guard missed the mutant's record (found %q): it cannot be trusted", leaks)
	}
	if leaks := rcLeaks(mterm.String()); !slices.Contains(leaks, rcStateSK) {
		t.Fatalf("the leak guard missed the mutant's terminal (found %q): it cannot be trusted", leaks)
	}

	w2 := newRcWorld(t)
	w2.tofuEcho = true
	w2.tofuScript["force-unlock"] = []string{"unmatched"}
	w2.s3Script["cond-put"] = []rcReply{{status: 403, code: "AccessDenied"}}
	w2.s3Script["copy"] = []rcReply{{status: 403, code: "AccessDenied"}}
	raw, _, err := rcObserve(t, w2)
	evs := w2.events()
	for _, op := range []string{"cond-put", "copy"} {
		if got := rcS3(evs, op); len(got) == 0 || got[0].status != 403 {
			t.Errorf("no %s answered 403, so no echoing answer was read: %v", op, evs)
		}
	}
	if !slices.ContainsFunc(evs, func(e rcEvent) bool { return e.tofu && e.cmd == "force-unlock" }) {
		t.Errorf("no force-unlock, so no echoing diagnostic was read: %v", evs)
	}
	if raw == nil {
		t.Errorf("Observe returned no record (error %v)", err)
	}
}

// TestRecoveryFakes: the fixtures and the fakes answer as the cases above say they do, so the
// cases' red on the stub is the stub's, not the fixture's (a control: it passes on the stub by
// design).
func TestRecoveryFakes(t *testing.T) {
	fx := rcLoadFixtures(t)
	entries, err := os.ReadDir(rcFixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".txt"); ok {
			if _, listed := fx[name]; !listed {
				t.Errorf("fixture %s is not in fixtures.json", e.Name())
			}
		}
	}
	cite := regexp.MustCompile(`^(opentofu|aws-sdk-go-v2|smithy-go|go-multierror v1\.1\.1) \S+\.go:\d+`)
	for name, f := range fx {
		if len(f.Sources) == 0 || !slices.Contains([]string{"locked", "allowed", "denied", "blocked"}, f.Class) {
			t.Errorf("fixture %s: class %q, %d sources", name, f.Class, len(f.Sources))
		}
		for _, s := range f.Sources {
			if !cite.MatchString(s) && !strings.HasPrefix(s, "synthetic:") {
				t.Errorf("fixture %s cites %q, want <module> <file>:<line>", name, s)
			}
		}
		if (f.Exit == 0) != (f.Class == "allowed") {
			t.Errorf("fixture %s: exit %d, class %s", name, f.Exit, f.Class)
		}
	}
	if lh := fx["lock-held"].text; !strings.Contains(lh, "Error acquiring the state lock") || !strings.Contains(lh, "  ID:        @LOCK_ID@") || !strings.Contains(lh, "  Path:      @PATH@") {
		t.Errorf("lock-held lacks the summary, the ID or the Path line")
	}
	for _, name := range []string{"unlock-denied", "unlock-denied-read"} {
		if !strings.Contains(fx[name].text, "Failed to unlock state: ") || !strings.Contains(fx[name].text, "api error AccessDenied") {
			t.Errorf("%s lacks the unlock failure or the AccessDenied code", name)
		}
	}

	// The fake tofu, over the shared bucket.
	ctx := context.Background()
	w := newRcWorld(t)
	run := func(kill bool, args ...string) (TofuRun, error) { return w.tofu(ctx, rcStateEnv(), kill, args...) }
	if r, _ := run(false, "plan", "-lock-timeout=0s"); r.Exit != 1 || !strings.Contains(string(r.Output), "Backend initialization required") {
		t.Errorf("plan before init = %d %s", r.Exit, r.Output)
	}
	run(false, "init", "-backend-config=bucket="+rcBucket)
	if r, err := run(true, "apply"); err != nil || !r.Signaled || w.held() == "" {
		t.Errorf("A's apply = %+v %v, held %q; want signalled holding a lock", r, err, w.held())
	}
	id := w.held()
	if r, _ := run(false, "plan", "-lock-timeout=0s"); r.Exit != 1 || !strings.Contains(string(r.Output), "  ID:        "+id+"\n") ||
		!strings.Contains(string(r.Output), "  Path:      "+rcBucket+"/"+rcStateKey+"\n") {
		t.Errorf("B's plan = %d %s, want the lock-held diagnostic naming %s", r.Exit, r.Output, id)
	}
	if r, _ := run(false, "force-unlock", "-force", "wrong"); r.Exit != 1 || !strings.Contains(string(r.Output), `lock id "wrong" from s3 does not match`) || w.held() != id {
		t.Errorf("force-unlock with a wrong id = %d %s", r.Exit, r.Output)
	}
	if r, _ := run(false, "force-unlock", "-force", id); r.Exit != 0 || w.held() != "" {
		t.Errorf("force-unlock = %d %s, held %q", r.Exit, r.Output, w.held())
	}
	if r, _ := run(false, "plan", "-lock-timeout=0s"); r.Exit != 0 || !strings.Contains(string(r.Output), "Plan: 1 to add") {
		t.Errorf("B's second plan = %d %s", r.Exit, r.Output)
	}
	if v, ok := w.current(rcLockKey); ok {
		t.Errorf("the lock object is current after the unlock and B's plan: %v", v.id)
	}

	// The fake S3, through raw signed-looking requests.
	w.mu.Lock()
	w.takeLock()
	w.mu.Unlock()
	client := w.client()
	do := func(who, method, key string, hdr map[string]string, body string) (int, http.Header, string) {
		ak := map[string]string{"state": rcStateAK, "janitor": rcJanAK}[who]
		req, _ := http.NewRequestWithContext(ctx, method, "https://s3.gra.io.cloud.ovh.net/"+rcBucket+"/"+key, strings.NewReader(body))
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+ak+"/20261016/gra/s3/aws4_request, SignedHeaders=host, Signature=0")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(b)
	}
	if st, _, body := do("state", http.MethodPut, rcLockKey, map[string]string{"If-None-Match": "*"}, "x"); st != 412 || !strings.Contains(body, "PreconditionFailed") || !strings.Contains(body, rcStateSK) {
		t.Errorf("conditional PUT on the held lock = %d %s, want 412 echoing the secret", st, body)
	}
	k := "lzprobe-locked-recovery/restore"
	_, h1, _ := do("state", http.MethodPut, k, nil, "first body 0123456789")
	do("state", http.MethodPut, k, nil, "second body 0123456789")
	v1 := h1.Get("X-Amz-Version-Id")
	src := rcBucket + "/" + k + "?versionId=" + url.QueryEscape(v1)
	if st, _, _ := do("state", http.MethodPut, k, map[string]string{"X-Amz-Copy-Source": src}, ""); st != 200 {
		t.Errorf("copy of v1 = %d", st)
	}
	if st, _, body := do("state", http.MethodGet, k, nil, ""); st != 200 || body != "first body 0123456789" {
		t.Errorf("GET after the copy = %d %q, want v1", st, body)
	}
	if st, _, _ := do("state", http.MethodPut, k, map[string]string{"X-Amz-Copy-Source": rcBucket + "/" + k + "?versionId=" + v1}, ""); st != 404 {
		t.Errorf("copy with an unescaped version id = %d, want 404 (+ reads as a space)", st)
	}
	vid := url.QueryEscape(v1)
	if st, _, _ := do("state", http.MethodDelete, k+"?versionId="+vid, nil, ""); st != 403 {
		t.Errorf("the state user's versioned DELETE = %d, want 403", st)
	}
	if st, _, _ := do("janitor", http.MethodDelete, k+"?versionId="+vid, nil, ""); st != 403 {
		t.Errorf("the janitor's versioned DELETE without the bypass = %d, want 403", st)
	}
	if st, _, _ := do("janitor", http.MethodDelete, k+"?versionId="+vid, map[string]string{"X-Amz-Bypass-Governance-Retention": "true"}, ""); st != 204 {
		t.Errorf("the janitor's versioned DELETE with the bypass = %d, want 204", st)
	}
	w.condIgnored = true
	if st, _, _ := do("state", http.MethodPut, rcLockKey, map[string]string{"If-None-Match": "*"}, "x"); st != 200 {
		t.Errorf("conditional PUT with condIgnored = %d, want 200", st)
	}
	w.s3Script["get"] = []rcReply{{hangup: true}}
	n := len(w.events())
	if st, _, _ := do("state", http.MethodGet, k, nil, ""); st != 0 || len(w.events()) != n+1 {
		t.Errorf("hangup GET = %d after %d requests, want no answer after exactly one (no resend)", st, len(w.events())-n)
	}

	// The API, with the sandbox admin's token.
	w.mu.Lock()
	w.buckets["GRA"] = []string{rcBucket}
	w.users = []rcAPIUser{{ID: 7, Description: rcUsers(rcRun)[0]}}
	w.mu.Unlock()
	tr, err := http.PostForm(w.api.URL+"/token", map[string][]string{"grant_type": {"client_credentials"}, "client_id": {rcAdminID}, "client_secret": {rcAdminSK}})
	if err != nil {
		t.Fatal(err)
	}
	tb, _ := io.ReadAll(tr.Body)
	tr.Body.Close()
	if !strings.Contains(string(tb), "tok-rcadmin") {
		t.Fatalf("admin token answer %s", tb)
	}
	api := func(method, path string) (int, string) {
		req, _ := http.NewRequestWithContext(ctx, method, w.api.URL+"/v1/cloud/project/"+rcProject+path, nil)
		req.Header.Set("Authorization", "Bearer tok-rcadmin")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	for _, c := range []struct{ method, path, want string }{
		{http.MethodGet, "/region", `["GRA","SBG"]`}, {http.MethodGet, "/region/GRA/storage", rcBucket},
		{http.MethodGet, "/user", rcUsers(rcRun)[0]}, {http.MethodDelete, "/region/GRA/storage/" + rcBucket, "null"},
		{http.MethodDelete, "/user/7", "null"}, {http.MethodGet, "/region/GRA/storage", "[]"}, {http.MethodGet, "/user", "[]"},
	} {
		if st, b := api(c.method, c.path); st != 200 || !strings.Contains(b, c.want) {
			t.Errorf("%s %s = %d %s, want 200 with %s", c.method, c.path, st, b, c.want)
		}
	}
}
