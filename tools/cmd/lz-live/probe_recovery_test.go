//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// Spec 007 T013 (FR-006, FR-007, SC-005): the budget-alert probe's recovery and start-up phases
// through the production entry, each `lz-live probe` a fresh process of the test binary (rcEntry,
// as spec 003 T037's swRun): the run(args, deps) of main with the probe world's host facts, the
// operator's terminal opened as production opens it (openTTY). The fake API (rcWorld.serve) is the
// probe world's, plus GET /v1/me and the alert routes of project p1 (kb api/v1/cloud.json:
// GET …/alerting answers uuid[], GET …/alerting/{id} cloud.alerting.Alerting, DELETE void). The
// fake tofu (rcTofu, the test binary behind bin/tofu while bin/budget-tofu.mode exists) creates
// alert B remotely through a file the fake API reads (remote-created), and its destroy removes it
// (remote-destroyed). Alert A is in the project before every run. No test calls OVHcloud or reads
// the owner's ~/.config/ovh-lz/.
//
// A kill is SIGKILL of the fresh process's group while it waits at a deterministic point (a
// blocked fake tofu, which the runner starts in a group of its own, exits when its parent dies): the first alert listing before any apply (the baseline listing, phase 3:
// probe.env and the passphrase are written), the first apply (phase 4: both baseline copies are
// written), or the first alert request after a failed apply (before the run's own end). The windows
// in between (after probe.env only; after the record copy only) are the disk of the nearest kill
// with the later write removed.

const (
	rcEntryArg = "lz-live-entry"    // argv[1] of a fresh lz-live process
	rcModeFile = "budget-tofu.mode" // next to the fake tofu: lost, create or block
	rcCreated  = "remote-created"   // ids the fake tofu created remotely, one per line
	rcDestroy  = "remote-destroyed" // ids the fake tofu's destroy removed, one per line
	rcRoot     = "tests/live/probes/budget-alert"
	rcAlertA   = "1a2b3c4d-0000-4000-8000-00000000000a" // in the project before every run
	rcAlertB   = "1a2b3c4d-0000-4000-8000-00000000000b" // the run's alert
	rcEmail    = "owner@example.org"                    // GET /me's address
	rcNetwork  = "/v1/cloud/project/p1/network/private"
	rcAlerting = "/v1/cloud/project/p1/alerting"
	rcWait     = 60 * time.Second
)

func init() {
	if len(os.Args) == 3 && os.Args[1] == rcEntryArg {
		os.Exit(rcEntry(os.Args[2]))
	}
	if filepath.Base(os.Args[0]) == "tofu" {
		if mode, err := os.ReadFile(filepath.Join(filepath.Dir(os.Args[0]), rcModeFile)); err == nil {
			os.Exit(rcTofu(strings.TrimSpace(string(mode)), os.Args[1:]))
		}
	}
}

// rcConfig is what a fresh lz-live process gets from its test: the probe world's host facts.
type rcConfig struct {
	Home, Checkout, Git, Marker, APIURL string
	Env                                 map[string]string
	Args                                []string
	Now                                 time.Time
}

// rcEntry is a fresh lz-live process: main's run with the test's host facts; nothing of an
// earlier process is in memory.
func rcEntry(cfgPath string) int {
	var c rcConfig
	raw, err := os.ReadFile(cfgPath)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		fmt.Fprintln(os.Stderr, "lz-live entry: bad config")
		return 2
	}
	bin := filepath.Dir(c.Git)
	return run(c.Args, deps{
		Getwd:         func() (string, error) { return c.Checkout, nil },
		Home:          func() (string, error) { return c.Home, nil },
		Getenv:        func(k string) string { return c.Env[k] },
		Git:           c.Git,
		OfflineMarker: c.Marker,
		Stderr:        os.Stderr,
		Stdout:        os.Stdout,
		LookPath: func(name string) (string, error) {
			p := filepath.Join(bin, name)
			_, err := os.Stat(p)
			return p, err
		},
		API: func(string) (live.API, error) {
			return live.API{TokenURL: c.APIURL + "/token", BaseURL: c.APIURL + "/v1", HTTP: http.DefaultClient}, nil
		},
		Now:      func() time.Time { return c.Now },
		Terminal: openTTY,
	})
}

// rcTofu is the budget-alert root's fake tofu: apply creates alert B remotely (lost: then fails
// before any state; create: reports it and writes state; block: waits to be killed), the apply of
// a destroy plan with state removes B, and `output -json` publishes the root's real output shape.
// Everything else is fakeTofu.
func rcTofu(mode string, args []string) int {
	bin := filepath.Dir(os.Args[0])
	sub, positional := rcSub(args)
	saved, _ := os.ReadFile(positional)
	switch {
	case sub == "output" && positional == "":
		logTofu(bin, args, "", false)
		fmt.Printf(`{"alert_id":{"sensitive":false,"type":"string","value":%q},"budget_alert_check":{"sensitive":false,"type":["map","string"],"value":{"alert_id":%q}},"run_id":{"sensitive":false,"type":"string","value":%q}}`+"\n",
			rcAlertB, rcAlertB, os.Getenv("TF_VAR_run_id"))
		return 0
	case sub == "apply" && string(saved) == fakeDestroyPlan:
		if _, err := os.Stat(os.Getenv("TF_VAR_state_path")); err == nil {
			rcAppend(filepath.Join(bin, rcDestroy), rcAlertB)
		}
	case sub == "apply" && saved != nil:
		logTofu(bin, args, "", false)
		return rcApply(bin, mode)
	}
	return fakeTofu(args)
}

func rcSub(args []string) (sub, positional string) {
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-"):
		case sub == "":
			sub = a
		default:
			positional = a
		}
	}
	return sub, positional
}

