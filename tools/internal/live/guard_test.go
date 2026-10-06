package live

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Host guard G10 (FR-011, research R11). Every refusal row fails exactly one guard condition,
// so a guard that skips that condition admits the row.

const (
	headSHA  = "1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b" // HEAD in every scenario
	olderSHA = "0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a" // reachable from origin/main, not HEAD
)

// guardWorld is a temporary host: the owner's main checkout, a linked worktree outside any
// worktrees/ directory, another clone, and symlinks to them.
type guardWorld struct {
	root, main, wt, clone string
}

func newGuardWorld(t *testing.T) guardWorld {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := guardWorld{
		root:  root,
		main:  filepath.Join(root, "owner", "landingzone"),
		wt:    filepath.Join(root, "elsewhere", "wt"),
		clone: filepath.Join(root, "clone"),
	}
	for _, d := range []string{w.main + "/.git/worktrees/wt", w.wt, w.clone + "/.git"} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(w.wt, ".git"), []byte("gitdir: "+w.main+"/.git/worktrees/wt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"link-main": w.main, "link-wt": w.wt, "swapped": w.clone} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// guardCase describes one host; the zero value of each field means "as admitted".
type guardCase struct {
	dir        string      // working directory (default: main)
	owner      string      // LZ_OWNER_CHECKOUT (default: main)
	scenario   string      // fake git scenario (default: main-clean)
	reviewed   string      // --reviewed-sha (default: HEAD)
	liveEnv    string      // live.env content (default: testdata/live.env); "-" = no file
	marker     bool        // /tcb marker present
	offlineEnv string      // LZ_OFFLINE value
	brokenGit  bool        // every git call exits 128
	liveMode   os.FileMode // live.env mode (default 0600)
	markerErr  bool        // the marker cannot be examined (its parent is a file)
}

func (w guardWorld) host(t *testing.T, c guardCase) Host {
	t.Helper()
	or := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	owner := or(c.owner, w.main)
	git := fakeGitDir(t, or(c.scenario, "main-clean"), map[string]string{"@MAIN@": w.main, "@CLONE@": w.clone})
	if c.brokenGit {
		if err := os.Remove(filepath.Join(filepath.Dir(git), "scenario.json")); err != nil {
			t.Fatal(err)
		}
	}
	cfg := t.TempDir()
	liveEnv := filepath.Join(cfg, "live.env")
	switch c.liveEnv {
	case "-":
	case "":
		raw, err := os.ReadFile(filepath.Join("testdata", "live.env"))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, liveEnv, strings.ReplaceAll(string(raw), "@OWNER@", owner))
	default:
		writeFile(t, liveEnv, c.liveEnv)
	}
	if c.liveMode != 0 {
		if err := os.Chmod(liveEnv, c.liveMode); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(cfg, "tcb")
	if c.markerErr {
		writeFile(t, marker, "")
		marker = filepath.Join(marker, "tcb")
	}
	if c.marker {
		if err := os.Mkdir(marker, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"LZ_OFFLINE": c.offlineEnv}
	return Host{
		Dir:           or(c.dir, w.main),
		ReviewedSHA:   or(c.reviewed, headSHA),
		LiveEnv:       liveEnv,
		OfflineMarker: marker,
		Getenv:        func(k string) string { return env[k] },
		Git:           git,
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGuardAdmitsOwnerCheckout(t *testing.T) {
	w := newGuardWorld(t)
	for name, c := range map[string]guardCase{
		// Ignored files (.terraform/, .local/) are present in every scenario.
		"main checkout":             {},
		"main checkout via symlink": {dir: filepath.Join(w.root, "link-main")},
	} {
		t.Run(name, func(t *testing.T) {
			h := w.host(t, c)
			err := h.Check()
			if err != nil || ExitCode(err) != 0 {
				t.Fatalf("Check() = %v (exit %d), want admitted", err, ExitCode(err))
			}
			if len(gitCalls(t, h.Git)) == 0 {
				t.Fatal("admitted without asking git")
			}
		})
	}
}

func TestGuardRefuses(t *testing.T) {
	w := newGuardWorld(t)
	rows := []struct {
		name string
		c    guardCase
		want []string // any of these conditions
	}{
		{"offline-marker", guardCase{marker: true}, []string{CondOffline}},
		{"offline-env", guardCase{offlineEnv: "1"}, []string{CondOffline}},
		// live.env names the linked worktree, so only the git-metadata clause can refuse it.
		{"linked-worktree", guardCase{dir: w.wt, owner: w.wt, scenario: "linked-worktree"}, []string{CondWorktree}},
		{"symlink-to-linked-worktree", guardCase{dir: filepath.Join(w.root, "link-wt"), scenario: "linked-worktree"}, []string{CondCheckout, CondWorktree}},
		// The owner path itself is now a symlink to another clean clone: only symlink
		// resolution of the working directory tells them apart.
		{"symlink-at-owner-path", guardCase{dir: filepath.Join(w.root, "swapped"), owner: filepath.Join(w.root, "swapped"), scenario: "clone-clean"}, []string{CondCheckout}},
		{"other-checkout", guardCase{dir: w.clone, scenario: "clone-clean"}, []string{CondCheckout}},
		{"dirty-modified", guardCase{scenario: "main-modified"}, []string{CondDirty}},
		{"dirty-untracked", guardCase{scenario: "main-untracked"}, []string{CondDirty}},
		// status.showUntrackedFiles=no in the user's git config must not hide them.
		{"dirty-untracked-hidden-by-config", guardCase{scenario: "main-untracked-config"}, []string{CondDirty}},
		// A git failure is a refusal, never an admission.
		{"git-fails", guardCase{brokenGit: true}, []string{CondCheckout, CondWorktree, CondDirty, CondHead, CondOrigin}},
		{"head-mismatch", guardCase{reviewed: olderSHA}, []string{CondHead}},
		{"not-on-origin-main", guardCase{scenario: "main-off-origin"}, []string{CondOrigin}},
		{"live-env-missing", guardCase{liveEnv: "-"}, []string{CondLiveEnv}},
		{"live-env-without-owner", guardCase{liveEnv: "# no owner checkout\nLZ_OTHER=x\n"}, []string{CondLiveEnv}},
		{"live-env-empty-owner", guardCase{liveEnv: "LZ_OWNER_CHECKOUT=\n"}, []string{CondLiveEnv}},
		// T053 review round 1: data-model "the lane refuses group/world-readable files"; whoever
		// can write live.env chooses the checkout the guard admits.
		{"live-env-group-writable", guardCase{liveMode: 0o664}, []string{CondLiveEnv}},
		// A marker that cannot be examined is not proof of being outside the entry.
		{"offline-marker-unreadable", guardCase{markerErr: true}, []string{CondOffline}},
		// HEAD must equal the reviewed SHA, not start with it.
		{"reviewed-sha-abbreviated", guardCase{reviewed: headSHA[:12]}, []string{CondHead}},
		// Index bits hide a modified tracked file from status (host git 2.53.0, t053-gitprobe3.sh).
		{"dirty-assume-unchanged", guardCase{scenario: "main-assume-unchanged"}, []string{CondDirty}},
		{"dirty-skip-worktree", guardCase{scenario: "main-skip-worktree"}, []string{CondDirty}},
		// core.worktree in the shared repository config makes status answer for another tree.
		{"core-worktree-elsewhere", guardCase{scenario: "main-core-worktree"}, []string{CondCheckout}},
		// A failure of any one git call refuses.
		{"status-fails", guardCase{scenario: "main-status-fails"}, []string{CondDirty}},
		{"ls-files-fails", guardCase{scenario: "main-ls-files-fails"}, []string{CondDirty}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			err := w.host(t, r.c).Check()
			assertRefused(t, err, r.want)
		})
	}
}

func assertRefused(t *testing.T, err error, want []string) {
	t.Helper()
	var ref *Refusal
	if !errors.As(err, &ref) {
		t.Fatalf("got %v, want a refusal (%s)", err, strings.Join(want, " or "))
	}
	if !slices.Contains(want, ref.Condition) {
		t.Fatalf("refused for %q (%v), want %s", ref.Condition, err, strings.Join(want, " or "))
	}
	if code := ExitCode(err); code != RefusalExit || RefusalExit != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
}

var guardedVerbs = []string{"bootstrap", "probe", "plan", "apply", "destroy", "chain"}

func TestGuardVerbs(t *testing.T) {
	for _, v := range guardedVerbs {
		if !Guarded(v) {
			t.Errorf("Guarded(%q) = false, want true", v)
		}
	}
	// Credential-free generation runs in authoring worktrees (R11, R16).
	for _, v := range []string{"reconcile", "generate", "stacks:reconcile", "stacks:generate", "", "help", "Plan"} {
		if Guarded(v) {
			t.Errorf("Guarded(%q) = true, want false", v)
		}
	}
}

// TestGuardRunsBeforeCredentials: for a guarded verb the guard finishes before next (the
// credential loader) starts, and a refusal never runs next; an unguarded verb runs next without
// the guard, even on a host the guard would refuse.
func TestGuardRunsBeforeCredentials(t *testing.T) {
	w := newGuardWorld(t)
	for _, verb := range guardedVerbs {
		t.Run(verb+"/admitted", func(t *testing.T) {
			h := w.host(t, guardCase{})
			calls, atLoad := 0, -1
			err := Run(verb, h, func() error {
				calls++
				atLoad = len(gitCalls(t, h.Git))
				return nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("Run = %v, credential loader ran %d times, want nil and 1", err, calls)
			}
			if atLoad == 0 {
				t.Fatal("credentials loaded before the guard asked git anything")
			}
			if after := len(gitCalls(t, h.Git)); after != atLoad {
				t.Fatalf("guard still running after credentials loaded (%d git calls before, %d after)", atLoad, after)
			}
		})
		t.Run(verb+"/refused", func(t *testing.T) {
			h := w.host(t, guardCase{scenario: "main-untracked"})
			loaded := false
			err := Run(verb, h, func() error { loaded = true; return nil })
			if loaded {
				t.Fatal("credentials loaded on a refused host")
			}
			assertRefused(t, err, []string{CondDirty})
		})
	}
	for _, verb := range []string{"reconcile", "generate"} {
		t.Run(verb+"/unguarded", func(t *testing.T) {
			h := w.host(t, guardCase{marker: true, scenario: "main-untracked"})
			ran := false
			if err := Run(verb, h, func() error { ran = true; return nil }); err != nil || !ran {
				t.Fatalf("Run(%q) = %v, ran=%v; want nil, true", verb, err, ran)
			}
			if n := len(gitCalls(t, h.Git)); n != 0 {
				t.Fatalf("unguarded verb ran the guard (%d git calls)", n)
			}
		})
	}
}
