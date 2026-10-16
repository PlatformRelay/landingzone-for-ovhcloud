package live

import (
	"context"
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
// max(DefaultDeadline, run), DefaultDeadline when run <= 0. Stub of the signature data-model
// fixes; T023 implements it and gives the chain's and the probe core's destroy-on-exit this budget.
func destroyBudget(run time.Duration) time.Duration {
	return 0 // not implemented
}

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
