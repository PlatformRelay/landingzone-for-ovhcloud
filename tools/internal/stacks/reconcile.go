// This file is the reconciler (FR-007, ADR-0007): it makes the stacks under stacks/ correspond to
// the rows of stacks/deployments.yaml by creating a missing stack with the pinned
// `terramate create`, and refuses every other mismatch with UNSUPPORTED_CHANGE until the rename
// and retirement workflows exist (research R3).
//
// 005 T035 stub: the types and the code the controls in reconcile_test.go pin; Reconcile does
// nothing yet. T036 implements it.
package stacks

// CodeUnsupportedChange refuses any mismatch between the manifest and the stack tree other than a
// missing stack: a directory without a row, a changed id, a changed dimension, or stack metadata
// (tags, after) that differs from the derived one.
const CodeUnsupportedChange = "UNSUPPORTED_CHANGE"

// ReconcileOptions configures one reconcile run.
type ReconcileOptions struct {
	Root      string // Terramate project root holding stacks/deployments.yaml
	Terramate string // the pinned terramate binary
	Check     bool   // report what would be created without writing (stacks:check)
}

// ReconcileReport is what a run did, or under Check would do.
type ReconcileReport struct {
	Created []string // stack paths (stacks/…), in manifest row order
}

// ReconcileError is a mismatch the reconciler refuses; a decoder refusal is returned as the
// decoder's *ManifestError instead.
type ReconcileError struct {
	Code   string
	Path   string // the stack path concerned
	Detail string
}

func (e *ReconcileError) Error() string { return e.Code + ": " + e.Path + ": " + e.Detail }

// Reconcile decodes Root/stacks/deployments.yaml and reconciles the stack tree under Root with it.
// Every mismatch is found before anything is written: a refused run leaves the tree unchanged.
func Reconcile(opts ReconcileOptions) (ReconcileReport, error) {
	return ReconcileReport{}, nil
}
