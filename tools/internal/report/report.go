// Package report turns a pinned `tofu test -json` stream into one observation
// that keeps the upstream diagnostics.
//
// tofu 1.13.0 exits 0 and reports `pass` for zero tests and for failed test
// cleanup, so neither its exit status nor its summary is the verdict: every
// clause below is checked independently, and the observation passes only when
// none of them found a reason to fail.
package report

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
)

// Status is the observation's verdict.
type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
)

// Reasons an observation fails. Each names the clause it guards.
const (
	ReasonEmpty           = "EMPTY_STREAM"     // C003.2
	ReasonTruncated       = "TRUNCATED_STREAM" // C003.2
	ReasonMalformed       = "MALFORMED_STREAM" // C003.2
	ReasonMissingVersion  = "MISSING_VERSION"  // C003.2
	ReasonMissingSummary  = "MISSING_SUMMARY"  // C003.2
	ReasonExitStatus      = "NONZERO_EXIT"     // C003.2
	ReasonZeroTests       = "ZERO_TESTS"       // C003.3
	ReasonSkipped         = "SKIPPED_RUN"      // C003.3
	ReasonFailed          = "TESTS_FAILED"     // C003.1
	ReasonCountMismatch   = "COUNT_MISMATCH"   // C003.2
	ReasonErrorDiagnostic = "ERROR_DIAGNOSTIC" // C003.1
	ReasonCleanupFailed   = "CLEANUP_FAILED"   // C003.4
	maxLine               = 4 << 20
)

// Diagnostic is an upstream diagnostic, preserved as tofu reported it.
type Diagnostic struct {
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail,omitempty"`
}

// Run is one tofu test run block and its upstream status.
type Run struct {
	File   string `json:"file"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Observation is the result of one tofu test run.
type Observation struct {
	Status      Status       `json:"status"`
	Tool        string       `json:"tool"`
	ToolVersion string       `json:"tool_version"`
	ExitCode    int          `json:"exit_code"`
	Discovered  int          `json:"discovered"`
	Passed      int          `json:"passed"`
	Failed      int          `json:"failed"`
	Errored     int          `json:"errored"`
	Skipped     int          `json:"skipped"`
	Runs        []Run        `json:"runs"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Reasons     []string     `json:"reasons"`
}

type event struct {
	Type       string              `json:"type"`
	Tofu       string              `json:"tofu"`
	Diagnostic *Diagnostic         `json:"diagnostic"`
	Abstract   map[string][]string `json:"test_abstract"`
	Run        *struct {
		Path   string `json:"path"`
		Run    string `json:"run"`
		Status string `json:"status"`
	} `json:"test_run"`
	Summary *struct {
		Status  string `json:"status"`
		Passed  int    `json:"passed"`
		Failed  int    `json:"failed"`
		Errored int    `json:"errored"`
		Skipped int    `json:"skipped"`
	} `json:"test_summary"`
	Cleanup *struct {
		FailedResources []json.RawMessage `json:"failed_resources"`
	} `json:"test_cleanup"`
}

// Evaluate reads a complete `tofu test -json` stream and the process's exit
// status. It never returns pass for a stream it could not fully account for.
func Evaluate(stream []byte, exitCode int) Observation {
	o := Observation{Tool: "tofu", ExitCode: exitCode, Runs: []Run{}, Diagnostics: []Diagnostic{}, Reasons: []string{}}
	fail := func(reason string) {
		for _, existing := range o.Reasons {
			if existing == reason {
				return
			}
		}
		o.Reasons = append(o.Reasons, reason)
	}
	if exitCode != 0 {
		fail(ReasonExitStatus)
	}
	if len(bytes.TrimSpace(stream)) == 0 {
		fail(ReasonEmpty)
		return finish(o)
	}
	if stream[len(stream)-1] != '\n' {
		fail(ReasonTruncated)
	}
	summarised, versioned := false, false
	scanner := bufio.NewScanner(bytes.NewReader(stream))
	scanner.Buffer(make([]byte, 64<<10), maxLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var e event
		if err := json.Unmarshal(line, &e); err != nil {
			fail(ReasonMalformed)
			continue
		}
		switch e.Type {
		case "version":
			versioned, o.ToolVersion = e.Tofu != "", e.Tofu
		case "test_abstract":
			for _, runs := range e.Abstract {
				o.Discovered += len(runs)
			}
		case "test_run":
			if e.Run != nil {
				o.Runs = append(o.Runs, Run{File: e.Run.Path, Name: e.Run.Run, Status: e.Run.Status})
				if e.Run.Status == "skip" {
					fail(ReasonSkipped)
				}
			}
		case "diagnostic":
			if e.Diagnostic != nil {
				o.Diagnostics = append(o.Diagnostics, *e.Diagnostic)
				if e.Diagnostic.Severity == "error" {
					fail(ReasonErrorDiagnostic)
				}
			}
		case "test_cleanup":
			if e.Cleanup != nil && len(e.Cleanup.FailedResources) > 0 {
				fail(ReasonCleanupFailed)
			}
		case "test_summary":
			if e.Summary != nil {
				summarised = true
				o.Passed, o.Failed, o.Errored, o.Skipped = e.Summary.Passed, e.Summary.Failed, e.Summary.Errored, e.Summary.Skipped
				if e.Summary.Status != "pass" || o.Failed+o.Errored > 0 {
					fail(ReasonFailed)
				}
				if o.Skipped > 0 {
					fail(ReasonSkipped)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		fail(ReasonMalformed)
	}
	if !versioned {
		fail(ReasonMissingVersion)
	}
	if !summarised {
		fail(ReasonMissingSummary)
	} else if o.Discovered != o.Passed+o.Failed+o.Errored+o.Skipped {
		fail(ReasonCountMismatch)
	}
	if o.Discovered == 0 {
		fail(ReasonZeroTests)
	}
	return finish(o)
}

func finish(o Observation) Observation {
	o.Status = Pass
	if len(o.Reasons) > 0 {
		o.Status = Fail
	}
	return o
}

// String summarises the observation for logs.
func (o Observation) String() string {
	return fmt.Sprintf("%s: %d discovered, %d passed, %d failed, %d errored, %d skipped; reasons %v",
		o.Status, o.Discovered, o.Passed, o.Failed, o.Errored, o.Skipped, o.Reasons)
}
