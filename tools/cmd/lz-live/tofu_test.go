package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T093 (FR-010, FR-011, ADR-0011): every guarded verb resolves tofu once, through the pinned
// rule (LZ_TOFU, else the entry's tofu, else PATH; the version must be mise.toml's opentofu pin),
// with LZ_TOFU from the process environment, else from ~/.config/ovh-lz/live.env. Any other
// version is blocked (exit 2) naming the pin and LZ_TOFU before a credential file is opened, the
// API is asked or tofu runs; the resolved binary is the one the children use. Fake tofus are the
// test binary behind a `tofu` symlink answering `version -json` from tofu.version beside it.

// otherTofu is the host tofu that sat ahead of mise's on the owner's PATH (first T010 run).
const otherTofu = "1.10.3"

var miseTofuPin = regexp.MustCompile(`(?m)^opentofu\s*=\s*"([^"]+)"`)

// tofuPin is the repository's opentofu pin, which every test checkout copies.
func tofuPin(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	m := miseTofuPin.FindSubmatch(raw)
	if m == nil {
		t.Fatal("mise.toml pins no opentofu")
	}
	return string(m[1])
}

// fakeTofuAt puts a fake tofu reporting version in dir and returns its path.
func fakeTofuAt(t *testing.T, dir, version string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "tofu")
	if err := os.Symlink(self, p); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(dir, "tofu.version"), version)
	return p
}

// setLiveTofu adds LZ_TOFU to the world's live.env (mode 0600, as the guard requires).
func setLiveTofu(t *testing.T, w world, tofu string) {
	t.Helper()
	p := filepath.Join(w.home, ".config", "ovh-lz", "live.env")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	writePrivate(t, p, string(raw)+"LZ_TOFU="+tofu+"\n")
}

var tofuLaneVerbs = []string{"plan", "apply", "destroy", "chain", "bootstrap"}

type tofuRun struct {
	code    int
	stderr  string
	tofu    string // the tofu the verb's body was handed ("" when never reached)
	reached int    // Apply, Chain or Bootstrap calls
}

// runLaneVerb runs a lane verb or bootstrap with Apply, Chain and Bootstrap captured.
func runLaneVerb(w *bootstrapWorld, verb string) *tofuRun {
	r := &tofuRun{}
	w.apply = func(_ context.Context, o live.ApplyOptions) error { r.tofu = o.Tofu; r.reached++; return nil }
	w.chain = func(_ context.Context, o live.ChainOptions) error { r.tofu = o.Tofu; r.reached++; return nil }
	w.state = func(o live.StateOptions) func(context.Context, live.BootstrapAccount) error {
		r.tofu = o.Tofu
		return nil
	}
	w.boot = func(context.Context, live.BootstrapOptions) ([]live.PhaseResult, error) { r.reached++; return nil, nil }
	args := []string{verb, "--reviewed-sha", fakeHead}
	if verb != "bootstrap" {
		args = append(args, "all")
	}
	r.code, r.stderr = w.run(args...)
	return r
}

// wantTofuBlocked: exit 2, and the message names the pin and LZ_TOFU.
func wantTofuBlocked(t *testing.T, code int, stderr string) {
	t.Helper()
	pin := tofuPin(t)
	if code != live.BlockedExit || !strings.Contains(stderr, "blocked (") || !strings.Contains(stderr, pin) || !strings.Contains(stderr, "LZ_TOFU") {
		t.Errorf("exit %d, stderr %q; want %d (blocked) naming the pin %s and LZ_TOFU", code, stderr, live.BlockedExit, pin)
	}
}

func TestTofuEntryBlocksAnotherVersion(t *testing.T) {
	for _, verb := range tofuLaneVerbs {
		t.Run(verb, func(t *testing.T) {
			w, cfg := laneEntryWorld(t, true)
			writePrivate(t, filepath.Join(filepath.Dir(w.tofu), "tofu.version"), otherTofu)
			opened := watchOpens(t, filepath.Join(cfg, "sandbox.env"))
			r := runLaneVerb(w, verb)
			wantTofuBlocked(t, r.code, r.stderr)
			if !strings.Contains(r.stderr, otherTofu) {
				t.Errorf("stderr %q does not name the version found (%s)", r.stderr, otherTofu)
			}
			if got := opened(); len(got) != 0 {
				t.Errorf("credential files opened before the refusal: %v", got)
			}
			if n := w.calls.Load(); n != 0 {
				t.Errorf("%d API calls before the refusal", n)
			}
			if r.reached != 0 || r.tofu != "" {
				t.Errorf("the verb ran (%d calls, tofu %q) with tofu %s", r.reached, r.tofu, otherTofu)
			}
			if n := len(w.childCalls(t, "tofu.log")); n != 0 {
				t.Errorf("%d tofu calls before the refusal", n)
			}
		})
	}
	t.Run("probe", func(t *testing.T) {
		w := newProbeWorld(t)
		bin := filepath.Dir(w.git)
		writePrivate(t, filepath.Join(bin, "tofu.version"), otherTofu)
		opened := watchOpens(t, filepath.Join(w.cfg, "sandbox.env"), filepath.Join(w.cfg, "accounts", probeAccount, "account.env"))
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		wantTofuBlocked(t, code, stderr)
		if got := opened(); len(got) != 0 {
			t.Errorf("credential files opened before the refusal: %v", got)
		}
		if *w.apiCalls != 0 {
			t.Errorf("%d API calls before the refusal", *w.apiCalls)
		}
		if n, m := len(w.childCalls(t, "tofu.log")), len(w.childCalls(t, "ovhcloud.log")); n+m != 0 {
			t.Errorf("%d tofu and %d ovhcloud calls before the refusal\nstdout:\n%s", n, m, stdout)
		}
	})
}

