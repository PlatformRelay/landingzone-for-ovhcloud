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

// guardWorld is a temporary host (D92): the owner's dedicated live clone (main: private .git, no
// linked worktrees), the owner's day-to-day checkout with a linked worktree outside any worktrees/
// directory (shared), another clone, the agent worktree root holding an agent's clone, a clone
// whose path only shares a prefix with that root, a clone made with --separate-git-dir, and
// symlinks to them.
type guardWorld struct {
	root, main, shared, wt, clone, agents, agentClone, sibling, sep, sepGit string
}

func newGuardWorld(t *testing.T) guardWorld {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := guardWorld{
		root:       root,
		main:       filepath.Join(root, "owner", "lz-live"),
		shared:     filepath.Join(root, "owner", "landingzone"),
		wt:         filepath.Join(root, "elsewhere", "wt"),
		clone:      filepath.Join(root, "clone"),
		agents:     filepath.Join(root, "worktrees"),
		agentClone: filepath.Join(root, "worktrees", "landingzone", "agent-clone"),
		sibling:    filepath.Join(root, "worktrees-owner", "lz-live"),
		sep:        filepath.Join(root, "sep"),
		sepGit:     filepath.Join(root, "sep.git"),
	}
	for _, d := range []string{w.main + "/.git/objects/info", w.shared + "/.git/worktrees/wt", w.shared + "/.git/objects", w.wt, w.clone + "/.git/objects",
		w.agentClone + "/.git/objects", w.sibling + "/.git/objects", w.sep, w.sepGit + "/objects"} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// As git worktree add and clone --separate-git-dir write them (t071-gitprobe.sh).
	writeFile(t, filepath.Join(w.wt, ".git"), "gitdir: "+w.shared+"/.git/worktrees/wt\n")
	writeFile(t, filepath.Join(w.shared, ".git", "worktrees", "wt", "gitdir"), w.wt+"/.git\n")
	writeFile(t, filepath.Join(w.sep, ".git"), "gitdir: "+w.sepGit+"\n")
	for link, target := range map[string]string{"link-main": w.main, "link-wt": w.wt, "swapped": w.clone, "link-agents": w.agents} {
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
	agentRoot  string      // LZ_AGENT_WORKTREE_ROOT in the default live.env (default: agents)
	sharedRepo string      // core.sharedRepository in scenario main-shared-config (default "1")
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
	dir := or(c.dir, w.main)
	git := fakeGitDir(t, or(c.scenario, "main-clean"), map[string]string{
		"@MAIN@": w.main, "@CLONE@": w.clone, "@SHARED@": w.shared, "@SEPGIT@": w.sepGit,
		"@ROOT@": w.root, "@DIR@": canonical(dir), "@SHAREDREPO@": or(c.sharedRepo, "1"),
	})
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
		text := strings.ReplaceAll(string(raw), "@OWNER@", owner)
		writeFile(t, liveEnv, strings.ReplaceAll(text, "@AGENTS@", or(c.agentRoot, w.agents)))
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
		Dir:           dir,
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
		// Ignored files (.terraform/, .local/) are present in every scenario. D92: the owner's
		// checkout is a dedicated clone with a private .git and no linked worktrees.
		"dedicated clone":             {},
		"dedicated clone via symlink": {dir: filepath.Join(w.root, "link-main")},
		// worktrees-owner/ shares only a name prefix with the agent root worktrees/.
		"dedicated clone beside the agent root": {dir: w.sibling, owner: w.sibling, scenario: "dir-clean"},
		// T072: an agent root given through a symlink to an existing directory.
		"agent root via symlink": {agentRoot: filepath.Join(w.root, "link-agents")},
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

// TestGuardSeparateClone (T071, operator decision D92): live runs start only from a dedicated
// clone. An agent worktree shares the owner's .git, so every guarded verb refuses a checkout that
// is a linked worktree, whose git dir lists a linked worktree, whose objects or git dir are shared,
// or that lies under the agent worktree root, before any credential loads. Each row fails exactly
// one of these clauses; the fixtures follow host git 2.53.0 (t071-gitprobe.sh, t071-gitprobe2.sh).
func TestGuardSeparateClone(t *testing.T) {
	rows := []struct {
		name string
		c    func(w guardWorld) guardCase
		prep func(t *testing.T, w guardWorld)
		want []string
	}{
		{name: "linked-worktree", want: []string{CondWorktree},
			c: func(w guardWorld) guardCase { return guardCase{dir: w.wt, owner: w.wt, scenario: "linked-worktree"} }},
		// The behavioural red of D92: the owner's main checkout, clean, with an agent's linked
		// worktree registered in its .git/worktrees/ (listed by git worktree list too).
		{name: "main-with-linked-worktrees", want: []string{CondLinked},
			c: func(w guardWorld) guardCase { return guardCase{dir: w.shared, owner: w.shared, scenario: "dir-clean"} }},
		// A worktrees/ entry without a gitdir file is not listed by git worktree list
		// (t071-gitprobe2.sh): only the directory says it is there.
		{name: "worktrees-entry-unlisted", want: []string{CondLinked},
			prep: func(t *testing.T, w guardWorld) { mkdir(t, filepath.Join(w.main, ".git", "worktrees", "half")) }},
		// A worktree whose directory was removed without git worktree remove: listed as prunable.
		{name: "worktrees-entry-prunable", want: []string{CondLinked},
			prep: func(t *testing.T, w guardWorld) {
				mkdir(t, filepath.Join(w.main, ".git", "worktrees", "gone"))
				writeFile(t, filepath.Join(w.main, ".git", "worktrees", "gone", "gitdir"), w.root+"/gone/.git\n")
			}},
		// A worktrees/ that cannot be listed is not proof that it is empty.
		{name: "worktrees-not-listable", want: []string{CondLinked, CondShared},
			prep: func(t *testing.T, w guardWorld) { writeFile(t, filepath.Join(w.main, ".git", "worktrees"), "") }},
		// git worktree list shows a second worktree although no worktrees/ entry is on disk.
		{name: "worktree-list-two", want: []string{CondLinked},
			c: func(w guardWorld) guardCase { return guardCase{scenario: "main-listed-worktree"} }},
		// A failing git worktree list is a refusal, never an admission.
		{name: "worktree-list-fails", want: []string{CondLinked},
			c: func(w guardWorld) guardCase { return guardCase{scenario: "main-worktree-fails"} }},
		// clone --shared and clone --reference write objects/info/alternates (t071-gitprobe.sh).
		{name: "alternates", want: []string{CondShared},
			prep: func(t *testing.T, w guardWorld) {
				writeFile(t, filepath.Join(w.main, ".git", "objects", "info", "alternates"), w.clone+"/.git/objects\n")
			}},
		// init --shared=group stores core.sharedRepository=1 (t071-gitprobe.sh).
		{name: "shared-repository-config", want: []string{CondShared},
			c: func(w guardWorld) guardCase { return guardCase{scenario: "main-shared-config"} },
			prep: func(t *testing.T, w guardWorld) {
				writeFile(t, filepath.Join(w.main, ".git", "config"), "[core]\n\tsharedRepository = 1\n")
			}},
		// Other values git accepts for a shared repository (git-config(1): group/true, all/world/
		// everybody, an octal mode; init --shared=all stores 2) — review round 2.
		{name: "shared-repository-all", want: []string{CondShared},
			c: func(w guardWorld) guardCase { return guardCase{scenario: "main-shared-config", sharedRepo: "2"} }},
		{name: "shared-repository-group-word", want: []string{CondShared},
			c: func(w guardWorld) guardCase { return guardCase{scenario: "main-shared-config", sharedRepo: "group"} }},
		{name: "shared-repository-octal", want: []string{CondShared},
			c: func(w guardWorld) guardCase { return guardCase{scenario: "main-shared-config", sharedRepo: "0660"} }},
		// .git/objects is a symlink into another repository's objects: shared without alternates
		// (T072 review round 1).
		{name: "objects-symlink", want: []string{CondShared},
			prep: func(t *testing.T, w guardWorld) {
				if err := os.RemoveAll(filepath.Join(w.main, ".git", "objects")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(w.clone, ".git", "objects"), filepath.Join(w.main, ".git", "objects")); err != nil {
					t.Fatal(err)
				}
			}},
		// A real objects directory whose pack/ is a symlink into another repository: git loads
		// packs through it, so the objects are shared all the same (T072 review round 2).
		{name: "objects-pack-symlink", want: []string{CondShared},
			prep: func(t *testing.T, w guardWorld) {
				if err := os.Symlink(filepath.Join(w.clone, ".git", "objects"), filepath.Join(w.main, ".git", "objects", "pack")); err != nil {
					t.Fatal(err)
				}
			}},
		// An objects that is a file holds no symlink, but is no object store of its own either.
		{name: "objects-is-file", want: []string{CondShared},
			prep: func(t *testing.T, w guardWorld) {
				if err := os.RemoveAll(filepath.Join(w.main, ".git", "objects")); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(w.main, ".git", "objects"), "")
			}},
		// No objects directory to examine is not proof of a private one.
		{name: "objects-missing", want: []string{CondShared},
			prep: func(t *testing.T, w guardWorld) {
				if err := os.RemoveAll(filepath.Join(w.main, ".git", "objects")); err != nil {
					t.Fatal(err)
				}
			}},
		// A failing git config (exit 128, not the exit 1 of an unset key) is a refusal, never an
		// admission (T072 review round 1).
		{name: "shared-repository-config-fails", want: []string{CondShared},
			c: func(w guardWorld) guardCase { return guardCase{scenario: "main-config-fails"} }},
		// .git is a symlink to another repository's git dir, so two checkouts share one .git;
		// rev-parse prints the resolved git dir and the checkout as top level (t071-gitprobe3.sh)
		// (review round 2).
		{name: "git-dir-symlink", want: []string{CondShared, CondWorktree},
			c: func(w guardWorld) guardCase { return guardCase{scenario: "separate-git-dir"} },
			prep: func(t *testing.T, w guardWorld) {
				if err := os.RemoveAll(filepath.Join(w.main, ".git")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(w.sepGit, filepath.Join(w.main, ".git")); err != nil {
					t.Fatal(err)
				}
			}},
		// clone --separate-git-dir: .git is a file and the git dir lives outside the checkout, so
		// the .git is not private to it.
		{name: "separate-git-dir", want: []string{CondShared, CondWorktree},
			c: func(w guardWorld) guardCase { return guardCase{dir: w.sep, owner: w.sep, scenario: "separate-git-dir"} }},
		// An agent's own clone (private .git, no linked worktrees) under the agent worktree root.
		{name: "under-agent-root", want: []string{CondAgentRoot},
			c: func(w guardWorld) guardCase {
				return guardCase{dir: w.agentClone, owner: w.agentClone, scenario: "dir-clean"}
			}},
		{name: "agent-root-via-symlink", want: []string{CondAgentRoot},
			c: func(w guardWorld) guardCase {
				return guardCase{dir: w.agentClone, owner: w.agentClone, scenario: "dir-clean", agentRoot: filepath.Join(w.root, "link-agents")}
			}},
		{name: "agent-root-is-checkout", want: []string{CondAgentRoot},
			c: func(w guardWorld) guardCase { return guardCase{agentRoot: w.main} }},
		{name: "agent-root-above-checkout-via-dotdot", want: []string{CondAgentRoot},
			c: func(w guardWorld) guardCase { return guardCase{agentRoot: w.agents + "/../owner"} }},
		// Every checkout lies under /: a root+"/" prefix test reads "//" and admits it (review r1).
		{name: "agent-root-is-filesystem-root", want: []string{CondAgentRoot, CondLiveEnv},
			c: func(w guardWorld) guardCase { return guardCase{agentRoot: "/"} }},
		// Fail closed: without an absolute root the clause cannot be judged.
		{name: "agent-root-missing", want: []string{CondLiveEnv, CondAgentRoot},
			c: func(w guardWorld) guardCase { return guardCase{liveEnv: "LZ_OWNER_CHECKOUT=" + w.main + "\n"} }},
		{name: "agent-root-empty", want: []string{CondLiveEnv, CondAgentRoot},
			c: func(w guardWorld) guardCase {
				return guardCase{liveEnv: "LZ_OWNER_CHECKOUT=" + w.main + "\nLZ_AGENT_WORKTREE_ROOT=\n"}
			}},
		{name: "agent-root-relative", want: []string{CondLiveEnv, CondAgentRoot},
			c: func(w guardWorld) guardCase { return guardCase{agentRoot: "worktrees"} }},
		// T072, coordinator decision on T071's request: the configured root must exist as a
		// directory, so a mistyped root does not turn the clause off silently.
		// A relative root that exists from the test's working directory (the package directory):
		// still refused as relative, not judged against the process's directory (review round 1).
		{name: "agent-root-relative-existing", want: []string{CondLiveEnv},
			c: func(w guardWorld) guardCase { return guardCase{agentRoot: "testdata"} }},
		{name: "agent-root-absent", want: []string{CondLiveEnv},
			c: func(w guardWorld) guardCase { return guardCase{agentRoot: filepath.Join(w.root, "worktres")} }},
		{name: "agent-root-is-file", want: []string{CondLiveEnv},
			c:    func(w guardWorld) guardCase { return guardCase{agentRoot: filepath.Join(w.root, "agents-file")} },
			prep: func(t *testing.T, w guardWorld) { writeFile(t, filepath.Join(w.root, "agents-file"), "") }},
		{name: "agent-root-dangling-symlink", want: []string{CondLiveEnv},
			c: func(w guardWorld) guardCase { return guardCase{agentRoot: filepath.Join(w.root, "link-gone")} },
			prep: func(t *testing.T, w guardWorld) {
				if err := os.Symlink(filepath.Join(w.root, "gone"), filepath.Join(w.root, "link-gone")); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "agent-root-symlink-to-file", want: []string{CondLiveEnv},
			c: func(w guardWorld) guardCase { return guardCase{agentRoot: filepath.Join(w.root, "link-file")} },
			prep: func(t *testing.T, w guardWorld) {
				writeFile(t, filepath.Join(w.root, "agents-file"), "")
				if err := os.Symlink(filepath.Join(w.root, "agents-file"), filepath.Join(w.root, "link-file")); err != nil {
					t.Fatal(err)
				}
			}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			w := newGuardWorld(t)
			if r.prep != nil {
				r.prep(t, w)
			}
			c := guardCase{}
			if r.c != nil {
				c = r.c(w)
			}
			for _, verb := range guardedVerbs {
				t.Run(verb, func(t *testing.T) {
					loaded := false
					err := Run(verb, w.host(t, c), func() error { loaded = true; return nil })
					if loaded {
						t.Fatal("credentials loaded on a refused host")
					}
					assertRefused(t, err, r.want)
				})
			}
		})
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
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
