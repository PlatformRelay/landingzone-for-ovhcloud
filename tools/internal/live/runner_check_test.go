package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// T096: a probe root's check after its apply (live.ProbeCheck). The run reads the root's sensitive
// output into memory, redacts every non-public value, runs Observe once after the apply and writes
// its record to observations.json; on every way out it runs Undo before the root's destroy, and
// keeps the root when Undo fails, for `--cleanup`, which reads the output again and undoes first.

const (
	checkStateSecret   = "check-state-secret-7f6e5d4c3b2a"
	checkJanitorSecret = "check-janitor-secret-1a2b3c4d5e6f"
	checkStateKeyID    = "AKSTATECHECK0001"
	checkBucket        = "lzprobe-state-20261006t120000z-a1b2"
)

var checkSecrets = []string{checkStateSecret, checkJanitorSecret, checkStateKeyID}

func checkEnvWant() map[string]string {
	return map[string]string{"bucket": checkBucket, "region": "GRA", "state_key_id": checkStateKeyID, "state_secret": checkStateSecret,
		"janitor_key_id": "AKJANITORCHECK01", "janitor_secret": checkJanitorSecret}
}

// checkOutputs writes what `tofu output -json` prints for a root publishing output (value as
// given, in tofu's format: sensitive, type, value).
func checkOutputs(t *testing.T, w *runWorld, output string, value any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"bucket": map[string]any{"sensitive": false, "type": "string", "value": checkBucket},
		output:   map[string]any{"sensitive": true, "type": []any{"map", "string"}, "value": value},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(w.bin, "outputs-check.json")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// checkHooks is a ProbeCheck whose calls are logged to tofu.log (cmd observe and undo, in order
// with the tofu calls) with the environment and whether the context was live.
type checkHooks struct {
	mu          sync.Mutex
	envs        []map[string]string
	observeErr  error
	undoErrs    []error // the n-th Undo fails with the n-th entry (nil: passes)
	undoCalls   int
	undoCtxErr  []error
	observeWait bool  // Observe waits until its context ends
	validateErr error // Validate refuses the published variables
	started     chan struct{}
	// T113: withRecorded sets Recorded, which logs `recorded`, keeps what it was handed (nil: no
	// record) and fails with recordedErr.
	withRecorded bool
	recordedErr  error
	records      [][]byte
}

func (h *checkHooks) check(w *runWorld) *ProbeCheck {
	h.started = make(chan struct{}, 1)
	c := h.probeCheck(w)
	if h.withRecorded {
		c.Recorded = func(record []byte, term io.Writer) error {
			logCall(w.bin, "tofu.log", tofuCall{Cmd: "recorded", Stack: "p"})
			h.mu.Lock()
			defer h.mu.Unlock()
			h.records = append(h.records, record)
			return h.recordedErr
		}
	}
	return c
}

func (h *checkHooks) probeCheck(w *runWorld) *ProbeCheck {
	return &ProbeCheck{
		Output: "deny_check",
		Validate: func(env map[string]string) error {
			if h.validateErr != nil {
				return fmt.Errorf("%w (secret %s)", h.validateErr, env["state_secret"])
			}
			return nil
		},
		Public: []string{"bucket", "region"},
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			logCall(w.bin, "tofu.log", tofuCall{Cmd: "observe", Stack: "p"})
			h.mu.Lock()
			h.envs = append(h.envs, maps.Clone(env))
			h.mu.Unlock()
			// Everything it prints and records passes the run's redactor.
			fmt.Fprintf(term, "LZ-LIVE observe bucket=%s secret=%s janitor=%s key=%s\n", env["bucket"], env["state_secret"], env["janitor_secret"], env["state_key_id"])
			h.started <- struct{}{}
			if h.observeWait {
				<-ctx.Done()
				return []byte(`{"check":"partial","secret":"` + env["state_secret"] + `"}`), ctx.Err()
			}
			return []byte(`{"check":"test","bucket":"` + env["bucket"] + `","secret":"` + env["state_secret"] + `"}`), h.observeErr
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			logCall(w.bin, "tofu.log", tofuCall{Cmd: "undo", Stack: "p"})
			h.mu.Lock()
			defer h.mu.Unlock()
			h.envs = append(h.envs, maps.Clone(env))
			h.undoCtxErr = append(h.undoCtxErr, ctx.Err())
			n := h.undoCalls
			h.undoCalls++
			fmt.Fprintf(term, "LZ-LIVE undo secret=%s\n", env["janitor_secret"])
			if n < len(h.undoErrs) && h.undoErrs[n] != nil {
				return fmt.Errorf("%w (secret %s)", h.undoErrs[n], env["janitor_secret"])
			}
			return nil
		},
	}
}

