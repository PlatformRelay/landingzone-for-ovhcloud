package live

import (
	"context"
	"errors"
)

// The runner's sweep (spec 003 T037, T038; data-model.md *Probe checks*): a lost create answer
// can leave neither a root output (then the run skips Undo) nor a state entry (then destroy does
// not know the resource). A probe check's Sweep therefore runs with only the run id and the
// project probe.env records, after destroy and before the leftover check, on every way out and on
// `--cleanup`, with the run's redacted terminal; probe.env, the passphrase and the probe record
// stay until Sweep and the leftover check pass.
//
// Stub (T037): not implemented, and the runner does not call it yet; T038 implements it.
func (s *session) sweep(ctx context.Context, projectID string) error {
	return errors.New("not implemented")
}
