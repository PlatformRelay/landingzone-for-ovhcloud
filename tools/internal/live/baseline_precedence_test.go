package live

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Spec 007 FR-006 and data-model *Probe baseline* (T014): which baseline copy LoadBaseline
// validates, how a copy is written (never rewritten), and the baseline listing's retry with the
// leftover check's backoff and clock and a budget ending at the run's deadline.

// TestLoadBaselinePrecedence: the record copy first; a missing or unreadable record copy falls back
// to the recovery copy, which must still validate; two readable copies that differ are
// BASELINE_INVALID copies-differ, whichever is valid. Exposes a lost record copy that strands a
// valid recovery copy, or a disagreement settled silently in favour of one copy.
func TestLoadBaselinePrecedence(t *testing.T) {
	t.Parallel()
	valid := pbBaselineFile(t, nil)
	other := pbBaselineFile(t, func(m map[string]any) { m["listed"] = map[string]any{tAlert: []string{"al-1", "al-2"}} })
	cases := []struct {
		name             string
		record, recovery []byte
		want             string // "" valid; else the error's prefix
	}{
		{name: "record-unreadable-recovery-valid", record: []byte(`{"run_id":`), recovery: valid},
		{name: "record-unreadable-recovery-another-run", record: []byte(`{"run_id":`), recovery: pbBaselineFile(t, func(m map[string]any) { m["run_id"] = pbRun2 }), want: "BASELINE_INVALID another-run"},
		{name: "both-valid-differ", record: valid, recovery: other, want: "BASELINE_INVALID copies-differ"},
		{name: "record-valid-recovery-invalid", record: valid, recovery: pbBaselineFile(t, func(m map[string]any) { m["complete"] = false }), want: "BASELINE_INVALID copies-differ"},
		{name: "null-alert-list", record: pbBaselineFile(t, func(m map[string]any) { m["listed"] = map[string]any{tAlert: nil} }), want: "BASELINE_INVALID no-alert-key"},
		{name: "both-same", record: valid, recovery: valid},
		{name: "record-only", record: valid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recordDir, recoveryPath := pbBaselinePaths(t, tc.record, tc.recovery)
			b, err := LoadBaseline(recordDir, recoveryPath, swRunID, probeProject)
			switch {
			case tc.want == "" && (err != nil || !slices.Equal(b.Listed[tAlert], []string{"al-1"})):
				t.Errorf("LoadBaseline = %+v, %v; want the valid copy", b, err)
			case tc.want != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.want)):
				t.Errorf("LoadBaseline error = %v, want one starting %s", err, tc.want)
			}
			if tc.want != "" && ClassifyRun(false, false, b, err) != RunUntrusted {
				t.Errorf("ClassifyRun after %v = %q, want %q", err, ClassifyRun(false, false, b, err), RunUntrusted)
			}
		})
	}
}

