package live

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestWithDeadline: T114 — the run's deadline ends the run's context with errDeadline after d
// (production: at is nil); a test's at channel reaches the deadline early, and d still bounds it.
func TestWithDeadline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		at   bool
	}{{"timer", false}, {"timer-with-at-open", true}} {
		t.Run(tc.name, func(t *testing.T) {
			var at chan struct{}
			if tc.at {
				at = make(chan struct{}) // never closed: the timer alone ends ctx
			}
			start := time.Now()
			ctx, cancel := withDeadline(context.Background(), 50*time.Millisecond, at)
			defer cancel()
			if dl, ok := ctx.Deadline(); !ok || dl.Sub(start) > 50*time.Millisecond+time.Second {
				t.Errorf("Deadline() %v %v, want about 50ms from now", dl, ok)
			}
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Second):
				t.Fatal("not ended 50ms after the start")
			}
			if took := time.Since(start); took < 50*time.Millisecond {
				t.Errorf("ended after %v, want at least 50ms", took)
			}
			if !errors.Is(context.Cause(ctx), errDeadline) {
				t.Errorf("cause %v, want errDeadline", context.Cause(ctx))
			}
		})
	}
	t.Run("at-closed", func(t *testing.T) {
		at := make(chan struct{})
		ctx, cancel := withDeadline(context.Background(), time.Hour, at)
		defer cancel()
		select {
		case <-ctx.Done():
			t.Fatalf("ended (%v) before at closed", context.Cause(ctx))
		case <-time.After(100 * time.Millisecond):
		}
		close(at)
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
			t.Fatal("not ended once at closed")
		}
		if !errors.Is(context.Cause(ctx), errDeadline) {
			t.Errorf("cause %v, want errDeadline", context.Cause(ctx))
		}
	})
	t.Run("parent-cancelled", func(t *testing.T) {
		parent, stop := context.WithCancel(context.Background())
		ctx, cancel := withDeadline(parent, time.Hour, make(chan struct{}))
		defer cancel()
		stop()
		<-ctx.Done()
		if errors.Is(context.Cause(ctx), errDeadline) {
			t.Error("a cancelled parent reported as the deadline")
		}
	})
}
