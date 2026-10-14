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