func (w *runWorld) checkProbe(t *testing.T, root string, outputs string, h *checkHooks) Probe {
	t.Helper()
	p := w.probe(t, root, tofuStack{Outputs: outputs})
	p.Check = h.check(w)
	return p
}

// cmds is the order of apply, output, observe, undo, destroy-plan and destroy calls.
func checkOrder(calls []tofuCall) []string {
	var out []string
	for _, c := range calls {
		switch {
		case c.Cmd == "plan" && slices.Contains(c.Args, "-destroy"):
			out = append(out, "plan-destroy")
		case slices.Contains([]string{"apply", "output", "observe", "recorded", "undo", "destroy"}, c.Cmd):
			out = append(out, c.Cmd)
		}
	}
	return out
}

// noCheckSecret: no file under the dirs and no terminal, summary or observation holds a check
// secret; the bucket (public) is not redacted.
func noCheckSecret(t *testing.T, w *runWorld, dirs ...string) {
	t.Helper()
	if left := holders(dirs, checkSecrets); len(left) > 0 {
		t.Errorf("%v hold a check secret", left)
	}
	for _, s := range checkSecrets {
		if strings.Contains(w.term.String(), s) {
			t.Errorf("the terminal shows the check secret %s", s)
		}
	}
}

func TestProbeCheckRun(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{}
		p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", checkEnvWant()), h)
		if err := p.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v\n%s", err, w.term)
		}
		calls := w.calls(t, "tofu.log")
		if got, want := checkOrder(calls), []string{"apply", "output", "observe", "output", "undo", "plan-destroy", "destroy"}; !slices.Equal(got, want) {
			t.Errorf("calls %v, want %v: observe once after the apply, undo before the destroy", got, want)
		}
		if len(h.envs) != 2 || !maps.Equal(h.envs[0], checkEnvWant()) || !maps.Equal(h.envs[1], checkEnvWant()) {
			t.Errorf("the check got %v, want the published output twice", h.envs)
		}
		rec, err := os.ReadFile(filepath.Join(w.runDir, "observations.json"))
		if err != nil {
			t.Fatalf("no observations.json in the run record: %v", err)
		}
		if mode(t, filepath.Join(w.runDir, "observations.json")) != 0o600 {
			t.Errorf("observations.json mode %v, want 0600", mode(t, filepath.Join(w.runDir, "observations.json")))
		}
		if !strings.Contains(string(rec), `"check":"test"`) || !strings.Contains(string(rec), checkBucket) {
			t.Errorf("observations.json %s: want the check's record, the public bucket name unredacted", rec)
		}
		if !strings.Contains(w.term.String(), "bucket="+checkBucket) {
			t.Errorf("the terminal lacks the public bucket name:\n%s", w.term)
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)
		// The check's credential never reaches a child: no tofu call's environment holds it.
		for _, c := range calls {
			if c.Cmd == "observe" || c.Cmd == "undo" {
				continue
			}
			for _, kv := range c.Env {
				for _, s := range checkSecrets {
					if strings.Contains(kv, s) {
						t.Errorf("tofu %s ran with a check secret in its environment", c.Cmd)
					}
				}
			}
			for _, a := range c.Args {
				for _, s := range checkSecrets {
					if strings.Contains(a, s) {
						t.Errorf("tofu %s ran with a check secret in argv", c.Cmd)
					}
				}
			}
		}
		if w.summary(t)["outcome"] != "pass" {
			t.Errorf("summary %v, want pass", w.summary(t))
		}
	})

	t.Run("observe-fails", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{observeErr: errors.New("put-v1: no answer")}
		p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", checkEnvWant()), h)
		err := p.Start(context.Background())
		if err == nil || !strings.Contains(err.Error(), "put-v1: no answer") {
			t.Fatalf("Start: %v, want the check's error", err)
		}
		if got, want := checkOrder(w.calls(t, "tofu.log")), []string{"apply", "output", "observe", "output", "undo", "plan-destroy", "destroy"}; !slices.Equal(got, want) {
			t.Errorf("calls %v, want %v: a failed check is undone and the root destroyed", got, want)
		}
		if !exists(filepath.Join(w.runDir, "observations.json")) {
			t.Error("a failed check left no observations.json")
		}
		if sum := w.summary(t); sum["outcome"] != "fail" || !strings.Contains(fmt.Sprint(sum["error"]), "put-v1") {
			t.Errorf("summary %v, want fail naming the check's error", sum)
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)
	})

	t.Run("interrupted", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{observeWait: true}
		p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", checkEnvWant()), h)
		sigs := make(chan os.Signal, 1)
		p.Run.Signals = sigs
		done := make(chan error, 1)
		go func() { done <- p.Start(context.Background()) }()
		select {
		case <-h.started:
		case <-time.After(30 * time.Second):
			t.Fatal("Observe never started")
		}
		sigs <- syscall.SIGINT
		var err error
		select {
		case err = <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("the run did not end after SIGINT")
		}
		if err == nil {
			t.Error("an interrupted run passed")
		}
		if got, want := checkOrder(w.calls(t, "tofu.log")), []string{"apply", "output", "observe", "output", "undo", "plan-destroy", "destroy"}; !slices.Equal(got, want) {
			t.Errorf("calls %v, want %v: an interrupted check is undone before the destroy", got, want)
		}
		if len(h.undoCtxErr) != 1 || h.undoCtxErr[0] != nil {
			t.Errorf("Undo ran with context errors %v, want one call with a live context", h.undoCtxErr)
		}
		if rec, _ := os.ReadFile(filepath.Join(w.runDir, "observations.json")); !strings.Contains(string(rec), "partial") {
			t.Errorf("observations.json %q, want the partial record", rec)
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)
	})

	t.Run("undo-fails-keeps-root-then-cleanup", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{undoErrs: []error{errors.New("bucket not empty")}}
		p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", checkEnvWant()), h)
		err := p.Start(context.Background())
		calls := w.calls(t, "tofu.log")
		if plans, applies, _ := destroyCalls(calls, "p"); plans+applies > 0 {
			t.Errorf("the root was planned for destroy %d times and destroyed %d times after a failed undo", plans, applies)
		}
		_, state, pass := probeFiles(t, root)
		if err == nil || !exists(state) || !exists(pass) {
			t.Fatalf("Start: %v; state kept %v, passphrase kept %v; want a failure and both kept", err, exists(state), exists(pass))
		}
		sum := w.summary(t)
		msg := fmt.Sprint(sum["error"])
		if sum["outcome"] != "fail" || !strings.Contains(msg, "bucket not empty") || !strings.Contains(msg, "kept") || !strings.Contains(msg, "--cleanup 20261006T120000Z-a1b2") {
			t.Errorf("summary %v: want fail, the undo's error, the kept root and --cleanup", sum)
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)

		before := len(calls)
		if err := p.Cleanup(context.Background()); err != nil {
			t.Fatalf("cleanup: %v\n%s", err, w.term)
		}
		after := w.calls(t, "tofu.log")[before:]
		if got, want := checkOrder(after), []string{"output", "undo", "plan-destroy", "destroy"}; !slices.Equal(got, want) {
			t.Errorf("cleanup calls %v, want %v: the output read again, undone, then destroyed", got, want)
		}
		if h.undoCalls != 2 || len(h.envs) != 3 || !maps.Equal(h.envs[2], checkEnvWant()) {
			t.Errorf("cleanup's Undo: %d calls, envs %v", h.undoCalls, h.envs)
		}
		for _, c := range after {
			if c.Cmd == "observe" {
				t.Error("cleanup observed again")
			}
		}
		if exists(state) || exists(pass) {
			t.Error("a passing cleanup kept the probe's files")
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)
	})

	t.Run("cleanup-undo-fails-keeps-root", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{undoErrs: []error{errors.New("first"), errors.New("second")}}
		p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", checkEnvWant()), h)
		_ = p.Start(context.Background())
		before := len(w.calls(t, "tofu.log"))
		err := p.Cleanup(context.Background())
		if plans, applies, _ := destroyCalls(w.calls(t, "tofu.log")[before:], "p"); err == nil || plans+applies > 0 {
			t.Errorf("cleanup with a failing undo: %v, %d destroy plans, %d destroys; want a failure and none", err, plans, applies)
		}
		if _, state, pass := probeFiles(t, root); !exists(state) || !exists(pass) {
			t.Error("a failed cleanup removed the probe's files")
		}
	})

	// T113 (T101 gap 2): `--cleanup` is a fresh process, so what the run's Observe recorded is known
	// only from the run directory's observations.json. The cleanup hands it to the check's Recorded
	// before Undo; none there is handed as nil and the undo runs; an unreadable one, or one Recorded
	// refuses, fails the undo step without Undo, and the root is kept. A run never calls Recorded.
	cleanupAfterKept := func(t *testing.T, h *checkHooks) (*runWorld, Probe, string, int) {
		t.Helper()
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h.withRecorded = true
		h.undoErrs = []error{errors.New("policy still listed")}
		p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", checkEnvWant()), h)
		if err := p.Start(context.Background()); err == nil {
			t.Fatal("Start passed with a failing undo")
		}
		if len(h.records) != 0 {
			t.Errorf("the run called Recorded %d times; only a cleanup hands the record", len(h.records))
		}
		return w, p, root, len(w.calls(t, "tofu.log"))
	}
	t.Run("cleanup-hands-the-record", func(t *testing.T) {
		h := &checkHooks{}
		w, p, root, before := cleanupAfterKept(t, h)
		want, err := os.ReadFile(filepath.Join(w.runDir, "observations.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Cleanup(context.Background()); err != nil {
			t.Fatalf("cleanup: %v\n%s", err, w.term)
		}
		if got, wantOrder := checkOrder(w.calls(t, "tofu.log")[before:]), []string{"output", "recorded", "undo", "plan-destroy", "destroy"}; !slices.Equal(got, wantOrder) {
			t.Errorf("cleanup calls %v, want %v: the record handed over before the undo", got, wantOrder)
		}
		if len(h.records) != 1 || string(h.records[0]) != string(want) {
			t.Errorf("Recorded got %q, want observations.json as the run wrote it, %q", h.records, want)
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)
	})
	t.Run("cleanup-without-record", func(t *testing.T) {
		h := &checkHooks{}
		w, p, _, before := cleanupAfterKept(t, h)
		if err := os.Remove(filepath.Join(w.runDir, "observations.json")); err != nil {
			t.Fatal(err)
		}
		if err := p.Cleanup(context.Background()); err != nil {
			t.Fatalf("cleanup: %v\n%s", err, w.term)
		}
		if got, wantOrder := checkOrder(w.calls(t, "tofu.log")[before:]), []string{"output", "recorded", "undo", "plan-destroy", "destroy"}; !slices.Equal(got, wantOrder) {
			t.Errorf("cleanup calls %v, want %v", got, wantOrder)
		}
		if len(h.records) != 1 || h.records[0] != nil {
			t.Errorf("Recorded got %q, want one call with nil (no record)", h.records)
		}
	})
	for name, tc := range map[string]struct {
		edit func(t *testing.T, h *checkHooks, runDir string)
		err  string
	}{
		"cleanup-unreadable-record": {func(t *testing.T, _ *checkHooks, runDir string) {
			rec := filepath.Join(runDir, "observations.json")
			if err := os.Remove(rec); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(rec, 0o700); err != nil {
				t.Fatal(err)
			}
		}, "observations.json"},
		"cleanup-record-refused": {func(_ *testing.T, h *checkHooks, _ string) {
			h.recordedErr = errors.New("observations.json malformed")
		}, "observations.json malformed"},
	} {
		t.Run(name, func(t *testing.T) {
			h := &checkHooks{}
			w, p, root, before := cleanupAfterKept(t, h)
			tc.edit(t, h, w.runDir)
			err := p.Cleanup(context.Background())
			after := w.calls(t, "tofu.log")[before:]
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("cleanup: %v; want a failure naming %q", err, tc.err)
			}
			if got := checkOrder(after); slices.Contains(got, "undo") || slices.Contains(got, "destroy") || slices.Contains(got, "plan-destroy") {
				t.Errorf("cleanup calls %v: want neither the undo nor a destroy (never name only)", got)
			}
			if _, state, pass := probeFiles(t, root); !exists(state) || !exists(pass) {
				t.Error("a failed cleanup removed the probe's files")
			}
		})
	}

	t.Run("plan-only", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{}
		p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", checkEnvWant()), h)
		p.Run.PlanOnly = true
		if err := p.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if got := checkOrder(w.calls(t, "tofu.log")); len(got) != 0 {
			t.Errorf("plan-only calls %v, want no apply, output, check or destroy", got)
		}
	})

	t.Run("apply-fails", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{}
		p := w.probe(t, root, tofuStack{ApplyExit: 1, Outputs: checkOutputs(t, w, "deny_check", checkEnvWant())})
		p.Check = h.check(w)
		if err := p.Start(context.Background()); err == nil {
			t.Fatal("a failed apply passed")
		}
		got := checkOrder(w.calls(t, "tofu.log"))
		if slices.Contains(got, "observe") || !slices.Contains(got, "destroy") || slices.Index(got, "undo") > slices.Index(got, "plan-destroy") {
			t.Errorf("calls %v: want no observe after a failed apply, any undo before the destroy, and the destroy", got)
		}
		if exists(filepath.Join(w.runDir, "observations.json")) {
			t.Error("observations.json without an observation")
		}
	})

	t.Run("output-missing", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{}
		p := w.checkProbe(t, root, checkOutputs(t, w, "other", checkEnvWant()), h)
		err := p.Start(context.Background())
		if err == nil || !strings.Contains(err.Error(), "deny_check") {
			t.Fatalf("Start: %v, want an error naming the missing output deny_check", err)
		}
		got := checkOrder(w.calls(t, "tofu.log"))
		if slices.Contains(got, "observe") || slices.Contains(got, "undo") || !slices.Contains(got, "destroy") {
			t.Errorf("calls %v: want no observe, no undo (nothing published), and the destroy", got)
		}
	})

	for name, value := range map[string]any{"not-a-map": "x", "number-values": map[string]any{"bucket": 1}, "nested": map[string]any{"bucket": map[string]any{"a": "b"}}} {
		t.Run("output-"+name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			h := &checkHooks{}
			p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", value), h)
			err := p.Start(context.Background())
			if err == nil || !strings.Contains(err.Error(), "deny_check") {
				t.Fatalf("Start: %v, want a refusal naming the output", err)
			}
			got := checkOrder(w.calls(t, "tofu.log"))
			// The same refusal at the undo: nothing was observed, so the root is destroyed, not kept.
			if slices.Contains(got, "observe") || slices.Contains(got, "undo") || !slices.Contains(got, "destroy") {
				t.Errorf("calls %v: want no check and the destroy", got)
			}
		})
	}

	// Review r1: what the check itself refuses in the published variables depends on them alone, so
	// nothing was observed: no check runs, the root is destroyed, not kept for a cleanup that would
	// meet the same refusal.
	t.Run("check-refuses-output", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{validateErr: errors.New("output deny_check publishes an empty region")}
		p := w.checkProbe(t, root, checkOutputs(t, w, "deny_check", checkEnvWant()), h)
		err := p.Start(context.Background())
		if err == nil || !strings.Contains(err.Error(), "empty region") {
			t.Fatalf("Start: %v, want the check's refusal", err)
		}
		got := checkOrder(w.calls(t, "tofu.log"))
		if slices.Contains(got, "observe") || slices.Contains(got, "undo") || !slices.Contains(got, "destroy") {
			t.Errorf("calls %v: want no observe, no undo, and the destroy", got)
		}
		if plans, applies, _ := destroyCalls(w.calls(t, "tofu.log"), "p"); plans != 1 || applies != 1 {
			t.Errorf("root: %d destroy plans, %d destroys; want one each", plans, applies)
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)
	})

	// T100: storage-iam has a companion and a check. The check observes after the companion's
	// apply (its state object exists then) and undoes after the companion's destroy, before the
	// root's; a failed observation still destroys both. Review r1: a companion that cannot be
	// destroyed keeps the root, but the undo still runs (an accepted IAM write must not wait for
	// the cleanup).
	for _, tc := range []struct {
		name             string
		observeErr       error
		companionDestroy int // the companion's destroy exit
		want             []string
	}{
		{"companion-pass", nil, 0, []string{"p apply", "companion apply", "observe", "companion destroy", "undo", "p destroy"}},
		{"companion-observe-fails", errors.New("p25-read-bucket-a: expected refused, observed allowed 200"), 0, []string{"p apply", "companion apply", "observe", "companion destroy", "undo", "p destroy"}},
		{"companion-destroy-fails", nil, 1, []string{"p apply", "companion apply", "observe", "undo"}},
	} {
		observeErr := tc.observeErr
		t.Run(tc.name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			h := &checkHooks{observeErr: observeErr}
			p := w.companionCheckProbe(t, root, tofuStack{DestroyExit: tc.companionDestroy}, h)
			err := p.Start(context.Background())
			failing := observeErr != nil || tc.companionDestroy != 0
			if (err != nil) != failing {
				t.Fatalf("Start: %v, want an error only from Observe or the companion's destroy\n%s", err, w.term)
			}
			if order := companionCheckOrder(w.calls(t, "tofu.log"), tc.companionDestroy != 0); !slices.Equal(order, tc.want) {
				t.Errorf("order %v, want %v", order, tc.want)
			}
			if !exists(filepath.Join(w.runDir, "observations.json")) {
				t.Error("no observations.json")
			}
			if want := map[bool]string{false: "pass", true: "fail"}[failing]; w.summary(t)["outcome"] != want {
				t.Errorf("summary %v, want %s", w.summary(t), want)
			}
			noCheckSecret(t, w, root, w.runDir, w.stacks)
		})
	}

	// T101 (T100 gap 5): a companion that cannot be destroyed and an undo that fails, together:
	// both errors reach the run's error, the root is kept for both reasons and never destroyed.
	t.Run("companion-destroy-and-undo-fail", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{undoErrs: []error{errors.New("policy still listed")}}
		p := w.companionCheckProbe(t, root, tofuStack{DestroyExit: 1}, h)
		err := p.Start(context.Background())
		if err == nil {
			t.Fatal("a run whose companion destroy and undo failed passed")
		}
		if order, want := companionCheckOrder(w.calls(t, "tofu.log"), true), []string{"p apply", "companion apply", "observe", "undo"}; !slices.Equal(order, want) {
			t.Errorf("order %v, want %v: the undo runs although the companion is kept, the root is not destroyed", order, want)
		}
		if plans, applies, _ := destroyCalls(w.calls(t, "tofu.log"), "p"); plans+applies > 0 {
			t.Errorf("the root was planned for destroy %d times and destroyed %d times", plans, applies)
		}
		if _, state, pass := probeFiles(t, root); !exists(state) || !exists(pass) {
			t.Errorf("state kept %v, passphrase kept %v; want both kept for --cleanup", exists(state), exists(pass))
		}
		sum := w.summary(t)
		msg := fmt.Sprint(sum["error"])
		for _, want := range []string{"policy still listed", "its check's writes may remain", "was not destroyed", "--cleanup 20261006T120000Z-a1b2"} {
			if !strings.Contains(msg, want) {
				t.Errorf("summary error %q lacks %q", msg, want)
			}
		}
		if sum["outcome"] != "fail" {
			t.Errorf("summary %v, want fail", sum)
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)
	})

	// T101 (T100 gap 5): `--cleanup` of a root with a companion and a check, kept because its
	// companion could not be destroyed: the companion goes first under the published identity, then
	// the check's undo, then the root; nothing is observed or applied again.
	t.Run("cleanup-companion-and-check", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		h := &checkHooks{}
		p := w.companionCheckProbe(t, root, tofuStack{DestroyExit: 1}, h)
		if err := p.Start(context.Background()); err == nil {
			t.Fatal("a run whose companion could not be destroyed passed")
		}
		st := w.scenario.Stacks["companion"]
		st.DestroyExit = 0
		w.scenario.Stacks["companion"] = st
		w.save(t)
		before := len(w.calls(t, "tofu.log"))
		if err := p.Cleanup(context.Background()); err != nil {
			t.Fatalf("cleanup: %v\n%s", err, w.term)
		}
		after := w.calls(t, "tofu.log")[before:]
		if order, want := companionCheckOrder(after, false), []string{"companion destroy", "undo", "p destroy"}; !slices.Equal(order, want) {
			t.Errorf("cleanup order %v, want %v", order, want)
		}
		for _, c := range after {
			if c.Cmd == "observe" || c.Cmd == "plan" && !slices.Contains(c.Args, "-destroy") {
				t.Errorf("cleanup ran %s %v: it must only undo and destroy", c.Cmd, c.Args)
			}
		}
		if h.undoCalls != 2 || len(h.envs) != 3 || !maps.Equal(h.envs[2], checkEnvWant()) {
			t.Errorf("Undo: %d calls, envs %v; want the run's and the cleanup's, with the published output", h.undoCalls, h.envs)
		}
		if _, state, pass := probeFiles(t, root); exists(state) || exists(pass) {
			t.Error("a passing cleanup kept the probe's files")
		}
		noCheckSecret(t, w, root, w.runDir, w.stacks)
	})

	t.Run("no-check-no-output-call", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{Outputs: checkOutputs(t, w, "deny_check", checkEnvWant())})
		if err := p.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if got := checkOrder(w.calls(t, "tofu.log")); slices.Contains(got, "output") {
			t.Errorf("calls %v: a root without a check reads no output", got)
		}
		if exists(filepath.Join(w.runDir, "observations.json")) {
			t.Error("observations.json without a check")
		}
	})
}

