package checks

import "github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/report"

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

// RunUnit runs the L1 tests of one directory with the pinned tofu.
// Stub (T003): reports pass without running anything; T004 implements it.
func RunUnit(tools StaticTools, dir string) UnitObservation {
	return UnitObservation{Dir: dir, Status: StatusPass, Pass: true}
}

// RunSlice runs the static and unit clauses on every directory in dirs.
// Stub (T003): reports pass without running anything; T004 implements it.
func RunSlice(tools StaticTools, dirs []string) SliceObservation {
	return SliceObservation{Status: StatusPass, Pass: true}
}
