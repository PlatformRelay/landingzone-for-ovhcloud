package live

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Child environment G12 (FR-010, research R6 *Child environment*): a child starts from scratch
// with PATH, HOME and the selected authority's variables; nothing ambient reaches it.

// ambient is the caller's environment, as a .envrc exporting the admin credential would leave
// it. Every value starts with "ambient-" so a leak is visible under any name.
var ambient = map[string]string{
	"OVH_ENDPOINT":            "ambient-endpoint",
	"OVH_CLIENT_ID":           "ambient-admin-client-id",
	"OVH_CLIENT_SECRET":       "ambient-admin-client-secret",
	"OVH_APPLICATION_KEY":     "ambient-application-key",
	"OVH_APPLICATION_SECRET":  "ambient-application-secret",
	"OVH_CONSUMER_KEY":        "ambient-consumer-key",
	"AWS_ACCESS_KEY_ID":       "ambient-aws-access-key",
	"AWS_SECRET_ACCESS_KEY":   "ambient-aws-secret-key",
	"AWS_SESSION_TOKEN":       "ambient-aws-session-token",
	"AWS_PROFILE":             "ambient-aws-profile",
	"TF_CLI_ARGS":             "ambient-tf-cli-args",
	"TF_VAR_state_passphrase": "ambient-state-passphrase",
	"LZ_ENV_CANARY":           "ambient-canary",
	// No OVH_/AWS_/TF_/LZ_ prefix: a prefix filter over the caller's environment keeps them.
	"OS_PASSWORD":   "ambient-openstack-password",
	"SSH_AUTH_SOCK": "ambient-ssh-agent",
}

func TestEnvChildFromScratch(t *testing.T) {
	for k, v := range ambient {
		t.Setenv(k, v)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []Authority{AuthorityTenant, AuthorityPlatform, AuthorityBootstrap} {
		t.Run(string(a), func(t *testing.T) {
			creds := readEnvFile(t, filepath.Join("testdata", "env", string(a)+".env"))
			out := filepath.Join(t.TempDir(), "child.env")
			cmd := Command(a, creds, t.TempDir(), self, fakeChildArg, out)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child: %v: %s", err, output)
			}
			raw, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			child := map[string]string{}
			for _, line := range strings.Split(string(raw), "\n") {
				if k, v, ok := strings.Cut(line, "="); ok {
					child[k] = v
				}
			}
			for k, v := range child {
				if strings.Contains(v, "ambient-") {
					t.Errorf("caller value reached the child: %s=%s", k, v)
				}
			}
			for k := range ambient {
				if _, own := creds[k]; !own {
					if _, ok := child[k]; ok {
						t.Errorf("caller variable %s reached the child", k)
					}
				}
			}
			// From scratch, not filtered: every key is PATH, HOME, PWD (set by os/exec), a
			// TF_* variable lz-live sets itself, or one of the authority's own (R6).
			for k := range child {
				_, own := creds[k]
				if !own && k != "PATH" && k != "HOME" && k != "PWD" && !strings.HasPrefix(k, "TF_") {
					t.Errorf("child carries %s, which is neither PATH, HOME, TF_* nor a %s credential", k, a)
				}
			}
			for k, v := range creds {
				if child[k] != v {
					t.Errorf("child %s = %q, want the %s authority's value", k, child[k], a)
				}
			}
			for _, k := range []string{"PATH", "HOME"} {
				if child[k] == "" {
					t.Errorf("child has no %s", k)
				}
			}
		})
	}
}

// Scratch home (FR-007, T009): no child lz-live starts inherits the caller's HOME. The caller's
// HOME is a sentinel directory, so a leak shows under any variable, not only HOME.

// sentinelHome points the caller's HOME at a fresh directory and returns it.
func sentinelHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "caller-home-sentinel")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home
}

// childHomeNotCallers fails when the child environment env has no HOME, or carries the caller's
// HOME (sentinel), or a path under it, in HOME or any other variable.
func childHomeNotCallers(t *testing.T, child, sentinel string, env []string) {
	t.Helper()
	home := ""
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == "HOME" {
			home = v
		}
		if strings.Contains(v, sentinel) {
			t.Errorf("%s: the caller's HOME reached the child: %s=%s", child, k, v)
		}
	}
	if home == "" {
		t.Errorf("%s: the child has no HOME", child)
	}
}

