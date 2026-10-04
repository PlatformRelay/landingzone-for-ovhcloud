package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlatformRelay/ovh-landing-zone-accelerator/tools/internal/checks"
)

const spec = "- **FR-001**: MUST pin tools.\n- **SC-001**: Fast.\n"

const tasks = "- [ ] T001 Write pins in tools/pin.go\n" +
	"  - Requirements: FR-001, SC-001; ADRs: 0011. Depends on: none.\n" +
	"  - Verify: `task pins`: missing tool rejected.\n" +
	"  - Evidence: `evidence/T001.md`.\n"

const registry = `{"schema_version": 1,
 "requirements": {"FR-001": {"adrs": ["0011"]}, "SC-001": {"adrs": ["0011"]}},
 "evaluators": [],
 "producers": ["test-producer"],
 "procedures": {},
 "checks": [{"id": "pins", "requirements": ["FR-001", "SC-001"], "command": "task pins", "creator": "T001", "kind": "behavioral", "scope": ["tools"]}]}
`

func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	base := map[string]string{
		"specs/001-x/spec.md":             spec,
		"specs/001-x/tasks.md":            tasks,
		"harness/checks.yaml":             registry,
		"docs/adr/0011-opentofu.md":       "# ADR\n",
		"docs/adr/README.md":              "# ADRs\n",
		"tools/pin.go":                    "package tools\n",
		".local/evidence/checks/.keep":    "",
		"specs/001-x/evidence/T001.md":    "# T001\n",
		"docs/adr/template-not-an-adr.md": "# no\n",
	}
	for name, content := range files {
		base[name] = content
	}
	for name, content := range base {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func lzCheck(root string, args ...string) (int, string) {
	var out bytes.Buffer
	code := run(append([]string{"-root", root}, args...), &out)
	return code, out.String()
}

func TestSpecsAccepted(t *testing.T) {
	code, out := lzCheck(repo(t, nil), "specs", "specs/001-x")
	if code != 0 || !strings.Contains(out, "TRACE_OK specs/001-x requirements=2 tasks=1 checks=1") {
		t.Errorf("BEHAVIORAL_RED: valid trace refused: code=%d\n%s", code, out)
	}
}

func TestSpecsRejected(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		code  int
		want  string
	}{
		"unknown ADR":        {map[string]string{"specs/001-x/tasks.md": strings.Replace(tasks, "ADRs: 0011", "ADRs: 0011, 0099", 1)}, 1, "UNKNOWN_ADR T001"},
		"unmapped":           {map[string]string{"specs/001-x/spec.md": spec + "- **FR-002**: MUST more.\n"}, 1, "UNMAPPED_REQUIREMENT FR-002"},
		"no tasks":           {map[string]string{"specs/001-x/tasks.md": "# Tasks\n"}, 2, "NO_TASKS"},
		"no requirements":    {map[string]string{"specs/001-x/spec.md": "# Spec\n"}, 2, "NO_REQUIREMENTS"},
		"malformed registry": {map[string]string{"harness/checks.yaml": "checks: []\n"}, 2, "REGISTRY_SYNTAX"},
		"creator never runs": {map[string]string{"specs/001-x/tasks.md": strings.Replace(tasks, "`task pins`", "`go test ./...`", 1)}, 1, "CHECK_NOT_VERIFIED pins"},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(repo(t, c.files), "specs", "specs/001-x")
			if code != c.code || !strings.Contains(out, c.want) {
				t.Errorf("BEHAVIORAL_RED: code=%d, want %d with %q:\n%s", code, c.code, c.want, out)
			}
		})
	}
}