// TestWriteOnceNeverRewrites: a baseline copy is written 0600 through a temporary file that does
// not stay; an existing copy is never replaced. Exposes a baseline rewritten after a create.
func TestWriteOnceNeverRewrites(t *testing.T) {
	t.Parallel()
	dir := tempPrivate(t)
	path := filepath.Join(dir, "baseline.json")
	if err := writeOnce(path, []byte("first")); err != nil {
		t.Fatalf("writeOnce: %v", err)
	}
	if err := writeOnce(path, []byte("second")); err == nil {
		t.Error("writeOnce replaced an existing baseline")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "first" {
		t.Errorf("baseline holds %q (%v), want the first write", raw, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("baseline mode %v (%v), want 0600", fi.Mode().Perm(), err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("directory holds %d entries, want the baseline only (no temporary file)", len(entries))
	}
}

// pbClock is a waitClock on the wall clock whose waits return at once and are recorded.
type pbClock struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (c *pbClock) Now() time.Time { return time.Now() }

func (c *pbClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

// TestProbeBaselineRetries: the baseline's alert listing answers 500 once; with the run's deadline
// leaving room for the wait, it is retried on the leftover check's clock after the first backoff,
// the retry line reaches the run's terminal, the baseline is complete and the run applies. Exposes
// a baseline listing that never retries (no budget), or waits on another clock.
func TestProbeBaselineRetries(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	alerts := "/cloud/project/" + probeProject + "/alerting"
	p.Run.Leftovers.Lister = &scriptLister{base: p.Run.Leftovers.Lister, script: map[string][]listAnswer{alerts + "#1": times(1, status(alerts, 500))}}
	clk := &pbClock{}
	p.Run.Leftovers.Clock = clk
	p.Run.Deadline = 2 * time.Minute
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("probe whose baseline listing answered 500 once: %v\n%s", err, w.term)
	}
	if pbApply(w.calls(t, "tofu.log")) == nil {
		t.Error("the probe root was never applied")
	}
	clk.mu.Lock()
	waits := slices.Clone(clk.waits)
	clk.mu.Unlock()
	if !slices.Equal(waits, defaultListingBackoff[:1]) {
		t.Errorf("waits on the check's clock %v, want %v", waits, defaultListingBackoff[:1])
	}
	want := "LZ-LIVE retry listing " + alerts + " in 5s: 500 (attempt 2 of 3)"
	if got := listingRetryLines(w.term.String()); !slices.Contains(got, want) {
		t.Errorf("retry lines %q, want %q", got, want)
	}
	if m := pbRecord(t, filepath.Join(w.runDir, "baseline.json")); m != nil && m["complete"] != true {
		t.Errorf("baseline complete = %v after a retried listing, want true", m["complete"])
	}
}

// pbInterrupting sends SIGINT to the run when path is listed, then waits for the request's context
// to end (at most 3 s) and answers with its error.
type pbInterrupting struct {
	inner Lister
	path  string
	sigs  chan os.Signal
}

func (l pbInterrupting) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	if path != l.path {
		return l.inner.Get(ctx, path, cursor)
	}
	l.sigs <- syscall.SIGINT
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-time.After(3 * time.Second):
		return nil, "", errors.New("the baseline listing was not cancelled by the signal")
	}
}

// TestProbeBaselineInterrupted (review round 1): a SIGINT during the baseline listing ends it
// through the run's own context: the run fails, interrupted, with no apply. Exposes a baseline
// outside the run's deadline and signals.
func TestProbeBaselineInterrupted(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	sigs := make(chan os.Signal, 1)
	p.Run.Signals = sigs
	p.Run.Leftovers.Lister = pbInterrupting{inner: p.Run.Leftovers.Lister, path: "/cloud/project/" + probeProject + "/alerting", sigs: sigs}
	err := p.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "interrupt") || strings.Contains(err.Error(), "not cancelled") {
		t.Errorf("probe interrupted during its baseline listing: %v; want the interruption to cancel the listing", err)
	}
	if pbApply(w.calls(t, "tofu.log")) != nil {
		t.Error("an interrupted baseline went on to apply")
	}
}

// TestProbeBaselineFailedNoID: a failed baseline listing of a 32-hex project records its errors
// with the project as <project>, never by id. Exposes the id written to the run record.
func TestProbeBaselineFailedNoID(t *testing.T) {
	t.Parallel()
	const project = "0123456789abcdef0123456789abcdef"
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	p.Run.Vars["TF_VAR_project_id"] = project
	p.Run.Leftovers.Lister = pbFailing{inner: p.Run.Leftovers.Lister, path: "/cloud/project/" + project + "/alerting"}
	if err := p.Start(context.Background()); err == nil {
		t.Error("a probe whose baseline listing failed passed")
	}
	record := filepath.Join(w.runDir, "baseline.json")
	raw, err := os.ReadFile(record)
	m := pbRecord(t, record)
	errs, _ := m["errors"].([]any)
	if err != nil || len(errs) == 0 || !strings.Contains(fmt.Sprint(errs[0]), "/cloud/project/<project>/alerting") || strings.Contains(string(raw), project) {
		t.Errorf("failed baseline %s (%v): want its error naming /cloud/project/<project>/alerting, never the id", raw, err)
	}
}

// TestProbeFailedRunRetires: a run whose plan failed (no state) while its sweep-less leftover check
// passed retires its directory and the baseline's recovery copy. Exposes a recovery copy that
// outlives its run.
func TestProbeFailedRunRetires(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{PlanExit: 1})
	if err := p.Start(context.Background()); err == nil {
		t.Fatal("premise: a probe whose plan failed passed")
	}
	if dir, _, _ := probeFiles(t, root); exists(dir) || exists(pbRecovery(root, p.Run.ID)) {
		t.Errorf("a failed run without state whose leftover check passed kept its directory %v, recovery copy %v", exists(dir), exists(pbRecovery(root, p.Run.ID)))
	}
}

// TestProbePlanOnlyNoBaseline: a plan-only probe creates nothing, so it lists and writes no
// baseline. Exposes a plan-only run that needs the alert listing.
func TestProbePlanOnlyNoBaseline(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	p.Run.PlanOnly = true
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("plan-only probe: %v\n%s", err, w.term)
	}
	if record := filepath.Join(w.runDir, "baseline.json"); exists(record) {
		t.Errorf("a plan-only probe wrote %s", record)
	}
}
