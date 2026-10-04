// Package report turns a pinned `tofu test -json` stream into one observation
// that keeps the upstream diagnostics.
//
// tofu 1.13.0 exits 0 and reports `pass` for zero tests and for failed test
// cleanup, so neither its exit status nor its summary is the verdict. The
// stream is first checked for structure (complete JSON, valid payloads, one
// version, one abstract, one final summary) and reconciled (every discovered
// run reported exactly once, summary and file statuses agreeing with the runs).
// Clause guards then decide from the observed runs alone, each in one place.
// The observation passes only when no guard found a reason.
package report

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Status is the observation's verdict.
type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
)

// Reasons an observation fails, with the clause each guards.
const (
	ReasonEmpty           = "EMPTY_STREAM"     // C003.2
	ReasonTruncated       = "TRUNCATED_STREAM" // C003.2: unparsable last line without newline
	ReasonMalformed       = "MALFORMED_STREAM" // C003.2: a line that is not JSON
	ReasonPayload         = "INVALID_PAYLOAD"  // C003.2: known event with missing or invalid fields
	ReasonLifecycle       = "LIFECYCLE_ORDER"  // C003.2: duplicated or misplaced version/abstract/file/summary
	ReasonMissingVersion  = "MISSING_VERSION"  // C003.2
	ReasonMissingSummary  = "MISSING_SUMMARY"  // C003.2
	ReasonRunMismatch     = "RUN_MISMATCH"     // C003.2: runs disagree with discovery, file status or summary
	ReasonExitStatus      = "NONZERO_EXIT"     // C003.2
	ReasonZeroTests       = "ZERO_TESTS"       // C003.3
	ReasonSkipped         = "SKIPPED_RUN"      // C003.3
	ReasonFailed          = "TESTS_FAILED"     // C003.1
	ReasonErrorDiagnostic = "ERROR_DIAGNOSTIC" // C003.1
	ReasonCleanupFailed   = "CLEANUP_FAILED"   // C003.4
	maxLine               = 4 << 20
)

// Reasons lists every reason Evaluate can report.
var Reasons = []string{ReasonEmpty, ReasonTruncated, ReasonMalformed, ReasonPayload, ReasonLifecycle,
	ReasonMissingVersion, ReasonMissingSummary, ReasonRunMismatch, ReasonExitStatus, ReasonZeroTests,
	ReasonSkipped, ReasonFailed, ReasonErrorDiagnostic, ReasonCleanupFailed}

var runStatuses = map[string]bool{"pass": true, "fail": true, "error": true, "skip": true}

// Diagnostic is an upstream diagnostic. Raw keeps tofu's complete object
// (range, snippet, difference, address, ...); File and Run give its context.
type Diagnostic struct {
	Severity string          `json:"severity"`
	Summary  string          `json:"summary"`
	Detail   string          `json:"detail,omitempty"`
	File     string          `json:"file,omitempty"`
	Run      string          `json:"run,omitempty"`
	Raw      json.RawMessage `json:"raw"`
}

