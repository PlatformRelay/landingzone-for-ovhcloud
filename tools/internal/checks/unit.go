package checks

import (
	"slices"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/report"
)

// UnitObservation is one directory's L1 result: mirror-only tofu init, then
// tofu test -json judged by the report adapter. Report is nil when the tests
// did not run (init failed or tofu is absent). Status is pass, fail or
// blocked; Reason names the first failing step.
type UnitObservation struct {
	Dir    string
	Init   StaticResult
	Report *report.Observation
	Status string
	Reason string
	Pass   bool
}

// SliceEntry is one discovered directory's static and unit result.
type SliceEntry struct {
	Dir    string
	Static StaticObservation
	Unit   UnitObservation
}

// SliceObservation is every discovered directory's result. An empty
// directory list is a failure, never a vacuous pass.
type SliceObservation struct {
	Entries []SliceEntry
	Status  string
	Reason  string
	Pass    bool
}

// RunUnit runs the L1 tests of one directory with the pinned tofu: the
// mirror-only init of the static clauses, then tofu test -json in the
// directory. The adapter judges the stream and the exit status together, so
// zero tests, a skipped run, a cut-off stream or a crash fail even when tofu
// exits 0; a missing tofu blocks.
func RunUnit(tools StaticTools, dir string) UnitObservation {
	o := UnitObservation{Dir: dir}
	if !executable(tools.Tofu) {
		o.Status, o.Reason = StatusBlocked, "TOOL_ABSENT"
		return o
	}
	o.Init = checkInit(tools.Tofu, dir)
	if o.Init.Status != StatusPass {
		o.Status, o.Reason = StatusFail, "INIT_FAILED"
		return o
	}
	// The stream carries tofu's diagnostics; stderr adds nothing the adapter
	// judges.
	stdout, _, code := runTool(dir, tools.Tofu, "test", "-json", "-no-color")
	r := report.Evaluate([]byte(stdout), code)
	o.Report = &r
	if r.Status == report.Pass {
		o.Status, o.Pass = StatusPass, true
	} else {
		o.Status, o.Reason = StatusFail, "TEST_REPORT_FAILED"
	}
	return o
}

// RunSlice runs the static and unit clauses on every directory in dirs, in
// order and without stopping at the first failure. It passes only when every
// directory passes both; an empty list fails with NO_DISCOVERY.
func RunSlice(tools StaticTools, dirs []string) SliceObservation {
	o := SliceObservation{Entries: []SliceEntry{}}
	if len(dirs) == 0 {
		o.Status, o.Reason = StatusFail, "NO_DISCOVERY"
		return o
	}
	o.Pass = true
	for _, d := range dirs {
		e := SliceEntry{Dir: d, Static: RunStatic(tools, d), Unit: RunUnit(tools, d)}
		o.Pass = o.Pass && e.Static.Pass && e.Unit.Pass
		o.Entries = append(o.Entries, e)
	}
	o.Status = StatusFail
	if o.Pass {
		o.Status = StatusPass
	}
	return o
}

// SliceDirs lists the graph's library and stage directories, sorted: the
// directories the slice tests. Tests, examples and generated instances are
// not slice members.
func SliceDirs(g DependencyGraph) []string {
	var dirs []string
	for dir, layer := range g.Layers {
		if layer == LayerLibrary || layer == LayerStage {
			dirs = append(dirs, dir)
		}
	}
	slices.Sort(dirs)
	return dirs
}
