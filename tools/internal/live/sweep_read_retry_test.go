package live

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// T027 (spec 007 FR-001, FR-002, FR-007; stage B review): the runner lends a probe check's Sweep
// the leftover check's own retry (its backoff, the budget ending at the destroy context's
// deadline, its clock), so a Sweep read through APIChecker.Read is retried as a listing is, and
// its failed attempts land in leftovers.json beside the listings'. And retire removes the run's
// own `.write-*` temporaries, but refuses, keeping probe.env, when the directory holds anything
// else, so a retired run never leaves a directory `--cleanup` cannot reach.

// srAlerts is a fake API whose alert listing of probeProject answers 500 the first `fail` times;
// slow, when set, runs as the first listing is answered (time passing on the test's clock).
type srAlerts struct {
	mu    sync.Mutex
	fail  int
	lists int
	slow  func()
}

func (a *srAlerts) serve(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/token" {
		fmt.Fprint(rw, `{"access_token":"tok-sweep-retry","token_type":"Bearer","expires_in":3599}`)
		return
	}
	if r.Method != http.MethodGet || r.URL.Path != "/v1/cloud/project/"+probeProject+"/alerting" {
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	a.mu.Lock()
	a.lists++
	n := a.lists
	a.mu.Unlock()
	if n == 1 && a.slow != nil {
		a.slow()
	}
	if n <= a.fail {
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	fmt.Fprint(rw, `[]`)
}

// srClock is destroyClock with time that passes during a request (advance), apart from the waits
// destroyClock records.
type srClock struct {
	*destroyClock
	mu    sync.Mutex
	extra time.Duration
}

func (c *srClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.destroyClock.Now().Add(c.extra)
}

func (c *srClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.extra += d
}

func (a *srAlerts) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lists
}

// srWait is the leftover check's backoff in TestProbeSweepReadRetry.
const srWait = time.Minute

// srAlertRoute is the alert listing's route as attempts record it.
const srAlertRoute = "/cloud/project/{serviceName}/alerting"

// srCase is one run: the time left before the destroy deadline when the first child starts, how
// long the Sweep's first listing takes, and whether its retry is admitted.
type srCase struct {
	name  string
	left  time.Duration
	slow  time.Duration
	admit bool
}

// srOutcome is what a run left: its error, the Sweep listings sent, the waits on the check's clock
// and the Sweep listing's attempts in leftovers.json.
type srOutcome struct {
	err   error
	sends int
	waits []time.Duration
	got   []ListingAttempt
	term  string
}

// TestProbeSweepReadRetry: admitted — the destroy deadline leaves the reserve, the wait and 5 s:
// the Sweep's listing answered 500 once is retried after the leftover check's backoff on its
// clock, the probe passes, and leftovers.json records the attempt (route with placeholders,
// retried). refused — 5 s less: one request, the attempt recorded with reason `budget`, and the
// probe fails. slow-read — the destroy deadline leaves 45 s beyond the reserve and the wait, but
// the failed read itself takes 40 s: the reserve rises to 40 s (the budget keeps the slowest
// request, spec 006 data-model *Retry budget*), so the wait is refused, where a budget that did not
// keep it would admit it (35 s left over the 30 s floor). Exposes a Sweep read sent once, a retry of
// its own (another budget, clock or backoff), a read whose duration the budget never keeps, and
// attempts kept out of the run record.
func TestProbeSweepReadRetry(t *testing.T) {
	for _, c := range []srCase{
		{name: "admitted", left: minRetryReserve + srWait + 5*time.Second, admit: true},
		{name: "refused", left: minRetryReserve + srWait - 5*time.Second},
		{name: "slow-read", left: 40*time.Second + srWait + 35*time.Second, slow: 40 * time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := srRun(t, c)
			if c.admit {
				srWantAdmitted(t, c, o)
				return
			}
			srWantRefused(t, o)
		})
	}
}

// srSweep is a Sweep that lists the project's alerts through Read against srv.
func srSweep(srv *httptest.Server) func(ctx context.Context, runID, project string, term io.Writer) error {
	return func(ctx context.Context, _, project string, _ io.Writer) error {
		ac, err := NewAPIChecker(ctx, API{TokenURL: srv.URL + "/token", BaseURL: srv.URL + "/v1", HTTP: srv.Client()}, Credential{Endpoint: "ovh-eu", ClientID: "EU.sweep", ClientSecret: "Sweep-Secret-9x"})
		if err != nil {
			return err
		}
		a, err := ac.Read(ctx, APICall{Op: APIListAlerts, Project: project})
		if err == nil && a.Status != http.StatusOK {
			err = fmt.Errorf("listing answered %d", a.Status)
		}
		return err
	}
}

