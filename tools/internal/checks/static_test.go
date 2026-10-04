//go:build offlinetools

// The static checks run the real pinned tools, which exist only inside the
// offline entry (/tcb). Run with: lz-offline … -- task test:static
package checks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const staticFixtures = "../../../tests/check/fixtures/static/"

func pinnedTools(t *testing.T) StaticTools {
	t.Helper()
	// A relative configuration path, as lz-check passes it: the tools run
	// inside the module directory, so RunStatic must resolve it first.
	return StaticTools{Tofu: "/tcb/tofu", TFLint: "/tcb/tflint", TFLintConfig: "../../../.tflint.hcl"}
}

// staticFixture copies a fixture into private scratch: tofu init writes state
// next to the configuration.
func staticFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(staticFixtures + name)
	if err != nil {
		t.Fatalf("FIXTURE_MISSING: %v", err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(staticFixtures+name, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func clauses(o StaticObservation) map[string]string {
	got := map[string]string{}
	for _, r := range o.Results {
		got[r.Clause] = r.Status + " " + r.Reason
	}
	return got
}

// The tools are real; a missing one is BLOCKED, never a pass or a red.
func requireTools(t *testing.T, tools StaticTools) {
	t.Helper()
	for _, p := range []string{tools.Tofu, tools.TFLint, tools.TFLintConfig} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("TOOL_ABSENT: %s (run through the offline entry): %v", p, err)
		}
	}
}

func TestStaticValid(t *testing.T) {
	tools := pinnedTools(t)
	requireTools(t, tools)
	o := RunStatic(tools, staticFixture(t, "valid"))
	want := map[string]string{"fmt": "pass ", "init": "pass ", "validate": "pass ", "tflint": "pass "}
	if got := clauses(o); !reflect.DeepEqual(got, want) || !o.Pass || !reflect.DeepEqual(o.Discovered, []string{"main.tf"}) {
		t.Errorf("BEHAVIORAL_RED: valid module rejected: pass=%v discovered=%v %v %+v", o.Pass, o.Discovered, got, o.Results)
	}
}

// Each fixture breaks exactly the clauses its defect concerns; every other
// clause still runs and passes, so no clause hides behind another.
func TestStaticRejected(t *testing.T) {
	tools := pinnedTools(t)
	requireTools(t, tools)
	for name, c := range map[string]struct {
		want   map[string]string
		detail map[string]string // clause → upstream text it must keep
	}{
		"unformatted": {map[string]string{"fmt": "fail UNFORMATTED", "init": "pass ", "validate": "pass ", "tflint": "pass "},
			map[string]string{"fmt": "main.tf"}},
		"lint": {map[string]string{"fmt": "pass ", "init": "pass ", "validate": "pass ", "tflint": "fail LINT_ISSUES"},
			map[string]string{"tflint": "terraform_typed_variables"}},
		// Flagged only by the repository configuration, not by TFLint's defaults.
		"undocumented": {map[string]string{"fmt": "pass ", "init": "pass ", "validate": "pass ", "tflint": "fail LINT_ISSUES"},
			map[string]string{"tflint": "terraform_documented_outputs"}},
		"malformed": {map[string]string{"fmt": "fail FORMAT_ERROR", "init": "fail INIT_FAILED", "validate": "fail INVALID", "tflint": "fail LINT_ERROR"},
			map[string]string{"fmt": "Invalid expression", "init": "Invalid expression", "validate": "Invalid expression", "tflint": "Invalid expression"}},
	} {
		t.Run(name, func(t *testing.T) {
			o := RunStatic(tools, staticFixture(t, name))
			if got := clauses(o); !reflect.DeepEqual(got, c.want) || o.Pass {
				t.Errorf("BEHAVIORAL_RED: clauses %v (pass=%v), want %v", got, o.Pass, c.want)
			}
			for _, r := range o.Results {
				kept := strings.Join(append(append([]string{}, r.Files...), r.Messages...), "\n")
				if want, ok := c.detail[r.Clause]; ok && !strings.Contains(kept, want) {
					t.Errorf("BEHAVIORAL_RED: %s did not keep %q: %+v", r.Clause, want, r)
				}
			}
		})
	}
}

// Both tools exit 0 on a directory without configuration, so discovery is
// counted independently and an empty scope never passes.
func TestStaticNoDiscovery(t *testing.T) {
	tools := pinnedTools(t)
	requireTools(t, tools)
	o := RunStatic(tools, staticFixture(t, "empty"))
	if got := clauses(o); !reflect.DeepEqual(got, map[string]string{"discovery": "fail NO_DISCOVERY"}) || o.Pass {
		t.Errorf("BEHAVIORAL_RED: empty scope accepted: %v pass=%v", got, o.Pass)
	}
	if o := RunStatic(tools, filepath.Join(t.TempDir(), "absent")); o.Pass || clauses(o)["discovery"] != "fail NO_DISCOVERY" {
		t.Errorf("BEHAVIORAL_RED: missing scope accepted: %+v", o)
	}
	// Module copies tofu init leaves in .terraform are tool state, not the
	// directory's own configuration.
	state := staticFixture(t, "empty")
	if err := os.MkdirAll(filepath.Join(state, ".terraform/modules/m"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, ".terraform/modules/m/main.tf"), []byte("variable \"x\" {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o := RunStatic(tools, state); o.Pass || clauses(o)["discovery"] != "fail NO_DISCOVERY" {
		t.Errorf("BEHAVIORAL_RED: tool state counted as configuration: %+v", o)
	}
}

// A missing tool blocks its clause; it is never mistaken for a pass or a red.
func TestStaticToolAbsent(t *testing.T) {
	tools := pinnedTools(t)
	requireTools(t, tools)
	absent := tools
	absent.TFLint = filepath.Join(t.TempDir(), "tflint")
	o := RunStatic(absent, staticFixture(t, "valid"))
	want := map[string]string{"fmt": "pass ", "init": "pass ", "validate": "pass ", "tflint": "blocked TOOL_ABSENT"}
	if got := clauses(o); !reflect.DeepEqual(got, want) || o.Pass {
		t.Errorf("BEHAVIORAL_RED: absent linter not blocked: %v pass=%v", got, o.Pass)
	}
	absent = tools
	absent.Tofu = filepath.Join(t.TempDir(), "tofu")
	o = RunStatic(absent, staticFixture(t, "valid"))
	want = map[string]string{"fmt": "blocked TOOL_ABSENT", "init": "blocked TOOL_ABSENT", "validate": "blocked TOOL_ABSENT", "tflint": "pass "}
	if got := clauses(o); !reflect.DeepEqual(got, want) || o.Pass {
		t.Errorf("BEHAVIORAL_RED: absent tofu not blocked: %v pass=%v", got, o.Pass)
	}
}
