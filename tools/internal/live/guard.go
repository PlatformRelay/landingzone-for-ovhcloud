// Package live is the live lane's safety core (spec 005 FR-010, FR-011): the host guard, the
// child environment, the account binding and the credential files. Everything that touches the
// host, git or the OVHcloud API is injected, so the tests run offline against fakes.
package live

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// RefusalExit is the exit code of every guard refusal.
const RefusalExit = 3

// Conditions a refusal names.
const (
	CondOffline  = "offline"  // inside the offline entry (/tcb present or LZ_OFFLINE=1)
	CondLiveEnv  = "live-env" // live.env missing, without LZ_OWNER_CHECKOUT, or without an existing absolute LZ_AGENT_WORKTREE_ROOT
	CondCheckout = "checkout" // canonical working directory is not LZ_OWNER_CHECKOUT
	CondWorktree = "worktree" // git dir differs from the common git dir (linked worktree)
	// D92: live runs start only from a dedicated clone (T071 tests, T072 enforces).
	CondLinked    = "linked-worktrees" // the git dir lists a linked worktree (worktrees/ entry or worktree list)
	CondShared    = "shared"           // alternates, core.sharedRepository, or a git dir outside the checkout
	CondAgentRoot = "agent-root"       // checkout at or under LZ_AGENT_WORKTREE_ROOT
	CondDirty     = "dirty"            // git status --porcelain not empty (ignored files excepted)
	CondHead      = "head"             // HEAD differs from --reviewed-sha
	CondOrigin    = "origin"           // HEAD is not an ancestor of origin/main
	CondAccount   = "account"          // GET /auth/details account differs from account.env
	CondEndpoint  = "endpoint"         // credential endpoint differs from account.env
	CondOrg       = "org"              // manifest org differs from account.env
)

// Refusal is a guard refusal; the process exits with RefusalExit.
type Refusal struct {
	Condition string
	Detail    string
}

func (r *Refusal) Error() string { return fmt.Sprintf("refused (%s): %s", r.Condition, r.Detail) }

func refuse(cond, format string, a ...any) error {
	return &Refusal{Condition: cond, Detail: fmt.Sprintf(format, a...)}
}

// BlockedExit is the exit code of a missing prerequisite (contracts/checks.md `lz-live`).
const BlockedExit = 2

// Blocked is a missing prerequisite the owner supplies before a re-run (a bootstrap phase
// `blocked`); the process exits with BlockedExit.
type Blocked struct {
	Phase  string
	Detail string
}

func (b *Blocked) Error() string { return fmt.Sprintf("blocked (%s): %s", b.Phase, b.Detail) }

// ExitCode maps an error to the process exit code: 0 for nil, RefusalExit for a Refusal,
// BlockedExit for a Blocked, else 1. A refusal wins over a joined failure.
func ExitCode(err error) int {
	var r *Refusal
	var b *Blocked
	switch {
	case err == nil:
		return 0
	case errors.As(err, &r):
		return RefusalExit
	case errors.As(err, &b):
		return BlockedExit
	default:
		return 1
	}
}

// Host is what the guard reads. Production fills it from the process; tests point every path at
// a temporary directory and a fake git.
type Host struct {
	Dir           string              // working directory, as given (may be a symlink)
	ReviewedSHA   string              // --reviewed-sha
	LiveEnv       string              // path of ~/.config/ovh-lz/live.env
	OfflineMarker string              // path whose existence means "inside lz-offline" (/tcb)
	Getenv        func(string) string // caller environment (LZ_OFFLINE)
	Git           string              // git executable
}

// originMain is the remote-tracking ref by its full name: a local branch or tag named
// origin/main would otherwise shadow it.
const originMain = "refs/remotes/origin/main"

