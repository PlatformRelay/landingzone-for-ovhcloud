package checks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// StaticTools locates the pinned tools and the TFLint configuration.
type StaticTools struct {
	Tofu, TFLint, TFLintConfig string
}

// StaticResult is one static clause's outcome with the upstream detail kept.
// Status is pass, fail or blocked.
type StaticResult struct {
	Clause   string
	Status   string
	Reason   string
	Files    []string
	Messages []string
}

// StaticObservation is every static clause for one module directory.
type StaticObservation struct {
	Dir        string
	Discovered []string
	Results    []StaticResult
	Pass       bool
}

func executable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

func regularFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// discover lists the module directory's own *.tf files. init, validate and
// TFLint read only those, so configuration in subdirectories (or tool state
// under .terraform) does not count.
func discover(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".tf") {
			files = append(files, e.Name())
		}
	}
	slices.Sort(files)
	return files
}

// located formats a diagnostic with its source location.
func located(file string, line, column int, text string) string {
	if file == "" {
		return text
	}
	return fmt.Sprintf("%s:%d:%d: %s", file, line, column, text)
}

// sourceRange is the location shape both tools report.
type sourceRange struct {
	Filename string `json:"filename"`
	Start    struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"start"`
}

// runTool runs a pinned tool in dir and returns its streams and exit code; a
// tool that cannot start at all reports code -1.
func runTool(dir, name string, args ...string) (stdout, stderr string, code int) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		code = -1
		errOut.WriteString(err.Error())
	}
	return out.String(), strings.TrimSpace(errOut.String()), code
}

func lines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func checkFormat(tofu, dir string) StaticResult {
	r := StaticResult{Clause: "fmt"}
	stdout, stderr, code := runTool(dir, tofu, "fmt", "-check", "-recursive", "-no-color", ".")
	switch code {
	case 0:
		r.Status = StatusPass
	case 3:
		r.Status, r.Reason, r.Files = StatusFail, "UNFORMATTED", lines(stdout)
	default:
		r.Status, r.Reason, r.Messages = StatusFail, "FORMAT_ERROR", lines(stderr)
	}
	return r
}

func checkInit(tofu, dir string) StaticResult {
	r := StaticResult{Clause: "init", Status: StatusPass}
	if _, stderr, code := runTool(dir, tofu, "init", "-backend=false", "-lockfile=readonly", "-input=false", "-no-color"); code != 0 {
		r.Status, r.Reason, r.Messages = StatusFail, "INIT_FAILED", lines(stderr)
	}
	return r
}

func checkValidate(tofu, dir string) StaticResult {
	return validateResult(runTool(dir, tofu, "validate", "-json", "-no-color"))
}

func validateResult(stdout, stderr string, code int) StaticResult {
	r := StaticResult{Clause: "validate"}
	var report struct {
		Valid       *bool `json:"valid"`
		ErrorCount  *int  `json:"error_count"`
		Diagnostics *[]struct {
			Severity string       `json:"severity"`
			Summary  string       `json:"summary"`
			Detail   string       `json:"detail"`
			Range    *sourceRange `json:"range"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || report.Valid == nil || report.ErrorCount == nil || report.Diagnostics == nil {
		r.Status, r.Reason, r.Messages = StatusFail, "MALFORMED_OUTPUT", append(lines(stdout), lines(stderr)...)
		return r
	}
	errorDiagnostics := 0
	for _, d := range *report.Diagnostics {
		if d.Severity == "error" {
			errorDiagnostics++
		}
		var where sourceRange
		if d.Range != nil {
			where = *d.Range
		}
		r.Messages = append(r.Messages, located(where.Filename, where.Start.Line, where.Start.Column, fmt.Sprintf("%s: %s: %s", d.Severity, d.Summary, d.Detail)))
	}
	r.Messages = append(r.Messages, lines(stderr)...)
	// The exit status, the summary and the diagnostics must all agree.
	clean := *report.Valid && *report.ErrorCount == 0
	switch {
	case errorDiagnostics != *report.ErrorCount:
		r.Status, r.Reason = StatusFail, "INCONSISTENT_OUTPUT"
	case code == 0 && clean:
		r.Status = StatusPass
	case code == 1 && !*report.Valid:
		r.Status, r.Reason = StatusFail, "INVALID"
	default:
		r.Status, r.Reason = StatusFail, "INCONSISTENT_OUTPUT"
	}
	return r
}

