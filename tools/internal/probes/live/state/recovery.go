package state

// The locked-recovery check (spec 003 T035, T036): whether a stale S3-lockfile lock on an
// Object-Locked (governance) state bucket can be recovered with the state credential alone
// (FR-005 C005.4, C005.8): writer A's `tofu apply` is killed holding the lock, writer B is refused,
// the pinned `tofu force-unlock` runs only after the seam proves A died, and B then acquires the
// lock; beside it, whether a conditional PutObject (`If-None-Match: *`) is honoured and whether an
// earlier state version can be restored by copy (UNVERIFIED on OVHcloud, research.md). lz-live
// runs it on tests/live/probes/locked-recovery, whose sensitive output recovery_check publishes
// the variables (data-model.md *Probe checks*).
//
// T035 is the stub: the signatures data-model.md fixes, with bodies that return "not implemented"
// (Observe, Undo, sweep) or nil (Validate), and make no request and run no tofu. T036 implements
// it.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// RecoveryOptions are the locked-recovery check's seams; lz-live fills them from the run.
type RecoveryOptions struct {
	HTTP *http.Client     // supporting S3 controls; nil: the S3 client's default
	Now  func() time.Time // nil: time.Now
	// Tofu runs the pinned OpenTofu on the disposable backend root; env holds the state key
	// pair only (no OVH_*, no janitor key). lz-live supplies it (scratch HOME, own TF_DATA_DIR,
	// redacted output). kill: start the command, wait until it waits at its approval prompt
	// (holding the lock), SIGKILL it and report how it exited.
	Tofu  func(ctx context.Context, env map[string]string, kill bool, args ...string) (TofuRun, error)
	API   live.API        // Sweep's listings and deletes
	Admin live.Credential // Sweep: the sandbox admin
}

// TofuRun is how one tofu invocation ended.
type TofuRun struct {
	Exit     int
	Signaled bool   // exited on the SIGKILL the seam sent: the owner-death proof
	Output   []byte // redacted; classified against the fixtures of OpenTofu 1.13.0's diagnostics
}

// LockedRecovery is the probe check lz-live runs on the locked-recovery root.
func LockedRecovery(o RecoveryOptions) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output:   "recovery_check",
		Public:   []string{"bucket", "region", "run_id"},
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
// exact names with the sandbox admin, the bucket lzprobe-locked-recovery-<lower(run id)> and the
// users lzprobe-locked-recovery-<run id> and lzprobe-locked-recovery-janitor-<run id> the root may
// have created, whether or not an output or a state entry exists. ProbeCheck gains its Sweep field
// in T038; until then T036 implements this method and T036 or T038, whichever lands second, sets
// Sweep: o.sweep.
func (o RecoveryOptions) sweep(ctx context.Context, runID, projectID string, term io.Writer) error {
	return errors.New("not implemented")
}
