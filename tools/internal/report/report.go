// Package report turns a pinned `tofu test -json` stream into one observation
// that keeps the upstream diagnostics.
package report

// Status is the observation's verdict.
type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
)

// Diagnostic is an upstream diagnostic, preserved as tofu reported it.
type Diagnostic struct {
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail,omitempty"`
}

// Observation is the result of one tofu test run.
type Observation struct {
	Status      Status       `json:"status"`
	Tool        string       `json:"tool"`
	ToolVersion string       `json:"tool_version"`
	Discovered  int          `json:"discovered"`
	Passed      int          `json:"passed"`
	Failed      int          `json:"failed"`
	Errored     int          `json:"errored"`
	Skipped     int          `json:"skipped"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Reasons     []string     `json:"reasons"`
}

// Evaluate is a deliberately permissive stub until T005 implements the adapter.
func Evaluate(stream []byte, exitCode int) Observation {
	return Observation{Status: Pass, Tool: "tofu"}
}