// TestTofuEntryLiveEnv: LZ_TOFU in live.env selects the pinned binary although PATH has another
// version, and every child tofu call is that binary.
func TestTofuEntryLiveEnv(t *testing.T) {
	for _, verb := range tofuLaneVerbs {
		t.Run(verb, func(t *testing.T) {
			w, _ := laneEntryWorld(t, true)
			writePrivate(t, filepath.Join(filepath.Dir(w.tofu), "tofu.version"), otherTofu)
			pinned := fakeTofuAt(t, filepath.Join(t.TempDir(), "pinned"), tofuPin(t))
			setLiveTofu(t, w.world, pinned)
			r := runLaneVerb(w, verb)
			if r.code != 0 || r.reached != 1 || r.tofu != pinned {
				t.Errorf("exit %d, stderr %q, verb ran %d times with tofu %q; want 0, once, %s", r.code, r.stderr, r.reached, r.tofu, pinned)
			}
		})
	}
	t.Run("probe", func(t *testing.T) {
		w := newProbeWorld(t)
		bin := filepath.Dir(w.git)
		writePrivate(t, filepath.Join(bin, "tofu.version"), otherTofu)
		dir := filepath.Join(t.TempDir(), "pinned")
		setLiveTofu(t, w.world, fakeTofuAt(t, dir, tofuPin(t)))
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code != 0 {
			t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		pinnedCalls := (world{git: filepath.Join(dir, "git")}).childCalls(t, "tofu.log")
		if len(pinnedCalls) == 0 {
			t.Error("the pinned tofu ran no call")
		}
		if n := len(w.childCalls(t, "tofu.log")); n != 0 {
			t.Errorf("%d child calls ran the PATH tofu %s", n, otherTofu)
		}
		if n := len(w.childCalls(t, "tofu-version.log")); n != 0 {
			t.Errorf("the PATH tofu was asked its version %d times although LZ_TOFU is set", n)
		}
	})
}

// TestTofuEntryEnvOverridesLiveEnv: LZ_TOFU in the process environment wins over live.env's.
func TestTofuEntryEnvOverridesLiveEnv(t *testing.T) {
	t.Run("plan", func(t *testing.T) {
		w, _ := laneEntryWorld(t, true)
		stale := fakeTofuAt(t, filepath.Join(t.TempDir(), "stale"), otherTofu)
		pinned := fakeTofuAt(t, filepath.Join(t.TempDir(), "pinned"), tofuPin(t))
		setLiveTofu(t, w.world, stale)
		w.env["LZ_TOFU"] = pinned
		r := runLaneVerb(w, "plan")
		if r.code != 0 || r.tofu != pinned {
			t.Errorf("exit %d, stderr %q, tofu %q; want 0 and %s", r.code, r.stderr, r.tofu, pinned)
		}
	})
	t.Run("probe", func(t *testing.T) {
		w := newProbeWorld(t)
		dir := filepath.Join(t.TempDir(), "pinned")
		setLiveTofu(t, w.world, fakeTofuAt(t, filepath.Join(t.TempDir(), "stale"), otherTofu))
		w.env["LZ_TOFU"] = fakeTofuAt(t, dir, tofuPin(t))
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code != 0 {
			t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		if n := len((world{git: filepath.Join(dir, "git")}).childCalls(t, "tofu.log")); n == 0 {
			t.Error("the environment's LZ_TOFU ran no call")
		}
	})
}

// TestTofuEntryWrongLZTofu: an LZ_TOFU of another version is blocked, never skipped for the
// pinned tofu on PATH; the environment's wrong one is not skipped for live.env's right one.
func TestTofuEntryWrongLZTofu(t *testing.T) {
	for name, set := range map[string]func(t *testing.T, w *bootstrapWorld){
		"live-env": func(t *testing.T, w *bootstrapWorld) {
			setLiveTofu(t, w.world, fakeTofuAt(t, filepath.Join(t.TempDir(), "stale"), otherTofu))
		},
		"env": func(t *testing.T, w *bootstrapWorld) {
			setLiveTofu(t, w.world, fakeTofuAt(t, filepath.Join(t.TempDir(), "pinned"), tofuPin(t)))
			w.env["LZ_TOFU"] = fakeTofuAt(t, filepath.Join(t.TempDir(), "stale"), otherTofu)
		},
	} {
		t.Run(name, func(t *testing.T) {
			w, _ := laneEntryWorld(t, true)
			set(t, w)
			r := runLaneVerb(w, "plan")
			wantTofuBlocked(t, r.code, r.stderr)
			if r.reached != 0 || w.calls.Load() != 0 {
				t.Errorf("Apply called %d times, %d API calls", r.reached, w.calls.Load())
			}
		})
	}
}

// TestTofuEntryVersionProbeEnvironment: the version probe is a child like any other: it gets PATH
// and a HOME, never the caller's exports (an .envrc may export the sandbox credential; review r1).
func TestTofuEntryVersionProbeEnvironment(t *testing.T) {
	w, _ := laneEntryWorld(t, true)
	t.Setenv("OVH_CLIENT_SECRET", "t093-exported-sentinel")
	t.Setenv("TF_CLI_ARGS", "-no-color")
	r := runLaneVerb(w, "plan")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	calls := w.childCalls(t, "tofu-version.log")
	if len(calls) != 1 {
		t.Fatalf("%d version probes, want 1", len(calls))
	}
	for _, kv := range calls[0].Env {
		k, _, _ := strings.Cut(kv, "=")
		if k != "PATH" && k != "HOME" {
			t.Errorf("the version probe inherited %s", k)
		}
	}
}

// TestTofuEntryVersionProbeHome (spec 006 FR-007, T056): the version probe's HOME is a fresh
// private scratch directory under TMPDIR (0700 while it runs, removed after), never TMPDIR itself
// (os.TempDir(), which every process shares) and never the caller's HOME.
func TestTofuEntryVersionProbeHome(t *testing.T) {
	for _, verb := range tofuLaneVerbs {
		t.Run(verb, func(t *testing.T) {
			w, _ := laneEntryWorld(t, true)
			tmp := resolvedPath(os.Getenv("TMPDIR"))
			r := runLaneVerb(w, verb)
			if r.code != 0 {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			calls := w.childCalls(t, "tofu-version.log")
			if len(calls) != 1 {
				t.Fatalf("%d version probes, want 1", len(calls))
			}
			wantProbeHome(t, calls[0], tmp, w.home)
		})
	}
}

// wantProbeHome: the probe's HOME was a fresh 0700 directory directly under tmp (resolved), not
// tmp itself nor the caller's home, and is gone now.
func wantProbeHome(t *testing.T, call childCall, tmp, callerHome string) {
	t.Helper()
	home := envMap(call.Env)["HOME"]
	real := resolvedPath(home)
	if real == tmp || real == resolvedPath(os.TempDir()) || filepath.Dir(real) != tmp {
		t.Errorf("version probe HOME %s, want a fresh directory directly under TMPDIR %s, not TMPDIR itself", home, tmp)
	}
	if real == resolvedPath(callerHome) {
		t.Errorf("version probe HOME %s is the caller's", home)
	}
	if call.HomeMode != "0700" {
		t.Errorf("version probe HOME mode %s, want 0700", call.HomeMode)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("version probe HOME %s after the run: %v, want it removed", home, err)
	}
}

// TestTofuEntryVersionProbeScratchInsideCheckout (T056): the probe's scratch HOME goes under
// TMPDIR only once TMPDIR is known to lie outside the checkout: a TMPDIR inside it is refused
// (exit 3) before the version probe runs, and nothing is made in it.
func TestTofuEntryVersionProbeScratchInsideCheckout(t *testing.T) {
	w, _ := laneEntryWorld(t, true)
	tmp := filepath.Join(w.checkout, ".local", "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	r := runLaneVerb(w, "plan")
	if r.code != live.RefusalExit || !strings.Contains(r.stderr, "("+live.CondScratch+")") {
		t.Errorf("exit %d, stderr %q; want %d naming %s", r.code, r.stderr, live.RefusalExit, live.CondScratch)
	}
	if n := len(w.childCalls(t, "tofu-version.log")); n != 0 {
		t.Errorf("%d version probes with TMPDIR inside the checkout, want none", n)
	}
	if left, err := os.ReadDir(tmp); err != nil || len(left) != 0 {
		t.Errorf("TMPDIR inside the checkout holds %v (%v), want nothing made in it", left, err)
	}
}

// TestTofuEntryRelativeLZTofu:a relative LZ_TOFU is blocked, even when it names the pinned
// version from the working directory: the children run in the stack's directory, where it names
// another file.
func TestTofuEntryRelativeLZTofu(t *testing.T) {
	w, _ := laneEntryWorld(t, true)
	base := t.TempDir()
	fakeTofuAt(t, filepath.Join(base, "pinned"), tofuPin(t))
	pin := tofuPin(t)
	t.Chdir(base)
	w.env["LZ_TOFU"] = filepath.Join("pinned", "tofu")
	r := runLaneVerb(w, "plan")
	if r.code != live.BlockedExit || !strings.Contains(r.stderr, "LZ_TOFU") || !strings.Contains(r.stderr, pin) || r.reached != 0 {
		t.Errorf("exit %d, stderr %q, Apply called %d times; want %d naming LZ_TOFU (pin %s), never", r.code, r.stderr, r.reached, live.BlockedExit, pin)
	}
}
