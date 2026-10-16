package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The runner's sweep (spec 003 T037; T038 implements it; data-model.md *Probe checks*). A lost
// create answer can leave neither a root output (then the run skips Undo, runner.go undo) nor a
// state entry (then destroy does not know the resource), and a run that wrote no state removes the
// passphrase and probe.env (runner.go Probe.run), so a `--cleanup` cannot find the run again. A
// probe check's Sweep therefore runs with only the run id and the project probe.env records, after
// destroy and before the leftover check, on every way out and on `--cleanup`, with the run's
// redacted terminal; probe.env, the passphrase and the probe record stay until Sweep and the
// leftover check pass. A check without a Sweep (spec 005's roots) runs as before.
//
// Every case logs Sweep's calls to tofu.log beside the fake tofu's, and the leftover check's
// listings too (swLister), so their order is one sequence.

const swRunID = "20261006T120000Z-a1b2"

// swSweep is what a test Sweep was handed.
type swSweep struct {
	runID, project string
	ctxErr         error
}

// swHooks is a probe check with a Sweep, logged as `sweep` (args: run id, project) in tofu.log.
type swHooks struct {
	checkHooks
	sweepErr error
	print    func() string // what Sweep writes to its terminal (nil: a marker line)
	smu      sync.Mutex
	sweeps   []swSweep
}

func (h *swHooks) check(w *runWorld) *ProbeCheck {
	c := h.checkHooks.check(w)
	c.Sweep = func(ctx context.Context, runID, projectID string, term io.Writer) error {
		logCall(w.bin, "tofu.log", tofuCall{Cmd: "sweep", Stack: "p", Args: []string{runID, projectID}})
		h.smu.Lock()
		h.sweeps = append(h.sweeps, swSweep{runID: runID, project: projectID, ctxErr: ctx.Err()})
		h.smu.Unlock()
		line := "LZ-LIVE sweep-marker listed and deleted nothing\n"
		if h.print != nil {
			line = h.print()
		}
		fmt.Fprint(term, line)
		return h.sweepErr
	}
	return c
}

func (h *swHooks) calls() []swSweep {
	h.smu.Lock()
	defer h.smu.Unlock()
	return slices.Clone(h.sweeps)
}

// swLister is the leftover check's lister, each listing logged as `list` in tofu.log.
type swLister struct {
	inner Lister
	bin   string
}

func (l swLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	logCall(l.bin, "tofu.log", tofuCall{Cmd: "list", Args: []string{path}})
	return l.inner.Get(ctx, path, cursor)
}

// swOrder is the order of apply, output, observe, undo, destroy-plan, destroy, sweep and the
// leftover check's listings (consecutive listings as one `list`).
func swOrder(calls []tofuCall) []string {
	var out []string
	for _, c := range calls {
		var k string
		switch {
		case c.Cmd == "plan" && slices.Contains(c.Args, "-destroy"):
			k = "plan-destroy"
		case slices.Contains([]string{"apply", "output", "observe", "undo", "destroy", "sweep", "list"}, c.Cmd):
			k = c.Cmd
		default:
			continue
		}
		if k == "list" && len(out) > 0 && out[len(out)-1] == "list" {
			continue
		}
		out = append(out, k)
	}
	return out
}

// swCount counts cmd in an order.
func swCount(order []string, cmd string) int {
	n := 0
	for _, c := range order {
		if c == cmd {
			n++
		}
	}
	return n
}

// swAfterDestroyBeforeList: one sweep, after the last destroy (when the run destroyed) and before
// the leftover check's first listing.
func swAfterDestroyBeforeList(t *testing.T, order []string, destroyed bool) {
	t.Helper()
	if n := swCount(order, "sweep"); n != 1 {
		t.Errorf("sweep ran %d times, want once: %v", n, order)
		return
	}
	at := slices.Index(order, "sweep")
	if list := slices.Index(order, "list"); list < at {
		t.Errorf("the leftover check listed before the sweep (or never): %v", order)
	}
	if destroyed {
		last := -1
		for i, c := range order {
			if c == "destroy" {
				last = i
			}
		}
		if last < 0 || last > at {
			t.Errorf("sweep before the destroy (or no destroy): %v", order)
		}
	}
}

