package live

// `lz-live chain -- <instance|all>` (FR-009, FR-010, FR-011, SC-005, research R12, R21; tests
// T046 in chain_test.go, implementation T047): the plan/apply lane's apply of the selected set in
// run order under the run locks, then, from a destroy-on-exit that also fires on an apply
// failure, SIGINT, SIGTERM and the run deadline, the destroy of the ephemeral stacks whose apply
// started (on success every ephemeral stack of the target) in reverse run order, each a saved
// `plan -destroy` judged by protect.go and applied as that file; the inventory appended per
// `apply_complete` event; then the leftover check (leftovers.go) over the retained instances'
// states and the admin exemption, and the summary line. A retained instance is never destroyed.
//
// This file is the stub T046's tests run red against: it applies like `apply` and nothing more.

import (
	"context"
	"os"
	"time"
)

// ChainOptions configures one `lz-live chain` run: the lane's options (Verb is ignored) and the
// run core's deadline, signals and leftover listings.
type ChainOptions struct {
	ApplyOptions
	// Deadline bounds the applies (0: DefaultDeadline); the destroy-on-exit has its own time.
	Deadline time.Duration
	// Signals stop the run as SIGINT/SIGTERM do (nil: those of this process).
	Signals <-chan os.Signal
	// Lister serves the leftover check's listings (tests); nil lists through API with the bound
	// sandbox admin credential.
	Lister Lister
}

// Chain runs one chain; the error maps to the exit code through ExitCode.
func Chain(ctx context.Context, o ChainOptions) error {
	a := o.ApplyOptions
	a.Verb = VerbApply
	return Apply(ctx, a)
}
