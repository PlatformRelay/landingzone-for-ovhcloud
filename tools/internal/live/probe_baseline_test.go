package live

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Spec 007 FR-006 (T011; T014 implements it): a probe start runs durable phases in order —
// probe.env, the passphrase, then the baseline of nameless kinds (the alert listing of the recorded
// project) in the record directory and its recovery copy under state/baselines/ — before its first
// tofu apply; a failed baseline listing ends the run there and retires its files; a probe start is
// refused while another probe run's directory exists under state/probes/; LoadBaseline and
// ClassifyRun decide what a later `--cleanup` may do (data-model *Probe baseline*, *Start-up phases
// and run classes*, *Unreconciled probe run*).

// pbRun2 is a second run id beside runWorld's.
const pbRun2 = "20261006T130000Z-c3d4"

// pbDigest is the hex SHA-256 of a project id: what a baseline records instead of the id.
func pbDigest(project string) string {
	h := sha256.Sum256([]byte(project))
	return hex.EncodeToString(h[:])
}

// pbStateDir is accounts/<account>/state below root.
func pbStateDir(root string) string {
	return filepath.Join(root, "accounts", probeAccount, "state")
}

// pbRecovery is the baseline's recovery copy of runID.
func pbRecovery(root, runID string) string {
	return filepath.Join(pbStateDir(root), "baselines", runID+".json")
}

// pbApply is the probe root's first `tofu apply` (not the destroy), or nil.
func pbApply(calls []tofuCall) *tofuCall {
	for i, c := range calls {
		if c.Cmd == "apply" && c.Stack == "p" {
			return &calls[i]
		}
	}
	return nil
}

// pbHolders splits the "<path>|<file mode>|<directory mode>" lines of a call's Holders.
func pbHolders(c *tofuCall) map[string][2]string {
	out := map[string][2]string{}
	for _, h := range c.Holders {
		parts := strings.Split(h, "|")
		if len(parts) == 3 {
			out[parts[0]] = [2]string{parts[1], parts[2]}
		}
	}
	return out
}

// pbSecond is another probe run (pbRun2) on the same config root, in its own fake world.
func pbSecond(t *testing.T, root string) Probe {
	t.Helper()
	w := newRunWorld(t)
	w.runDir = filepath.Join(filepath.Dir(w.runDir), pbRun2)
	p := w.probe(t, root, tofuStack{})
	p.Run.ID = pbRun2
	p.Run.Leftovers.RunID = pbRun2
	return p
}

// pbSecondStarts: a later probe start on root is not refused, and passes.
func pbSecondStarts(t *testing.T, root string) {
	t.Helper()
	if err := pbSecond(t, root).Start(context.Background()); err != nil {
		t.Errorf("the next probe start: %v; want it to proceed and pass", err)
	}
}

// pbRecord reads a baseline file as a JSON object; nil when it is missing or not one.
func pbRecord(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("baseline %s: %v", path, err)
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Errorf("baseline %s is not a JSON object: %v", path, err)
		return nil
	}
	return m
}

// pbAlerts is the baseline's listed ovh_cloud_project_alerting ids, and whether the key is there.
func pbAlerts(m map[string]any) ([]string, bool) {
	listed, _ := m["listed"].(map[string]any)
	raw, ok := listed[tAlert].([]any)
	if !ok {
		return nil, false
	}
	var out []string
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	slices.Sort(out)
	return out, true
}