// swNoCheckOutput writes outputs that hold no output for the check: the root published nothing
// for it (its apply failed before the outputs).
func swNoCheckOutput(t *testing.T, w *runWorld) string {
	t.Helper()
	p := filepath.Join(w.bin, "outputs-none.json")
	if err := os.WriteFile(p, []byte(`{"bucket":{"sensitive":false,"type":"string","value":"`+checkBucket+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// swLostAnswer is an apply stream that starts creating one resource and never reports it created
// (the create answer lost).
func swLostAnswer(t *testing.T, w *runWorld) string {
	t.Helper()
	full := w.stream(t, "p", resourceLine{"ovh_cloud_project_user", "lost", "u-lost-1"})
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], `"apply_start"`) {
		t.Fatalf("stream fixture: want version, apply_start, apply_complete; got %d lines", len(lines))
	}
	p := filepath.Join(w.bin, "stream-p-lost.jsonl")
	if err := os.WriteFile(p, []byte(lines[0]+"\n"+lines[1]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// swProbe is a probe of root p with the check h (a Sweep when h is an *swHooks), its leftover
// check's listings logged.
func (w *runWorld) swProbe(t *testing.T, root string, st tofuStack, c *ProbeCheck) Probe {
	t.Helper()
	p := w.probe(t, root, st)
	p.Check = c
	p.Run.Leftovers.Lister = swLister{inner: p.Run.Leftovers.Lister, bin: w.bin}
	return p
}

// swFiles reports which of the probe's run files remain: probe.env, passphrase.env, the state.
func swFiles(t *testing.T, root string) (record, pass, state bool) {
	t.Helper()
	dir, st, ps := probeFiles(t, root)
	return exists(filepath.Join(dir, "probe.env")), exists(ps), exists(st)
}

// TestSweepLostCreateAnswer: an apply fails after starting one resource whose answer was lost (the
// stream never reports it created, so the run's inventory has no id for it; the fake's state file
// is opaque to the runner) and the root publishes no output for the check; or the run's deadline
// is reached while that create is pending; or, after the failed apply, the destroy fails too. The
// run ends: Sweep runs once with the run id and the project probe.env records, after the destroy
// (a failed one too: "on every way out", data-model.md *Probe checks*) and before the leftover
// check, with a live context (the destroy's, not the expired run's); Undo is not run (nothing
// published). Exposes an orphan nobody deletes.
func TestSweepLostCreateAnswer(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"apply-fails", "deadline", "destroy-fails"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := tempPrivate(t)
			w := newRunWorld(t)
			h := &swHooks{}
			st := tofuStack{ApplyStream: swLostAnswer(t, w), ApplyExit: 1, Outputs: swNoCheckOutput(t, w)}
			switch name {
			case "deadline":
				st.ApplyExit, st.HangAfter = 0, 2
			case "destroy-fails":
				st.DestroyExit = 1
			}
			p := w.swProbe(t, root, st, h.check(w))
			if name == "deadline" {
				p.Run.deadlineAt = w.hanging(t, "p")
			}
			if err := p.Start(context.Background()); ExitCode(err) == 0 {
				t.Fatal("a probe whose apply failed exited 0")
			}
			entries, err := ReadInventory(w.runDir)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.ID == "u-lost-1" {
					t.Fatalf("fixture: the lost create reached the inventory: %+v", e)
				}
			}
			order := swOrder(w.calls(t, "tofu.log"))
			swAfterDestroyBeforeList(t, order, true)
			if name == "destroy-fails" {
				if record, pass, _ := swFiles(t, root); !record || !pass {
					t.Errorf("after a failed destroy: probe.env kept %v, passphrase kept %v; want both", record, pass)
				}
			}
			if n := swCount(order, "undo") + swCount(order, "observe"); n != 0 {
				t.Errorf("observe or undo ran for a root that published nothing: %v", order)
			}
			got := h.calls()
			if len(got) != 1 || got[0].runID != swRunID || got[0].project != probeProject {
				t.Fatalf("Sweep got %+v, want once with run id %s and project %s", got, swRunID, probeProject)
			}
			if got[0].ctxErr != nil {
				t.Errorf("Sweep ran with a finished context: %v", got[0].ctxErr)
			}
			if !strings.Contains(w.term.String(), "LZ-LIVE sweep-marker") {
				t.Errorf("Sweep's terminal output never reached the run's terminal:\n%s", w.term)
			}
		})
	}
}

