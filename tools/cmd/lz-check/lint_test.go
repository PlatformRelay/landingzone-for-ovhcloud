//go:build offlinetools

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With the pinned tools inside the offline entry, lint passes a valid module
// and fails a linter offence, keeping the rule name.
func TestLintWithTools(t *testing.T) {
	config, err := os.ReadFile("../../../.tflint.hcl")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		code int
		want []string
	}{
		"valid": {0, []string{"pass tflint", "LINT_PASS modules/x files=1"}},
		"lint":  {1, []string{"fail tflint LINT_ISSUES", "terraform_typed_variables", "LINT_FAIL modules/x"}},
	} {
		t.Run(name, func(t *testing.T) {
			module, err := os.ReadFile(filepath.Join("../../../tests/check/fixtures/static", name, "main.tf"))
			if err != nil {
				t.Fatal(err)
			}
			root := repo(t, map[string]string{"modules/x/main.tf": string(module), ".tflint.hcl": string(config)})
			code, out := lzCheck(root, "lint", "modules/x")
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("BEHAVIORAL_RED: %q missing:\n%s", w, out)
				}
			}
			if code != c.code {
				t.Errorf("BEHAVIORAL_RED: exit %d, want %d:\n%s", code, c.code, out)
			}
		})
	}
}

// unitModule reads one unit-runner fixture's files under modules/x.
func unitModule(t *testing.T, name string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, f := range []string{"main.tf", "main.tftest.hcl"} {
		data, err := os.ReadFile(filepath.Join("../../../tests/check/fixtures/unit", name, f))
		if err != nil {
			t.Fatal(err)
		}
		files["modules/x/"+f] = string(data)
	}
	return files
}

// With the pinned tools inside the offline entry, unit passes a module whose
// tests pass and fails one whose test fails, keeping the adapter's reason and
// the assertion message; slice prints the per-directory counts and passes
// only a repository whose libraries pass lint and their tests.
func TestUnitCommandsWithTools(t *testing.T) {
	config, err := os.ReadFile("../../../.tflint.hcl")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		code  int
		unit  []string
		slice []string
	}{
		"pass": {0, []string{"pass init", "UNIT_PASS modules/x tests=1 passed=1"}, []string{"pass modules/x lint=pass tests=1 passed=1 failed=0", "SLICE_PASS dirs=1"}},
		"fail": {1, []string{"pass init", "TESTS_FAILED", "unexpected greeting", "UNIT_FAIL modules/x TEST_REPORT_FAILED"}, []string{"fail modules/x lint=pass tests=1 passed=0 failed=1", "SLICE_FAIL dirs=1"}},
	} {
		t.Run(name, func(t *testing.T) {
			files := unitModule(t, name)
			files[".tflint.hcl"] = string(config)
			root := repo(t, files)
			for _, step := range []struct {
				args []string
				want []string
			}{{[]string{"unit", "modules/x"}, c.unit}, {[]string{"slice"}, c.slice}} {
				code, out := lzCheck(root, step.args...)
				for _, w := range step.want {
					if !strings.Contains(out, w) {
						t.Errorf("BEHAVIORAL_RED: %v: %q missing:\n%s", step.args, w, out)
					}
				}
				if code != c.code {
					t.Errorf("BEHAVIORAL_RED: %v exit %d, want %d:\n%s", step.args, code, c.code, out)
				}
			}
		})
	}
}
