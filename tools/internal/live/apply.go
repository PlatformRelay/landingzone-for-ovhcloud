package live

// `lz-live plan|apply -- <instance|all>` (FR-009, FR-010, FR-011, research R21, ADR-0007): the
// lane acts on the selected set (stacks.Select over the current code digests, the published
// artefacts and the resolved references against the records of the last applies), in run order,
// holding the run locks (stacks.HoldRun over the acted instances) from before the first plan until
// the run ends, on every path. Per stack: the authority's credentials (credentials.go), the
// consumed inputs (stacks.Adapt; a producer without an artefact blocks the stack, exit 2), init,
// plan to a file, `show -json` of that file through the retained-resource guard (protect.go), the
// rendered plan redacted to plan-<id>.txt, and for apply exactly that file applied; then the
// credential files the stack's sensitive outputs carry (account-governance, tenant-state; written
// by files.go), the envelope published and the record written. `destroy` refuses every retained
// instance (G7); the rest of destroy is T047's.
//
// T058 pins the behaviour (apply_test.go); T059 implements it. Until then this file is a
// permissive stub that does nothing.

import (
	"context"
	"io"
	"io/fs"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Verbs of the lane and the target naming every selected stack.
const (
	VerbPlan    = "plan"
	VerbApply   = "apply"
	VerbDestroy = "destroy"
	TargetAll   = "all"
)

// Refusal conditions of the lane (exit 3).
const (
	CondLocked           = "locked"            // another run holds one of the run's locks
	CondProducerSelected = "producer-selected" // `-- <instance>` while a producer it consumes is selected and not applied
	CondBootstrapOwned   = "bootstrap-owned"   // account-bootstrap is bootstrap:account's, never the lane's
)

// ApplyOptions configures one `lz-live plan|apply|destroy` run.
type ApplyOptions struct {
	Verb       string           // VerbPlan, VerbApply or VerbDestroy
	Target     string           // TargetAll or one instance id
	Checkout   string           // the reviewed checkout: generated stacks and their stage/component/module closure
	Manifest   *stacks.Manifest // the checkout's decoded stacks/deployments.yaml
	ConfigRoot string           // ~/.config/ovh-lz
	Account    string           // the bound account id (accounts/<account>/)
	RunID      string           // YYYYMMDDThhmmssZ-<4 hex>
	RunDir     string           // .local/live/<run-id>: rendered plans, inputs/<id>/
	Records    string           // .local/live/records: records/<id>.json of the last applies
	Revision   string           // source_revision of the applied tree
	Tofu       string           // tofu executable
	Schemas    fs.FS            // schemas/outputs
	Locks      stacks.LockStore // DirLocks(accounts/<account>/locks) in production
	// Store opens the state buckets with one authority's S3 keys (AWS_ACCESS_KEY_ID,
	// AWS_SECRET_ACCESS_KEY): artefacts are read and published with the acting stack's keys.
	Store    func(keys map[string]string) (ObjectStore, error)
	Terminal io.Writer
}

// Apply runs one plan, apply or destroy; the error maps to the exit code through ExitCode.
func Apply(ctx context.Context, o ApplyOptions) error {
	return nil
}
