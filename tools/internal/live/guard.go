// Package live is the live lane's safety core (spec 005 FR-010, FR-011): the host guard, the
// child environment and the account binding. Everything that touches the host, git or the
// OVHcloud API is injected, so the tests run offline against fakes.
//
// This file is a stub written with the tests (T052): it admits everything. T053 implements it.
package live

import "fmt"

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

// ExitCode maps an error to the process exit code: 0 for nil, RefusalExit for a Refusal, else 1.
func ExitCode(err error) int {
	return 0
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

// Check runs the host guard (research R11). It returns a *Refusal naming the first failed
// condition, or nil when the host is admitted.
func (h Host) Check() error {
	return nil
}

// Guarded reports whether a lz-live verb runs the host guard.
func Guarded(verb string) bool {
	return false
}

// Run runs the host guard for a guarded verb, then next (which loads credentials). A refusal
// returns before next runs.
func Run(verb string, h Host, next func() error) error {
	return next()
}
