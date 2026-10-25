package configure

import "errors"

// This file is the dry-run diff (FR-005, data-model.md *Dry-run diff entry*): the instance rows of
// the generated manifest against today's stacks/deployments.yaml, by id, rows added and rows
// removed, each with what stacks:reconcile is expected to do.

// Diff returns the diff lines of the generated manifest against the current one: one line per row
// removed, then one per row added, the count line, and the warning line when a row is removed.
// current is today's stacks/deployments.yaml, nil when that file is absent; bytes that do not
// decode (an empty file included) return a CURRENT_MANIFEST_INVALID *AnswersError and no line.
func Diff(current, generated []byte) ([]string, error) {
	return nil, errors.New("not implemented")
}