// ---------------------------------------------------------------- fresh process

// swRunArg starts the test binary as a fresh lz-live process running a probe with a Sweep
// (swRun); the dispatch is in init, before TestMain, so no other fixture changes.
const swRunArg = "lz-sweep-run"

// swRunConfig is what swRun needs: the fresh-process run of runner_test.go, a mode (start or
// cleanup) and whether its Sweep fails.
type swRunConfig struct {
	fakeRunConfig
	SweepErr bool
}

func init() {
	if len(os.Args) == 3 && os.Args[1] == swRunArg {
		os.Exit(swRun(os.Args[2]))
	}
}

// swRun is a fresh lz-live process: the probe is rebuilt from its config alone, its check has only
// a Sweep worth calling (Observe and Undo log and fail), and its listings are logged.
func swRun(cfgPath string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "sweep run: fixtures:", r)
			code = 2
		}
	}()
	var c swRunConfig
	raw, err := os.ReadFile(cfgPath)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		fmt.Fprintln(os.Stderr, "sweep run: bad config")
		return 2
	}
	r := fakeRunner(c.fakeRunConfig)
	r.Leftovers.Lister = swLister{inner: r.Leftovers.Lister, bin: c.Bin}
	check := &ProbeCheck{
		Output: "deny_check",
		Observe: func(context.Context, map[string]string, io.Writer) ([]byte, error) {
			logCall(c.Bin, "tofu.log", tofuCall{Cmd: "observe", Stack: "p"})
			return nil, errors.New("observe in the sweep run")
		},
		Undo: func(context.Context, map[string]string, io.Writer) error {
			logCall(c.Bin, "tofu.log", tofuCall{Cmd: "undo", Stack: "p"})
			return errors.New("undo in the sweep run")
		},
		Sweep: func(ctx context.Context, runID, projectID string, term io.Writer) error {
			logCall(c.Bin, "tofu.log", tofuCall{Cmd: "sweep", Stack: "p", Args: []string{runID, projectID}})
			fmt.Fprintln(term, "LZ-LIVE sweep-marker fresh process")
			if c.SweepErr {
				return errors.New("sweep: a listed resource is still there")
			}
			return nil
		},
	}
	p := Probe{Run: r, ConfigRoot: c.ConfigRoot, Account: probeAccount, Check: check}
	switch c.Mode {
	case "start":
		err = p.Start(context.Background())
	case "cleanup":
		err = p.Cleanup(context.Background())
	default:
		err = fmt.Errorf("unknown mode %q", c.Mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sweep run:", err)
	}
	return ExitCode(err)
}

func (w *runWorld) swSubprocess(t *testing.T, c swRunConfig) *exec.Cmd {
	t.Helper()
	c.Bin, c.StacksDir, c.RunDir = w.bin, w.stacks, w.runDir
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "sweep-run.json")
	if err := os.WriteFile(cfg, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exec.Command(self, swRunArg, cfg)
}

