package live

import (
	"context"
	"errors"
	"fmt"
)

// The runner's sweep (spec 003 T037, T038; data-model.md *Probe checks*): a lost create answer
// can leave neither a root output (then the run skips Undo) nor a state entry (then destroy does
// not know the resource). A probe check's Sweep therefore runs with only the run id and the
// project probe.env records (Probe.run reads it into Runner.sweepProject), after destroy and
// before the leftover check, on every way out and on `--cleanup`, with the run's redacted
// terminal; probe.env, the passphrase and the probe record stay until Sweep and the leftover check
// pass (unreconciled).
//
// A run that keeps its root (its Undo failed or was not reached, or its companion was not
// destroyed) does not sweep: Sweep deletes by name what the kept root's Undo may still need, such
// as the credential the check uses. The `--cleanup` that finally undoes and destroys the root
// sweeps (coordinator decision, 2026-10-16). A plan-only run created nothing and does not sweep.

// unreconciled marks a run whose Sweep or leftover check did not pass: Probe.run keeps the run's
// files, even without state, for `--cleanup`.
type unreconciled struct{ error }

func (u unreconciled) Unwrap() error { return u.error }

// sweep runs the check's Sweep, if it has one, once the run's stacks are destroyed; rootKept: the
// root stays for `--cleanup`, which sweeps instead.
func (s *session) sweep(ctx context.Context, rootKept bool) error {
	c, r := s.r.check, s.r
	if c == nil || c.Sweep == nil || r.PlanOnly {
		return nil
	}
	if rootKept {
		return unreconciled{fmt.Errorf("sweep of run %s deferred: its root is kept; finish with --cleanup %s", r.ID, r.ID)}
	}
	if err := c.Sweep(ctx, r.ID, r.sweepProject, s.term); err != nil {
		return unreconciled{fmt.Errorf("sweep of run %s: %w; finish with --cleanup %s", r.ID, err, r.ID)}
	}
	return nil
}

// reconciled marks a failed leftover check of a run whose check has a Sweep as unreconciled, so
// its files stay (also without state) and its error names `--cleanup`; not in a plan-only run,
// which applied nothing to sweep.
func (s *session) reconciled(leftErr error) error {
	c := s.r.check
	if leftErr == nil || c == nil || c.Sweep == nil || s.r.PlanOnly {
		return leftErr
	}
	return unreconciled{fmt.Errorf("%w; finish with --cleanup %s", leftErr, s.r.ID)}
}

// keepRecord archives a run directory's earlier record before a probe `--cleanup` writes its own
// (spec 007 FR-002, data-model *Run record archive*): every entry but inventory.jsonl,
// baseline.json, observations.json and attempts/ moves into attempts/<n>/ (n = 1 + the highest
// existing number), observations.json is copied there and stays in place; it returns that
// directory, or "" when there was nothing to move. Stub (spec 007 T005): not implemented until
// T007.
func keepRecord(runDir string) (kept string, err error) {
	return "", fmt.Errorf("keepRecord %s: not implemented", runDir)
}

// isUnreconciled reports whether a run's error says its Sweep or leftover check did not pass.
func isUnreconciled(err error) bool {
	var u unreconciled
	return errors.As(err, &u)
}
