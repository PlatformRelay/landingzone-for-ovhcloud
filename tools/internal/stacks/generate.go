// This file is stack generation (FR-007, FR-008, ADR-0007, research R16): rendering each stack's
// backend, provider, stage call, typed inputs, import and offline test from stacks/deployments.yaml
// with the pinned `terramate generate`, and the planned-name check (NAME_COLLISION) over the
// generated stacks' plans.
//
// Stub of 005 T037: the signatures the generation controls pin, no behaviour. T038 implements
// them (and the Terramate configuration in terramate.tm.hcl and stacks/_lz/).
package stacks

// Refusal codes of generation.
const (
	// CodeStageSourceNotImplemented refuses a manifest whose spec.stage_source is the `git` seam:
	// nothing is generated from it (research R22).
	CodeStageSourceNotImplemented = "STAGE_SOURCE_NOT_IMPLEMENTED"
	// CodeNameCollision refuses one bucket name planned twice across the generated stacks (FR-007,
	// data-model *Derived instance fields*, research R15).
	CodeNameCollision = "NAME_COLLISION"
)

// GenerateOptions configures one generation run.
type GenerateOptions struct {
	Root      string // Terramate project root holding stacks/deployments.yaml and the reconciled stacks
	Terramate string // the pinned terramate binary
}

// GenerateError is a refusal of generation or of the planned-name check; a decoder refusal is
// returned as the decoder's *ManifestError instead.
type GenerateError struct {
	Code   string
	Path   string // the stack path concerned; empty for a manifest-wide refusal
	Detail string
}

func (e *GenerateError) Error() string { return e.Code + ": " + e.Path + ": " + e.Detail }

// StackPlan is one generated stack's plan: Stack is its path (stacks/…), Plan the plan JSON
// (`tofu show -json` form, as the `test_plan` of a verbose `tofu test -json` message carries it).
type StackPlan struct {
	Stack string
	Plan  []byte
}

// Generate decodes Root/stacks/deployments.yaml, refuses what generation does not support and
// runs the pinned `terramate generate` in Root. Stub: does nothing.
func Generate(opts GenerateOptions) error {
	return nil
}

// PlannedBucketNames returns the bucket names (ovh_cloud_project_storage `name`) a plan creates
// or keeps, sorted. Stub: none.
func PlannedBucketNames(plan []byte) ([]string, error) {
	return nil, nil
}

// CheckPlannedNames refuses with NAME_COLLISION a bucket name that two stacks, or one stack
// twice, plan. Stub: refuses nothing.
func CheckPlannedNames(plans []StackPlan) error {
	return nil
}