func rcApply(bin, mode string) int {
	if mode == "block" {
		// Until lz-live is killed (then this process is reparented), bounded.
		parent := os.Getppid()
		for end := time.Now().Add(2 * rcWait); os.Getppid() == parent && time.Now().Before(end); {
			time.Sleep(10 * time.Millisecond)
		}
		return 1
	}
	rcAppend(filepath.Join(bin, rcCreated), rcAlertB)
	if mode == "lost" {
		fmt.Fprintln(os.Stderr, "Error: fake: the create succeeded remotely and its answer was lost")
		return 1
	}
	fmt.Println(`{"@level":"info","@message":"module.project.ovh_cloud_project_alerting.this[0]: Creation complete after 1s [id=` + rcAlertB + `]","type":"apply_complete","hook":{"resource":{"addr":"module.project.ovh_cloud_project_alerting.this[0]","resource_type":"ovh_cloud_project_alerting"},"action":"create","id_key":"id","id_value":"` + rcAlertB + `"}}`)
	state := os.Getenv("TF_VAR_state_path")
	return writeOK(state, "state") + writeOK(state+".backup", "previous state")
}

func rcAppend(path, line string) {
	if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, line)
		f.Close()
	}
}

// rcWorld is the probe world with the budget-alert root and the alert routes.
type rcWorld struct {
	*probeWorld
	bin      string
	mu       sync.Mutex
	alerts   map[string]bool // ids listed in p1
	synced   map[string]bool // lines of remote-created and remote-destroyed already applied
	reqs     []string        // alert requests ("GET", "GET <id>", "DELETE <id>"; " 404" when absent, "GET retired" when watch is gone) and "destroyed <id>"
	block    func(r *http.Request) bool
	fail     func(r *http.Request) bool // answer 403
	watch    string                     // a run's directory under state/probes/
	blocked  chan struct{}
	release  chan struct{}
	runs     int
	inflight []*rcProc
}

func newRCWorld(t *testing.T, mode string) *rcWorld {
	t.Helper()
	w := &rcWorld{probeWorld: newProbeWorld(t), alerts: map[string]bool{rcAlertA: true}, synced: map[string]bool{},
		blocked: make(chan struct{}, 1), release: make(chan struct{})}
	w.bin = filepath.Dir(w.git)
	writePrivate(t, filepath.Join(w.checkout, rcRoot, "main.tf"), "# budget-alert probe\n")
	w.setMode(t, mode)
	w.apiHook = w.serve
	// Before the API server closes (cleanups run last-registered first).
	t.Cleanup(func() {
		close(w.release)
		for _, p := range w.inflight {
			p.stop()
		}
	})
	return w
}

func (w *rcWorld) setBlock(b func(*http.Request) bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.block = b
}

func (w *rcWorld) setFail(f func(*http.Request) bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fail = f
}

func (w *rcWorld) setWatch(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.watch = dir
}

func (w *rcWorld) setMode(t *testing.T, mode string) {
	t.Helper()
	writePrivate(t, filepath.Join(w.bin, rcModeFile), mode)
}

// serve answers GET /v1/me and the alert routes of p1 with the sandbox admin's token; it blocks a
// request block selects until the requesting process is gone.
func (w *rcWorld) serve(rw http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer tok-1" {
		return false
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/me" {
		fmt.Fprintf(rw, `{"nichandle":%q,"email":%q}`, probeAccount, rcEmail)
		return true
	}
	rest, ok := strings.CutPrefix(r.URL.Path, rcAlerting)
	if !ok || rest != "" && !strings.HasPrefix(rest, "/") {
		return false
	}
	w.mu.Lock()
	block, fail := w.block, w.fail
	w.mu.Unlock()
	if fail != nil && fail(r) {
		w.mu.Lock()
		w.reqs = append(w.reqs, "FAIL "+r.Method)
		w.mu.Unlock()
		rw.WriteHeader(http.StatusForbidden)
		return true
	}
	if block != nil && block(r) {
		select {
		case w.blocked <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-w.release:
		}
		return true
	}
	w.answerAlert(rw, r.Method, strings.TrimPrefix(rest, "/"))
	return true
}

