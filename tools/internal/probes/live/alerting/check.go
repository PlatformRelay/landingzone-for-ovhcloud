// Package alerting is the budget-alert probe check (spec 007 FR-007, FR-008): its Observe reads
// the root's alert back before the destroy, and its Sweep deletes, against the run's baseline, the
// alerts the run may have created. Stub for T015 (spec 007 T012).
package alerting

import (
	"context"
	"errors"
	"io"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// Options are what the check needs from lz-live (data-model *Fixed signatures*).
type Options struct {
	API   live.API
	Admin live.Credential
	// RunDir is a run's record directory (.local/live/<run-id>/), which holds baseline.json and
	// inventory.jsonl.
	RunDir func(runID string) string
	// StateDir is accounts/<account>/state: the recovery copy is baselines/<run-id>.json and the
	// run's state probes/<run-id>/budget-alert.tfstate under it.
	StateDir string
	// Project is the run's project (probe.env's), which Observe reads the alert in.
	Project string
}

// Check is the budget-alert root's check. Stub for T015 (spec 007 T012).
func Check(o Options) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output: "budget_alert_check",
		Public: []string{"alert_id"},
		Observe: func(context.Context, map[string]string, io.Writer) ([]byte, error) {
			return nil, errors.New("alerting: Observe not implemented")
		},
		Sweep: func(context.Context, string, string, io.Writer) error {
			return errors.New("alerting: Sweep not implemented")
		},
	}
}
