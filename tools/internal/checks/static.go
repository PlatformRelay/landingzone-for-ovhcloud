package checks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// discover lists the *.tf files the clauses must cover, relative to dir. Tool
// state directories such as .terraform are not configuration.
func discover(dir string) []string {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != dir && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".tf") {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil
	}
	slices.Sort(files)
	return files
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
	r := StaticResult{Clause: "validate"}
	stdout, stderr, code := runTool(dir, tofu, "validate", "-json", "-no-color")
	// The exit status decides; the JSON report supplies the diagnostics, or the
	// raw output is kept when it cannot be read.
	var report struct {
		Diagnostics []struct {
			Severity string `json:"severity"`
			Summary  string `json:"summary"`
			Detail   string `json:"detail"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		r.Messages = append(lines(stdout), lines(stderr)...)
	}
	for _, d := range report.Diagnostics {
		r.Messages = append(r.Messages, fmt.Sprintf("%s: %s: %s", d.Severity, d.Summary, d.Detail))
	}
	r.Status = StatusPass
	if code != 0 {
		r.Status, r.Reason = StatusFail, "INVALID"
	}
	return r
}

func checkLint(tflint, config, dir string) StaticResult {
	r := StaticResult{Clause: "tflint"}
	stdout, stderr, code := runTool(dir, tflint, "--config="+config, "--format=json", "--no-color")
	// The exit status decides (0 clean, 2 issues, 1 errors); the JSON report
	// supplies rule names, messages and files, or the raw output is kept.
	var report struct {
		Issues []struct {
			Rule struct {
				Name string `json:"name"`
			} `json:"rule"`
			Message string `json:"message"`
			Range   struct {
				Filename string `json:"filename"`
			} `json:"range"`
		} `json:"issues"`
		Errors []struct {
			Summary string `json:"summary"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		r.Messages = append(lines(stdout), lines(stderr)...)
	}
	for _, e := range report.Errors {
		r.Messages = append(r.Messages, e.Summary+": "+e.Message)
	}
	for _, issue := range report.Issues {
		r.Messages = append(r.Messages, issue.Rule.Name+": "+issue.Message)
		r.Files = append(r.Files, issue.Range.Filename)
	}
	switch code {
	case 0:
		r.Status = StatusPass
	case 2:
		r.Status, r.Reason = StatusFail, "LINT_ISSUES"
	default:
		r.Status, r.Reason = StatusFail, "LINT_ERROR"
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