// TestProbeBaselineBeforeApply: when the root's first apply starts, both baseline copies exist
// (0600; state/baselines/ 0700), nothing under state/probes/<run-id>/ holds the baseline, and no
// other file (a temporary one left behind) does; the record copy names the run and the project
// digest and lists the project's alerts with complete true and no errors. The root's destroy fails,
// so the run stays unreconciled and keeps its recovery copy (FR-007), which is checked the same way.
// Once apply has run, the alert listing also shows the alert it created (al-created-0009): a
// baseline listed, rewritten or completed after the apply lists it (review round 2).
func TestProbeBaselineBeforeApply(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	digest := pbDigest(probeProject)
	p := w.probe(t, root, tofuStack{ScanRoots: []string{w.runDir, pbStateDir(root)}, ScanFor: []string{digest}, DestroyExit: 1})
	alerts := "/cloud/project/" + probeProject + "/alerting"
	inner := p.Run.Leftovers.Lister.(*fakeLister)
	inner.responses[alerts+"/al-created-0009"] = []json.RawMessage{json.RawMessage(`{"id":"al-created-0009","name":"","email":"owner@example.invalid","delay":3600,"monthlyThreshold":1}`)}
	p.Run.Leftovers.Lister = pbAfterApply{inner: inner, log: filepath.Join(w.bin, "tofu.log"), path: alerts}
	if err := p.Start(context.Background()); err == nil {
		t.Error("a probe whose destroy failed passed")
	}
	apply := pbApply(w.calls(t, "tofu.log"))
	if apply == nil {
		t.Fatal("the probe root was never applied")
	}
	record := filepath.Join(w.runDir, "baseline.json")
	pbBothCopiesAtApply(t, pbHolders(apply), record, pbRecovery(root, p.Run.ID), filepath.Join(pbStateDir(root), "probes"))
	pbCompleteBaseline(t, pbRecord(t, record), p.Run.ID, digest)
	pbCompleteBaseline(t, pbRecord(t, pbRecovery(root, p.Run.ID)), p.Run.ID, digest)
}

// pbAfterApply lists path with one more alert, al-created-0009, once the fake tofu logged an apply.
type pbAfterApply struct {
	inner     Lister
	log, path string
}

func (a pbAfterApply) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	if raw, err := os.ReadFile(a.log); path == a.path && err == nil && strings.Contains(string(raw), `"cmd":"apply"`) {
		return []byte(`["al-budget-0001","al-ops-0002","al-created-0009"]`), "", nil
	}
	return a.inner.Get(ctx, path, cursor)
}

// pbBothCopiesAtApply: the files holding the digest when apply started are exactly the record and
// recovery copies (0600; the recovery directory 0700), none under state/probes/.
func pbBothCopiesAtApply(t *testing.T, got map[string][2]string, record, recovery, probes string) {
	t.Helper()
	for _, path := range []string{record, recovery} {
		m, ok := got[path]
		switch {
		case !ok:
			t.Errorf("when apply started, %s did not exist (or held no project digest): the baseline is written after the first create, or in one place only", path)
		case m[0] != "600":
			t.Errorf("baseline %s has mode %s, want 600", path, m[0])
		}
	}
	if m, ok := got[recovery]; ok && m[1] != "700" {
		t.Errorf("state/baselines/ has mode %s, want 700", m[1])
	}
	for path := range got {
		switch {
		case path == record || path == recovery:
		case within(path, probes):
			t.Errorf("a baseline under state/probes/ (%s): the success path removes that directory", path)
		default:
			t.Errorf("when apply started, %s also held the project digest (a temporary or third copy)", path)
		}
	}
}

// pbCompleteBaseline: the record copy names the run and the digest and lists the project's alerts,
// the nameless kinds only, complete and without errors.
func pbCompleteBaseline(t *testing.T, m map[string]any, runID, digest string) {
	t.Helper()
	if m == nil {
		return
	}
	if m["run_id"] != runID {
		t.Errorf("baseline run_id = %v, want %s", m["run_id"], runID)
	}
	if m["project_sha256"] != digest {
		t.Errorf("baseline project_sha256 = %v, want %s", m["project_sha256"], digest)
	}
	if alerts, ok := pbAlerts(m); !ok || !slices.Equal(alerts, []string{"al-budget-0001", "al-ops-0002"}) {
		t.Errorf("baseline lists alerts %v (key present: %v), want the project's [al-budget-0001 al-ops-0002]", alerts, ok)
	}
	if listed, _ := m["listed"].(map[string]any); len(listed) != 1 {
		t.Errorf("baseline lists kinds %v, want the nameless kinds only (%s)", listed, tAlert)
	}
	if m["complete"] != true {
		t.Errorf("baseline complete = %v, want true", m["complete"])
	}
	if errs, ok := m["errors"].([]any); !ok || len(errs) != 0 {
		t.Errorf("baseline errors = %v, want []", m["errors"])
	}
}