// TestSweepAfterCrash: the same apply (one create started, its answer lost) crashes the process:
// lz-live and the fake tofu child are killed while the apply runs. `--cleanup <run-id>` in a new
// process, whose caller now passes another project, runs Sweep from probe.env alone (the recorded
// run id and project), once, after the destroy and before the leftover check, never Undo; the
// passing cleanup removes the run's files. Exposes cleanup that needs the dead process's memory or
// the root's outputs. Probe.Cleanup already puts probe.env's project into Vars (runner.go), so a
// Sweep given Vars' project passes too: the p9 check pins that the cleanup's project is the record's.
func TestSweepAfterCrash(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	stream := swLostAnswer(t, w)
	outputs := swNoCheckOutput(t, w)
	w.stack(t, "p", true, tofuStack{ApplyStream: stream, HangAfter: 2, Outputs: outputs})
	stacks := []fakeRunStack{{"p", true}}
	start := w.swSubprocess(t, swRunConfig{fakeRunConfig: fakeRunConfig{Mode: "start", ConfigRoot: root, Stacks: stacks,
		Vars: map[string]string{"TF_VAR_project_id": probeProject}}})
	var out syncBuffer
	start.Stdout, start.Stderr = &out, &out
	if err := start.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.hanging(t, "p"):
	case <-time.After(testWaitCap + 5*time.Second):
	}
	if !exists(filepath.Join(w.bin, "hanging-p")) {
		_ = start.Process.Kill()
		_ = start.Wait()
		t.Fatalf("the apply never started creating:\n%s", out.String())
	}
	// The crash: lz-live killed, then the apply child's whole process group.
	_ = start.Process.Kill()
	_ = start.Wait()
	for _, c := range w.calls(t, "tofu.log") {
		if c.Cmd == "apply" && c.Pgrp > 1 && c.Pgrp != syscall.Getpgrp() {
			_ = syscall.Kill(-c.Pgrp, syscall.SIGKILL)
		}
	}
	before := len(w.calls(t, "tofu.log"))
	if n := swCount(swOrder(w.calls(t, "tofu.log")), "sweep"); n != 0 {
		t.Fatalf("the killed process swept %d times", n)
	}
	if record, pass, _ := swFiles(t, root); !record || !pass {
		t.Fatalf("after the crash: probe.env kept %v, passphrase kept %v; want both", record, pass)
	}

	// A fresh process; the stack's apply no longer matters (a cleanup applies nothing).
	w.scenario.Stacks["p"] = tofuStack{ApplyStream: stream, Outputs: outputs}
	w.save(t)
	cleanup := w.swSubprocess(t, swRunConfig{fakeRunConfig: fakeRunConfig{Mode: "cleanup", ConfigRoot: root, Stacks: stacks,
		Vars: map[string]string{"TF_VAR_project_id": "p9"}}})
	cout, err := cleanup.CombinedOutput()
	calls := w.calls(t, "tofu.log")[before:]
	order := swOrder(calls)
	swAfterDestroyBeforeList(t, order, true)
	var sweep tofuCall
	for _, c := range calls {
		if c.Cmd == "sweep" {
			sweep = c
		}
	}
	if !slices.Equal(sweep.Args, []string{swRunID, probeProject}) {
		t.Errorf("the cleanup's Sweep got %v, want [%s %s] from probe.env (the caller passes p9)", sweep.Args, swRunID, probeProject)
	}
	if n := swCount(order, "undo") + swCount(order, "observe"); n != 0 {
		t.Errorf("the cleanup ran observe or undo for a root that published nothing: %v", order)
	}
	if err != nil {
		t.Errorf("cleanup: %v\n%s", err, cout)
	}
	if record, pass, state := swFiles(t, root); record || pass || state {
		t.Errorf("after a passing cleanup: probe.env %v, passphrase %v, state %v kept; want none", record, pass, state)
	}
}

