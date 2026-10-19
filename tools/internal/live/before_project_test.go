package live

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Spec 007 FR-006 (T014): a probe's baseline lists the alerts of the project probe.env records, so
// its Before judges that project's alerts only; an alert of another project the leftover check
// lists (account.env names every project) cannot be the run's and keeps today's matching.

// TestLeftoverBeforeProject: with Before listed in project p1, p1's alert absent from it is a
// leftover; with Before listed in another project, the same alert of p1 is not a candidate.
// Exposes a Before that judges every project's alerts (each alert outside the run's project a
// leftover, the cleanup never passing).
func TestLeftoverBeforeProject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		project string
		want    []string
	}{
		// p1's baseline lists al-x only: p1's alerts are absent from it (al-budget-0001 is retained).
		{project: "p1", want: []string{pbNewAlert, tAlert + " al-ops-0002"}},
		{project: "p-other", want: nil},
	} {
		t.Run(tc.project, func(t *testing.T) {
			t.Parallel()
			c, _, inv := pbBeforeWorld(t)
			c.Before = map[string][]string{tAlert: {"al-x"}}
			c.beforeProject = tc.project
			got := pbLeftovers(t, c, inv)
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Before listed in %s: leftovers %v, want %v", tc.project, got, tc.want)
			}
		})
	}
}

// TestProbeBeforeOtherProject: a probe run against project q, whose leftover check also lists p1
// (another project of the account) with alerts its baseline never listed, passes: the run's Before
// is q's. Exposes a probe that does not tie its Before to the recorded project.
func TestProbeBeforeOtherProject(t *testing.T) {
	t.Parallel()
	const project = "q1"
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	p.Run.Vars["TF_VAR_project_id"] = project
	l := p.Run.Leftovers.Lister.(*fakeLister)
	l.responses["/cloud/project/"+project+"/alerting"] = []json.RawMessage{json.RawMessage(`[]`)}
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("probe on %s beside p1's alerts: %v; want a pass\n%s", project, err, w.term)
	}
	if pbApply(w.calls(t, "tofu.log")) == nil {
		t.Error("the probe root was never applied")
	}
}

// TestProbeCleanupClassBefore: the cleanup of a baseline-backed run (its destroy failed, state
// kept) takes the run's baseline as Before, for the project probe.env records only. Exposes a
// cleanup that forgets the baseline, or whose Before judges every project's alerts.
func TestProbeCleanupClassBefore(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{DestroyExit: 1})
	if err := p.Start(context.Background()); err == nil {
		t.Fatal("premise: a probe whose destroy failed passed")
	}
	_, state, _ := probeFiles(t, root)
	c := p
	if err := c.classify(state, probeProject); err != nil {
		t.Fatalf("classify a baseline-backed run: %v", err)
	}
	if got := c.Run.Leftovers.Before[tAlert]; !slices.Equal(got, []string{"al-budget-0001", "al-ops-0002"}) || c.Run.Leftovers.beforeProject != probeProject {
		t.Errorf("cleanup Before %v of project %q, want the baseline's [al-budget-0001 al-ops-0002] of %s", got, c.Run.Leftovers.beforeProject, probeProject)
	}
	if c.Run.stateless {
		t.Error("a run with state classed stateless")
	}
}

// TestProbeCleanupFailsEarlyKeeps (review round 1): a cleanup of a run without state that fails
// before its leftover check ran (its record cannot be archived: attempts is a file) keeps the
// run's files and recovery copy. Exposes a cleanup that retires a run it never reconciled.
func TestProbeCleanupFailsEarlyKeeps(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{PlanExit: 1})
	alerts := "/cloud/project/" + probeProject + "/alerting"
	inner := p.Run.Leftovers.Lister.(*fakeLister)
	inner.responses[alerts+"/al-lost-0009"] = []json.RawMessage{json.RawMessage(`{"id":"al-lost-0009","name":"","email":"owner@example.invalid","delay":3600,"monthlyThreshold":1}`)}
	p.Run.Leftovers.Lister = &pbGainsAlert{inner: inner, path: alerts}
	if err := p.Start(context.Background()); err == nil {
		t.Fatal("premise: a probe run with a lost alert passed")
	}
	if err := os.WriteFile(filepath.Join(w.runDir, "attempts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.Cleanup(context.Background()); err == nil {
		t.Fatal("premise: a cleanup whose record cannot be archived passed")
	}
	pbKept(t, root, p.Run.ID, "after a cleanup that failed before its leftover check")
}

