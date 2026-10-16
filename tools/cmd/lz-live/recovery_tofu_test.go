package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/probes/live/state"
)

// Spec 003 T036: the locked-recovery check's tofu seam (recovery_tofu.go) against a fake tofu (the
// test binary behind a `seam-tofu` symlink, TestMain). The seam is the one child that receives a
// published credential: the state key pair only, refused otherwise before a child starts; the
// child's environment is PATH, a scratch HOME under TMPDIR (0700, kept for the run, removed by
// close), its TF_DATA_DIR inside it and the pair; it runs with -chdir on <root>/backend; a kill
// run keeps stdin open, waits for the approval prompt, SIGKILLs and reports Signaled only for an
// exit on that signal; the output comes back redacted.

const (
	seamAK = "AKSEAMSTATE00001"
	seamSK = "seam-state-secret-6d5c4b3a2f1e"
)

func seamEnv() map[string]string {
	return map[string]string{"AWS_ACCESS_KEY_ID": seamAK, "AWS_SECRET_ACCESS_KEY": seamSK}
}

// seamCall is one fake tofu start as it logged it.
type seamCall struct {
	Args []string `json:"args"`
	Env  []string `json:"env"`
}

// fakeSeamTofu logs its arguments and environment, prints the key pair on stdout and stderr, and
// answers by command: apply as apply.mode beside it says (prompt: print OpenTofu's prompt and wait
// for a line on stdin, failing on its end; early: exit 0; silent: wait on stdin without a prompt),
// plan exit 1, anything else exit 0.
func fakeSeamTofu(args []string) int {
	bin := filepath.Dir(os.Args[0])
	env := os.Environ()
	slices.Sort(env)
	if raw, err := json.Marshal(seamCall{Args: args, Env: env}); err == nil {
		if f, err := os.OpenFile(filepath.Join(bin, "seam.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintln(f, string(raw))
			f.Close()
		}
	}
	fmt.Printf("fake tofu key=%s\n", os.Getenv("AWS_ACCESS_KEY_ID"))
	fmt.Fprintf(os.Stderr, "fake tofu secret=%s\n", os.Getenv("AWS_SECRET_ACCESS_KEY"))
	cmd := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			cmd = a
			break
		}
	}
	switch cmd {
	case "apply":
		mode, _ := os.ReadFile(filepath.Join(bin, "apply.mode"))
		switch strings.TrimSpace(string(mode)) {
		case "early":
			return 0
		case "prompt":
			fmt.Print("Do you want to perform these actions?\n  OpenTofu will perform the actions described above.\n  Only 'yes' will be accepted to approve.\n\n  Enter a value: ")
		}
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			fmt.Fprintln(os.Stderr, "Error: input closed")
			return 1
		}
		return 0
	case "plan":
		return 1
	}
	return 0
}

// seamWorld is a fake tofu, a probe root and a TMPDIR of the test's own.
type seamWorld struct {
	bin, root, tmp string
	seam           *recoverySeam
}

func newSeamWorld(t *testing.T, applyMode string) *seamWorld {
	t.Helper()
	w := &seamWorld{bin: t.TempDir(), root: filepath.Join(t.TempDir(), "locked-recovery"), tmp: t.TempDir()}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(w.bin, "seam-tofu")); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(w.bin, "apply.mode"), applyMode)
	t.Setenv("TMPDIR", w.tmp)
	t.Setenv("OVH_CLIENT_SECRET", "caller-secret-must-not-pass")
	w.seam = newRecoverySeam(filepath.Join(w.bin, "seam-tofu"), w.root)
	t.Cleanup(w.seam.close)
	return w
}

