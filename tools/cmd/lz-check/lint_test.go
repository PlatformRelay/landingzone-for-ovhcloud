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

// consumerRoot is a root that passes name to the greeting module at source
// and outputs its greeting; consumerTest asserts the greeting is want.
func consumerRoot(source string) string {
	return "terraform {\n  required_version = \">= 1.13.0\"\n}\n\nvariable \"name\" {\n  type        = string\n  description = \"Name to greet.\"\n}\n\n" +
		"module \"x\" {\n  source = \"" + source + "\"\n  name   = var.name\n}\n\n" +
		"output \"greeting\" {\n  description = \"The greeting.\"\n  value       = module.x.greeting\n}\n"
}

func consumerTest(want string) string {
	return "run \"greets\" {\n  command = plan\n\n  variables {\n    name = \"lz\"\n  }\n\n  assert {\n    condition     = output.greeting == \"" + want +
		"\"\n    error_message = \"unexpected greeting\"\n  }\n}\n"
}

// checkTree is a passing docs tree with the greeting module modules/x (the
// unit fixture named), a component and a test root that consume it, and an
// unrelated module modules/z.
func checkTree(t *testing.T, fixture string, extra map[string]string) string {
	t.Helper()
	config, err := os.ReadFile("../../../.tflint.hcl")
	if err != nil {
		t.Fatal(err)
	}
	files := unitModule(t, fixture)
	for f, content := range unitModule(t, "pass") {
		files[strings.Replace(f, "modules/x/", "modules/z/", 1)] = content
	}
	files[".tflint.hcl"] = string(config)
	files["modules/x/.terraform.lock.hcl"] = ""
	files["components/fam/y/main.tf"] = consumerRoot("../../../modules/x")
	files["components/fam/y/main.tftest.hcl"] = consumerTest("hello lz")
	files["tests/live/probes/p/main.tf"] = consumerRoot("../../../../modules/x")
	files["tests/live/probes/p/main.tftest.hcl"] = consumerTest("hello lz")
	files[docsMapPage] = docsBaseMap + "| `components/` | Components. |\n"
	for k, v := range extra {
		files[k] = v
	}
	return docsRepo(t, files)
}

// With the pinned tools inside the offline entry, `lz-check check` (task
// check, ADR-0019) runs lint and the unit tests on the changed library and
// every library, component and stage that consumes it, the unit tests on
// every test root that does, and never an unrelated directory; then the
// dependency scan and the docs rules. It passes only when all of them pass:
// a failing consumer, test root or docs rule fails it. Without a path, or on
// shared tooling or an unknown path, every library and stage runs. L2 is
// claimed only when a component or stage ran.
func TestUnitCheckWithTools(t *testing.T) {
	for name, c := range map[string]struct {
		fixture string
		extra   map[string]string
		args    []string
		code    int
		want    []string
		absent  []string
	}{
		"changed library": {"pass", nil, []string{"modules/x/main.tf"}, 0, []string{"CHECK_SELECTION dirs=components/fam/y modules/x tests/live/probes/p",
			"pass modules/x lint=pass tests=1 passed=1", "pass components/fam/y lint=pass tests=1 passed=1", "pass tests/live/probes/p unit tests=1 passed=1",
			"DOCS_OK", "CHECK_LAYERS ran=L0-partial,L1,L2 not_built=L3,L4", "CHECK_PASS dirs=3 not_run=0"}, []string{"modules/z lint="}},
		"library alone": {"pass", nil, []string{"modules/z"}, 0, []string{"CHECK_SELECTION dirs=modules/z", "pass modules/z lint=pass",
			"CHECK_LAYERS ran=L0-partial,L1 not_built=L3,L4", "CHECK_PASS dirs=1"}, []string{"modules/x lint="}},
		"every library": {"pass", nil, nil, 0, []string{"CHECK_SELECTION full reason=ALL", "pass modules/z lint=pass", "pass components/fam/y lint=pass", "CHECK_PASS dirs=3 not_run=0"}, nil},
		"shared tool":   {"pass", nil, []string{"Taskfile.yml"}, 0, []string{"CHECK_SELECTION full reason=SHARED_TOOL", "pass modules/z lint=pass", "CHECK_PASS dirs=3"}, nil},
		"unknown path":  {"pass", nil, []string{"nowhere/x.txt"}, 0, []string{"CHECK_SELECTION full reason=UNKNOWN_PATH", "pass modules/z lint=pass", "CHECK_PASS dirs=3"}, nil},
		"failing test":  {"fail", nil, []string{"modules/x"}, 1, []string{"fail modules/x lint=pass tests=1 passed=0 failed=1", "DOCS_OK", "CHECK_LAYERS attempted=", "CHECK_FAIL DIRS"}, nil},
		"failing consumer": {"pass", map[string]string{"components/fam/y/main.tftest.hcl": consumerTest("hello other")}, []string{"modules/x"}, 1,
			[]string{"pass modules/x lint=pass", "fail components/fam/y lint=pass tests=1 passed=0 failed=1", "CHECK_FAIL DIRS"}, nil},
		"failing test root": {"pass", map[string]string{"tests/live/probes/p/main.tftest.hcl": consumerTest("hello other")}, []string{"modules/x"}, 1,
			[]string{"pass components/fam/y lint=pass", "fail tests/live/probes/p unit tests=1 passed=0 failed=1", "CHECK_FAIL DIRS"}, nil},
		"failing docs": {"pass", map[string]string{"docs/how-to/a.md": pageA + "[gone](c.md)\n"}, []string{"modules/x"}, 1,
			[]string{"pass modules/x lint=pass", "LINK_BROKEN docs/how-to/a.md", "CHECK_FAIL DOCS"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(checkTree(t, c.fixture, c.extra), append([]string{"check"}, c.args...)...)
			judgeCheck(t, code, out, c.code, c.want)
			for _, a := range c.absent {
				if strings.Contains(out, a) {
					t.Errorf("BEHAVIORAL_RED: %q ran, but the change does not select it:\n%s", a, out)
				}
			}
		})
	}
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