func (w *rcWorld) answerAlert(rw http.ResponseWriter, method, id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sync()
	req := strings.TrimSpace(method + " " + id)
	if _, err := os.Stat(w.watch); w.watch != "" && err != nil && req == "GET" {
		req = "GET retired"
	}
	if id != "" && !w.alerts[id] {
		req += " 404"
	}
	w.reqs = append(w.reqs, req)
	rw.Header().Set("Content-Type", "application/json")
	switch {
	case id == "" && method == http.MethodGet:
		ids := []string{}
		for a := range w.alerts {
			ids = append(ids, a)
		}
		slices.Sort(ids)
		_ = json.NewEncoder(rw).Encode(ids)
	case w.alerts[id] && method == http.MethodGet:
		fmt.Fprintf(rw, `{"id":%q,"delay":3600,"email":%q,"emails":[%q],"monthlyThreshold":1,"name":"","service":"all"}`, id, rcEmail, rcEmail)
	case w.alerts[id] && method == http.MethodDelete:
		delete(w.alerts, id)
		fmt.Fprint(rw, "null")
	default:
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"The requested object does not exist"}`)
	}
}

// sync applies what the fake tofu did remotely since the last request (w.mu held).
func (w *rcWorld) sync() {
	// Creates before destroys: B is destroyed only after it was created.
	for _, f := range []struct {
		name    string
		present bool
	}{{rcCreated, true}, {rcDestroy, false}} {
		raw, _ := os.ReadFile(filepath.Join(w.bin, f.name))
		for n, id := range strings.Fields(string(raw)) {
			key := fmt.Sprintf("%s:%d", f.name, n)
			if !w.synced[key] {
				w.synced[key] = true
				w.setAlert(id, f.present)
			}
		}
	}
}

func (w *rcWorld) setAlert(id string, present bool) {
	if present {
		w.alerts[id] = true
		return
	}
	delete(w.alerts, id)
	w.reqs = append(w.reqs, "destroyed "+id)
}

func (w *rcWorld) has(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sync()
	return w.alerts[id]
}

// removeByHand is the owner deleting an alert in the Control Panel.
func (w *rcWorld) removeByHand(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sync()
	delete(w.alerts, id)
}

func (w *rcWorld) requests() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.reqs)
}

func (w *rcWorld) deletes() []string {
	var out []string
	for _, r := range w.requests() {
		if strings.HasPrefix(r, "DELETE ") {
			out = append(out, r)
		}
	}
	return out
}

// applies counts the fake tofu's forward applies so far.
func (w *rcWorld) applies() int {
	raw, _ := os.ReadFile(filepath.Join(w.bin, "tofu.log"))
	n := 0
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var c childCall
		if json.Unmarshal(line, &c) == nil && slices.Equal(subcommands([]childCall{c}), []string{"apply"}) {
			n++
		}
	}
	return n
}

func (w *rcWorld) tofuCalls() int { return countLines(filepath.Join(w.bin, "tofu.log")) }

// rcProc is one fresh lz-live process.
type rcProc struct {
	cmd            *exec.Cmd
	stdout, stderr bytes.Buffer
	done           chan struct{}
	prompted       chan struct{} // closed once a process on a terminal has shown rcTypeBack
	code           int
}

// rcTypeBack ends the --accept-lost-baseline prompt: the owner types after it.
const rcTypeBack = "Type the run id to continue: "

// promptTap closes seen once the stream has carried mark (one writer: exec's stderr copier).
type promptTap struct {
	mark, got []byte
	seen      chan struct{}
	done      bool
}

func (w *promptTap) Write(b []byte) (int, error) {
	if !w.done {
		w.got = append(w.got, b...)
		if bytes.Contains(w.got, w.mark) {
			w.done = true
			close(w.seen)
		}
	}
	return len(b), nil
}

func (p *rcProc) kill() { _ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL) }

// stop kills a process still running and waits for it.
func (p *rcProc) stop() {
	select {
	case <-p.done:
	default:
		p.kill()
		<-p.done
	}
}

// start starts a fresh lz-live process in its own session (no controlling terminal), or with tty
// as its stdin and controlling terminal.
func (w *rcWorld) start(t *testing.T, tty *os.File, args ...string) *rcProc {
	t.Helper()
	w.runs++
	c := rcConfig{Home: w.home, Checkout: w.checkout, Git: w.git, Marker: w.marker, APIURL: w.api.URL, Env: w.env, Args: args,
		Now: time.Date(2026, 10, 19, 12, w.runs, 0, 0, time.UTC)}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "entry.json")
	writePrivate(t, cfg, string(raw))
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &rcProc{cmd: exec.Command(self, rcEntryArg, cfg), done: make(chan struct{}), prompted: make(chan struct{})}
	p.cmd.Stdout, p.cmd.Stderr = &p.stdout, &p.stderr
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if tty != nil {
		p.cmd.Stdin = tty
		p.cmd.SysProcAttr.Setctty = true
		p.cmd.Stderr = io.MultiWriter(&p.stderr, &promptTap{mark: []byte(rcTypeBack), seen: p.prompted})
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w.inflight = append(w.inflight, p)
	go func() {
		_ = p.cmd.Wait()
		p.code = p.cmd.ProcessState.ExitCode()
		close(p.done)
	}()
	return p
}

func (p *rcProc) wait(t *testing.T) (int, string, string) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(rcWait):
		p.kill()
		<-p.done
		t.Fatalf("lz-live %v did not end within %s\nstdout:\n%s\nstderr:\n%s", p.cmd.Args, rcWait, p.stdout.String(), p.stderr.String())
	}
	return p.code, p.stdout.String(), p.stderr.String()
}

func (w *rcWorld) fresh(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return w.start(t, nil, args...).wait(t)
}

func (w *rcWorld) probeStart(t *testing.T) (int, string, string) {
	t.Helper()
	return w.fresh(t, "probe", "--reviewed-sha", fakeHead, rcRoot)
}

func (w *rcWorld) cleanup(t *testing.T, id string, extra ...string) (int, string, string) {
	t.Helper()
	return w.fresh(t, append([]string{"probe", "--reviewed-sha", fakeHead, "--cleanup", id}, extra...)...)
}

// killAtBlock waits until the process is held by the fake API's block, then kills its group.
func (w *rcWorld) killAtBlock(t *testing.T, p *rcProc, point string) string {
	t.Helper()
	select {
	case <-w.blocked:
	case <-p.done:
		t.Fatalf("premise: the run ended without reaching %s\nstdout:\n%s\nstderr:\n%s", point, p.stdout.String(), p.stderr.String())
	case <-time.After(rcWait):
		t.Fatalf("premise: the run did not reach %s within %s", point, rcWait)
	}
	return w.killed(t, p)
}

// killAtApply waits until the fake tofu's first apply runs (mode block), then kills the group.
func (w *rcWorld) killAtApply(t *testing.T, p *rcProc) string {
	t.Helper()
	for deadline := time.Now().Add(rcWait); w.applies() == 0; time.Sleep(10 * time.Millisecond) {
		select {
		case <-p.done:
			t.Fatalf("premise: the run ended before its first apply\nstdout:\n%s\nstderr:\n%s", p.stdout.String(), p.stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("premise: no apply within %s", rcWait)
		}
	}
	return w.killed(t, p)
}

func (w *rcWorld) killed(t *testing.T, p *rcProc) string {
	t.Helper()
	p.kill()
	<-p.done
	w.setBlock(nil)
	return runIDOf(t, p.stdout.String())
}

// beforeAnyApply holds the first alert listing before any apply: the baseline listing (phase 3).
func (w *rcWorld) beforeAnyApply(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == rcAlerting && w.applies() == 0
}

// afterApply holds the first alert request after the first apply: the failed run before its end.
func (w *rcWorld) afterApply(*http.Request) bool { return w.applies() > 0 }

func (w *rcWorld) recordCopy(id string) string {
	return filepath.Join(w.checkout, ".local", "live", id, "baseline.json")
}

func (w *rcWorld) recoveryCopy(id string) string {
	return filepath.Join(w.cfg, "accounts", probeAccount, "state", "baselines", id+".json")
}

// recoveryFiles fails unless every file a later --cleanup needs is on disk.
func (w *rcWorld) recoveryFiles(t *testing.T, id, when string) {
	t.Helper()
	for _, f := range []string{filepath.Join(w.probeDir(id), "probe.env"), filepath.Join(w.probeDir(id), "passphrase.env"), w.recordCopy(id), w.recoveryCopy(id)} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s: %s is gone: %v", when, f, err)
		}
	}
}

// retired fails unless the run's directory under state/probes/ and its recovery copy are gone.
func (w *rcWorld) retired(t *testing.T, id string) {
	t.Helper()
	for _, f := range []string{w.probeDir(id), w.recoveryCopy(id)} {
		if _, err := os.Lstat(f); err == nil {
			t.Errorf("run %s is not retired: %s is still there", id, f)
		}
	}
}

// startProceeds fails unless a probe start is not refused and passes.
func (w *rcWorld) startProceeds(t *testing.T, after string) {
	t.Helper()
	w.setMode(t, "create")
	code, stdout, stderr := w.probeStart(t)
	if strings.Contains(stdout+stderr, live.CondUnreconciledRun) || code != 0 {
		t.Errorf("after %s the next start: exit %d, want 0 and no %s refusal\nstdout:\n%s\nstderr:\n%s", after, code, live.CondUnreconciledRun, stdout, stderr)
	}
}

// lostCreate runs a probe whose create succeeds remotely and loses its answer (no state, no
// inventory); killed, the process is SIGKILLed at its first alert request after the failed apply
// instead of ending by itself.
func (w *rcWorld) lostCreate(t *testing.T, killed bool) string {
	t.Helper()
	w.setMode(t, "lost")
	if killed {
		w.setBlock(w.afterApply)
		return w.killAtBlock(t, w.start(t, nil, "probe", "--reviewed-sha", fakeHead, rcRoot), "an alert request after its failed apply")
	}
	// The run's own sweep would delete B and retire the run: its alert requests after the apply
	// are refused, so it ends unreconciled, as a run whose sweep failed.
	w.setFail(w.afterApply)
	code, stdout, stderr := w.probeStart(t)
	w.setFail(nil)
	if code == 0 {
		t.Fatalf("premise: a probe whose apply failed exited 0\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	return runIDOf(t, stdout)
}

// TestProbeRecoveryLostCreate (SC-005; FR-007 recovery files): given a fake apply that creates B
// remotely and exits non-zero before any state, so no state and no inventory exist / when the
// process ends (by itself, or SIGKILLed before its own cleanup) / then probe.env, the passphrase
// and both baseline copies are on disk; a fresh --cleanup then deletes only B, lists again, leaves
// A, passes its leftover check, retires the run, and the next start proceeds → exposes
// Probe.run's no-state branch removing the recovery files after an error, a cleanup that
// cannot reach a nameless orphan, or one that deletes a baseline alert.
func TestProbeRecoveryLostCreate(t *testing.T) {
	for _, killed := range []bool{false, true} {
		t.Run(map[bool]string{false: "exit", true: "kill"}[killed], func(t *testing.T) {
			w := newRCWorld(t, "lost")
			id := w.lostCreate(t, killed)
			if !w.has(rcAlertB) {
				t.Fatal("premise: the fake create did not make alert B")
			}
			w.recoveryFiles(t, id, "after the lost create")
			w.setWatch(w.probeDir(id))
			code, stdout, stderr := w.cleanup(t, id)
			if code != 0 {
				t.Errorf("cleanup: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			w.sweptOnlyB(t)
			w.retired(t, id)
			w.startProceeds(t, "a passing cleanup")
		})
	}
}

// sweptOnlyB fails unless the only DELETE was B's, a listing followed it, B is gone and A is kept.
func (w *rcWorld) sweptOnlyB(t *testing.T) {
	t.Helper()
	reqs := w.requests()
	if got := w.deletes(); !slices.Equal(got, []string{"DELETE " + rcAlertB}) {
		t.Errorf("DELETEs %v, want only B's (requests %v)", got, reqs)
	}
	if i := slices.Index(reqs, "DELETE "+rcAlertB); i < 0 || !slices.Contains(reqs[i+1:], "GET") {
		t.Errorf("no alert listing after B's DELETE while the run's files were kept (requests %v)", reqs)
	}
	if w.has(rcAlertB) || !w.has(rcAlertA) {
		t.Errorf("after the cleanup B listed %v, A listed %v; want B gone and A kept", w.has(rcAlertB), w.has(rcAlertA))
	}
}

// TestProbeRecoveryCleanupLeftoverFails (FR-007): given the lost create's disk / when the
// cleanup's leftover check fails (an lzprobe- network) / then the run's files stay and the output
// names `--cleanup <run-id>` again → exposes files removed before reconciliation succeeds.
func TestProbeRecoveryCleanupLeftoverFails(t *testing.T) {
	w := newRCWorld(t, "lost")
	id := w.lostCreate(t, true)
	w.listings[rcNetwork] = `[{"id":"pn-old","name":"lzprobe-old","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}]`
	code, stdout, stderr := w.cleanup(t, id)
	if code == 0 {
		t.Fatalf("premise: a cleanup with an lzprobe- leftover exited 0\nstdout:\n%s", stdout)
	}
	for _, f := range []string{filepath.Join(w.probeDir(id), "probe.env"), filepath.Join(w.probeDir(id), "passphrase.env"), w.recordCopy(id), w.recoveryCopy(id)} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("a cleanup whose leftover check failed removed %s: %v", f, err)
		}
	}
	if !strings.Contains(stdout+stderr, "--cleanup "+id) {
		t.Errorf("the failed cleanup does not name --cleanup %s again\nstdout:\n%s\nstderr:\n%s", id, stdout, stderr)
	}
	// The failure is the leftover check's: the sweep before it passed.
	if d := w.deletes(); !slices.Equal(d, []string{"DELETE " + rcAlertB}) || !strings.Contains(stdout+stderr, "leftover") {
		t.Errorf("cleanup DELETEs %v, want B's, and a failure naming the leftover check\nstdout:\n%s\nstderr:\n%s", d, stdout, stderr)
	}
}

