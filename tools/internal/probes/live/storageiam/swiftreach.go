package storageiam

// The swift-reach check (spec 003 T029, T030): whether a tenant S3 user's cloud-project
// (OpenStack) user reaches the other tenant's bucket through Keystone and Swift, bypassing the S3
// bucket-ARN policy (spec 005 G6; UNVERIFIED, as is whether Swift serves S3-API buckets at all).
// lz-live runs it on tests/live/probes/swift-reach, whose sensitive output swift_check publishes
// the variables (data-model.md *Probe checks*).
//
// T029 is the stub: the signatures data-model.md fixes, with bodies that return "not implemented"
// (Observe, Undo, Sweep), nil (Validate) or nothing (JudgeReach), and make no request. T030
// implements it.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// SwiftOptions are the swift-reach check's seams; lz-live fills them from the run.
type SwiftOptions struct {
	HTTP  *http.Client     // Keystone, Swift and S3; nil: a default client
	Now   func() time.Time // nil: time.Now
	API   live.API         // Sweep's listings and deletes
	Admin live.Credential  // Sweep: the sandbox admin
}

// SwiftAnswer is one Swift call's answer as JudgeReach weighs it.
type SwiftAnswer struct {
	Status int   // 0: no answer
	Err    error // transport error
}

// JudgeReach: the other bucket's answer next to the own bucket's same-route control.
// "reach" (other 2xx), "denied" (control 2xx, other 401/403, the repeated control with the
// same token 2xx again), "blocked" (anything else, including a repeated control that fails).
func JudgeReach(control, other, repeat SwiftAnswer) string { return "" }

// SwiftReach is the probe check lz-live runs on the swift-reach root.
func SwiftReach(o SwiftOptions) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output:   "swift_check",
		Public:   []string{"auth_url", "bucket_a", "bucket_b", "region", "run_id", "seed_key", "tenant_id", "user_a_name", "user_b_name"},
		Validate: func(env map[string]string) error { return nil },
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			return nil, errors.New("not implemented")
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			return errors.New("not implemented")
		},
	}
}

// sweep is the check's Sweep (data-model.md *Probe checks*): it deletes, by listing the run's
// exact names with the sandbox admin, the buckets and users the root may have created, whether or
// not an output or a state entry exists. ProbeCheck gains its Sweep field in T038; until then
// T030 implements this method and T030 or T038, whichever lands second, sets Sweep: o.sweep.
func (o SwiftOptions) sweep(ctx context.Context, runID, projectID string, term io.Writer) error {
	return errors.New("not implemented")
}
