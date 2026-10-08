package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// The entry point runs the host guard (tools/internal/live) for every verb before anything else
// and maps its refusal to exit 3 (FR-011). Every host fact is injected: HOME is a temporary
// directory, so no test reads the owner's ~/.config/ovh-lz/.

const fakeHead = "1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b"

// TestMain doubles the test binary as a fake git for a clean main checkout whose HEAD is
// fakeHead on origin/main (it logs each call next to itself), and as the fake tofu and ovhcloud of
// probe_test.go.
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case "git":
		os.Exit(fakeGit(os.Args[1:]))
	case "tofu":
		os.Exit(fakeTofu(os.Args[1:]))
	case "ovhcloud":
		os.Exit(fakeOvhcloud(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeGit(args []string) int {
	dir := filepath.Dir(os.Args[0])
	if f, err := os.OpenFile(filepath.Join(dir, "git.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
	}
	for len(args) > 0 {
		if len(args) > 1 && (args[0] == "-c" || args[0] == "-C") {
			args = args[2:]
		} else if strings.HasPrefix(args[0], "--no-") {
			args = args[1:]
		} else {
			break
		}
	}
	cwd, _ := os.Getwd()
	switch {
	case len(args) > 0 && args[0] == "status":
		return 0
	case len(args) > 0 && args[0] == "merge-base":
		return 0
	case len(args) > 0 && args[0] == "ls-files":
		fmt.Print("H README.md\x00")
		return 0
	case len(args) == 3 && args[0] == "worktree" && args[1] == "list" && args[2] == "--porcelain":
		// A dedicated clone (D92): one worktree, no linked ones.
		fmt.Printf("worktree %s\nHEAD %s\nbranch refs/heads/main\n\n", cwd, fakeHead)
		return 0
	case len(args) > 1 && args[0] == "config" && (args[1] == "--get" || len(args) > 2 && args[1] == "--local" && args[2] == "--get"):
		return 1 // unset, as git config --get answers for a key the repository does not set
	case len(args) > 1 && args[0] == "config" && (args[len(args)-1] == "--list" || args[len(args)-1] == "-l"):
		return 0 // no repository configuration
	case len(args) > 0 && args[0] == "rev-parse":
		for _, a := range args[1:] {
			switch a {
			case "--git-dir", "--git-common-dir", "--absolute-git-dir":
				fmt.Println(filepath.Join(cwd, ".git"))
			case "--show-toplevel":
				fmt.Println(cwd)
			case "HEAD", "HEAD^{commit}":
				fmt.Println(fakeHead)
			}
		}
		return 0
	}
	return 128
}

type world struct {
	home, checkout, git, marker string
	env                         map[string]string
}

func newWorld(t *testing.T, withLiveEnv bool) world {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := world{
		home:     filepath.Join(base, "home"),
		checkout: filepath.Join(base, "checkout"),
		git:      filepath.Join(base, "bin", "git"),
		marker:   filepath.Join(base, "tcb"),
		env:      map[string]string{},
	}
	// worktrees/ is the agent worktree root live.env names; the guard requires it to exist (T072).
	for _, d := range []string{w.home, w.checkout + "/.git/objects", filepath.Dir(w.git), filepath.Join(base, "worktrees")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, w.git); err != nil {
		t.Fatal(err)
	}
	if withLiveEnv {
		cfg := filepath.Join(w.home, ".config", "ovh-lz")
		if err := os.MkdirAll(cfg, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg, "live.env"), []byte("LZ_OWNER_CHECKOUT="+w.checkout+"\nLZ_AGENT_WORKTREE_ROOT="+filepath.Join(base, "worktrees")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (w world) run(args ...string) (int, string) {
	var stderr bytes.Buffer
	code := run(args, deps{
		Getwd:         func() (string, error) { return w.checkout, nil },
		Home:          func() (string, error) { return w.home, nil },
		Getenv:        func(k string) string { return w.env[k] },
		Git:           w.git,
		OfflineMarker: w.marker,
		Stderr:        &stderr,
	})
	return code, stderr.String()
}

func (w world) gitCalls(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(w.git), "git.log"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), "\n")
}

var verbs = []string{"bootstrap", "probe", "plan", "apply", "destroy", "chain"}

func TestUsage(t *testing.T) {
	w := newWorld(t, true)
	for _, args := range [][]string{
		nil, {"help"}, {"reconcile", "--reviewed-sha", fakeHead}, {"Plan", "--reviewed-sha", fakeHead},
		{"plan"}, {"plan", "--reviewed-sha"}, {"plan", "--unknown", "x"},
		// probe (T055): a root, or --cleanup <run-id>; never both, never neither.
		{"probe", "--reviewed-sha", fakeHead},
		{"probe", "--reviewed-sha", fakeHead, "tests/live/probes/a", "tests/live/probes/b"},
		{"probe", "--reviewed-sha", fakeHead, "--cleanup", "../x"},
		{"probe", "--reviewed-sha", fakeHead, "--cleanup", "20261006T120000Z-a1b2", "tests/live/probes/a"},
		{"probe", "--reviewed-sha", fakeHead, "--cleanup", "20261006T120000Z-a1b2", "--plan-only"},
		{"probe", "--reviewed-sha", fakeHead, "tests/live/probes/a", "--deadline", "soon"},
		{"probe", "--reviewed-sha", fakeHead, "tests/live/probes/a", "--deadline", "-5m"},
		{"probe", "--reviewed-sha", fakeHead, "tests/live/probes/a", "--deadline", "0s"},
		{"plan", "--reviewed-sha", fakeHead, "--plan-only", "x"},
		{"chain", "--reviewed-sha", fakeHead, "--cleanup", "20261006T120000Z-a1b2"},
	} {
		code, stderr := w.run(args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Errorf("lz-live %q: exit %d, stderr %q; want 2 with usage", args, code, stderr)
		}
	}
	if n := w.gitCalls(t); n != 0 {
		t.Errorf("a usage error ran the guard (%d git calls)", n)
	}
}

// TestEntryGuardsEveryVerb: on a refused host every verb exits 3 naming the condition.
func TestEntryGuardsEveryVerb(t *testing.T) {
	for _, verb := range verbs {
		for name, setup := range map[string]func(*world){
			"offline-marker": func(w *world) { os.Mkdir(w.marker, 0o700) },
			"offline-env":    func(w *world) { w.env["LZ_OFFLINE"] = "1" },
			"head-mismatch":  func(w *world) {},
		} {
			t.Run(verb+"/"+name, func(t *testing.T) {
				w := newWorld(t, true)
				setup(&w)
				sha := fakeHead
				if name == "head-mismatch" {
					sha = strings.Repeat("0a", 20)
				}
				args := []string{verb, "--reviewed-sha", sha}
				if verb == "probe" {
					args = append(args, "tests/live/probes/x")
				}
				if verb == "plan" || verb == "apply" || verb == "destroy" || verb == "chain" {
					args = append(args, "all") // one target (T059, T047)
				}
				code, stderr := w.run(args...)
				want := map[string]string{"offline-marker": "offline", "offline-env": "offline", "head-mismatch": "head"}[name]
				if code != 3 || !strings.Contains(stderr, "("+want+")") {
					t.Fatalf("exit %d, stderr %q; want 3 naming %s", code, stderr, want)
				}
			})
		}
	}
}

// TestEntryReadsLiveEnvFromHome: the guard reads ~/.config/ovh-lz/live.env of the injected home;
// without it the run is refused, with it the guard admits and the verb body runs.
func TestEntryReadsLiveEnvFromHome(t *testing.T) {
	w := newWorld(t, false)
	code, stderr := w.run("chain", "--reviewed-sha", fakeHead, "all")
	if code != 3 || !strings.Contains(stderr, "(live-env)") {
		t.Fatalf("no live.env: exit %d, stderr %q; want 3 naming live-env", code, stderr)
	}
	w = newWorld(t, true)
	code, stderr = w.run("chain", "--reviewed-sha", fakeHead, "stage-x")
	if w.gitCalls(t) == 0 {
		t.Fatal("admitted without asking git")
	}
	// Admitted, the chain body runs (T047): this world's checkout has no stacks/deployments.yaml,
	// so it fails (exit 1) past the guard, never 0 and never a guard refusal.
	if code != 1 || strings.Contains(stderr, "(live-env)") {
		t.Fatalf("admitted: exit %d, stderr %q; want 1 from the chain body", code, stderr)
	}
}

// checkoutSnapshot maps every path of the checkout outside .local/ (the run record) and .git/ to
// its content; directories end in "/".
func checkoutSnapshot(t *testing.T, checkout string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(checkout, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(checkout, p)
		if rel == ".local" || rel == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		raw, err := os.ReadFile(p)
		out[rel] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestProbeRerunEntry (T073; found in T008, evidence/T008.md decision requests 3 and 4): the
// owner probes a root a second time under a new run id, then cleans up the first run. Both
// initialise without "Backend configuration changed" (the backend path changes per run id; tofu
// keeps the previous one in its data directory) and without deleting or writing anything in the
// checkout: each invocation hands its children a TF_DATA_DIR of its own outside the checkout. Every
// child receives TF_VAR_run_id (the invocation's run id) and TF_VAR_project_id (account.env's
// LZ_PROJECT_ID_STATE, the project the run sheet compares the probes with; not the first
// LZ_PROJECT_ID_* in order). Without LZ_PROJECT_ID_STATE no child runs.
func TestProbeRerunEntry(t *testing.T) {
	t.Run("second-run-and-cleanup", func(t *testing.T) {
		w := newProbeWorld(t)
		writePrivate(t, filepath.Join(w.cfg, "accounts", probeAccount, "account.env"),
			"LZ_ACCOUNT_ID="+probeAccount+"\nOVH_ENDPOINT=ovh-eu\nLZ_ORG=demo\nLZ_PROJECT_ID_DEMO_DEV=p2\nLZ_PROJECT_ID_STATE=p1\nLZ_ADMIN_POLICY_ID=pol-admin\n")
		unchanged := func(phase string, before map[string]string) {
			t.Helper()
			after := checkoutSnapshot(t, w.checkout)
			for p, v := range before {
				if got, ok := after[p]; !ok {
					t.Errorf("%s: the checkout lost %s", phase, p)
				} else if got != v {
					t.Errorf("%s: the checkout's %s changed", phase, p)
				}
			}
			for p := range after {
				if _, ok := before[p]; !ok {
					t.Errorf("%s: a run wrote %s into the checkout", phase, p)
				}
			}
		}
		before := checkoutSnapshot(t, w.checkout)
		marker := filepath.Join(filepath.Dir(w.git), "destroy-fails")
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		code, stdout1, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code == 0 {
			t.Fatalf("run 1: a probe whose destroy failed exited 0:\n%s", stdout1)
		}
		id1 := runIDOf(t, stdout1)
		if strings.Contains(stderr, "Backend") {
			t.Fatalf("run 1 failed in init, not in its destroy:\n%s", stderr)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		n1 := len(w.childCalls(t, "tofu.log"))
		unchanged("run 1", before)
		// What a later in-place init of this root left behind (another run id's backend; gitignored,
		// not lz-live's to delete): neither run 2 nor the cleanup of run 1 may trip over it.
		stale := filepath.Join(w.checkout, "tests", "live", "probes", "net", ".terraform")
		if err := os.MkdirAll(stale, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stale, "terraform.tfstate"), []byte(filepath.Join(w.probeDir("20261006T123000Z-ffff"), "net.tfstate")), 0o644); err != nil {
			t.Fatal(err)
		}
		before = checkoutSnapshot(t, w.checkout)
		w.now = time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC)
		code, stdout2, stderr2 := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		id2 := runIDOf(t, stdout2)
		if code != 0 || id2 == id1 {
			t.Errorf("run 2 of the same root under run id %s (run 1: %s): exit %d\nstdout:\n%s\nstderr:\n%s", id2, id1, code, stdout2, stderr2)
		}
		n2 := len(w.childCalls(t, "tofu.log"))
		code, stdout3, stderr3 := w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id1)
		if code != 0 {
			t.Errorf("cleanup of run 1 after run 2: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout3, stderr3)
		}
		for _, out := range []string{stdout1, stdout2, stdout3, stderr2, stderr3} {
			for _, msg := range []string{"Backend configuration changed", "Backend initialization required"} {
				if strings.Contains(out, msg) {
					t.Errorf("a run's tofu said %q:\n%s", msg, out)
				}
			}
		}
		calls := w.childCalls(t, "tofu.log")
		runs := []struct {
			name, id string
			calls    []childCall
			want     []string
		}{
			{"run 1", id1, calls[:n1], []string{"init", "plan", "show", "show", "apply", "plan-destroy", "show-destroy", "destroy"}},
			{"run 2", id2, calls[n1:n2], []string{"init", "plan", "show", "show", "apply", "plan-destroy", "show-destroy", "destroy"}},
			{"cleanup of run 1", id1, calls[n2:], []string{"init", "plan-destroy", "show-destroy", "destroy"}},
		}
		dataDirs := make([]string, len(runs))
		for i, r := range runs {
			if got := subcommands(r.calls); !slices.Equal(got, r.want) {
				t.Errorf("%s: tofu calls %v, want %v", r.name, got, r.want)
			}
			for _, c := range r.calls {
				env := c.env()
				// Where the data directory really is (the fake resolves -chdir and symlinks), so
				// neither a relative path nor a symlink hides a directory in the checkout.
				dd := c.DataDir
				rel, err := filepath.Rel(w.checkout, dd)
				switch {
				case env["TF_DATA_DIR"] == "" || dd == "":
					t.Errorf("%s: tofu %v without TF_DATA_DIR: want a data directory of its own", r.name, c.Args)
				case err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)):
					t.Errorf("%s: tofu %v with its data directory %s inside the checkout", r.name, c.Args, dd)
				case dataDirs[i] == "":
					dataDirs[i] = dd
				case dataDirs[i] != dd:
					t.Errorf("%s: tofu %v with TF_DATA_DIR %s, earlier calls %s: one data directory per run", r.name, c.Args, dd, dataDirs[i])
				}
				if env["TF_VAR_run_id"] != r.id {
					t.Errorf("%s: tofu %v with TF_VAR_run_id %q, want %q", r.name, c.Args, env["TF_VAR_run_id"], r.id)
				}
				if env["TF_VAR_project_id"] != "p1" {
					t.Errorf("%s: tofu %v with TF_VAR_project_id %q, want LZ_PROJECT_ID_STATE p1", r.name, c.Args, env["TF_VAR_project_id"])
				}
			}
		}
		if dataDirs[1] != "" && (dataDirs[1] == dataDirs[0] || dataDirs[1] == dataDirs[2]) {
			t.Errorf("run 2 shares its data directory %s with run 1 or its cleanup", dataDirs[1])
		}
		unchanged("run 2 and the cleanup", before)
		w.noSecret(t, stdout1, stdout2, stdout3, stderr, stderr2, stderr3)
	})

	t.Run("no-state-project", func(t *testing.T) {
		w := newProbeWorld(t)
		writePrivate(t, filepath.Join(w.cfg, "accounts", probeAccount, "account.env"),
			"LZ_ACCOUNT_ID="+probeAccount+"\nOVH_ENDPOINT=ovh-eu\nLZ_ORG=demo\nLZ_PROJECT_ID_DEMO_DEV=p2\nLZ_ADMIN_POLICY_ID=pol-admin\n")
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code == 0 || !strings.Contains(stderr, "LZ_PROJECT_ID_STATE") {
			t.Errorf("no LZ_PROJECT_ID_STATE: exit %d, stderr %q; want a failure naming LZ_PROJECT_ID_STATE", code, stderr)
		}
		if n := len(w.childCalls(t, "tofu.log")); n != 0 {
			t.Errorf("%d tofu calls without a project id", n)
		}
		w.noSecret(t, stdout, stderr)
	})
}

// TestProbePlanOnlyRoots (T073; T008 run sheet rows 1 and 3, review r1): `project-import` imports
// the retained sandbox project and `quota` changes an account singleton, so either applied would
// leave a destroy that fails (prevent_destroy) or semantics nobody verified. lz-live refuses both
// without --plan-only (exit 3, naming --plan-only) before any child runs and before any probe file
// is written, however the root is spelled (trailing slash, absolute path, symlink alias); with
// --plan-only they plan and apply nothing.
func TestProbePlanOnlyRoots(t *testing.T) {
	for name, root := range map[string]func(t *testing.T, w *probeWorld) string{
		"quota":          func(*testing.T, *probeWorld) string { return "tests/live/probes/quota" },
		"project-import": func(*testing.T, *probeWorld) string { return "tests/live/probes/project-import" },
		"trailing-slash": func(*testing.T, *probeWorld) string { return "tests/live/probes/quota/" },
		"absolute": func(_ *testing.T, w *probeWorld) string {
			return filepath.Join(w.checkout, "tests", "live", "probes", "project-import")
		},
		"symlink-alias": func(t *testing.T, w *probeWorld) string {
			if err := os.Symlink("project-import", filepath.Join(w.checkout, "tests", "live", "probes", "pi")); err != nil {
				t.Fatal(err)
			}
			return "tests/live/probes/pi"
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newProbeWorld(t)
			for _, r := range []string{"quota", "project-import"} {
				dir := filepath.Join(w.checkout, "tests", "live", "probes", r)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte("# probe\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			arg := root(t, w)
			code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, arg)
			if code != 3 || !strings.Contains(stderr, "--plan-only") {
				t.Errorf("%s without --plan-only: exit %d, stderr %q; want 3 naming --plan-only", arg, code, stderr)
			}
			if n := len(w.childCalls(t, "tofu.log")); n != 0 {
				t.Errorf("%d tofu calls before the refusal", n)
			}
			if left, _ := os.ReadDir(filepath.Join(w.cfg, "accounts", probeAccount, "state", "probes")); len(left) != 0 {
				t.Errorf("the refused probe left probe files %v", left)
			}
			w.noSecret(t, stdout, stderr)

			code, stdout, stderr = w.run(t, "probe", "--reviewed-sha", fakeHead, arg, "--plan-only")
			got := subcommands(w.childCalls(t, "tofu.log"))
			if code != 0 || !slices.Contains(got, "plan") || slices.Contains(got, "apply") || slices.Contains(got, "destroy") {
				t.Errorf("%s --plan-only: exit %d, tofu calls %v; want a plan, no apply, no destroy\nstdout:\n%s\nstderr:\n%s", arg, code, got, stdout, stderr)
			}
		})
	}
}

// treeOf maps every path under dir (relative; directories end in "/") to its content; a missing dir
// is empty.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		raw, err := os.ReadFile(p)
		out[rel] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sameTreeAs fails for every path added, changed or lost between two treeOf snapshots.
func sameTreeAs(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	for p, v := range before {
		if got, ok := after[p]; !ok {
			t.Errorf("%s lost %s", what, p)
		} else if got != v {
			t.Errorf("%s: %s changed", what, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("the refused run wrote %s into %s", p, what)
		}
	}
}

func mkdirPrivate(t *testing.T, d string) string {
	t.Helper()
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

// failedProbe runs tests/live/probes/net with a failing destroy, so its state, passphrase and
// probe.env stay for a cleanup, and returns its run id; later destroys work again.
func (w *probeWorld) failedProbe(t *testing.T) string {
	t.Helper()
	marker := filepath.Join(filepath.Dir(w.git), "destroy-fails")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
	if code == 0 {
		t.Fatalf("a probe whose destroy failed exited 0:\n%s", stdout)
	}
	id := runIDOf(t, stdout)
	if _, err := os.Stat(filepath.Join(w.probeDir(id), "passphrase.env")); err != nil {
		t.Fatalf("the failed probe kept no passphrase: %v\n%s", err, stderr)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestProbeScratchOutsideCheckout (T077; T074 review r1 gap): the run's scratch HOME and the tofu
// data directories in it are made under $TMPDIR. A TMPDIR that resolves inside the checkout
// (.local/ included: gitignored, so the guard's clean-tree check does not see it; symlinks
// resolved) is refused (exit 3, naming TMPDIR) before any credential (no API call; refused before
// sandbox.env is read), any file (no run record, no probe file, nothing in TMPDIR, the checkout
// unchanged) and any child, for a probe and a --cleanup alike. A TMPDIR beside the checkout, a
// prefix sibling included, is admitted and used.
func TestProbeScratchOutsideCheckout(t *testing.T) {
	for name, c := range map[string]struct {
		place     func(t *testing.T, w *probeWorld) string
		noSandbox bool
		cleanup   bool
	}{
		"local-tmp": {place: func(t *testing.T, w *probeWorld) string {
			return mkdirPrivate(t, filepath.Join(w.checkout, ".local", "tmp"))
		}},
		"checkout-itself": {place: func(_ *testing.T, w *probeWorld) string { return w.checkout }},
		"probe-root": {place: func(t *testing.T, w *probeWorld) string {
			return mkdirPrivate(t, filepath.Join(w.checkout, "tests", "live", "probes", "net", "tmp"))
		}},
		"symlink-alias": {place: func(t *testing.T, w *probeWorld) string {
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(mkdirPrivate(t, filepath.Join(w.checkout, ".local", "tmp")), alias); err != nil {
				t.Fatal(err)
			}
			return alias
		}},
		"no-sandbox-env": {place: func(t *testing.T, w *probeWorld) string {
			return mkdirPrivate(t, filepath.Join(w.checkout, ".local", "tmp"))
		}, noSandbox: true},
		"cleanup": {place: func(t *testing.T, w *probeWorld) string {
			return mkdirPrivate(t, filepath.Join(w.checkout, ".local", "tmp"))
		}, cleanup: true},
	} {
		t.Run(name, func(t *testing.T) {
			w := newProbeWorld(t)
			args := []string{"probe", "--reviewed-sha", fakeHead, "tests/live/probes/net"}
			if c.cleanup {
				args = []string{"probe", "--reviewed-sha", fakeHead, "--cleanup", w.failedProbe(t)}
			}
			if c.noSandbox {
				if err := os.Remove(filepath.Join(w.cfg, "sandbox.env")); err != nil {
					t.Fatal(err)
				}
			}
			tmp := c.place(t, w)
			tofuN, ovhN, apiN, listN := len(w.childCalls(t, "tofu.log")), len(w.childCalls(t, "ovhcloud.log")), *w.apiCalls, len(*w.listed)
			checkout := checkoutSnapshot(t, w.checkout)
			local := treeOf(t, filepath.Join(w.checkout, ".local"))
			state := treeOf(t, filepath.Join(w.cfg, "accounts", probeAccount, "state"))
			// The credentials this run could read: the sandbox credential and, for a cleanup, the
			// run's passphrase. Watched after the snapshots (which read them) and read before
			// anything else opens them (review r1: a read whose result is discarded leaves no
			// other trace, and a missing sandbox.env cannot tell a read from no read).
			var creds []string
			if !c.noSandbox {
				creds = append(creds, filepath.Join(w.cfg, "sandbox.env"))
			}
			if c.cleanup {
				creds = append(creds, filepath.Join(w.probeDir(args[4]), "passphrase.env"))
			}
			opened := watchOpens(t, creds...)
			t.Setenv("TMPDIR", tmp)
			w.env["TMPDIR"] = tmp
			code, stdout, stderr := w.run(t, args...)
			if got := opened(); len(got) != 0 {
				t.Errorf("the refused run read the credential files %v", got)
			}
			if code != 3 || !strings.Contains(stderr, "TMPDIR") {
				t.Errorf("TMPDIR %s: exit %d, stderr %q; want 3 naming TMPDIR", tmp, code, stderr)
			}
			if n := len(w.childCalls(t, "tofu.log")) - tofuN; n != 0 {
				t.Errorf("%d tofu calls before the refusal", n)
			}
			if n := len(w.childCalls(t, "ovhcloud.log")) - ovhN; n != 0 {
				t.Errorf("%d ovhcloud calls before the refusal", n)
			}
			if n := len(*w.listed) - listN; n != 0 {
				t.Errorf("%d leftover listings before the refusal", n)
			}
			if n := *w.apiCalls - apiN; n != 0 {
				t.Errorf("%d API calls (the credential was used) before the refusal", n)
			}
			sameTreeAs(t, "the checkout", checkout, checkoutSnapshot(t, w.checkout))
			sameTreeAs(t, "the checkout's .local", local, treeOf(t, filepath.Join(w.checkout, ".local")))
			sameTreeAs(t, "the account's probe state", state, treeOf(t, filepath.Join(w.cfg, "accounts", probeAccount, "state")))
			w.noSecret(t, stdout, stderr)
		})
	}

	t.Run("prefix-sibling", func(t *testing.T) {
		w := newProbeWorld(t)
		tmp := mkdirPrivate(t, w.checkout+"-tmp")
		sandbox := filepath.Join(w.cfg, "sandbox.env")
		opened := watchOpens(t, sandbox)
		t.Setenv("TMPDIR", tmp)
		w.env["TMPDIR"] = tmp
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code != 0 {
			t.Fatalf("TMPDIR %s beside the checkout: exit %d\nstdout:\n%s\nstderr:\n%s", tmp, code, stdout, stderr)
		}
		// The watch sees a credential read (else the refused rows' "no read" proves nothing).
		if got := opened(); opensObservable && !slices.Contains(got, sandbox) {
			t.Errorf("an admitted run's read of sandbox.env was not observed (opened %v)", got)
		}
		calls := w.childCalls(t, "tofu.log")
		if len(calls) == 0 {
			t.Fatal("no tofu call")
		}
		for _, c := range calls {
			if h := resolvedPath(c.env()["HOME"]); !strings.HasPrefix(h, resolvedPath(tmp)+string(filepath.Separator)) {
				t.Errorf("tofu %v ran with HOME %s, not under TMPDIR %s", c.Args, h, tmp)
			}
		}
	})
}

// TestProbeCleanupRecordedProjectEntry (T077; T074 review r1/r2 gap): `lz-live probe` records the
// project it ran against (LZ_PROJECT_ID_STATE at start) in the run's probe.env; `--cleanup
// <run-id>` destroys with that recorded id (TF_VAR_project_id) and lists leftovers in that project
// even when account.env now names another; a run record without a project id is refused for
// cleanup (exit 3 naming the project id, T078): no child, the probe's files kept.
func TestProbeCleanupRecordedProjectEntry(t *testing.T) {
	t.Run("account-env-changed", func(t *testing.T) {
		w := newProbeWorld(t)
		id := w.failedProbe(t)
		rec, err := live.ReadCredentialFile(filepath.Join(w.probeDir(id), "probe.env"))
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, v := range rec {
			found = found || v == "p1"
		}
		if !found {
			t.Errorf("probe.env %v records no project id p1 (LZ_PROJECT_ID_STATE at start)", rec)
		}
		writePrivate(t, filepath.Join(w.cfg, "accounts", probeAccount, "account.env"),
			"LZ_ACCOUNT_ID="+probeAccount+"\nOVH_ENDPOINT=ovh-eu\nLZ_ORG=demo\nLZ_PROJECT_ID_STATE=p9\nLZ_ADMIN_POLICY_ID=pol-admin\n")
		tofuN, listN := len(w.childCalls(t, "tofu.log")), len(*w.listed)
		pass := filepath.Join(w.probeDir(id), "passphrase.env")
		opened := watchOpens(t, pass)
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
		if code != 0 {
			t.Errorf("cleanup after account.env changed: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		// Positive control of the passphrase watch of TestProbeScratchOutsideCheckout/cleanup.
		if got := opened(); opensObservable && !slices.Contains(got, pass) {
			t.Errorf("a cleanup's read of passphrase.env was not observed (opened %v)", got)
		}
		calls := w.childCalls(t, "tofu.log")[tofuN:]
		if got := subcommands(calls); !slices.Equal(got, []string{"init", "plan-destroy", "show-destroy", "destroy"}) {
			t.Errorf("cleanup tofu calls %v, want [init plan-destroy show-destroy destroy]", got)
		}
		for _, c := range calls {
			if got := c.env()["TF_VAR_project_id"]; got != "p1" {
				t.Errorf("cleanup: tofu %v with TF_VAR_project_id %q, want the recorded p1 (account.env now names p9)", c.Args, got)
			}
		}
		asked := map[string]bool{}
		for _, l := range (*w.listed)[listN:] {
			asked[l.Path] = true
		}
		if !asked["/v1/cloud/project/p1/network/private"] {
			t.Errorf("the cleanup's leftover check never listed the recorded project p1 (asked %v)", asked)
		}
		w.noSecret(t, stdout, stderr)
	})

	t.Run("record-without-project", func(t *testing.T) {
		w := newProbeWorld(t)
		id := w.failedProbe(t)
		path := filepath.Join(w.probeDir(id), "probe.env")
		rec, err := live.ReadCredentialFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for k, v := range rec {
			if v != "p1" {
				lines = append(lines, k+"="+v+"\n")
			}
		}
		slices.Sort(lines)
		writePrivate(t, path, strings.Join(lines, ""))
		tofuN := len(w.childCalls(t, "tofu.log"))
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
		// "project id": the temporary directories carry the test name, so "project" alone would
		// match any path in an unrelated error (review r1). A refusal (exit 3), as a recorded root
		// outside the probes (T078, coordinator decision).
		if code != 3 || !strings.Contains(stderr, "project id") {
			t.Errorf("cleanup of a run record without a project id: exit %d, stderr %q; want 3 naming the project id", code, stderr)
		}
		if n := len(w.childCalls(t, "tofu.log")) - tofuN; n != 0 {
			t.Errorf("%d tofu calls for a run record without a project id", n)
		}
		for _, f := range []string{"passphrase.env", "probe.env", "net.tfstate"} {
			if _, err := os.Stat(filepath.Join(w.probeDir(id), f)); err != nil {
				t.Errorf("the refused cleanup lost %s: %v", f, err)
			}
		}
		w.noSecret(t, stdout, stderr)
	})
}

// TestLiveLaneRunsProbeTests (T073; T008 gap "probe tests in no CI target"): every probe root
// with a `tofu test` control (*.tftest.hcl) runs it under `task test:live-lane`, through the unit
// runner (a command `go -C tools run ./cmd/lz-check … unit tests/live/probes/<root>`), next to
// the Go controls of the live lane. A static check of the wiring (layout as protect_test.go reads
// it); that the probe tests pass is the entry run of the target.
// A root added without its line, or a line left in a comment, fails here.
func TestLiveLaneRunsProbeTests(t *testing.T) {
	repo := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(repo, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(raw), "\n  test:live-lane:\n")
	if !ok {
		t.Fatal("Taskfile.yml defines no test:live-lane")
	}
	var cmds []string
	for _, l := range strings.Split(block, "\n") {
		if strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "   ") && strings.TrimSpace(l) != "" {
			break // the next target or its comment
		}
		if c, ok := strings.CutPrefix(l, "      - "); ok {
			cmds = append(cmds, c)
		}
	}
	if !slices.ContainsFunc(cmds, func(c string) bool {
		return strings.HasPrefix(c, "go -C tools test ") && strings.Contains(c, "./internal/live") && strings.Contains(c, "./cmd/lz-live")
	}) {
		t.Errorf("test:live-lane %q no longer runs the Go controls of ./internal/live and ./cmd/lz-live", cmds)
	}
	// Every directory below tests/live/probes holding a *.tftest.hcl, nested ones included (T076: a
	// root's companion/ carries its own tofu test, which the root's unit run does not reach).
	probes := filepath.Join(repo, "tests", "live", "probes")
	roots := map[string]bool{}
	err = filepath.WalkDir(probes, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".tftest.hcl") {
			rel, err := filepath.Rel(probes, filepath.Dir(p))
			if err != nil {
				return err
			}
			roots[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !roots["alerting"] || !roots["storage-iam/companion"] {
		t.Fatalf("found probe tests in %v only: the discovery misses tests/live/probes/alerting or storage-iam/companion", roots)
	}
	for root := range roots {
		re := regexp.MustCompile(` unit tests/live/probes/` + regexp.QuoteMeta(root) + `/?$`) // the unit argument, nothing after it
		if !slices.ContainsFunc(cmds, func(c string) bool {
			return strings.HasPrefix(c, "go -C tools run ./cmd/lz-check ") && strings.Contains(c, " unit ") && re.MatchString(c)
		}) {
			t.Errorf("test:live-lane runs no `lz-check … unit tests/live/probes/%s`: its tofu test controls run in no target", root)
		}
	}
}