// TestProbeBaselinePassingRunRetires: a passing run leaves no directory under state/probes/ and no
// recovery copy, keeps the record copy in its run record, and the next start is not refused.
func TestProbeBaselinePassingRunRetires(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("probe run in the clean world: %v", err)
	}
	dir, _, _ := probeFiles(t, root)
	if exists(dir) {
		left, _ := os.ReadDir(dir)
		t.Errorf("a passing run kept state/probes/%s/ (%v): it blocks every later probe", p.Run.ID, left)
	}
	if recovery := pbRecovery(root, p.Run.ID); exists(recovery) {
		t.Errorf("a passing run kept the recovery copy %s", recovery)
	}
	if record := filepath.Join(w.runDir, "baseline.json"); !exists(record) {
		t.Errorf("a passing run has no baseline.json in its run record %s", w.runDir)
	}
	pbSecondStarts(t, root)
}

// TestProbeBaselineListingFails: an alert listing that fails on every attempt ends the run before
// any create: no tofu apply, the record copy written with complete false and its errors, the error
// naming the baseline, the run's own files retired, and the next start not refused.
func TestProbeBaselineListingFails(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	alerts := "/cloud/project/" + probeProject + "/alerting"
	p.Run.Leftovers.Lister = pbFailing{inner: p.Run.Leftovers.Lister, path: alerts}
	err := p.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "baseline") && !strings.Contains(err.Error(), alerts) {
		t.Errorf("probe run with a failing alert listing: %v; want an error naming the baseline or %s", err, alerts)
	}
	if pbApply(w.calls(t, "tofu.log")) != nil {
		t.Error("tofu apply ran without a baseline")
	}
	if m := pbRecord(t, filepath.Join(w.runDir, "baseline.json")); m != nil {
		if m["complete"] != false {
			t.Errorf("failed baseline complete = %v, want false", m["complete"])
		}
		if errs, _ := m["errors"].([]any); len(errs) == 0 {
			t.Errorf("failed baseline records no errors: %v", m["errors"])
		}
	}
	dir, _, _ := probeFiles(t, root)
	if exists(dir) {
		left, _ := os.ReadDir(dir)
		t.Errorf("a run ended by its baseline kept state/probes/%s/ (%v): it blocks every later probe", p.Run.ID, left)
	}
	if recovery := pbRecovery(root, p.Run.ID); exists(recovery) {
		t.Errorf("a run ended by its baseline kept the recovery copy %s", recovery)
	}
	pbSecondStarts(t, root)
}

// pbFailing answers 500 to every request for path; the rest goes to inner.
type pbFailing struct {
	inner Lister
	path  string
}

func (f pbFailing) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	if path == f.path {
		return nil, "", &statusError{path: path, code: 500}
	}
	return f.inner.Get(ctx, path, cursor)
}

// TestProbeBaselineEmptyListing: a project without alerts has a valid baseline whose alert key
// holds an empty list (not null, not absent), and the run applies.
func TestProbeBaselineEmptyListing(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	p.Run.Leftovers.Lister.(*fakeLister).responses["/cloud/project/"+probeProject+"/alerting"] = []json.RawMessage{json.RawMessage(`[]`)}
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("probe run on a project without alerts: %v", err)
	}
	if pbApply(w.calls(t, "tofu.log")) == nil {
		t.Error("the probe root was never applied")
	}
	m := pbRecord(t, filepath.Join(w.runDir, "baseline.json"))
	if m == nil {
		return
	}
	if alerts, ok := pbAlerts(m); !ok || len(alerts) != 0 {
		t.Errorf("baseline listed = %v, want %s: [] (an empty list, present)", m["listed"], tAlert)
	}
	if m["complete"] != true {
		t.Errorf("baseline complete = %v, want true", m["complete"])
	}
}

