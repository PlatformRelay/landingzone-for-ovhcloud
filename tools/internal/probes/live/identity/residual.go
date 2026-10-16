// Package identity holds the identity probes' checks (spec 003 T031–T034): what an OVHcloud OAuth2
// identity may still do, observed by lz-live after the probe root's apply.
package identity

// The token-residual check (spec 003 T031, T032): how long an access token already issued to an
// OAuth2 client stays usable after the client is deleted (FR-007, C007.5, C007.6; UNVERIFIED until
// T026). lz-live runs it on tests/live/probes/token-residual, whose sensitive output
// residual_check publishes run_id, project_id and read_path (data-model.md *Probe checks*). The
// check, not the root, creates and deletes the client and policy lzprobe-token-residual-<run id>.
//
// T031 is the stub: the signatures data-model.md fixes, with bodies that return "not implemented"
// (Observe, Undo, sweep), nil (Validate) or the zero value (ResidualWindow), and make no request.
// T032 implements it.

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// Options are the identity checks' seams; lz-live fills them from the run.
type Options struct {
	API      live.API
	Admin    live.Credential                              // creates, lists and deletes the disposable clients and policies
	Bind     func(context.Context, live.Credential) error // nil: binds nothing
	Now      func() time.Time
	Sleep    func(context.Context, time.Duration) error // nil: a timer that honours ctx
	Interval time.Duration                              // between polls and control retries; zero: 30 s
	Settle   time.Duration                              // the bound on control retries (both checks); zero: 5 min
}

// Poll is one granted read with the issued token.
type Poll struct {
	At     time.Time
	Status int // 0: no answer
	Err    error
}

// Residual is the judged window of a token whose client was deleted.
type Residual struct {
	Observed  string        // "refused-before-expiry", "valid-until-expiry", "blocked"
	Reason    string        // blocked: "expiry", "control", "outages", "deadline"
	Lower     time.Duration // delete to the last allowed poll ("valid ≥ Lower")
	Upper     time.Duration // delete to the first refusal; zero when no refusal bounds the window
	ExpiresIn time.Duration // the validated expires_in of the token answer
}

// ResidualWindow judges only observed polls: an outage bounds nothing, and no validity is
// inferred across a tail of outages or past a deadline stop.
func ResidualWindow(issued, deleted time.Time, expiresIn time.Duration, control Poll, polls []Poll, deadlineHit bool) Residual {
	return Residual{}
}

// TokenResidual is the probe check lz-live runs on the token-residual root.
func TokenResidual(o Options) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output:   "residual_check",
		Public:   []string{"project_id", "read_path", "run_id"},
		Validate: func(env map[string]string) error { return nil },
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			return nil, errors.New("not implemented")
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			return errors.New("not implemented")
		},
	}
}

// sweep is the check's Sweep (data-model.md *Probe checks*): it deletes, by listing the exact name
// lzprobe-token-residual-<run id> with the sandbox admin, the client and policy the check may have
// created, whether or not an output or a record exists. ProbeCheck gains its Sweep field in T038;
// until then T032 implements this method and T032 or T038, whichever lands second, sets
// Sweep: o.sweep.
func (o Options) sweep(ctx context.Context, runID, projectID string, term io.Writer) error {
	return errors.New("not implemented")
}