func checkLint(tflint, config, dir string) StaticResult {
	return lintResult(runTool(dir, tflint, "--config="+config, "--format=json", "--no-color"))
}

func lintResult(stdout, stderr string, code int) StaticResult {
	r := StaticResult{Clause: "tflint"}
	var report struct {
		Issues *[]struct {
			Rule struct {
				Name string `json:"name"`
			} `json:"rule"`
			Message string      `json:"message"`
			Range   sourceRange `json:"range"`
		} `json:"issues"`
		Errors *[]struct {
			Summary string      `json:"summary"`
			Message string      `json:"message"`
			Range   sourceRange `json:"range"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || report.Issues == nil || report.Errors == nil {
		r.Status, r.Reason, r.Messages = StatusFail, "MALFORMED_OUTPUT", append(lines(stdout), lines(stderr)...)
		return r
	}
	for _, e := range *report.Errors {
		r.Messages = append(r.Messages, located(e.Range.Filename, e.Range.Start.Line, e.Range.Start.Column, e.Summary+": "+e.Message))
	}
	for _, issue := range *report.Issues {
		r.Messages = append(r.Messages, located(issue.Range.Filename, issue.Range.Start.Line, issue.Range.Start.Column, issue.Rule.Name+": "+issue.Message))
		r.Files = append(r.Files, issue.Range.Filename)
	}
	r.Messages = append(r.Messages, lines(stderr)...)
	// The exit status (0 clean, 2 issues, 1 errors) and the report must agree.
	issues, errs := len(*report.Issues), len(*report.Errors)
	switch {
	case code == 0 && issues == 0 && errs == 0:
		r.Status = StatusPass
	case code == 2 && issues > 0 && errs == 0:
		r.Status, r.Reason = StatusFail, "LINT_ISSUES"
	case code == 1 && errs > 0:
		r.Status, r.Reason = StatusFail, "LINT_ERROR"
	default:
		r.Status, r.Reason = StatusFail, "INCONSISTENT_OUTPUT"
	}
	return r
}

// RunStatic runs the applicable L0 clauses on one module directory with the
// pinned tools: tofu fmt -check, mirror-only tofu init, tofu validate -json and
// TFLint with the repository configuration. Discovery is counted here, because
// both tools exit 0 on a directory without configuration; a missing tool
// blocks its clauses rather than passing or failing them.
func RunStatic(tools StaticTools, dir string) StaticObservation {
	o := StaticObservation{Dir: dir, Discovered: discover(dir)}
	if len(o.Discovered) == 0 {
		o.Results = []StaticResult{{Clause: "discovery", Status: StatusFail, Reason: "NO_DISCOVERY"}}
		return o
	}
	blocked := func(clause string) StaticResult {
		return StaticResult{Clause: clause, Status: StatusBlocked, Reason: "TOOL_ABSENT"}
	}
	if executable(tools.Tofu) {
		o.Results = append(o.Results, checkFormat(tools.Tofu, dir), checkInit(tools.Tofu, dir), checkValidate(tools.Tofu, dir))
	} else {
		o.Results = append(o.Results, blocked("fmt"), blocked("init"), blocked("validate"))
	}
	// The tools run inside dir, so a relative configuration path would resolve
	// against the module rather than the caller.
	config, err := filepath.Abs(tools.TFLintConfig)
	if err == nil && executable(tools.TFLint) && regularFile(config) {
		o.Results = append(o.Results, checkLint(tools.TFLint, config, dir))
	} else {
		o.Results = append(o.Results, blocked("tflint"))
	}
	o.Pass = true
	for _, r := range o.Results {
		o.Pass = o.Pass && r.Status == StatusPass
	}
	return o
}