// TestProbeBaselineDigestNotID: the baseline holds the project's SHA-256 digest, never the
// 32-hex id, and lists the project probe.env records.
func TestProbeBaselineDigestNotID(t *testing.T) {
	t.Parallel()
	const project = "0123456789abcdef0123456789abcdef"
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{ScanRoots: []string{w.runDir, filepath.Join(pbStateDir(root), "baselines")}, ScanFor: []string{project}})
	p.Run.Vars["TF_VAR_project_id"] = project
	l := p.Run.Leftovers.Lister.(*fakeLister)
	l.responses["/cloud/project/"+project+"/alerting"] = []json.RawMessage{json.RawMessage(`["al-x"]`)}
	l.responses["/cloud/project/"+project+"/alerting/al-x"] = []json.RawMessage{json.RawMessage(`{"id":"al-x","name":"","email":"owner@example.invalid","delay":3600,"monthlyThreshold":50}`)}
	if err := p.Start(context.Background()); err != nil {
		t.Logf("probe run: %v", err)
	}
	apply := pbApply(w.calls(t, "tofu.log"))
	if apply == nil {
		t.Error("the probe root was never applied")
	} else {
		for path := range pbHolders(apply) {
			if within(path, w.runDir) || within(path, pbStateDir(root)) {
				t.Errorf("when apply started, %s held the project id", path)
			}
		}
	}
	record := filepath.Join(w.runDir, "baseline.json")
	if raw, err := os.ReadFile(record); err == nil && strings.Contains(string(raw), project) {
		t.Errorf("baseline.json holds the project id %s", project)
	}
	m := pbRecord(t, record)
	if m == nil {
		return
	}
	if m["project_sha256"] != pbDigest(project) {
		t.Errorf("baseline project_sha256 = %v, want %s", m["project_sha256"], pbDigest(project))
	}
	if alerts, _ := pbAlerts(m); !slices.Equal(alerts, []string{"al-x"}) {
		t.Errorf("baseline lists %v, want the recorded project's [al-x]", alerts)
	}
}

// pbBeforeWorld is the clean synthetic world's leftover check with alert al-new-0003 added to
// project p1's listing (no run id, no prefix, no inventory line).
func pbBeforeWorld(t *testing.T) (LeftoverCheck, *fakeLister, []InventoryEntry) {
	t.Helper()
	w, _ := loadSynthetic(t)
	c, l, inv := worldCheck(t, w, syntheticSeed{})
	pr := "/cloud/project/p1/alerting"
	l.responses[pr] = []json.RawMessage{json.RawMessage(`["al-budget-0001","al-ops-0002","al-new-0003"]`)}
	l.responses[pr+"/al-new-0003"] = []json.RawMessage{json.RawMessage(`{"id":"al-new-0003","name":"","email":"owner@example.invalid","emails":[],"delay":3600,"monthlyThreshold":1,"service":"cloud","status":"ok"}`)}
	return c, l, inv
}

// pbLeftovers runs the check and fails on any listing error (the world must list cleanly).
func pbLeftovers(t *testing.T, c LeftoverCheck, inv []InventoryEntry) []string {
	t.Helper()
	rep := c.Check(context.Background(), inv)
	if len(rep.Errors) > 0 {
		t.Fatalf("leftover check errors: %v", rep.Errors)
	}
	return leftoverKeys(rep.Leftovers)
}

// TestLeftoverBefore: Before decides the nameless kinds only. An alert absent from the baseline is
// the run's leftover without an inventory line; an alert in it is not; a run-named bucket stays a
// leftover even when Before lists it; a nil Before, or one without the alert kind (the chain),
// leaves today's matching: a pre-existing alert is no leftover, one in the inventory is.
func TestLeftoverBefore(t *testing.T) {
	t.Parallel()
	t.Run("alert-absent-from-baseline", pbBeforeAlertAbsent)
	t.Run("named-kind-in-before", pbBeforeNamedKind)
	t.Run("nil-before", func(t *testing.T) { pbBeforeUnset(t, nil) })
	t.Run("no-alert-entry", func(t *testing.T) { pbBeforeUnset(t, map[string][]string{tStorage: {}}) })
}