// TestProbeRecoveryBudgetAlertPasses (FR-007, FR-008): given a create that works and the root's
// real output shape (alert_id scalar, budget_alert_check map) / when a probe runs to the end /
// then observations.json records the read-back, B is gone after destroy and sweep, A is kept, the
// run passes and is retired → exposes a check output the runner cannot decode on the success path.
func TestProbeRecoveryBudgetAlertPasses(t *testing.T) {
	w := newRCWorld(t, "create")
	code, stdout, stderr := w.probeStart(t)
	if code != 0 {
		t.Errorf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	id := runIDOf(t, stdout)
	raw, err := os.ReadFile(filepath.Join(w.checkout, ".local", "live", id, "observations.json"))
	var obs map[string]any
	if err != nil || json.Unmarshal(raw, &obs) != nil {
		t.Fatalf("observations.json %q (%v): want the read-back", raw, err)
	}
	digest := sha256.Sum256([]byte("p1"))
	want := map[string]any{"alert_id": rcAlertB, "project_sha256": hex.EncodeToString(digest[:]), "monthly_threshold": 1.0, "delay": 3600.0, "matches_plan": true}
	for k, v := range want {
		if obs[k] != v {
			t.Errorf("observations.json %s = %v, want %v (%s)", k, obs[k], v, raw)
		}
	}
	w.readBackThenSwept(t)
	if w.has(rcAlertB) || !w.has(rcAlertA) || len(w.deletes()) != 0 {
		t.Errorf("after the run B listed %v, A listed %v, DELETEs %v; want B destroyed, A kept, nothing swept", w.has(rcAlertB), w.has(rcAlertA), w.deletes())
	}
	if !strings.Contains(stdout, "LZ-LIVE summary "+id+" pass") {
		t.Errorf("stdout lacks the pass summary:\n%s", stdout)
	}
	w.retired(t, id)
}

// readBackThenSwept fails unless B was read (found) before its destroy and the alerts were
// listed after it (the sweep).
func (w *rcWorld) readBackThenSwept(t *testing.T) {
	t.Helper()
	reqs := w.requests()
	d := slices.Index(reqs, "destroyed "+rcAlertB)
	r := slices.Index(reqs, "GET "+rcAlertB)
	if d < 0 || r < 0 || r > d || !slices.Contains(reqs[d+1:], "GET") {
		t.Errorf("requests %v: want B read before its destroy and a listing after it", reqs)
	}
}

// rcWindow is a kill between two start-up writes: where the process is killed and which later
// write is then removed (the disk of a kill one write earlier); noTofu rows are retired with no
// tofu call, and the pre-create ones (rows 1, 2) need no passphrase (its file is gone or cut).
type rcWindow struct {
	name     string
	atApply  bool   // killed at the first apply, else at the baseline listing
	drop     string // "passphrase", "recovery" or ""
	truncate bool   // the passphrase file is cut to an unusable value
	noTofu   bool
}

// TestProbeRecoveryStartUpKill (FR-006, SC-005): a SIGKILL after probe.env, after the passphrase,
// after the record copy of the baseline, and after both copies / when the next start runs / then
// it is refused unreconciled-run naming the run; a --cleanup whose leftover check fails keeps the
// run; a --cleanup then retires the first three with no tofu call (the first two with no usable
// passphrase), sweeps the fourth with nothing to delete, and the next start proceeds → exposes a
// start-up window that blocks every later probe, or a cleanup that needs the passphrase of a run
// that never applied. The record-copy row keeps its passphrase: a valid record copy makes it
// baseline-backed by data-model's predicate (evidence T013, DR-T013-b).
func TestProbeRecoveryStartUpKill(t *testing.T) {
	for _, c := range []rcWindow{
		{name: "after-probe-env", drop: "passphrase", noTofu: true},
		{name: "after-passphrase", truncate: true, noTofu: true},
		{name: "after-record-copy", atApply: true, drop: "recovery", noTofu: true},
		{name: "after-both-copies", atApply: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newRCWorld(t, "create")
			id := w.killWindow(t, c)
			w.refusedBeside(t, id)
			w.cleanupNeedsLeftovers(t, id)
			before := w.tofuCalls()
			code, stdout, stderr := w.cleanup(t, id)
			if code != 0 {
				t.Errorf("cleanup: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			if n := w.tofuCalls() - before; c.noTofu && n != 0 {
				t.Errorf("the cleanup of a run that never applied made %d tofu calls", n)
			}
			if d := w.deletes(); len(d) != 0 || !w.has(rcAlertA) {
				t.Errorf("cleanup DELETEs %v, A listed %v; want nothing deleted", d, w.has(rcAlertA))
			}
			w.retired(t, id)
			w.startProceeds(t, "the cleanup of a killed start")
		})
	}
}

// killWindow kills a probe start at c's point and shapes the disk to c's window.
func (w *rcWorld) killWindow(t *testing.T, c rcWindow) string {
	t.Helper()
	var id string
	if c.atApply {
		w.setMode(t, "block")
		id = w.killAtApply(t, w.start(t, nil, "probe", "--reviewed-sha", fakeHead, rcRoot))
		w.setMode(t, "create")
	} else {
		w.setBlock(w.beforeAnyApply)
		id = w.killAtBlock(t, w.start(t, nil, "probe", "--reviewed-sha", fakeHead, rcRoot), "its baseline listing")
	}
	w.windowDisk(t, id, c.atApply)
	pass := filepath.Join(w.probeDir(id), "passphrase.env")
	switch c.drop {
	case "passphrase":
		mustRemove(t, pass)
	case "recovery":
		mustRemove(t, w.recoveryCopy(id))
	}
	if c.truncate {
		writePrivate(t, pass, "TF_VAR_state_passphrase=x\n")
	}
	return id
}

// windowDisk fails unless the killed start left no state and no inventory, and both baseline
// copies exactly when it was killed at its first apply.
func (w *rcWorld) windowDisk(t *testing.T, id string, atApply bool) {
	t.Helper()
	for _, f := range []string{filepath.Join(w.probeDir(id), "budget-alert.tfstate"), filepath.Join(w.checkout, ".local", "live", id, "inventory.jsonl")} {
		if fi, err := os.Stat(f); err == nil && fi.Size() > 0 {
			t.Fatalf("premise: the killed start left %s", f)
		}
	}
	for _, f := range []string{w.recordCopy(id), w.recoveryCopy(id)} {
		if _, err := os.Stat(f); (err == nil) != atApply {
			t.Fatalf("premise: killed at its first apply %v, %s exists %v", atApply, f, err == nil)
		}
	}
}

// cleanupNeedsLeftovers fails unless a --cleanup whose leftover check fails (an lzprobe- network)
// keeps the run's directory.
func (w *rcWorld) cleanupNeedsLeftovers(t *testing.T, id string) {
	t.Helper()
	w.listings[rcNetwork] = `[{"id":"pn-old","name":"lzprobe-old","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}]`
	defer delete(w.listings, rcNetwork)
	code, stdout, _ := w.cleanup(t, id)
	if _, err := os.Stat(w.probeDir(id)); code == 0 || err != nil {
		t.Errorf("a cleanup with an lzprobe- leftover: exit %d, run directory kept %v; want a failure that keeps it\nstdout:\n%s", code, err == nil, stdout)
	}
}

func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("premise: the killed start did not write %s: %v", path, err)
	}
}