// TestEnvCommandTakesHome (FR-007, data-model *Fixed signatures*): the exported Command gives the
// child the home it is handed, never the caller's.
func TestEnvCommandTakesHome(t *testing.T) {
	sentinel := sentinelHome(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "child-home")
	out := filepath.Join(t.TempDir(), "child.env")
	cmd := Command(AuthorityTenant, map[string]string{"OVH_CLIENT_ID": "id"}, home, self, fakeChildArg, out)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, output)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	env := strings.Split(string(raw), "\n")
	if !slices.Contains(env, "HOME="+home) {
		var got []string
		for _, kv := range env {
			if strings.HasPrefix(kv, "HOME=") {
				got = append(got, kv)
			}
		}
		t.Errorf("child %v, want HOME=%s (the home Command was handed)", got, home)
	}
	childHomeNotCallers(t, "Command", sentinel, env)
}

// TestEnvChildHomeEveryPath (FR-007): probe, plan, apply, destroy and chain start every child with
// a HOME that is not the caller's. A pin: each path takes the run's scratch home today.
func TestEnvChildHomeEveryPath(t *testing.T) {
	t.Run("probe", func(t *testing.T) {
		w := newRunWorld(t)
		sentinel := sentinelHome(t)
		if err, _ := execute(t, w.runner(t, w.stack(t, "a", true, tofuStack{}))); err != nil {
			t.Fatalf("Execute: %v\n%s", err, w.term.String())
		}
		calls := w.calls(t, "tofu.log")
		// Every child of the run, its destroy-on-exit included, ran.
		for _, cmd := range []string{"init", "plan", "apply", "destroy"} {
			if got := sequence(calls, cmd); !slices.Contains(got, "a") {
				t.Errorf("probe %s %v, want a call on a", cmd, got)
			}
		}
		for _, c := range calls {
			if c.Cmd != "protect" {
				childHomeNotCallers(t, "probe: tofu "+c.Cmd, sentinel, c.Env)
			}
		}
	})
	lane := func(t *testing.T, sentinel, verb string, r laneRun) {
		t.Helper()
		laneExit(t, r, 0)
		calls := r.tofu()
		if len(calls) == 0 {
			t.Errorf("%s started no child", verb)
		}
		for _, c := range calls {
			childHomeNotCallers(t, verb+": tofu "+c.Cmd+" "+c.Stack, sentinel, c.Env)
		}
	}
	t.Run("plan", func(t *testing.T) {
		// Plan follows a steady account with changed credential producers (TestApplyPlanOnly's
		// credential-producers row): on a fresh one the consumers are blocked (exit 2).
		h := newLaneHarness(t)
		h.steady()
		want := []string{"account-governance", "demo-state"}
		for _, id := range want {
			h.touch(id)
		}
		sentinel := sentinelHome(t)
		r := h.run(VerbPlan, TargetAll)
		lane(t, sentinel, "plan", r)
		if got := r.stacksOf("plan"); !slices.Equal(got, want) {
			t.Errorf("planned %v, want %v", got, want)
		}
	})
	t.Run("apply", func(t *testing.T) {
		h := newLaneHarness(t)
		sentinel := sentinelHome(t)
		r := h.run(VerbApply, TargetAll)
		lane(t, sentinel, "apply", r)
		if got := r.stacksOf("apply"); !slices.Equal(got, laneOrder) {
			t.Errorf("applied %v, want %v", got, laneOrder)
		}
	})
	t.Run("destroy", func(t *testing.T) {
		h := newChainHarness(t)
		h.steady()
		sentinel := sentinelHome(t)
		r := h.run(VerbDestroy, chainDestroyOrder[0])
		lane(t, sentinel, "destroy", r)
		if got := r.applies(true); !slices.Equal(got, chainDestroyOrder[:1]) {
			t.Errorf("destroyed %v, want %v", got, chainDestroyOrder[:1])
		}
	})
	t.Run("chain", func(t *testing.T) {
		h := newChainHarness(t)
		sentinel := sentinelHome(t)
		r := h.chain(chainExec{})
		lane(t, sentinel, "chain", r.laneRun)
		if got := r.applies(true); !slices.Equal(got, chainDestroyOrder) {
			t.Errorf("chain destroyed %v, want %v (its destroy-on-exit children judged too)", got, chainDestroyOrder)
		}
	})
}
