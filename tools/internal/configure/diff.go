package configure

import (
	"fmt"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// This file is the dry-run diff (FR-005, data-model.md *Dry-run diff entry*): the instance rows of
// the generated manifest against today's stacks/deployments.yaml, by id, rows added and rows
// removed, each with what stacks:reconcile is expected to do. The marks are judged against the
// current manifest, assuming the stack tree matches it; values of a kept row are not compared.

const (
	diffMarkAdded   = "expected: created by stacks:reconcile"
	diffMarkRemoved = "expected: refused by stacks:reconcile (UNSUPPORTED_CHANGE)"
)

// Diff returns the diff lines of the generated manifest against the current one: one line per row
// removed, then one per row added, the count line, and the warning line when a row is removed.
// current is today's stacks/deployments.yaml, nil when that file is absent; bytes that do not
// decode (an empty file included) return a CURRENT_MANIFEST_INVALID *AnswersError and no line.
// generated is Export's output; a refusal of it returns the decoder's *stacks.ManifestError.
func Diff(current, generated []byte) ([]string, error) {
	var currentRows []stacks.Instance
	if current != nil {
		m, err := stacks.DecodeManifest(current)
		if err != nil {
			return nil, &AnswersError{Code: CodeCurrentManifestInvalid, Detail: "stacks/deployments.yaml: " + err.Error()}
		}
		currentRows = m.Instances
	}
	m, err := stacks.DecodeManifest(generated)
	if err != nil {
		return nil, err
	}
	removed := diffMissing(currentRows, m.Instances)
	added := diffMissing(m.Instances, currentRows)
	var lines []string
	for _, r := range removed {
		lines = append(lines, "- "+diffRow(r)+": "+diffMarkRemoved)
	}
	for _, r := range added {
		lines = append(lines, "+ "+diffRow(r)+": "+diffMarkAdded)
	}
	if len(lines) == 0 {
		return []string{"diff: none"}, nil
	}
	lines = append(lines, fmt.Sprintf("diff: %d added, %d removed", len(added), len(removed)))
	if len(removed) > 0 {
		lines = append(lines, fmt.Sprintf("configure: warning: stacks:reconcile is expected to refuse this export (%d removed rows)", len(removed)))
	}
	return lines, nil
}

// diffMissing is the rows of from whose id is not in to, in the order of from.
func diffMissing(from, to []stacks.Instance) []stacks.Instance {
	ids := make(map[string]bool, len(to))
	for _, r := range to {
		ids[r.ID] = true
	}
	var missing []stacks.Instance
	for _, r := range from {
		if !ids[r.ID] {
			missing = append(missing, r)
		}
	}
	return missing
}

// diffRow is `row <id> (<stage> <tenant>/<env>/<region>)`, the dimensions the row has.
func diffRow(r stacks.Instance) string {
	where := r.Stage
	var dims []string
	for _, d := range []string{r.Tenant, r.Environment, r.Region} {
		if d != "" {
			dims = append(dims, d)
		}
	}
	if len(dims) > 0 {
		where += " " + strings.Join(dims, "/")
	}
	return "row " + r.ID + " (" + where + ")"
}
