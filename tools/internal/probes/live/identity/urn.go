package identity

// The iam-urn check (spec 003 T033, T034): which URN OVHcloud checks an `account:apiovh:iam/*`
// action against, the project's or the account's (FR-006 C006.2, C006.5; FR-008 C008.1; D109:
// `account:apiovh:iam/resource/edit` granted on a project URN did not authorise tagging that
// project, so the account URN is the likely target, UNVERIFIED until T027). lz-live runs it on
// tests/live/probes/iam-urn, whose sensitive output urn_check publishes run_id, project_urn,
// account_urn, bucket = lzprobe-iam-urn-<lower(run id)> (the only resource the root owns, the
// write's target) and bucket_urn (data-model.md *Probe checks*). The check, not the root, creates
// and owns the identities and policies lzprobe-iam-urn-<run id>-project|account.
//
// T033 is the stub: the signatures data-model.md fixes, with bodies that return "not implemented"
// (Observe, Undo, sweep), nil (Validate) or the zero value (JudgeURN), and make no request. T034
// implements it.

import (
	"context"
	"errors"
	"io"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// URNAnswer is one identity's answer to a control or a probed call.
type URNAnswer struct {
	Status  int
	Actions []string // the IAM actions a refusal named (live.APIChecker)
	Err     error
}

// JudgeURN, once per probed action: "project", "account", "either" (both allowed) or "blocked"; a
// control that never succeeded within Settle makes it "blocked" whatever the probed calls
// answered. The record keeps each result with its action.
func JudgeURN(action string, projectControl, project, accountControl, account URNAnswer) string {
	return ""
}

// IamURN is the probe check lz-live runs on the iam-urn root.
func IamURN(o Options) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output:   "urn_check",
		Public:   []string{"account_urn", "bucket", "bucket_urn", "project_urn", "run_id"},
		Validate: func(env map[string]string) error { return nil },
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			return nil, errors.New("not implemented")
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			return errors.New("not implemented")
		},
	}
}

// sweepURN is the check's Sweep (data-model.md *Probe checks*): it deletes, by listing the exact
// names lzprobe-iam-urn-<run id>-project|account with the sandbox admin, the clients and policies
// the check may have created, and removes the tag key lzprobe-iam-urn:run-id from the probe bucket
// lzprobe-iam-urn-<lower(run id)>, whether or not an output or a record exists. ProbeCheck gains
// its Sweep field in T038; until then T034 implements this method and T034 or T038, whichever
// lands second, sets Sweep: o.sweepURN.
func (o Options) sweepURN(ctx context.Context, runID, projectID string, term io.Writer) error {
	return errors.New("not implemented")
}