// TestSweepKeepsRunFiles: no state file was written (the run failed before any apply wrote one:
// its plan failed) and then Sweep fails, or Sweep passes and the leftover check fails: probe.env,
// the passphrase and the probe directory stay, Sweep ran once with the recorded run id and project,
// and the run ends `fail` naming `--cleanup <run-id>`. With state, an otherwise passing run whose
// Sweep alone fails ends the same way (sweep-fails-alone: the failure is Sweep's, not the plan's).
// Exposes run metadata deleted before reconciliation passed (today a run without state removes
// them whatever the outcome), and a swallowed Sweep error.
func TestSweepKeepsRunFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		sweepErr error
		seed     string
		full     bool // an otherwise passing run (apply, outputs, destroy): Sweep alone fails
	}{
		{name: "sweep-fails", sweepErr: errors.New("sweep: lzprobe-user still listed")},
		{name: "leftover-check-fails", seed: "network"},
		{name: "sweep-fails-alone", sweepErr: errors.New("sweep: lzprobe-user still listed"), full: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := tempPrivate(t)
			w := newRunWorld(t)
			h := &swHooks{sweepErr: tc.sweepErr}
			st := tofuStack{PlanExit: 1, Outputs: swNoCheckOutput(t, w)}
			if tc.full {
				st = tofuStack{Outputs: checkOutputs(t, w, "deny_check", checkEnvWant())}
			}
			p := w.swProbe(t, root, st, h.check(w))
			if tc.seed != "" {
				c, _ := cleanCheck(t, tc.seed)
				p.Run.Leftovers = c
				p.Run.Leftovers.Lister = swLister{inner: c.Lister, bin: w.bin}
			}
			err := p.Start(context.Background())
			if ExitCode(err) == 0 {
				t.Fatal("the run exited 0")
			}
			if _, _, state := swFiles(t, root); state != tc.full {
				t.Fatalf("fixture: state file %v, want %v", state, tc.full)
			}
			got := h.calls()
			if len(got) != 1 || got[0].runID != swRunID || got[0].project != probeProject {
				t.Errorf("Sweep got %+v, want once with run id %s and project %s", got, swRunID, probeProject)
			}
			swAfterDestroyBeforeList(t, swOrder(w.calls(t, "tofu.log")), tc.full)
			if record, pass, _ := swFiles(t, root); !record || !pass {
				t.Errorf("probe.env kept %v, passphrase kept %v; want both until Sweep and the leftover check pass", record, pass)
			}
			if !strings.Contains(fmt.Sprint(err), "--cleanup "+swRunID) {
				t.Errorf("the run's error does not name --cleanup %s: %v", swRunID, err)
			}
		})
	}
}

// TestSweepRemovesRunFiles: no state file was written, and Sweep and the leftover check pass: the
// run's files are removed as today, after Sweep ran once. Exposes metadata kept forever.
func TestSweepRemovesRunFiles(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	h := &swHooks{}
	p := w.swProbe(t, root, tofuStack{PlanExit: 1, Outputs: swNoCheckOutput(t, w)}, h.check(w))
	if err := p.Start(context.Background()); ExitCode(err) == 0 {
		t.Fatal("a probe whose plan failed exited 0")
	}
	if got := h.calls(); len(got) != 1 || got[0].project != probeProject {
		t.Errorf("Sweep got %+v, want once with project %s", got, probeProject)
	}
	swAfterDestroyBeforeList(t, swOrder(w.calls(t, "tofu.log")), false)
	dir, _, _ := probeFiles(t, root)
	if exists(dir) {
		left, _ := os.ReadDir(dir)
		t.Errorf("Sweep and the leftover check passed with no state, yet the probe kept %v", left)
	}
}

// TestSweepWithUndo: a full apply with outputs: Observe, then Undo before the destroy and Sweep
// after it, each once, then the leftover check; the run passes and its files go. Exposes a sweep
// that replaces Undo, or runs twice.
func TestSweepWithUndo(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	h := &swHooks{}
	p := w.swProbe(t, root, tofuStack{Outputs: checkOutputs(t, w, "deny_check", checkEnvWant())}, h.check(w))
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v\n%s", err, w.term)
	}
	want := []string{"apply", "output", "observe", "output", "undo", "plan-destroy", "destroy", "sweep", "list"}
	if got := swOrder(w.calls(t, "tofu.log")); !slices.Equal(got, want) {
		t.Errorf("calls %v, want %v", got, want)
	}
	if h.undoCalls != 1 || len(h.calls()) != 1 {
		t.Errorf("Undo ran %d times and Sweep %d times; want each once", h.undoCalls, len(h.calls()))
	}
	if record, pass, state := swFiles(t, root); record || pass || state {
		t.Errorf("after a passing run: probe.env %v, passphrase %v, state %v kept; want none", record, pass, state)
	}
}