func (w *seamWorld) calls(t *testing.T) []seamCall {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.bin, "seam.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []seamCall
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var c seamCall
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			t.Fatalf("seam.log line %q: %v", l, err)
		}
		out = append(out, c)
	}
	return out
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// TestRecoverySeamEnvironment: the child's environment and directory, the scratch HOME's life.
// Exposes a child that inherits the caller's environment, a data directory in the checkout, or a
// HOME the next command does not share.
func TestRecoverySeamEnvironment(t *testing.T) {
	w := newSeamWorld(t, "early")
	ctx := context.Background()
	if r, err := w.seam.run(ctx, seamEnv(), false, "init", "-input=false"); err != nil || r.Exit != 0 || r.Signaled {
		t.Fatalf("init = %+v, %v; want exit 0", r, err)
	}
	r, err := w.seam.run(ctx, seamEnv(), false, "plan", "-lock-timeout=0s")
	if err != nil || r.Exit != 1 || r.Signaled {
		t.Errorf("plan = %+v, %v; want exit 1, not signalled, no error", r, err)
	}
	calls := w.calls(t)
	if len(calls) != 2 {
		t.Fatalf("%d starts, want 2", len(calls))
	}
	var home string
	for i, c := range calls {
		if c.Args[0] != "-chdir="+filepath.Join(w.root, "backend") {
			t.Errorf("call %d args %q, want -chdir=<root>/backend first", i, c.Args)
		}
		env := envMap(c.Env)
		keys := slices.Sorted(maps.Keys(env))
		if want := []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "HOME", "PATH", "TF_DATA_DIR"}; !slices.Equal(keys, want) {
			t.Errorf("call %d environment keys %q, want %q", i, keys, want)
		}
		if env["AWS_ACCESS_KEY_ID"] != seamAK || env["AWS_SECRET_ACCESS_KEY"] != seamSK {
			t.Errorf("call %d did not get the state key pair", i)
		}
		if i == 0 {
			home = env["HOME"]
		}
		if env["HOME"] != home || env["TF_DATA_DIR"] != filepath.Join(home, "tofu-data") {
			t.Errorf("call %d HOME %s TF_DATA_DIR %s, want one scratch HOME for the run and its tofu-data", i, env["HOME"], env["TF_DATA_DIR"])
		}
	}
	tmp, _ := filepath.EvalSymlinks(w.tmp)
	real, _ := filepath.EvalSymlinks(home)
	if rel, err := filepath.Rel(tmp, real); err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		t.Errorf("scratch HOME %s is not under TMPDIR %s", home, w.tmp)
	}
	if fi, err := os.Stat(home); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("scratch HOME %v mode %v, want 0700", err, fi)
	}
	if strings.Contains(string(r.Output), seamSK) || strings.Contains(string(r.Output), seamAK) || !strings.Contains(string(r.Output), "[REDACTED]") {
		t.Errorf("output %q, want the key pair redacted", r.Output)
	}
	if !strings.Contains(string(r.Output), "fake tofu secret=") || !strings.Contains(string(r.Output), "fake tofu key=") {
		t.Errorf("output %q, want stdout and stderr", r.Output)
	}
	w.seam.close()
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("scratch HOME after close: %v, want it removed", err)
	}
}

// TestRecoverySeamRefusesKeys: anything but the state key pair is refused before a child starts.
// Exposes an unlock that works only with broader rights, or a credential leaked into a child.
func TestRecoverySeamRefusesKeys(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"admin":   {"AWS_ACCESS_KEY_ID": seamAK, "AWS_SECRET_ACCESS_KEY": seamSK, "OVH_CLIENT_SECRET": "x"},
		"janitor": {"AWS_ACCESS_KEY_ID": seamAK, "AWS_SECRET_ACCESS_KEY": seamSK, "LZ_JANITOR_SECRET": "x"},
		"no pair": {"AWS_ACCESS_KEY_ID": seamAK},
		"empty":   {"AWS_ACCESS_KEY_ID": seamAK, "AWS_SECRET_ACCESS_KEY": ""},
		"none":    {},
	} {
		t.Run(name, func(t *testing.T) {
			w := newSeamWorld(t, "early")
			if _, err := w.seam.run(context.Background(), env, false, "plan"); err == nil {
				t.Errorf("run = nil, want a refusal")
			} else if strings.Contains(err.Error(), seamSK) {
				t.Errorf("refusal carries the secret: %v", err)
			}
			if c := w.calls(t); len(c) != 0 {
				t.Errorf("%d starts, want none", len(c))
			}
		})
	}
}