// Run is one tofu test run block and its upstream status.
type Run struct {
	File   string `json:"file"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Observation is the result of one tofu test run.
type Observation struct {
	Status      Status            `json:"status"`
	Tool        string            `json:"tool"`
	ToolVersion string            `json:"tool_version"`
	ExitCode    int               `json:"exit_code"`
	Discovered  int               `json:"discovered"`
	Passed      int               `json:"passed"`
	Failed      int               `json:"failed"`
	Errored     int               `json:"errored"`
	Skipped     int               `json:"skipped"`
	Runs        []Run             `json:"runs"`
	Diagnostics []Diagnostic      `json:"diagnostics"`
	Cleanup     []json.RawMessage `json:"cleanup"`
	Reasons     []string          `json:"reasons"`
}

type summary struct {
	Status  *string `json:"status"`
	Passed  *int    `json:"passed"`
	Failed  *int    `json:"failed"`
	Errored *int    `json:"errored"`
	Skipped *int    `json:"skipped"`
}

type event struct {
	Type       string               `json:"type"`
	Tofu       string               `json:"tofu"`
	Diagnostic json.RawMessage      `json:"diagnostic"`
	Abstract   *map[string][]string `json:"test_abstract"`
	File       *struct {
		Path   string `json:"path"`
		Status string `json:"status"`
	} `json:"test_file"`
	Run *struct {
		Path   string `json:"path"`
		Run    string `json:"run"`
		Status string `json:"status"`
	} `json:"test_run"`
	Summary *summary `json:"test_summary"`
	Cleanup *struct {
		FailedResources *[]json.RawMessage `json:"failed_resources"`
	} `json:"test_cleanup"`
}

type evaluation struct {
	o        Observation
	expected map[[2]string]bool // discovered (file, run) pairs
	reported map[[2]string]bool
	files    map[string]string // reported test_file status
	summary  *summary
	version  bool
	abstract bool
	after    bool // a known event followed the summary
	file     string
	run      string
}

func (e *evaluation) fail(reason string) {
	for _, existing := range e.o.Reasons {
		if existing == reason {
			return
		}
	}
	e.o.Reasons = append(e.o.Reasons, reason)
}

// Evaluate reads a complete `tofu test -json` stream and the exit status of
// the tofu process that produced it. It never returns pass for a stream it
// could not fully account for.
func Evaluate(stream []byte, exitCode int) Observation {
	e := &evaluation{
		o:        Observation{Tool: "tofu", ExitCode: exitCode, Runs: []Run{}, Diagnostics: []Diagnostic{}, Cleanup: []json.RawMessage{}, Reasons: []string{}},
		expected: map[[2]string]bool{}, reported: map[[2]string]bool{}, files: map[string]string{},
	}
	if exitCode != 0 {
		e.fail(ReasonExitStatus)
	}
	if len(bytes.TrimSpace(stream)) == 0 {
		e.fail(ReasonEmpty)
		return e.finish()
	}
	scanner := bufio.NewScanner(bytes.NewReader(stream))
	scanner.Buffer(make([]byte, 64<<10), maxLine)
	consumed := 0
	for scanner.Scan() {
		raw := scanner.Bytes()
		consumed += len(raw) + 1
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		var ev event
		if err := json.Unmarshal(line, &ev); err != nil {
			if consumed > len(stream) { // last line, no newline: cut off mid-write
				e.fail(ReasonTruncated)
			} else {
				e.fail(ReasonMalformed)
			}
			continue
		}
		e.apply(ev)
	}
	if scanner.Err() != nil {
		e.fail(ReasonMalformed)
	}
	e.reconcile()
	e.decide()
	return e.finish()
}

func (e *evaluation) known(ev event) {
	if e.summary != nil {
		e.after = true
	}
}

func (e *evaluation) apply(ev event) {
	switch ev.Type {
	case "version":
		e.known(ev)
		if e.version || e.abstract {
			e.fail(ReasonLifecycle)
		}
		if ev.Tofu == "" {
			e.fail(ReasonPayload)
		}
		e.version, e.o.ToolVersion = true, ev.Tofu
	case "test_abstract":
		e.known(ev)
		if e.abstract { // a missing version is its own reason
			e.fail(ReasonLifecycle)
			return // a second abstract never adds runs
		}
		e.abstract = true
		if ev.Abstract == nil {
			e.fail(ReasonPayload)
			return
		}
		for file, runs := range *ev.Abstract {
			for _, run := range runs {
				key := [2]string{file, run}
				if file == "" || run == "" || e.expected[key] {
					e.fail(ReasonPayload)
				}
				e.expected[key] = true
			}
		}
		e.o.Discovered = len(e.expected)
	case "test_file":
		e.known(ev)
		if !e.abstract {
			e.fail(ReasonLifecycle)
		}
		if ev.File == nil || ev.File.Path == "" || !runStatuses[ev.File.Status] {
			e.fail(ReasonPayload)
			return
		}
		if _, seen := e.files[ev.File.Path]; seen {
			e.fail(ReasonLifecycle)
		}
		e.files[ev.File.Path], e.file, e.run = ev.File.Status, ev.File.Path, ""
	case "test_run":
		// A run must follow its file's test_file event, which itself must follow
		// the abstract, so no separate abstract check is needed here.
		e.known(ev)
		if ev.Run == nil || ev.Run.Path == "" || ev.Run.Run == "" || !runStatuses[ev.Run.Status] {
			e.fail(ReasonPayload)
			return
		}
		if _, fileSeen := e.files[ev.Run.Path]; !fileSeen {
			e.fail(ReasonLifecycle)
		}
		key := [2]string{ev.Run.Path, ev.Run.Run}
		if !e.expected[key] || e.reported[key] {
			e.fail(ReasonRunMismatch)
		}
		e.reported[key] = true
		e.file, e.run = ev.Run.Path, ev.Run.Run
		e.o.Runs = append(e.o.Runs, Run{File: ev.Run.Path, Name: ev.Run.Run, Status: ev.Run.Status})
	case "diagnostic":
		var d Diagnostic
		if len(ev.Diagnostic) == 0 || json.Unmarshal(ev.Diagnostic, &d) != nil || d.Severity == "" || d.Summary == "" {
			e.fail(ReasonPayload)
			return
		}
		d.Raw, d.File, d.Run = ev.Diagnostic, e.file, e.run
		e.o.Diagnostics = append(e.o.Diagnostics, d)
	case "test_cleanup":
		if ev.Cleanup == nil || ev.Cleanup.FailedResources == nil {
			e.fail(ReasonPayload)
			return
		}
		e.o.Cleanup = append(e.o.Cleanup, *ev.Cleanup.FailedResources...)
	case "test_summary":
		e.known(ev)
		if e.summary != nil || !e.abstract {
			e.fail(ReasonLifecycle)
		}
		s := ev.Summary
		if s == nil || s.Status == nil || s.Passed == nil || s.Failed == nil || s.Errored == nil || s.Skipped == nil ||
			*s.Passed < 0 || *s.Failed < 0 || *s.Errored < 0 || *s.Skipped < 0 ||
			(*s.Status != "pass" && *s.Status != "fail" && *s.Status != "error") {
			e.fail(ReasonPayload)
			return
		}
		e.summary = s
	}
}

// reconcile checks that the runs, file statuses and summary describe one
// consistent execution of exactly the discovered runs.
func (e *evaluation) reconcile() {
	if !e.version {
		e.fail(ReasonMissingVersion)
	}
	if e.summary == nil {
		e.fail(ReasonMissingSummary)
	}
	if e.after {
		e.fail(ReasonLifecycle)
	}
	for key := range e.expected {
		if !e.reported[key] {
			e.fail(ReasonRunMismatch)
		}
	}
	tally := map[string]int{}
	worst := map[string]string{} // per file: "fail" if any run failed or errored
	for _, r := range e.o.Runs {
		tally[r.Status]++
		if r.Status == "fail" || r.Status == "error" {
			worst[r.File] = "fail"
		}
	}
	e.o.Passed, e.o.Failed, e.o.Errored, e.o.Skipped = tally["pass"], tally["fail"], tally["error"], tally["skip"]
	for file, status := range e.files {
		if (status == "fail" || status == "error") != (worst[file] == "fail") {
			e.fail(ReasonRunMismatch)
		}
	}
	if s := e.summary; s != nil {
		if *s.Passed != e.o.Passed || *s.Failed != e.o.Failed || *s.Errored != e.o.Errored || *s.Skipped != e.o.Skipped ||
			(*s.Status == "pass") != (e.o.Failed+e.o.Errored == 0) {
			e.fail(ReasonRunMismatch)
		}
	}
}

// decide applies each clause guard once, to the observed runs and events.
func (e *evaluation) decide() {
	if e.o.Discovered == 0 {
		e.fail(ReasonZeroTests)
	}
	if e.o.Failed+e.o.Errored > 0 {
		e.fail(ReasonFailed)
	}
	if e.o.Skipped > 0 {
		e.fail(ReasonSkipped)
	}
	for _, d := range e.o.Diagnostics {
		if d.Severity == "error" {
			e.fail(ReasonErrorDiagnostic)
		}
	}
	if len(e.o.Cleanup) > 0 {
		e.fail(ReasonCleanupFailed)
	}
}

func (e *evaluation) finish() Observation {
	sort.Strings(e.o.Reasons)
	e.o.Status = Pass
	if len(e.o.Reasons) > 0 {
		e.o.Status = Fail
	}
	return e.o
}

// String summarises the observation for logs.
func (o Observation) String() string {
	return fmt.Sprintf("%s: %d discovered, %d passed, %d failed, %d errored, %d skipped; reasons %v",
		o.Status, o.Discovered, o.Passed, o.Failed, o.Errored, o.Skipped, o.Reasons)
}