// srRun runs a probe whose Sweep's first listing answers 500, on the check's backoff and clock.
func srRun(t *testing.T, c srCase) srOutcome {
	api := &srAlerts{fail: 1}
	srv := httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(srv.Close)
	w := newRunWorld(t)
	ch := (&checkHooks{}).check(w)
	ch.Sweep = srSweep(srv)
	p := w.swProbe(t, tempPrivate(t), tofuStack{Outputs: checkOutputs(t, w, "deny_check", checkEnvWant())}, ch)
	clk := &srClock{destroyClock: &destroyClock{t: t, left: c.left}}
	if c.slow > 0 {
		api.slow = func() { clk.advance(c.slow) }
	}
	p.Run.Leftovers.Backoff = []time.Duration{srWait, srWait}
	p.Run.Leftovers.Clock = clk
	p.Run.childStarted = clk.started
	o := srOutcome{err: p.Start(context.Background())}
	var rec struct {
		Attempts []ListingAttempt `json:"attempts"`
	}
	readJSON(t, filepath.Join(w.runDir, "leftovers.json"), &rec)
	o.got = attemptsOf(Report{Attempts: rec.Attempts}, srAlertRoute)
	dc := clk.destroyClock
	dc.mu.Lock()
	o.waits = slices.Clone(dc.waits)
	dc.mu.Unlock()
	o.sends, o.term = api.count(), w.term.String()
	return o
}

func srWantAdmitted(t *testing.T, c srCase, o srOutcome) {
	t.Helper()
	if o.err != nil {
		t.Errorf("probe whose Sweep listing answered 500 once with %v left: %v\n%s", c.left, o.err, o.term)
	}
	if o.sends != 2 {
		t.Errorf("Sweep listing sent %d times, want 2 (retried once)", o.sends)
	}
	if !slices.Equal(o.waits, []time.Duration{srWait}) {
		t.Errorf("waits %v, want [%v] on the leftover check's clock and backoff", o.waits, srWait)
	}
	want := []ListingAttempt{{Path: srAlertRoute, Attempt: 1, Status: 500, Class: "status", Retried: true}}
	if !slices.Equal(o.got, want) {
		t.Errorf("leftovers.json attempts %+v, want %+v", o.got, want)
	}
}

func srWantRefused(t *testing.T, o srOutcome) {
	t.Helper()
	if ExitCode(o.err) == 0 {
		t.Errorf("probe exited 0 (%v) although its Sweep listing failed and the wait did not fit the budget", o.err)
	}
	if o.sends != 1 {
		t.Errorf("Sweep listing sent %d times, want once: the budget refuses the wait", o.sends)
	}
	if len(o.waits) != 0 {
		t.Errorf("waits %v, want none", o.waits)
	}
	want := []ListingAttempt{{Path: srAlertRoute, Attempt: 1, Status: 500, Class: "status", Reason: "budget"}}
	if !slices.Equal(o.got, want) {
		t.Errorf("leftovers.json attempts %+v, want %+v", o.got, want)
	}
}

// srRunDir is a run directory holding probe.env, passphrase.env and extra (names in it).
func srRunDir(t *testing.T, extra ...string) (dir, root, pass string) {
	t.Helper()
	dir = filepath.Join(tempPrivate(t), "probes", swRunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	root, pass = filepath.Join(dir, "probe.env"), filepath.Join(dir, "passphrase.env")
	for _, f := range append([]string{"probe.env", "passphrase.env"}, extra...) {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("X=1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, root, pass
}

// TestRetireWriteTemporary: retire removes a `.write-*` temporary (a kill inside
// WriteCredentialFile) with the run's files and the directory.
func TestRetireWriteTemporary(t *testing.T) {
	dir, root, pass := srRunDir(t, ".write-1234567")
	if err := retire(dir, root, pass); err != nil {
		t.Fatalf("retire with a .write-* temporary: %v", err)
	}
	if exists(dir) {
		t.Errorf("run directory %s kept after retire", dir)
	}
}

// TestRetireUnknownFile: a file the run did not write makes retire fail before it removes
// anything, naming it, so probe.env stays and `--cleanup` can still find the run.
func TestRetireUnknownFile(t *testing.T) {
	dir, root, pass := srRunDir(t, "notes.txt")
	stray := filepath.Join(dir, "notes.txt")
	err := retire(dir, root, pass)
	if err == nil {
		t.Fatal("retire passed with a file the run did not write")
	}
	if !strings.Contains(err.Error(), "notes.txt") {
		t.Errorf("error %q does not name notes.txt", err)
	}
	if !exists(root) || !exists(pass) || !exists(stray) {
		t.Errorf("after the refusal probe.env %v, passphrase.env %v, notes.txt %v; want all kept (nothing removed)", exists(root), exists(pass), exists(stray))
	}
}

// TestRetireMissingDirectory: a directory already gone retires as before: nothing to remove, no
// error.
func TestRetireMissingDirectory(t *testing.T) {
	dir := filepath.Join(tempPrivate(t), "probes", swRunID)
	if err := retire(dir, filepath.Join(dir, "probe.env"), filepath.Join(dir, "passphrase.env")); err != nil {
		t.Errorf("retire of a missing directory: %v", err)
	}
}

// TestCheckReportsLentAttempts: the attempts a Sweep recorded in the lent retry are reported also
// when the check cannot build its lister (no API client): the run record keeps them (FR-002).
func TestCheckReportsLentAttempts(t *testing.T) {
	c := LeftoverCheck{Projects: []Project{{ID: probeProject}}}
	c.lent = c.retry()
	c.lent.attempts = []ListingAttempt{{Path: srAlertRoute, Attempt: 1, Status: 500, Class: "status", Reason: "budget"}}
	rep := c.Check(context.Background(), nil)
	if rep.Outcome == "pass" {
		t.Errorf("check without a lister passed: %+v", rep)
	}
	if !slices.Equal(rep.Attempts, c.lent.attempts) {
		t.Errorf("attempts %+v, want the Sweep's %+v", rep.Attempts, c.lent.attempts)
	}
}