// TestSweepNone: a check without a Sweep (state-backend, storage-iam) runs as today, on a passing
// run and on a run that wrote no state. A control: it passes before T038 by design (spec 005's
// behaviour) and must stay green after it. Exposes a regression of spec 005's roots.
func TestSweepNone(t *testing.T) {
	t.Parallel()
	t.Run("full-run", func(t *testing.T) {
		t.Parallel()
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{}
		p := w.swProbe(t, root, tofuStack{Outputs: checkOutputs(t, w, "deny_check", checkEnvWant())}, h.check(w))
		if err := p.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v\n%s", err, w.term)
		}
		want := []string{"apply", "output", "observe", "output", "undo", "plan-destroy", "destroy", "list"}
		if got := swOrder(w.calls(t, "tofu.log")); !slices.Equal(got, want) {
			t.Errorf("calls %v, want %v", got, want)
		}
		if record, pass, state := swFiles(t, root); record || pass || state {
			t.Errorf("after a passing run: probe.env %v, passphrase %v, state %v kept; want none", record, pass, state)
		}
	})
	t.Run("no-state", func(t *testing.T) {
		t.Parallel()
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{}
		p := w.swProbe(t, root, tofuStack{PlanExit: 1, Outputs: swNoCheckOutput(t, w)}, h.check(w))
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Fatal("a probe whose plan failed exited 0")
		}
		dir, _, _ := probeFiles(t, root)
		if exists(dir) {
			left, _ := os.ReadDir(dir)
			t.Errorf("a run without state and without a Sweep kept %v", left)
		}
	})
}

// swLeaks returns the secrets s shows.
func swLeaks(s string, secrets []string) []string {
	var out []string
	for _, x := range secrets {
		if x != "" && strings.Contains(s, x) {
			out = append(out, x)
		}
	}
	return out
}

// TestSweepRedacted: what Sweep writes to its terminal passes the run's redactor: the run's
// credential, the probe passphrase and a value the root published for its check are redacted on
// the terminal and in every file of the run record and the probe directory, while the line itself
// arrives. The assertion is first shown to catch a leaking line. Exposes a sweep printing
// unredacted secrets the run knows (a raw terminal, or a redactor of its own).
func TestSweepRedacted(t *testing.T) {
	t.Parallel()
	if got := swLeaks("LZ-LIVE sweep-marker secret="+seedSecret, []string{seedSecret}); len(got) != 1 {
		t.Fatalf("the leak assertion misses a leaking line: %v", got)
	}
	root := tempPrivate(t)
	w := newRunWorld(t)
	h := &swHooks{}
	var phrase string // read from passphrase.env before the apply (Protect), never inside Sweep
	h.print = func() string {
		return fmt.Sprintf("LZ-LIVE sweep-marker client=%s passphrase=%s published=%s\n", seedSecret, phrase, checkStateSecret)
	}
	p := w.swProbe(t, root, tofuStack{Outputs: checkOutputs(t, w, "deny_check", checkEnvWant())}, h.check(w))
	p.Run.Protect = func(Stack, []byte) error {
		if phrase == "" {
			_, _, pass := probeFiles(t, root)
			v, _ := ReadCredentialFile(pass)
			phrase = v["TF_VAR_state_passphrase"]
		}
		return nil
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v\n%s", err, w.term)
	}
	if len(phrase) < 16 {
		t.Fatal("fixture: the passphrase was never read")
	}
	if len(h.calls()) != 1 || !strings.Contains(w.term.String(), "LZ-LIVE sweep-marker client=") {
		t.Fatalf("Sweep ran %d times; its line never reached the terminal:\n%s", len(h.calls()), w.term)
	}
	secrets := []string{seedSecret, phrase, checkStateSecret}
	if got := swLeaks(w.term.String(), secrets); len(got) > 0 {
		t.Errorf("the terminal shows %d secrets Sweep printed", len(got))
	}
	dir, _, _ := probeFiles(t, root)
	if left := holders([]string{w.runDir, dir}, secrets); len(left) > 0 {
		t.Errorf("%v hold a secret Sweep printed", left)
	}
}
