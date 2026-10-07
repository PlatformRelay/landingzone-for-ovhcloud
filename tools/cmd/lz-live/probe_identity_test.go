package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P26 tenant binding of a probe's second identity (T075; review r1): the identity a probe root
// publishes for its companion (output companion_env, internal/live probe_identity_test.go) is
// bound to the run's account through GET /auth/details — as lz-live binds the sandbox credential
// (G13) — before any companion child runs. A credential of another account, or one the API
// refuses, stops the run before the companion; the root is still destroyed (the identity with it)
// and the credential does not stay on disk.

const identitySecret = "probe-identity-secret-7e1d22c9a0b4"

// countLines counts the lines of a log (0 when it does not exist).
func countLines(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(raw), "\n")
}

// identityWorld is a probe world whose root net has a companion and publishes a probe identity.
func identityWorld(t *testing.T) *probeWorld {
	t.Helper()
	w := newProbeWorld(t)
	root := filepath.Join(w.checkout, "tests", "live", "probes", "net")
	if err := os.MkdirAll(filepath.Join(root, "companion"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "companion", "main.tf"), []byte("# companion\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, _ := json.Marshal(map[string]string{"OVH_CLIENT_ID": "EU.probeidentity", "OVH_CLIENT_SECRET": identitySecret})
	if err := os.WriteFile(filepath.Join(filepath.Dir(w.git), "companion-env.json"), env, 0o600); err != nil {
		t.Fatal(err)
	}
	return w
}

// noIdentitySecret fails when the identity's secret is on lz-live's output or in any file of the
// config root or the checkout's run records.
func (w *probeWorld) noIdentitySecret(t *testing.T, outputs ...string) {
	t.Helper()
	for _, o := range outputs {
		if strings.Contains(o, identitySecret) {
			t.Error("the identity's secret reached lz-live's output")
		}
	}
	for _, dir := range []string{w.cfg, filepath.Join(w.checkout, ".local")} {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if raw, _ := os.ReadFile(p); strings.Contains(string(raw), identitySecret) {
					t.Errorf("%s holds the identity's secret", p)
				}
			}
			return nil
		})
	}
}

// companionCalls returns the indexes of the tofu calls made in the companion root.
func companionCalls(calls []childCall) []int {
	var out []int
	for i, c := range calls {
		if len(c.Args) > 0 && strings.HasSuffix(filepath.Clean(strings.TrimPrefix(c.Args[0], "-chdir=")), string(filepath.Separator)+"companion") {
			out = append(out, i)
		}
	}
	return out
}

func TestProbeIdentityBinding(t *testing.T) {
	t.Run("bound-before-the-companion", func(t *testing.T) {
		w := identityWorld(t)
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code != 0 {
			t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		calls := w.childCalls(t, "tofu.log")
		comp := companionCalls(calls)
		if len(comp) == 0 {
			t.Fatalf("the companion never ran (tofu calls %v)", subcommands(calls))
		}
		if len(*w.identityAsked) == 0 {
			t.Fatal("the published identity was never bound: no GET /auth/details with its token (P26)")
		}
		if first := (*w.identityAsked)[0]; first > comp[0] {
			t.Errorf("the identity was bound after %d tofu calls, but the companion's first call was call %d: bind before the companion", first, comp[0])
		}
		w.noIdentitySecret(t, stdout, stderr)
	})

	for name, tc := range map[string]struct {
		account  string
		refused  bool
		wantExit int // 0: any non-zero
	}{
		"other-account": {account: "zz99999-ovh", wantExit: 3},
		"binding-error": {account: probeAccount, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			w := identityWorld(t)
			w.identityAccount, w.identityRefused = tc.account, tc.refused
			code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
			if code == 0 || (tc.wantExit != 0 && code != tc.wantExit) {
				t.Errorf("exit %d, want %v (a refused binding of the probe identity)\nstderr:\n%s", code, map[bool]any{true: tc.wantExit, false: "non-zero"}[tc.wantExit != 0], stderr)
			}
			calls := w.childCalls(t, "tofu.log")
			if comp := companionCalls(calls); len(comp) > 0 {
				t.Errorf("the companion ran %d tofu calls with an unbound identity", len(comp))
			}
			destroyed := false
			for _, c := range calls {
				if len(subcommands([]childCall{c})) > 0 && subcommands([]childCall{c})[0] == "destroy" {
					destroyed = true
				}
			}
			if !destroyed {
				t.Error("the root was not destroyed: the probe identity outlives the run")
			}
			w.noIdentitySecret(t, stdout, stderr)
		})
	}
}
