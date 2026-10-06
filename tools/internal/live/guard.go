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
	CondLiveEnv  = "live-env" // live.env missing or without LZ_OWNER_CHECKOUT
	CondCheckout = "checkout" // canonical working directory is not LZ_OWNER_CHECKOUT
	CondWorktree = "worktree" // git dir differs from the common git dir (linked worktree)
	CondDirty    = "dirty"    // git status --porcelain not empty (ignored files excepted)
	CondHead     = "head"     // HEAD differs from --reviewed-sha
	CondOrigin   = "origin"   // HEAD is not an ancestor of origin/main
	CondAccount  = "account"  // GET /auth/details account differs from account.env
	CondEndpoint = "endpoint" // credential endpoint differs from account.env
	CondOrg      = "org"      // manifest org differs from account.env
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

// ExitCode maps an error to the process exit code: 0 for nil, RefusalExit for a Refusal, else 1.
func ExitCode(err error) int {
	var r *Refusal
	switch {
	case err == nil:
		return 0
	case errors.As(err, &r):
		return RefusalExit
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
