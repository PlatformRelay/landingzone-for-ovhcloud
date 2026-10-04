package checks

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const staticReports = "../../../tests/check/fixtures/static/reports/"

// capturedReport reads a report captured from the pinned tool through the
// offline entry (tests/check/fixtures/static/capture.sh) and its exit status.
func capturedReport(t *testing.T, name string) (string, int) {
	t.Helper()
	report, err := os.ReadFile(staticReports + name + ".json")
	if err != nil {
		t.Fatalf("FIXTURE_MISSING: %v", err)
	}
	status, err := os.ReadFile(staticReports + name + ".exit")
	if err != nil {
		t.Fatalf("FIXTURE_MISSING: %v", err)
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(status)))
	if err != nil {
		t.Fatalf("FIXTURE_INVALID: %v", err)
	}
	return string(report), code
}

func kept(r StaticResult) string {
	return strings.Join(append(append([]string{}, r.Files...), r.Messages...), "\n")
}

// The pinned tools' own reports are judged by status and content together,
// and keep each diagnostic's location.
func TestStaticReportsCaptured(t *testing.T) {
	for name, c := range map[string]struct {
		evaluate func(stdout, stderr string, code int) StaticResult
		want     string
		detail   []string
	}{
		"validate-valid":     {validateResult, "pass ", nil},
		"validate-malformed": {validateResult, "fail INVALID", []string{"main.tf:2:22", "Invalid expression"}},
		"tflint-valid":       {lintResult, "pass ", nil},
		"tflint-lint":        {lintResult, "fail LINT_ISSUES", []string{"main.tf:5:1", "terraform_typed_variables", "terraform_unused_declarations"}},
		"tflint-malformed":   {lintResult, "fail LINT_ERROR", []string{"main.tf:2:22", "Invalid expression"}},
	} {
		t.Run(name, func(t *testing.T) {
			report, code := capturedReport(t, name)
			r := c.evaluate(report, "", code)
			if got := r.Status + " " + r.Reason; got != c.want {
				t.Errorf("BEHAVIORAL_RED: %s = %q, want %q (%+v)", name, got, c.want, r)
			}
			for _, d := range c.detail {
				if !strings.Contains(kept(r), d) {
					t.Errorf("BEHAVIORAL_RED: %s lost %q: %+v", name, d, r)
				}
			}
		})
	}
}

// Corrupted reports and statuses that contradict the report never pass, and
// the raw output is kept.
func TestStaticReportsCorrupted(t *testing.T) {
	validValidate, _ := capturedReport(t, "validate-valid")
	invalidValidate, _ := capturedReport(t, "validate-malformed")
	validLint, _ := capturedReport(t, "tflint-valid")
	issues, _ := capturedReport(t, "tflint-lint")
	lintErrors, _ := capturedReport(t, "tflint-malformed")
	for name, c := range map[string]struct {
		evaluate func(stdout, stderr string, code int) StaticResult
		stdout   string
		code     int
		want     string
	}{
		"validate empty report":                        {validateResult, "", 0, "fail MALFORMED_OUTPUT"},
		"validate truncated report":                    {validateResult, validValidate[:len(validValidate)/2], 0, "fail MALFORMED_OUTPUT"},
		"validate without valid":                       {validateResult, strings.Replace(validValidate, `"valid"`, `"other"`, 1), 0, "fail MALFORMED_OUTPUT"},
		"validate without diagnostics":                 {validateResult, strings.Replace(validValidate, `"diagnostics"`, `"other"`, 1), 0, "fail MALFORMED_OUTPUT"},
		"validate without error count":                 {validateResult, strings.Replace(validValidate, `"error_count"`, `"other"`, 1), 0, "fail MALFORMED_OUTPUT"},
		"validate invalid but exit 0":                  {validateResult, invalidValidate, 0, "fail INCONSISTENT_OUTPUT"},
		"validate valid but exit 1":                    {validateResult, validValidate, 1, "fail INCONSISTENT_OUTPUT"},
		"validate valid with errors":                   {validateResult, strings.Replace(validValidate, `"error_count": 0`, `"error_count": 1`, 1), 0, "fail INCONSISTENT_OUTPUT"},
		"validate not valid without errors":            {validateResult, strings.Replace(validValidate, `"valid": true`, `"valid": false`, 1), 0, "fail INCONSISTENT_OUTPUT"},
		"validate error diagnostic in a clean summary": {validateResult, strings.Replace(strings.Replace(invalidValidate, `"valid": false`, `"valid": true`, 1), `"error_count": 1`, `"error_count": 0`, 1), 0, "fail INCONSISTENT_OUTPUT"},
		"tflint empty report":                          {lintResult, "", 0, "fail MALFORMED_OUTPUT"},
		"tflint truncated report":                      {lintResult, issues[:len(issues)/2], 2, "fail MALFORMED_OUTPUT"},
		"tflint without issues":                        {lintResult, `{"errors":[]}`, 0, "fail MALFORMED_OUTPUT"},
		"tflint without errors":                        {lintResult, `{"issues":[]}`, 0, "fail MALFORMED_OUTPUT"},
		"tflint issues but exit 0":                     {lintResult, issues, 0, "fail INCONSISTENT_OUTPUT"},
		"tflint clean but exit 2":                      {lintResult, validLint, 2, "fail INCONSISTENT_OUTPUT"},
		"tflint clean but exit 1":                      {lintResult, validLint, 1, "fail INCONSISTENT_OUTPUT"},
		"tflint errors but exit 0":                     {lintResult, lintErrors, 0, "fail INCONSISTENT_OUTPUT"},
		"tflint errors but exit 2":                     {lintResult, lintErrors, 2, "fail INCONSISTENT_OUTPUT"},
		"tflint issues with an unknown exit":           {lintResult, issues, 3, "fail INCONSISTENT_OUTPUT"},
	} {
		t.Run(name, func(t *testing.T) {
			r := c.evaluate(c.stdout, "tool stderr line", c.code)
			if got := r.Status + " " + r.Reason; got != c.want {
				t.Errorf("BEHAVIORAL_RED: %s = %q, want %q (%+v)", name, got, c.want, r)
			}
			if !strings.Contains(kept(r), "tool stderr line") {
				t.Errorf("BEHAVIORAL_RED: stderr not kept: %+v", r)
			}
			if c.stdout != "" && strings.HasSuffix(c.want, "MALFORMED_OUTPUT") && !strings.Contains(kept(r), strings.SplitN(strings.TrimSpace(c.stdout), "\n", 2)[0]) {
				t.Errorf("BEHAVIORAL_RED: raw report not kept: %+v", r)
			}
		})
	}
}

// Discovery counts the module directory's own configuration: the tools run
// there, so files only in subdirectories would leave them checking nothing.
func TestStaticDiscoveryIsTheModuleDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "child/main.tf"), []byte("variable \"x\" {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := RunStatic(StaticTools{}, root)
	if o.Pass || !reflect.DeepEqual(o.Results, []StaticResult{{Clause: "discovery", Status: StatusFail, Reason: "NO_DISCOVERY"}}) {
		t.Errorf("BEHAVIORAL_RED: nested-only configuration discovered: %+v", o)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte("variable \"y\" {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o := RunStatic(StaticTools{}, root); !reflect.DeepEqual(o.Discovered, []string{"main.tf"}) {
		t.Errorf("BEHAVIORAL_RED: module files %v, want [main.tf]", o.Discovered)
	}
}
