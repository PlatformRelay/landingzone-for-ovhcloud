package live

import (
	"context"
	"fmt"
	"time"
)

// DefaultDeadline bounds a live run when no --deadline is given (FR-011: default 45 min, then
// the destroy-on-exit fires).
const DefaultDeadline = 45 * time.Minute

// withDeadline bounds ctx by d from now, ending it with errDeadline as its cause. When at is
// non-nil (tests only: production passes nil), its closing also ends ctx as the deadline does, so a
// test stops the run at the call it chooses without waiting through a deadline long enough for
// every machine to get there (T114); d still bounds the run and, in the caller, the destroy.
func withDeadline(ctx context.Context, d time.Duration, at <-chan struct{}) (context.Context, context.CancelFunc) {
	c, cancelTimeout := context.WithTimeoutCause(ctx, d, errDeadline)
	if at == nil {
		return c, cancelTimeout
	}
	c2, cancel := context.WithCancelCause(c)
	go func() {
		select {
		case <-at:
			cancel(errDeadline)
		case <-c2.Done():
		}
	}()
	return c2, func() { cancel(context.Canceled); cancelTimeout() }
}

// destroyBudget is the destroy-on-exit's budget for a run given deadline run (spec 006 FR-002):
// max(DefaultDeadline, run), DefaultDeadline when run <= 0. A run deadline shorter than
// DefaultDeadline never shortens the destroy; a longer one is kept.
func destroyBudget(run time.Duration) time.Duration {
	return max(DefaultDeadline, run)
}

// destroyBudgetSpent is the error a destroy-on-exit reports when its context ended while it ran,
// whether or not a call returned an error for it: what it did not reach stays for --cleanup.
func destroyBudgetSpent(dctx context.Context, budget time.Duration) error {
	if dctx.Err() == nil {
		return nil
	}
	return fmt.Errorf("destroy-on-exit: its budget of %v ran out (%w); what it did not destroy or check stays for --cleanup", budget, context.Cause(dctx))
}

// retryBudget is the one retry budget a leftover check shares across every retained instance it
// reads (spec 006 FR-008, data-model *Retry budget*): it ends at end, and a wait is admitted only
// while reserve (the read and the remaining listings) is still left after it. Stub of the signature
// data-model fixes; T024 implements it and has readRetained ask it before every wait.
type retryBudget struct{}

// newRetryBudget is the budget ending at end that keeps reserve. Stub (T024).
func newRetryBudget(end time.Time, reserve time.Duration) *retryBudget {
	return &retryBudget{} // not implemented
}

// admit reports whether a wait of wait starting at now leaves at least the reserve before the end:
// end - now - wait >= reserve. Stub (T024).
func (b *retryBudget) admit(now time.Time, wait time.Duration) bool {
	return false // not implemented
}

// waitClock is the time the leftover check's state-read retries read and wait on: Now against the
// destroy context's deadline, After for a wait. nil is the wall clock (production); tests pass a
// clock that skips the waits and starts late, so a budget of tens of minutes runs out without
// waiting for it (spec 006 FR-008, T019).
type waitClock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// wallClock is the production waitClock.
type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// destroyContext is the destroy-on-exit's context: detached from ctx's cancellation and bounded by
// budget from now. When at is non-nil (tests only: production passes nil), its closing ends the
// context as the budget running out does, so a test exhausts a 45-minute budget at the call it
// chooses without waiting for it (spec 006 FR-002, T018). Through at, Err() is Canceled and
// context.Cause is DeadlineExceeded: a caller telling the two apart reads the cause.
func destroyContext(ctx context.Context, budget time.Duration, at <-chan struct{}) (context.Context, context.CancelFunc) {
	c, cancelTimeout := context.WithTimeout(context.WithoutCancel(ctx), budget)
	if at == nil {
		return c, cancelTimeout
	}
	c2, cancel := context.WithCancelCause(c)
	go func() {
		select {
		case <-at:
			cancel(context.DeadlineExceeded)
		case <-c2.Done():
		}
	}()
	return c2, func() { cancel(context.Canceled); cancelTimeout() }
}