// pbGainsAlert lists path with al-lost-0009 added from its second request on (an alert the API
// created after the baseline listing whose answer was lost), until gone is set.
type pbGainsAlert struct {
	inner Lister
	path  string
	mu    sync.Mutex
	n     int
	gone  bool
}

func (g *pbGainsAlert) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	if path == g.path {
		g.mu.Lock()
		g.n++
		extra := g.n > 1 && !g.gone
		g.mu.Unlock()
		if extra {
			return []byte(`["al-budget-0001","al-ops-0002","al-lost-0009"]`), "", nil
		}
	}
	return g.inner.Get(ctx, path, cursor)
}

// pbKept fails unless the run's probe.env, passphrase and recovery copy are still on disk.
func pbKept(t *testing.T, root, runID, when string) {
	t.Helper()
	dir, _, pass := probeFiles(t, root)
	for _, f := range []string{filepath.Join(dir, "probe.env"), pass, pbRecovery(root, runID)} {
		if !exists(f) {
			t.Errorf("%s: %s removed before reconciliation", when, f)
		}
	}
}

// TestProbeCleanupLostAlert (FR-006, FR-007; a root without a Sweep): a run whose plan failed (no
// state) while an alert absent from its baseline appeared keeps its files and names its cleanup;
// a cleanup, stateless and so without tofu, judges the project's alerts against the run's
// baseline: it fails and keeps the files while the alert is listed, and retires the run, recovery
// copy included, once it is gone. Exposes files removed after a failed leftover check without a
// Sweep, and a cleanup that forgets the baseline (the lost alert no leftover).
func TestProbeCleanupLostAlert(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{PlanExit: 1})
	alerts := "/cloud/project/" + probeProject + "/alerting"
	inner := p.Run.Leftovers.Lister.(*fakeLister)
	inner.responses[alerts+"/al-lost-0009"] = []json.RawMessage{json.RawMessage(`{"id":"al-lost-0009","name":"","email":"owner@example.invalid","delay":3600,"monthlyThreshold":1}`)}
	g := &pbGainsAlert{inner: inner, path: alerts}
	p.Run.Leftovers.Lister = g
	if err := p.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "--cleanup "+p.Run.ID) {
		t.Errorf("probe run with a lost alert: %v; want a failure naming --cleanup %s", err, p.Run.ID)
	}
	pbKept(t, root, p.Run.ID, "after the run")
	n := len(w.calls(t, "tofu.log"))
	if err := p.Cleanup(context.Background()); err == nil {
		t.Error("a cleanup with the lost alert still listed passed: the baseline was not its Before")
	}
	pbKept(t, root, p.Run.ID, "after the failed cleanup")
	g.mu.Lock()
	g.gone = true
	g.mu.Unlock()
	if err := p.Cleanup(context.Background()); err != nil {
		t.Errorf("cleanup once the alert is gone: %v\n%s", err, w.term)
	}
	if calls := w.calls(t, "tofu.log")[n:]; len(calls) != 0 {
		t.Errorf("the cleanups of a run without state made %d tofu calls", len(calls))
	}
	if dir, _, _ := probeFiles(t, root); exists(dir) || exists(pbRecovery(root, p.Run.ID)) {
		t.Errorf("after a passing cleanup the run is not retired (directory %v, recovery copy %v)", exists(dir), exists(pbRecovery(root, p.Run.ID)))
	}
}