func writeEvidence(t *testing.T, root, producer string) {
	t.Helper()
	reg, err := checks.ParseRegistry([]byte(registry))
	if err != nil {
		t.Fatal(err)
	}
	d, err := checks.CheckDigest(root, reg.Checks[0])
	if err != nil {
		t.Fatal(err)
	}
	e := `{"check": "pins", "status": "pass", "input_digest": "` + d + `", "discovered": 2, "failed": 0, "producer": "` + producer + `"}`
	if err := os.WriteFile(filepath.Join(root, ".local/evidence/checks/pins.json"), []byte(e), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoD(t *testing.T) {
	root := repo(t, nil)
	if code, out := lzCheck(root, "dod", "tools"); code != 1 || !strings.Contains(out, "not-run pins NO_EVIDENCE") || !strings.Contains(out, "DOD_FAIL tools") {
		t.Errorf("BEHAVIORAL_RED: missing evidence not reported as not-run: code=%d\n%s", code, out)
	}
	writeEvidence(t, root, "written-by-hand")
	if code, out := lzCheck(root, "dod", "tools"); code != 1 || !strings.Contains(out, "review-required pins UNATTESTED_EVIDENCE") {
		t.Errorf("BEHAVIORAL_RED: unattested evidence passed: code=%d\n%s", code, out)
	}
	writeEvidence(t, root, "test-producer")
	if code, out := lzCheck(root, "dod", "tools"); code != 0 || !strings.Contains(out, "pass pins") || !strings.Contains(out, "DOD_PASS tools") {
		t.Errorf("BEHAVIORAL_RED: fresh pass refused: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(root, "dod", "docs"); code != 1 || !strings.Contains(out, "DOD_FAIL docs: no registered check applies") {
		t.Errorf("BEHAVIORAL_RED: path without checks passed: code=%d\n%s", code, out)
	}
	if err := os.WriteFile(filepath.Join(root, "tools/pin.go"), []byte("package changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := lzCheck(root, "dod", "tools"); code != 1 || !strings.Contains(out, "not-run pins STALE_EVIDENCE") {
		t.Errorf("BEHAVIORAL_RED: stale pass accepted: code=%d\n%s", code, out)
	}
}

func TestDoDRefusesMalformedEvidence(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":       "pass\n",
		"unknown field":  `{"check": "pins", "status": "pass", "verdict": "ok"}`,
		"missing failed": `{"check": "pins", "status": "pass", "input_digest": "x", "discovered": 1, "producer": "test-producer"}`,
		"null failed":    `{"check": "pins", "status": "pass", "input_digest": "x", "discovered": 1, "failed": null, "producer": "test-producer"}`,
		"case variant":   `{"check": "pins", "status": "pass", "input_digest": "x", "discovered": 1, "failed": 1, "FAILED": 0, "producer": "test-producer"}`,
		"duplicate key":  `{"check": "pins", "status": "fail", "status": "pass"}`,
		"trailing data":  `{"check": "pins", "status": "pass"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := repo(t, map[string]string{".local/evidence/checks/pins.json": content})
			if code, out := lzCheck(root, "dod", "tools"); code != 2 || !strings.Contains(out, "EVIDENCE_SYNTAX") {
				t.Errorf("BEHAVIORAL_RED: malformed evidence not refused: code=%d\n%s", code, out)
			}
		})
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"specs"}, {"dod"}, {"dod", "a", "b"}, {"other", "x"}} {
		if code, _ := lzCheck(repo(t, nil), args...); code != 2 {
			t.Errorf("BEHAVIORAL_RED: %v accepted (code %d)", args, code)
		}
	}
}

func TestDependencyCommands(t *testing.T) {
	root := repo(t, map[string]string{
		"modules/naming/main.tf":          `variable "name" {}`,
		"components/runtime/kube/main.tf": `module "n" { source = "../../../modules/naming" }`,
	})
	if code, out := lzCheck(root, "deps"); code != 0 || !strings.Contains(out, "DEPENDENCIES_OK dirs=2") {
		t.Errorf("BEHAVIORAL_RED: valid graph refused: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(root, "select", "modules/naming/main.tf"); code != 0 || !strings.Contains(out, "SELECT components/runtime/kube modules/naming") {
		t.Errorf("BEHAVIORAL_RED: selection not printed: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(root, "select", "misc/x.txt"); code != 0 || !strings.Contains(out, "SELECT_FULL UNKNOWN_PATH") {
		t.Errorf("BEHAVIORAL_RED: unknown path not widened: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(root, "select", "docs/x.md"); code != 0 || !strings.Contains(out, "SELECT_NONE DOCS_ONLY") {
		t.Errorf("BEHAVIORAL_RED: docs-only change not explicit: code=%d\n%s", code, out)
	}
	broken := repo(t, map[string]string{"modules/naming/main.tf": `module "k" { source = "../../components/runtime/kube" }`, "components/runtime/kube/main.tf": `variable "x" {}`})
	if code, out := lzCheck(broken, "deps"); code != 1 || !strings.Contains(out, "LAYER_VIOLATION modules/naming") {
		t.Errorf("BEHAVIORAL_RED: reversed edge accepted: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(broken, "select", "modules/naming/main.tf"); code != 1 || !strings.Contains(out, "LAYER_VIOLATION") {
		t.Errorf("BEHAVIORAL_RED: selection on a broken graph accepted: code=%d\n%s", code, out)
	}
	for _, args := range [][]string{{"deps", "x"}, {"select"}, {"select", "a", "b"}} {
		if code, _ := lzCheck(root, args...); code != 2 {
			t.Errorf("BEHAVIORAL_RED: %v accepted (code %d)", args, code)
		}
	}
}

// Without the pinned tools (outside the offline entry, or a wrong path) lint
// is blocked, and a module directory that does not exist yet is not run; both
// exit non-zero. The real tool runs are covered by task test:static.
func TestLintWithoutTools(t *testing.T) {
	root := repo(t, map[string]string{"modules/a/main.tf": `variable "x" {}`, ".tflint.hcl": "config {}\n"})
	absent := []string{"-tofu", filepath.Join(root, "no-tofu"), "-tflint", filepath.Join(root, "no-tflint")}
	if code, out := lzCheck(root, append(absent, "lint", "modules/a")...); code != 1 || !strings.Contains(out, "blocked fmt TOOL_ABSENT") || !strings.Contains(out, "LINT_BLOCKED modules/a") {
		t.Errorf("BEHAVIORAL_RED: absent tools not blocked: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(root, append(absent, "lint", "modules/naming")...); code != 1 || !strings.Contains(out, "LINT_NOT_RUN modules/naming") {
		t.Errorf("BEHAVIORAL_RED: missing module not reported as not run: code=%d\n%s", code, out)
	}
	empty := repo(t, map[string]string{"modules/a/README.md": "# a\n"})
	if code, out := lzCheck(empty, append(absent, "lint", "modules/a")...); code != 1 || !strings.Contains(out, "fail discovery NO_DISCOVERY") || !strings.Contains(out, "LINT_FAIL modules/a") {
		t.Errorf("BEHAVIORAL_RED: empty module accepted: code=%d\n%s", code, out)
	}
	for _, args := range [][]string{{"lint"}, {"lint", "a", "b"}, {"lint", "../outside"}, {"lint", "/etc"}} {
		if code, _ := lzCheck(root, append(absent, args...)...); code != 2 {
			t.Errorf("BEHAVIORAL_RED: %v accepted (code %d)", args, code)
		}
	}
}