// pbNewAlert is the leftover key of the alert pbBeforeWorld adds.
const pbNewAlert = tAlert + " al-new-0003"

func pbBeforeAlertAbsent(t *testing.T) {
	t.Parallel()
	c, _, inv := pbBeforeWorld(t)
	c.Before = map[string][]string{tAlert: {"al-budget-0001", "al-ops-0002"}}
	got := pbLeftovers(t, c, inv)
	if !slices.Contains(got, pbNewAlert) {
		t.Errorf("leftovers %v: alert al-new-0003, absent from the baseline, is not the run's leftover", got)
	}
	if slices.Contains(got, tAlert+" al-ops-0002") {
		t.Errorf("leftovers %v: baseline alert al-ops-0002 is a leftover", got)
	}
	if len(got) != 1 {
		t.Errorf("leftovers %v, want [%s]", got, pbNewAlert)
	}
}

func pbBeforeNamedKind(t *testing.T) {
	t.Parallel()
	c, l, inv := pbBeforeWorld(t)
	bucket := "lzprobe-" + c.RunID + "-x"
	path := "/cloud/project/p1/region/GRA11/storage"
	var page []map[string]any
	if err := json.Unmarshal(l.responses[path][0], &page); err != nil {
		t.Fatal(err)
	}
	page = append(page, map[string]any{"name": bucket, "region": "GRA11"})
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	l.responses[path] = []json.RawMessage{raw}
	c.Before = map[string][]string{tAlert: {"al-budget-0001", "al-ops-0002"}, tStorage: {bucket}}
	got := pbLeftovers(t, c, inv)
	if !slices.Contains(got, tStorage+" "+bucket) {
		t.Errorf("leftovers %v: bucket %s, named by the run, is no leftover because Before lists it", got, bucket)
	}
	if !slices.Contains(got, pbNewAlert) {
		t.Errorf("leftovers %v: alert al-new-0003, absent from the baseline, is not the run's leftover", got)
	}
}

func pbBeforeUnset(t *testing.T, before map[string][]string) {
	t.Parallel()
	c, _, inv := pbBeforeWorld(t)
	c.Before = before
	inv = append(inv, InventoryEntry{Stack: "project", Address: "module.project.ovh_cloud_project_alerting.this[0]", Type: tAlert, ID: "al-new-0003"})
	got := pbLeftovers(t, c, inv)
	if slices.Contains(got, tAlert+" al-ops-0002") {
		t.Errorf("leftovers %v: pre-existing alert al-ops-0002 (no inventory line, no run id) is a leftover", got)
	}
	if !slices.Equal(got, []string{pbNewAlert}) {
		t.Errorf("leftovers %v, want the inventory's alert [%s] only", got, pbNewAlert)
	}
}