// refusedBeside fails unless a probe start beside the unreconciled run id is refused naming it and
// its cleanup, before any child.
func (w *rcWorld) refusedBeside(t *testing.T, id string) {
	t.Helper()
	before := w.tofuCalls()
	code, stdout, stderr := w.probeStart(t)
	out := stdout + stderr
	if code == 0 || !strings.Contains(out, live.CondUnreconciledRun) || !strings.Contains(out, id) || !strings.Contains(out, "--cleanup "+id) {
		t.Errorf("a start beside unreconciled run %s: exit %d, want refused %s naming it and --cleanup %s\nstdout:\n%s\nstderr:\n%s",
			id, code, live.CondUnreconciledRun, id, stdout, stderr)
	}
	if n := w.tofuCalls() - before; n != 0 {
		t.Errorf("the refused start made %d tofu calls", n)
	}
}

// TestProbeRecoveryLostBaseline (FR-007 owner recovery): given a lost create (no state, no
// inventory) whose two baseline copies no longer read / when --cleanup runs / then it is refused
// as untrusted and deletes nothing; --accept-lost-baseline without a terminal, or with another
// line typed, is refused; with a pseudo-terminal and the run id typed back it deletes nothing,
// runs the leftover check and retires the files only when it passes → exposes a baseline rebuilt
// from a later listing, or an owner-only retirement an automated session can run. The task's
// given is both copies deleted; under FR-006's predicate (data-model *Start-up phases and run
// classes*, T011's ClassifyRun rows) no state, no inventory and no baseline is pre-create, so the
// untrusted run here is the one the predicate names: a baseline present but unreadable (evidence
// T013, decision request).
func TestProbeRecoveryLostBaseline(t *testing.T) {
	w := newRCWorld(t, "lost")
	id := w.lostCreate(t, true)
	for _, f := range []string{w.recordCopy(id), w.recoveryCopy(id)} {
		writePrivate(t, f, `{"run_id":`)
	}
	code, stdout, stderr := w.cleanup(t, id)
	if code == 0 || !strings.Contains(stdout+stderr, "BASELINE_INVALID") {
		t.Errorf("cleanup with unreadable baselines: exit %d, want refused BASELINE_INVALID\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	w.keptUnswept(t, id, "the untrusted cleanup")
	code, stdout, stderr = w.cleanup(t, id, "--accept-lost-baseline")
	w.acceptRefused(t, code, stdout, stderr, "without a terminal")
	w.keptUnswept(t, id, "--accept-lost-baseline without a terminal")
	code, stdout, stderr = w.acceptOnTTY(t, id, "20261019T120000Z-ffff")
	w.acceptRefused(t, code, stdout, stderr, "with another run id typed")
	w.keptUnswept(t, id, "--accept-lost-baseline with another run id typed")

	// The owner removed B by hand; the leftover check still fails (an lzprobe- network).
	w.removeByHand(rcAlertB)
	w.listings[rcNetwork] = `[{"id":"pn-old","name":"lzprobe-old","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}]`
	if code, stdout, _ := w.acceptOnTTY(t, id, id); code == 0 || code == 2 {
		t.Errorf("--accept-lost-baseline with a failing leftover check exited 0\nstdout:\n%s", stdout)
	}
	w.keptUnswept(t, id, "--accept-lost-baseline with a failing leftover check")

	delete(w.listings, rcNetwork)
	if code, stdout, stderr := w.acceptOnTTY(t, id, id); code != 0 {
		t.Errorf("--accept-lost-baseline with the run id typed back: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if d := w.deletes(); len(d) != 0 {
		t.Errorf("--accept-lost-baseline sent DELETEs %v, want none", d)
	}
	w.retired(t, id)
}

// TestProbeRecoveryAcceptLostBaselineState (spec 007 T015, FR-007 owner recovery; the maintainer's
// run directories from before baselines existed): given a run of a root whose sweep does not judge
// by a baseline (tests/live/probes/net) that kept its state, passphrase, probe.env and inventory
// but has no baseline in either place / when it is cleaned up / then a plain --cleanup is refused
// BASELINE_MISSING with no tofu call and nothing removed, and a probe start beside it is refused;
// --cleanup --accept-lost-baseline with the run id typed back destroys from the retained state,
// runs the leftover check, retires the run, and the next start proceeds → exposes an owner
// recovery that only finishes a stateless budget-alert run, or one that skips the destroy.
func TestProbeRecoveryAcceptLostBaselineState(t *testing.T) {
	w := newRCWorld(t, "create")
	mustRemove(t, filepath.Join(w.bin, rcModeFile)) // the net root runs the plain fake tofu
	id := w.failedProbe(t)
	mustRemove(t, w.recordCopy(id))
	mustRemove(t, w.recoveryCopy(id))
	state := filepath.Join(w.probeDir(id), "net.tfstate")
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("premise: the failed net run kept no state: %v", err)
	}
	before := w.tofuCalls()
	code, stdout, stderr := w.cleanup(t, id)
	if code == 0 || !strings.Contains(stdout+stderr, "BASELINE_MISSING") || w.tofuCalls() != before {
		t.Errorf("plain cleanup of a run with state and no baseline: exit %d, %d tofu calls; want refused BASELINE_MISSING with none\nstdout:\n%s\nstderr:\n%s",
			code, w.tofuCalls()-before, stdout, stderr)
	}
	if _, err := os.Stat(state); err != nil {
		t.Errorf("the refused cleanup removed the state: %v", err)
	}
	w.refusedBeside(t, id)
	n := len(w.childCalls(t, "tofu.log"))
	code, stdout, stderr = w.acceptOnTTY(t, id, id)
	if code != 0 {
		t.Errorf("--accept-lost-baseline with the run id typed back: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if calls := subcommands(w.childCalls(t, "tofu.log")[n:]); !slices.Contains(calls, "destroy") || slices.Contains(calls, "apply") {
		t.Errorf("the accepted cleanup's tofu calls %v, want a destroy from the retained state and no forward apply", calls)
	}
	if !strings.Contains(stdout, "LZ-LIVE summary "+id+" pass") {
		t.Errorf("stdout lacks the accepted cleanup's pass summary (leftover check):\n%s", stdout)
	}
	w.retired(t, id)
	w.startProceeds(t, "an accepted cleanup")
}

// TestProbeRecoveryAcceptBudgetAlertState (spec 007 T015, FR-007 "deletes nothing"; review round
// 1): given a budget-alert run whose destroy failed, so it kept its state, and whose two baseline
// copies are gone / when the owner removed B by hand and runs --cleanup
// --accept-lost-baseline with the run id typed back / then no tofu runs (no destroy), no DELETE is
// sent, the leftover check passes and the run is retired, state included → exposes an owner
// recovery that deletes through the state of a root only a baseline can judge.
func TestProbeRecoveryAcceptBudgetAlertState(t *testing.T) {
	w := newRCWorld(t, "create")
	marker := filepath.Join(w.bin, "destroy-fails")
	writePrivate(t, marker, "")
	code, stdout, _ := w.probeStart(t)
	if code == 0 {
		t.Fatalf("premise: a probe whose destroy failed exited 0\nstdout:\n%s", stdout)
	}
	id := runIDOf(t, stdout)
	mustRemove(t, marker)
	mustRemove(t, w.recordCopy(id))
	mustRemove(t, w.recoveryCopy(id))
	state := filepath.Join(w.probeDir(id), "budget-alert.tfstate")
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("premise: the failed run kept no state: %v", err)
	}
	// The owner removed B by hand (the fake's failed destroy may already have).
	w.removeByHand(rcAlertB)
	before := w.tofuCalls()
	code, stdout, stderr := w.acceptOnTTY(t, id, id)
	if code != 0 {
		t.Errorf("--accept-lost-baseline on a budget-alert run with state: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if n := w.tofuCalls() - before; n != 0 {
		t.Errorf("the accepted budget-alert cleanup made %d tofu calls, want none (it deletes nothing)", n)
	}
	if d := w.deletes(); len(d) != 0 {
		t.Errorf("the accepted budget-alert cleanup sent DELETEs %v, want none", d)
	}
	w.retired(t, id)
	if _, err := os.Lstat(state); err == nil {
		t.Errorf("the retired run kept its state %s", state)
	}
}

// TestProbeRecoveryOneCopy (spec 007 T015, FR-007; T012's deferral, the run paths through the
// entry): given a lost create with only one baseline copy left / when a fresh --cleanup runs /
// then the budget-alert check finds that copy where lz-live keeps it (the record copy under the
// checkout's .local/live/<run-id>/, the recovery copy under the account's state/baselines/),
// deletes only B and retires the run → exposes a check handed another record or state directory,
// which reads the run as pre-create and leaves B.
func TestProbeRecoveryOneCopy(t *testing.T) {
	for _, keep := range []string{"record", "recovery"} {
		t.Run(keep, func(t *testing.T) {
			w := newRCWorld(t, "lost")
			id := w.lostCreate(t, true)
			drop := map[string]string{"record": w.recoveryCopy(id), "recovery": w.recordCopy(id)}[keep]
			mustRemove(t, drop)
			code, stdout, stderr := w.cleanup(t, id)
			if code != 0 {
				t.Errorf("cleanup with only the %s copy: exit %d\nstdout:\n%s\nstderr:\n%s", keep, code, stdout, stderr)
			}
			w.sweptOnlyB(t)
			w.retired(t, id)
		})
	}
}

// TestProbeAcceptNotUntrusted (spec 007 T015, FR-007): given a lost create whose baseline is
// valid (baseline-backed) / when --accept-lost-baseline runs with the run id typed back / then it
// is refused (accept-lost-baseline) with nothing deleted and the files kept, and the plain
// --cleanup still finishes it → exposes an owner recovery that drops a trustworthy baseline sweep.
func TestProbeAcceptNotUntrusted(t *testing.T) {
	w := newRCWorld(t, "lost")
	id := w.lostCreate(t, true)
	code, stdout, stderr := w.acceptOnTTY(t, id, id)
	if code != live.RefusalExit || !strings.Contains(stdout+stderr, live.CondAcceptLostBaseline) {
		t.Errorf("--accept-lost-baseline on a baseline-backed run: exit %d, want %d refused %s\nstdout:\n%s\nstderr:\n%s",
			code, live.RefusalExit, live.CondAcceptLostBaseline, stdout, stderr)
	}
	w.keptUnswept(t, id, "--accept-lost-baseline on a baseline-backed run")
	if code, stdout, stderr := w.cleanup(t, id); code != 0 {
		t.Errorf("the plain cleanup after it: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	w.sweptOnlyB(t)
	w.retired(t, id)
}

// acceptOnTTY runs --cleanup <id> --accept-lost-baseline with a pseudo-terminal as its stdin and
// controlling terminal, typing typed once the prompt is shown.
func (w *rcWorld) acceptOnTTY(t *testing.T, id, typed string) (int, string, string) {
	t.Helper()
	return w.acceptTyped(t, id, "", typed)
}

// acceptTyped is acceptOnTTY with ahead already in the terminal's input before lz-live starts
// (earlier keystrokes of a shell loop). A run that ends before its prompt gets nothing typed.
func (w *rcWorld) acceptTyped(t *testing.T, id, ahead, typed string) (int, string, string) {
	t.Helper()
	master, slave := openPTY(t)
	if _, err := master.Write([]byte(ahead)); err != nil {
		t.Fatal(err)
	}
	if ahead != "" {
		waitQueued(t, slave)
	}
	p := w.start(t, slave, "probe", "--reviewed-sha", fakeHead, "--cleanup", id, "--accept-lost-baseline")
	select {
	case <-p.prompted:
		if _, err := master.Write([]byte(typed + "\n")); err != nil {
			t.Fatal(err)
		}
	case <-p.done:
	case <-time.After(rcWait):
	}
	return p.wait(t)
}

// TestProbeAcceptTypeAhead (spec 007 T026, FR-007 owner recovery; the owner's live session of
// 2026-10-19, eight runs in a shell loop): given a lost create with unreadable baseline copies, B
// removed by hand, and earlier keystrokes and an arrow key already in the terminal's input / when
// --cleanup --accept-lost-baseline prompts / then those lines are discarded: a wrong or empty line
// typed after the prompt is refused saying what was read (its length, or that it was empty, never
// the line) and the run id expected, and the run id typed after the prompt is accepted → exposes a
// prompt that reads a stale line, so every later answer is read one run late.
func TestProbeAcceptTypeAhead(t *testing.T) {
	const ahead = "earlier keystrokes\n\x1b[D\n"
	w := newRCWorld(t, "lost")
	id := w.lostCreate(t, true)
	for _, f := range []string{w.recordCopy(id), w.recoveryCopy(id)} {
		writePrivate(t, f, `{"run_id":`)
	}
	w.removeByHand(rcAlertB)
	for _, c := range []struct{ typed, want string }{
		{"notTheRunIdSecret", "read 17 characters"},
		{"", "read an empty line"},
	} {
		code, stdout, stderr := w.acceptTyped(t, id, ahead, c.typed)
		w.acceptRefused(t, code, stdout, stderr, "with "+c.want)
		if out := stdout + stderr; !strings.Contains(out, c.want) || !strings.Contains(out, "expected the run id "+id) {
			t.Errorf("refusal %s: want %q and the expected run id in it\nstdout:\n%s\nstderr:\n%s", c.want, c.want, stdout, stderr)
		}
		if c.typed != "" && strings.Contains(stdout+stderr, c.typed) {
			t.Errorf("the refusal echoed the line typed back %q", c.typed)
		}
		w.keptUnswept(t, id, "--accept-lost-baseline "+c.want)
	}
	if code, stdout, stderr := w.acceptTyped(t, id, ahead, id); code != 0 {
		t.Errorf("--accept-lost-baseline with stale input and the run id typed after the prompt: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if d := w.deletes(); len(d) != 0 {
		t.Errorf("--accept-lost-baseline sent DELETEs %v, want none", d)
	}
	w.retired(t, id)
}

func (w *rcWorld) acceptRefused(t *testing.T, code int, stdout, stderr, when string) {
	t.Helper()
	if code == 0 || code == 2 || strings.HasPrefix(stderr, "usage:") {
		t.Errorf("--accept-lost-baseline %s: exit %d, want a refusal (not a usage error)\nstdout:\n%s\nstderr:\n%s", when, code, stdout, stderr)
	}
}

// keptUnswept fails if any DELETE was sent or the run's files are gone.
func (w *rcWorld) keptUnswept(t *testing.T, id, when string) {
	t.Helper()
	if d := w.deletes(); len(d) != 0 {
		t.Errorf("%s sent DELETEs %v, want none", when, d)
	}
	for _, f := range []string{filepath.Join(w.probeDir(id), "probe.env"), filepath.Join(w.probeDir(id), "passphrase.env"), w.recordCopy(id), w.recoveryCopy(id)} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s removed %s: %v", when, f, err)
		}
	}
}