// companionCheckProbe is the storage-iam shape: a root with a companion (identityProbe) that also
// publishes the check's output beside companion_env, and h's check.
func (w *runWorld) companionCheckProbe(t *testing.T, root string, companion tofuStack, h *checkHooks) Probe {
	t.Helper()
	p := w.identityProbe(t, root, companion)
	outputs := filepath.Join(w.bin, "outputs-p.json")
	raw, err := os.ReadFile(outputs)
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	all["deny_check"] = map[string]any{"sensitive": true, "type": []any{"map", "string"}, "value": checkEnvWant()}
	raw, _ = json.Marshal(all)
	if err := os.WriteFile(outputs, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	p.Check = h.check(w)
	return p
}

// companionCheckOrder is the order of applies, observe, undo and destroys by stack; a destroy
// retried (T095) counts once, and with companionFails a companion's destroy is a failed one, not a
// destroy.
func companionCheckOrder(calls []tofuCall, companionFails bool) []string {
	var order []string
	for _, c := range calls {
		var e string
		switch {
		case c.Cmd == "observe" || c.Cmd == "undo":
			e = c.Cmd
		case c.Cmd == "apply" || c.Cmd == "destroy":
			e = c.Stack + " " + c.Cmd
		default:
			continue
		}
		if c.Cmd == "destroy" && companionFails && c.Stack == "companion" || len(order) > 0 && order[len(order)-1] == e {
			continue
		}
		order = append(order, e)
	}
	return order
}