// pbBaselineFile is a baseline of run swRunID on probeProject, changed by edit.
func pbBaselineFile(t *testing.T, edit func(m map[string]any)) []byte {
	t.Helper()
	m := map[string]any{
		"run_id":         swRunID,
		"project_sha256": pbDigest(probeProject),
		"listed":         map[string]any{tAlert: []string{"al-1"}},
		"complete":       true,
		"errors":         []string{},
	}
	if edit != nil {
		edit(m)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// pbBaselinePaths writes record and recovery (nil: absent) and returns LoadBaseline's arguments.
func pbBaselinePaths(t *testing.T, record, recovery []byte) (recordDir, recoveryPath string) {
	t.Helper()
	base := tempPrivate(t)
	recordDir = mkdirPrivate(t, filepath.Join(base, "live", swRunID))
	recoveryPath = filepath.Join(mkdirPrivate(t, filepath.Join(base, "baselines")), swRunID+".json")
	for path, raw := range map[string][]byte{filepath.Join(recordDir, "baseline.json"): record, recoveryPath: recovery} {
		if raw == nil {
			continue
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return recordDir, recoveryPath
}

// TestClassifyRun: the class of a run from its state, its inventory and its baseline as
// LoadBaseline reads it. Missing state alone never proves that nothing was created, and a readable
// baseline is not enough: it must name this run and project, list the alert kind, be complete and
// carry no errors.
func TestClassifyRun(t *testing.T) {
	t.Parallel()
	incomplete := func(m map[string]any) {
		m["complete"] = false
		m["listed"] = map[string]any{}
		m["errors"] = []string{"listing /cloud/project/p1/alerting: 503"}
	}
	cases := []struct {
		name             string
		record, recovery []byte
		state, inv       bool
		want             RunClass
		errPrefix        string // "" with a valid baseline: no error
	}{
		{name: "nothing", want: RunPreCreate, errPrefix: "BASELINE_MISSING"},
		{name: "incomplete-no-state", record: pbBaselineFile(t, incomplete), want: RunPreCreate, errPrefix: "-"},
		{name: "valid", record: pbBaselineFile(t, nil), want: RunBaselineBacked},
		{name: "valid-empty-alert-list", record: pbBaselineFile(t, func(m map[string]any) { m["listed"] = map[string]any{tAlert: []string{}} }), want: RunBaselineBacked},
		{name: "valid-recovery-beside-state", recovery: pbBaselineFile(t, nil), state: true, inv: true, want: RunBaselineBacked},
		{name: "state-no-baseline", state: true, want: RunUntrusted, errPrefix: "BASELINE_MISSING"},
		{name: "inventory-no-baseline", inv: true, want: RunUntrusted, errPrefix: "BASELINE_MISSING"},
		{name: "unreadable", record: []byte(`{"run_id":`), want: RunUntrusted, errPrefix: "BASELINE_INVALID"},
		{name: "incomplete-another-run", record: pbBaselineFile(t, func(m map[string]any) { incomplete(m); m["run_id"] = pbRun2 }), want: RunUntrusted, errPrefix: "BASELINE_INVALID"},
		{name: "another-run", record: pbBaselineFile(t, func(m map[string]any) { m["run_id"] = pbRun2 }), want: RunUntrusted, errPrefix: "BASELINE_INVALID"},
		{name: "another-project", record: pbBaselineFile(t, func(m map[string]any) { m["project_sha256"] = pbDigest("p2") }), want: RunUntrusted, errPrefix: "BASELINE_INVALID"},
		{name: "no-alert-key", record: pbBaselineFile(t, func(m map[string]any) { m["listed"] = map[string]any{} }), want: RunUntrusted, errPrefix: "BASELINE_INVALID"},
		{name: "incomplete-beside-state", record: pbBaselineFile(t, incomplete), state: true, want: RunUntrusted, errPrefix: "-"},
		{name: "incomplete-beside-inventory", record: pbBaselineFile(t, incomplete), inv: true, want: RunUntrusted, errPrefix: "-"},
		{name: "errors-beside-state", record: pbBaselineFile(t, func(m map[string]any) { m["errors"] = []string{"x"} }), state: true, want: RunUntrusted, errPrefix: "BASELINE_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recordDir, recoveryPath := pbBaselinePaths(t, tc.record, tc.recovery)
			b, err := LoadBaseline(recordDir, recoveryPath, swRunID, probeProject)
			switch {
			case tc.errPrefix == "" && (err != nil || b.RunID != swRunID):
				t.Errorf("LoadBaseline of a valid baseline = %+v, %v; want it read, no error", b, err)
			case tc.errPrefix != "" && tc.errPrefix != "-" && (err == nil || !strings.HasPrefix(err.Error(), tc.errPrefix)):
				t.Errorf("LoadBaseline error = %v, want one starting %s", err, tc.errPrefix)
			}
			if got := ClassifyRun(tc.state, tc.inv, b, err); got != tc.want {
				t.Errorf("ClassifyRun(state %v, inventory %v, LoadBaseline %v) = %q, want %q", tc.state, tc.inv, err, got, tc.want)
			}
		})
	}
}

// TestLoadBaselineRecoveryCopy: with the record copy lost, the recovery copy is read; with both
// lost, BASELINE_MISSING.
func TestLoadBaselineRecoveryCopy(t *testing.T) {
	t.Parallel()
	recordDir, recoveryPath := pbBaselinePaths(t, nil, pbBaselineFile(t, nil))
	b, err := LoadBaseline(recordDir, recoveryPath, swRunID, probeProject)
	if err != nil || b.RunID != swRunID || !slices.Equal(b.Listed[tAlert], []string{"al-1"}) || !b.Complete {
		t.Errorf("LoadBaseline with only the recovery copy = %+v, %v; want the recovery copy", b, err)
	}
	recordDir, recoveryPath = pbBaselinePaths(t, nil, nil)
	if _, err := LoadBaseline(recordDir, recoveryPath, swRunID, probeProject); err == nil || !strings.HasPrefix(err.Error(), "BASELINE_MISSING") {
		t.Errorf("LoadBaseline with both copies lost: %v; want BASELINE_MISSING", err)
	}
}

// TestProbeStartUnreconciledRun: another run's directory under state/probes/ refuses a probe start
// (unreconciled-run, naming that run and its --cleanup) before any file of the new run is written,
// and leaves the other run's files untouched.
func TestProbeStartUnreconciledRun(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	other := mkdirPrivate(t, filepath.Join(pbStateDir(root), "probes", pbRun2))
	if err := os.WriteFile(filepath.Join(other, "probe.env"), []byte("LZ_PROBE_ROOT=/x\nLZ_PROBE_PROJECT_ID=p1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, other)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	err := p.Start(context.Background())
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Condition != CondUnreconciledRun {
		t.Errorf("probe start beside unreconciled run %s: %v; want refused (%s)", pbRun2, err, CondUnreconciledRun)
	} else if !strings.Contains(ref.Detail, pbRun2) || !strings.Contains(ref.Detail, "--cleanup "+pbRun2) {
		t.Errorf("refusal %q does not name run %s and --cleanup %s", ref.Detail, pbRun2, pbRun2)
	}
	if dir, _, _ := probeFiles(t, root); exists(dir) {
		t.Errorf("the refused start wrote %s", dir)
	}
	if exists(w.runDir) {
		t.Errorf("the refused start wrote its run record %s", w.runDir)
	}
	if exists(pbRecovery(root, p.Run.ID)) {
		t.Error("the refused start wrote a recovery copy")
	}
	if calls := w.calls(t, "tofu.log"); len(calls) > 0 {
		t.Errorf("the refused start ran %d tofu calls", len(calls))
	}
	sameTree(t, "the other run's directory", other, before)
}

// TestProbeStartNoOtherRun: pin — with no other run's directory a probe start proceeds (the
// refusal never counts the run's own directory).
func TestProbeStartNoOtherRun(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("probe start with no other run: %v; want it to proceed", err)
	}
	if pbApply(w.calls(t, "tofu.log")) == nil {
		t.Error("the probe root was never applied")
	}
}

// TestUnreconciledRuns: the run ids of the directories under state/probes/ other than self, sorted;
// a regular file is no run.
func TestUnreconciledRuns(t *testing.T) {
	t.Parallel()
	probes := tempPrivate(t)
	for _, id := range []string{pbRun2, swRunID, "20261006T110000Z-0f0f"} {
		mkdirPrivate(t, filepath.Join(probes, id))
	}
	if err := os.WriteFile(filepath.Join(probes, "notes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := unreconciledRuns(probes, swRunID)
	if err != nil || !slices.Equal(got, []string{"20261006T110000Z-0f0f", pbRun2}) {
		t.Errorf("unreconciledRuns = %v, %v; want [20261006T110000Z-0f0f %s]", got, err, pbRun2)
	}
	only := tempPrivate(t)
	mkdirPrivate(t, filepath.Join(only, swRunID))
	if got, err := unreconciledRuns(only, swRunID); err != nil || len(got) != 0 {
		t.Errorf("unreconciledRuns with only the run's own directory = %v, %v; want none", got, err)
	}
}