// TestRecoverySeamKill: writer A's run. Exposes a kill before the lock is held, a death reported
// for a writer that exited by itself, or a wait without bound.
func TestRecoverySeamKill(t *testing.T) {
	t.Run("at-prompt", func(t *testing.T) {
		w := newSeamWorld(t, "prompt")
		start := time.Now()
		r, err := w.seam.run(context.Background(), seamEnv(), true, "apply", "-no-color")
		if err != nil || !r.Signaled || r.Exit != -1 {
			t.Errorf("apply = %+v, %v; want signalled (exit -1), no error", r, err)
		}
		if !strings.Contains(string(r.Output), "Enter a value:") || strings.Contains(string(r.Output), "input closed") {
			t.Errorf("output %q: want the prompt reached with stdin open", r.Output)
		}
		if d := time.Since(start); d > 30*time.Second {
			t.Errorf("took %s", d)
		}
	})
	t.Run("exits-early", func(t *testing.T) {
		w := newSeamWorld(t, "early")
		r, err := w.seam.run(context.Background(), seamEnv(), true, "apply", "-no-color")
		if err != nil || r.Signaled || r.Exit != 0 {
			t.Errorf("apply = %+v, %v; want exit 0, not signalled", r, err)
		}
	})
	t.Run("no-prompt", func(t *testing.T) {
		w := newSeamWorld(t, "silent")
		w.seam.promptWait = 300 * time.Millisecond
		r, err := w.seam.run(context.Background(), seamEnv(), true, "apply", "-no-color")
		if err == nil || r.Signaled {
			t.Errorf("apply = %+v, %v; want an error and no death reported", r, err)
		}
	})
	t.Run("context-ends", func(t *testing.T) {
		w := newSeamWorld(t, "silent")
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		r, err := w.seam.run(ctx, seamEnv(), true, "apply", "-no-color")
		if err == nil || r.Signaled {
			t.Errorf("apply = %+v, %v; want an error and no death reported", r, err)
		}
	})
	t.Run("no-kill-keeps-stdin-closed", func(t *testing.T) {
		// Without kill, stdin is empty: an apply that asks gets its end at once (no hang).
		w := newSeamWorld(t, "prompt")
		r, err := w.seam.run(context.Background(), seamEnv(), false, "apply")
		if err != nil || r.Signaled || r.Exit != 1 {
			t.Errorf("apply without kill = %+v, %v; want exit 1 on closed input", r, err)
		}
	})
}

// TestProbeCheckLockedRecovery (spec 003 T036): the locked-recovery root gets the check with a
// Sweep, and its Observe runs tofu through the seam it is handed, with the state key pair only;
// a root of another name gets none.
func TestProbeCheckLockedRecovery(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]string
	seam := func(_ context.Context, env map[string]string, _ bool, args ...string) (state.TofuRun, error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, env)
		return state.TofuRun{Exit: 1}, nil
	}
	noNet := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	c := probeCheck("locked-recovery", deps{S3HTTP: noNet}, live.API{}, live.Credential{}, nil, seam, "")
	if c == nil || c.Output != "recovery_check" || c.Sweep == nil || c.Observe == nil || c.Undo == nil || c.Validate == nil {
		t.Fatalf("probeCheck(locked-recovery) = %+v, want the locked-recovery check with a Sweep", c)
	}
	env := map[string]string{"run_id": "20261016T111500Z-7c3d", "bucket": "lzprobe-locked-recovery-20261016t111500z-7c3d", "region": "GRA",
		"state_key_id": seamAK, "state_secret": seamSK, "janitor_key_id": "AKJAN", "janitor_secret": "janitor-secret-x"}
	if _, err := c.Observe(context.Background(), env, &strings.Builder{}); err == nil {
		t.Errorf("Observe = nil with no S3 answer, want an error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatalf("Observe ran no tofu through the seam")
	}
	for _, e := range got {
		if len(e) != 2 || e["AWS_ACCESS_KEY_ID"] != seamAK || e["AWS_SECRET_ACCESS_KEY"] != seamSK {
			t.Errorf("seam got keys %q, want the state key pair only", slices.Sorted(maps.Keys(e)))
		}
	}
	if c := probeCheck("locked-recovery-x", deps{}, live.API{}, live.Credential{}, nil, seam, ""); c != nil {
		t.Errorf("probeCheck(locked-recovery-x) = %+v, want none", c)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestRecoverySeamPromptAfterLongOutput (review r1): the prompt is found after more output than
// the seam keeps, and when it arrives split across two writes. Exposes a prompt hidden by the cap.
func TestRecoverySeamPromptAfterLongOutput(t *testing.T) {
	w := &seamOutput{seen: make(chan struct{})}
	w.Write([]byte(strings.Repeat("x", recoveryOutputCap+10)))
	w.Write([]byte("\n  Enter a v"))
	select {
	case <-w.seen:
		t.Fatal("prompt seen before it was complete")
	default:
	}
	w.Write([]byte("alue: "))
	select {
	case <-w.seen:
	default:
		t.Error("prompt after the cap, split across writes, not seen")
	}
	if len(w.bytes()) != recoveryOutputCap {
		t.Errorf("kept %d bytes, want the cap %d", len(w.bytes()), recoveryOutputCap)
	}
}
