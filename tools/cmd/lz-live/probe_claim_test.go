//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec 007 T029 (stage B follow-up, D131) through the production entry: a probe run directory
// that holds only its claim (a start killed between claiming the directory and writing probe.env:
// nothing can exist in OVHcloud yet) is retired by a plain `--cleanup <run-id>`.

// claimID is a run id whose start was killed right after its claim.
const claimID = "20261025T100000Z-0e0e"

// TestProbeCleanupEmptyClaimEntry: given a run directory without probe.env / when a start runs,
// then `--cleanup <run-id>` / then the start is refused naming the run's cleanup and not manual
// removal; an empty claim is retired by that cleanup (exit 0, no tofu call, nothing deleted) and
// the next start proceeds; a claim holding any other file is not retired and the file stays →
// exposes an empty claim that only the owner's manual removal clears, or a retire of a directory
// the run may have written to.
func TestProbeCleanupEmptyClaimEntry(t *testing.T) {
	for _, c := range []struct {
		name, extra string
		retired     bool
	}{{"empty", "", true}, {"other-file", "notes.txt", false}} {
		t.Run(c.name, func(t *testing.T) {
			w := newRCWorld(t, "create")
			dir := w.probeDir(claimID)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if c.extra != "" {
				writePrivate(t, filepath.Join(dir, c.extra), "x\n")
			}
			w.refusedBeside(t, claimID)
			claimCleanup(t, w, dir, c.extra, c.retired)
		})
	}
}

func claimCleanup(t *testing.T, w *rcWorld, dir, extra string, retired bool) {
	t.Helper()
	if _, stdout, stderr := w.probeStart(t); strings.Contains(stdout+stderr, "by hand") {
		t.Errorf("the refusal beside an empty claim sends the owner to manual removal:\n%s%s", stdout, stderr)
	}
	before := w.tofuCalls()
	code, stdout, stderr := w.cleanup(t, claimID)
	if n := w.tofuCalls() - before; n != 0 {
		t.Errorf("the cleanup of a claim without probe.env made %d tofu calls", n)
	}
	if d := w.deletes(); len(d) != 0 {
		t.Errorf("the cleanup of a claim deleted %v", d)
	}
	if !retired {
		if _, err := os.Stat(filepath.Join(dir, extra)); code == 0 || err != nil {
			t.Errorf("cleanup of a claim holding %s: exit %d, file kept %v; want refused, file kept\nstdout:\n%s\nstderr:\n%s", extra, code, err == nil, stdout, stderr)
		}
		return
	}
	if code != 0 {
		t.Errorf("cleanup of an empty claim: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	w.retired(t, claimID)
	w.startProceeds(t, "the cleanup of an empty claim")
}