// git runs one git command in dir and returns its standard output. The caller's GIT_* variables
// and the global and system configuration never reach it, replace refs are off, and
// core.fsmonitor (which the repository's own config, shared by every linked worktree, could point
// at a program answering status) is disabled, as are the stat shortcuts (core.checkStat=minimal
// with core.trustctime=false lets a same-size edit with a restored mtime read as clean), so the
// answers describe dir as committed.
func (h Host) git(dir string, args ...string) (string, error) {
	argv := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false",
		"-c", "core.checkStat=default", "-c", "core.trustctime=true"}, args...)
	cmd := exec.Command(h.Git, argv...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"LC_ALL=C",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_NO_REPLACE_OBJECTS=1",
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// Check runs the host guard (research R11). It returns a *Refusal naming the first failed
// condition, or nil when the host is admitted. Every git failure is a refusal.
func (h Host) Check() error {
	// Only a marker that is known to be absent admits; one that cannot be examined refuses.
	if _, err := os.Lstat(h.OfflineMarker); !errors.Is(err, fs.ErrNotExist) {
		return refuse(CondOffline, "%s present or not examinable (%v)", h.OfflineMarker, err)
	}
	if h.Getenv("LZ_OFFLINE") == "1" {
		return refuse(CondOffline, "LZ_OFFLINE=1")
	}
	// A credential-class read: whoever can write live.env chooses the admitted checkout, so a
	// file another user can read or write is refused (data-model: all files 0600).
	liveEnv, err := ReadCredentialFile(h.LiveEnv)
	if err != nil {
		return refuse(CondLiveEnv, "%v", err)
	}
	// Compared as written: the owner names the canonical path (FR-011); resolving it too would
	// admit a symlink swapped in at the owner's path.
	owner := liveEnv["LZ_OWNER_CHECKOUT"]
	if owner == "" {
		return refuse(CondLiveEnv, "%s has no LZ_OWNER_CHECKOUT", h.LiveEnv)
	}
	dir, err := filepath.EvalSymlinks(h.Dir)
	if err != nil {
		return refuse(CondCheckout, "%v", err)
	}
	if dir != owner {
		return refuse(CondCheckout, "%s is not the owner's checkout %s", dir, owner)
	}
	// D92: no live run starts at or below the agent worktree root. The root must be an existing
	// directory, so a mistyped root cannot turn the clause off (T072); it is resolved, so a
	// symlinked or dotted root still covers its target.
	agents := liveEnv["LZ_AGENT_WORKTREE_ROOT"]
	if agents == "" || !filepath.IsAbs(agents) {
		return refuse(CondLiveEnv, "%s has no absolute LZ_AGENT_WORKTREE_ROOT", h.LiveEnv)
	}
	if fi, err := os.Stat(agents); err != nil {
		return refuse(CondLiveEnv, "LZ_AGENT_WORKTREE_ROOT %s: %v", agents, err)
	} else if !fi.IsDir() {
		return refuse(CondLiveEnv, "LZ_AGENT_WORKTREE_ROOT %s is not a directory", agents)
	}
	agents = canonicalPath(agents)
	// Inside the root by path segment: Rel answers "." for the root itself and a path that does
	// not climb out of it for anything below (worktrees-owner/ beside worktrees/ climbs out).
	if rel, err := filepath.Rel(agents, dir); err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return refuse(CondAgentRoot, "%s lies under the agent worktree root %s", dir, agents)
	}
	out, err := h.git(dir, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir", "--show-toplevel")
	if err != nil {
		return refuse(CondWorktree, "%v", err)
	}
	dirs := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(dirs) != 3 || canonicalPath(dirs[0]) != canonicalPath(dirs[1]) {
		return refuse(CondWorktree, "linked worktree (git dir and common dir %q)", dirs)
	}
	// core.worktree in the repository config would make status answer for another tree.
	if canonicalPath(dirs[2]) != dir {
		return refuse(CondCheckout, "git work tree is %s, not %s", dirs[2], dir)
	}
	// D92: a private .git — a directory inside the checkout, not a gitfile to a git dir elsewhere.
	gitDir := filepath.Join(dir, ".git")
	if fi, err := os.Lstat(gitDir); err != nil || !fi.IsDir() || canonicalPath(dirs[0]) != gitDir {
		return refuse(CondShared, "git dir %s is not the private %s", dirs[0], gitDir)
	}
	// D92: no linked worktree registered on disk (git does not list an entry without a gitdir
	// file) nor listed by git.
	entries, err := os.ReadDir(filepath.Join(gitDir, "worktrees"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return refuse(CondLinked, "%v", err)
	}
	if len(entries) > 0 {
		return refuse(CondLinked, "%s/worktrees has %d entries", gitDir, len(entries))
	}
	out, err = h.git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return refuse(CondLinked, "%v", err)
	}
	if n := strings.Count("\n"+out, "\nworktree "); n != 1 {
		return refuse(CondLinked, "git worktree list shows %d worktrees", n)
	}
	// D92: objects of no other repository, and no group-shared repository.
	if _, err := os.Lstat(filepath.Join(gitDir, "objects", "info", "alternates")); !errors.Is(err, fs.ErrNotExist) {
		return refuse(CondShared, "alternates present or not examinable (%v)", err)
	}
	// An object store of its own: no symlink at or below objects/, which would share another
	// repository's objects without alternates (T072 review rounds 1 and 2: objects/ itself, a
	// pack/ or fan-out directory). WalkDir does not follow symlinks; a missing objects/ is an
	// error, and an objects that is a file was refused by the alternates Lstat above (ENOTDIR).
	if err := filepath.WalkDir(filepath.Join(gitDir, "objects"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", p)
		}
		return nil
	}); err != nil {
		return refuse(CondShared, "objects not private: %v", err)
	}
	if shared, set, err := h.gitConfig(dir, "core.sharedRepository"); err != nil {
		return refuse(CondShared, "%v", err)
	} else if set && shared != "umask" && shared != "false" && shared != "0" {
		return refuse(CondShared, "core.sharedRepository=%s", shared)
	}
	// An explicit untracked mode: status.showUntrackedFiles in the repository's config must not
	// hide untracked files. Ignored files are excepted (no --ignored).
	out, err = h.git(dir, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return refuse(CondDirty, "%v", err)
	}
	if strings.TrimSpace(out) != "" {
		return refuse(CondDirty, "uncommitted changes or untracked files")
	}
	// status does not see changes to files marked assume-unchanged (lowercase tag) or
	// skip-worktree ("S"): only "H" (cached, no bit) entries are admitted.
	out, err = h.git(dir, "ls-files", "-v", "-z")
	if err != nil {
		return refuse(CondDirty, "%v", err)
	}
	for _, entry := range strings.Split(strings.TrimSuffix(out, "\x00"), "\x00") {
		if entry != "" && !strings.HasPrefix(entry, "H ") {
			return refuse(CondDirty, "index entry flagged %q: status cannot see its changes", entry[:1])
		}
	}
	out, err = h.git(dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return refuse(CondHead, "%v", err)
	}
	if head := strings.TrimSpace(out); head != h.ReviewedSHA {
		return refuse(CondHead, "HEAD %s is not the reviewed SHA %q", head, h.ReviewedSHA)
	}
	if _, err := h.git(dir, "merge-base", "--is-ancestor", h.ReviewedSHA, originMain); err != nil {
		return refuse(CondOrigin, "%s is not reachable from %s: %v", h.ReviewedSHA, originMain, err)
	}
	return nil
}

// gitConfig reads one key of the repository's configuration: git config --get exits 1 for an
// unset key, which is not a failure.
func (h Host) gitConfig(dir, key string) (string, bool, error) {
	out, err := h.git(dir, "config", "--get", key)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

func canonicalPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// guarded are the lz-live verbs that load credentials (R11). Credential-free generation
// (stacks:reconcile, stacks:generate) runs in authoring worktrees and is not guarded.
var guarded = []string{"bootstrap", "probe", "plan", "apply", "destroy", "chain"}

// Guarded reports whether a lz-live verb runs the host guard.
func Guarded(verb string) bool { return slices.Contains(guarded, verb) }

// Run runs the host guard for a guarded verb, then next (which loads credentials). A refusal
// returns before next runs.
func Run(verb string, h Host, next func() error) error {
	if Guarded(verb) {
		if err := h.Check(); err != nil {
			return err
		}
	}
	return next()
}
