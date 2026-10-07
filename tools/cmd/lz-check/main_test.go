package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
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

// Spec 005 defines FR-001 and SC-001 again, in its own heading form; its
// registry keys carry the "005/" prefix, spec 001's none.
const (
	spec005  = "- **FR-001 Naming** — `modules/naming` is pure.\n- **FR-002 Labels** — labels apply.\n- **SC-001**: Fast slice.\n"
	tasks005 = "- [ ] T001 Write naming in modules/naming/main.tf\n" +
		"  - Requirements: FR-001, FR-002, SC-001; ADRs: 0002. Depends on: none.\n" +
		"  - Verify: `task naming`: names derive.\n" +
		"  - Evidence: `evidence/T001.md`.\n"
	registry005 = `{"schema_version": 1,
 "requirements": {"FR-001": {"adrs": ["0011"]}, "SC-001": {"adrs": ["0011"]},
                  "005/FR-001": {"adrs": ["0002"]}, "005/FR-002": {"adrs": ["0002"]}, "005/SC-001": {"adrs": ["0002"]}},
 "evaluators": [],
 "producers": ["test-producer"],
 "procedures": {},
 "checks": [{"id": "pins", "requirements": ["FR-001", "SC-001"], "command": "task pins", "creator": "T001", "kind": "behavioral", "scope": ["tools"]},
            {"id": "naming", "requirements": ["005/FR-001", "005/FR-002", "005/SC-001"], "command": "task naming", "creator": "005/T001", "kind": "behavioral", "scope": ["modules/naming"]}]}
`
)

func twoSpecRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	base := map[string]string{
		"specs/005-y/spec.md":    spec005,
		"specs/005-y/tasks.md":   tasks005,
		"harness/checks.yaml":    registry005,
		"docs/adr/0002-x.md":     "# ADR\n",
		"modules/naming/main.tf": "locals {}\n",
	}
	for name, content := range files {
		base[name] = content
	}
	return repo(t, base)
}

// Both specs define FR-001; each traces against its own registry entries.
func TestSpecsScoped(t *testing.T) {
	root := twoSpecRepo(t, nil)
	if code, out := lzCheck(root, "specs", "specs/001-x"); code != 0 || !strings.Contains(out, "TRACE_OK specs/001-x requirements=2 tasks=1 checks=1") {
		t.Errorf("BEHAVIORAL_RED: spec 001 refused beside spec 005: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(root, "specs", "specs/005-y/"); code != 0 || !strings.Contains(out, "TRACE_OK specs/005-y") || !strings.Contains(out, " requirements=3 tasks=1 checks=1") {
		t.Errorf("BEHAVIORAL_RED: spec 005 refused beside spec 001: code=%d\n%s", code, out)
	}
	// A directory without a spec number selects no keys; it is refused, not
	// traced as spec 001.
	if code, out := lzCheck(twoSpecRepo(t, map[string]string{"specs/y/spec.md": spec005, "specs/y/tasks.md": tasks005}), "specs", "specs/y"); code != 2 || !strings.Contains(out, "SPEC_NUMBER") {
		t.Errorf("BEHAVIORAL_RED: unnumbered spec directory: code=%d\n%s", code, out)
	}
}

func TestSpecsScopedRejected(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		want  []string
	}{
		"ADR of spec 001": {map[string]string{"specs/005-y/tasks.md": strings.Replace(tasks005, "ADRs: 0002", "ADRs: 0011", 1)},
			[]string{"MISSING_ADR T001", "UNRELATED_ADR T001", "TRACE_FAIL specs/005-y findings=4"}},
		"unmapped": {map[string]string{"specs/005-y/tasks.md": strings.Replace(tasks005, "FR-001, FR-002, SC-001", "FR-001, SC-001", 1)},
			[]string{"UNMAPPED_REQUIREMENT FR-002", "TRACE_FAIL specs/005-y findings=1"}},
		"no Verify": {map[string]string{"specs/005-y/tasks.md": strings.Replace(tasks005, "  - Verify: `task naming`: names derive.\n", "", 1)},
			[]string{"MISSING_VERIFY T001", "CHECK_NOT_VERIFIED naming", "TRACE_FAIL specs/005-y findings=2"}},
		// Spec 001's unprefixed FR-001 entry must not stand in for spec 005's.
		"registered only for spec 001": {map[string]string{"harness/checks.yaml": strings.Replace(strings.Replace(registry005,
			`"005/FR-001": {"adrs": ["0002"]}, `, "", 1), `"005/FR-001", "005/FR-002"`, `"005/FR-002"`, 1)},
			[]string{"UNREGISTERED_REQUIREMENT FR-001", "UNCHECKED_REQUIREMENT FR-001", "TRACE_FAIL specs/005-y findings=2"}},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(twoSpecRepo(t, c.files), "specs", "specs/005-y")
			for _, want := range c.want {
				if code != 1 || !strings.Contains(out, want) {
					t.Errorf("BEHAVIORAL_RED: code=%d, want 1 with %q:\n%s", code, want, out)
				}
			}
		})
	}
}

// Spec 001's real tree and registry still trace.
func TestSpecsRealTree(t *testing.T) {
	if code, out := lzCheck("../../..", "specs", "specs/001-offline-foundation"); code != 0 || !strings.Contains(out, "TRACE_OK specs/001-offline-foundation") {
		t.Errorf("BEHAVIORAL_RED: spec 001's real tree refused: code=%d\n%s", code, out)
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
	// Only ci-source takes more than one argument, and at least one.
	withSource := repo(t, map[string]string{"pipelines/github/foundation-source.json": foundationSource})
	for _, args := range [][]string{{"ci-source"}, {"specs", "specs/001-x", "specs/001-x"}, {"deps", "x"}} {
		if code, out := lzCheck(withSource, args...); code != 2 || !strings.HasPrefix(out, "usage:") {
			t.Errorf("BEHAVIORAL_RED: %v not refused with usage (code %d)\n%s", args, code, out)
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

// Without the pinned tofu, unit is blocked and a directory that does not exist
// yet is not run; both exit non-zero. The real tool runs are covered by
// TestUnitCommandsWithTools inside the offline entry (task test:slice).
func TestUnitCommandWithoutTools(t *testing.T) {
	root := repo(t, map[string]string{"modules/a/main.tf": `variable "x" {}`})
	absent := []string{"-tofu", filepath.Join(root, "no-tofu")}
	if code, out := lzCheck(root, append(absent, "unit", "modules/a")...); code != 1 || !strings.Contains(out, "UNIT_BLOCKED modules/a TOOL_ABSENT") || strings.Contains(out, "UNIT_PASS") {
		t.Errorf("BEHAVIORAL_RED: absent tofu not blocked: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(root, append(absent, "unit", "modules/naming")...); code != 1 || !strings.Contains(out, "UNIT_NOT_RUN modules/naming") {
		t.Errorf("BEHAVIORAL_RED: missing directory not reported as not run: code=%d\n%s", code, out)
	}
	for _, args := range [][]string{{"unit"}, {"unit", "a", "b"}, {"unit", "../outside"}, {"unit", "/etc"}, {"slice", "x"}} {
		if code, _ := lzCheck(root, append(absent, args...)...); code != 2 {
			t.Errorf("BEHAVIORAL_RED: %v accepted (code %d)", args, code)
		}
	}
}

// slice discovers exactly the library and stage directories of the dependency
// graph, in order, and runs every one; zero directories and a broken graph
// fail before any tool runs.
func TestUnitSliceDiscovery(t *testing.T) {
	absent := func(root string) []string {
		return []string{"-tofu", filepath.Join(root, "no-tofu"), "-tflint", filepath.Join(root, "no-tflint")}
	}
	for name, files := range map[string]map[string]string{
		"no directories": nil,
		"only tests and examples": {
			"tests/check/fixtures/x/main.tf": `variable "x" {}`,
			"examples/basic/main.tf":         `variable "x" {}`,
		},
	} {
		root := repo(t, files)
		if code, out := lzCheck(root, append(absent(root), "slice")...); code != 1 || !strings.Contains(out, "SLICE_FAIL NO_DISCOVERY dirs=0") {
			t.Errorf("BEHAVIORAL_RED: %s: zero discovery accepted: code=%d\n%s", name, code, out)
		}
	}
	root := repo(t, map[string]string{
		"modules/naming/main.tf":                 `variable "name" {}`,
		"components/runtime/kube/main.tf":        `module "n" { source = "../../../modules/naming" }`,
		"stages/platform/main.tf":                `module "k" { source = "../../components/runtime/kube" }`,
		"examples/basic/main.tf":                 `variable "z" {}`,
		"modules/naming/tests/setup/main.tf":     `variable "x" {}`,
		"modules/naming/tests/unit.tftest.hcl":   "",
		"components/runtime/kube/variables.tf":   `variable "y" {}`,
		"stages/platform/README.md":              "# platform\n",
		"components/runtime/README.md":           "# family\n",
		"components/runtime/kube/docs/README.md": "# kube\n",
	})
	code, out := lzCheck(root, append(absent(root), "slice")...)
	var dirs []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && (f[0] == "pass" || f[0] == "fail" || f[0] == "blocked") {
			dirs = append(dirs, f[1])
			// Missing tools block every directory; nothing failed.
			if f[0] != "blocked" || !strings.Contains(line, "lint=blocked") {
				t.Errorf("BEHAVIORAL_RED: directory with absent tools not blocked: %q", line)
			}
		}
	}
	want := []string{"components/runtime/kube", "modules/naming", "stages/platform"}
	if code != 1 || strings.Join(dirs, " ") != strings.Join(want, " ") || !strings.Contains(out, "SLICE_FAIL dirs=3") {
		t.Errorf("BEHAVIORAL_RED: discovery %v, want %v (blocked tools fail the slice): code=%d\n%s", dirs, want, code, out)
	}
	broken := repo(t, map[string]string{"modules/naming/main.tf": `module "k" { source = "../../components/runtime/kube" }`, "components/runtime/kube/main.tf": `variable "x" {}`})
	if code, out := lzCheck(broken, append(absent(broken), "slice")...); code != 1 || !strings.Contains(out, "LAYER_VIOLATION modules/naming") || !strings.Contains(out, "SLICE_FAIL") || strings.Contains(out, "blocked") {
		t.Errorf("BEHAVIORAL_RED: slice on a broken graph ran or passed: code=%d\n%s", code, out)
	}
}

const foundationSource = `{"runner": "ubuntu-24.04", "timeout_minutes": 10,
 "manifest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
 "layer": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
 "entry_sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
 "bwrap_sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
 "targets": ["pins"], "deferred": {}}
`

// ci-workflow prints the workflow rendered from the source; ci-source accepts
// exactly that workflow and a source that runs every check of a closed task.
func TestFoundationCICommands(t *testing.T) {
	root := repo(t, map[string]string{
		"specs/001-x/tasks.md":                    strings.Replace(tasks, "- [ ] T001", "- [x] T001", 1),
		"pipelines/github/foundation-source.json": foundationSource,
	})
	code, rendered := lzCheck(root, "ci-workflow")
	if code != 0 || !strings.Contains(rendered, "LZ_TARGETS: pins\n") {
		t.Fatalf("BEHAVIORAL_RED: ci-workflow exit %d:\n%s", code, rendered)
	}
	workflow := filepath.Join(root, ".github/workflows/foundation.yml")
	if err := os.MkdirAll(filepath.Dir(workflow), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte(rendered), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := lzCheck(root, "ci-source", "specs/001-x"); code != 0 || out != "FOUNDATION_CI_OK targets=1 deferred=0\n" {
		t.Errorf("BEHAVIORAL_RED: valid CI refused: exit %d\n%s", code, out)
	}
	if err := os.WriteFile(workflow, []byte(strings.Replace(rendered, "permissions: {}", "permissions: write-all", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := lzCheck(root, "ci-source", "specs/001-x"); code != 1 || !strings.Contains(out, "WORKFLOW_DRIFT") || !strings.Contains(out, "FOUNDATION_CI_FAIL findings=1") {
		t.Errorf("BEHAVIORAL_RED: widened workflow accepted: exit %d\n%s", code, out)
	}
	if err := os.Remove(workflow); err != nil {
		t.Fatal(err)
	}
	if code, out := lzCheck(root, "ci-source", "specs/001-x"); code != 1 || !strings.Contains(out, "WORKFLOW_DRIFT") {
		t.Errorf("BEHAVIORAL_RED: absent workflow accepted: exit %d\n%s", code, out)
	}
}

// ci-source resolves a spec-scoped creator against the tasks of the spec dirs
// it is given: a closed 005/T055 admits its check only when spec 005 is read,
// and an open one never does.
func TestFoundationCISpecScopedCreators(t *testing.T) {
	scoped := strings.Replace(registry, `"scope": ["tools"]}]}`, `"scope": ["tools"]},
 {"id": "live", "requirements": ["FR-001"], "command": "task live", "creator": "005/T055", "kind": "behavioral", "scope": ["tools"]},
 {"id": "later", "requirements": ["FR-001"], "command": "task later", "creator": "005/T056", "kind": "behavioral", "scope": ["tools"]}]}`, 1)
	spec005 := strings.Replace(strings.Replace(tasks, "- [ ] T001", "- [x] T055", 1), "T001.md", "T055.md", 1) +
		strings.Replace(tasks, "T001", "T056", -1)
	root := repo(t, map[string]string{
		"specs/001-x/tasks.md": strings.Replace(tasks, "- [ ] T001", "- [x] T001", 1),
		"specs/005-y/tasks.md": spec005,
		"harness/checks.yaml":  scoped,
	})
	withTargets := func(targets string) {
		t.Helper()
		writeFiles(t, root, map[string]string{"pipelines/github/foundation-source.json": strings.Replace(foundationSource, `["pins"]`, targets, 1)})
		code, rendered := lzCheck(root, "ci-workflow")
		if code != 0 {
			t.Fatalf("ci-workflow exit %d:\n%s", code, rendered)
		}
		writeFiles(t, root, map[string]string{".github/workflows/foundation.yml": rendered})
	}
	withTargets(`["pins", "live"]`)
	if code, out := lzCheck(root, "ci-source", "specs/001-x", "specs/005-y"); code != 0 || out != "FOUNDATION_CI_OK targets=2 deferred=0\n" {
		t.Errorf("BEHAVIORAL_RED: check of closed 005/T055 refused with spec 005 read: exit %d\n%s", code, out)
	}
	if code, out := lzCheck(root, "ci-source", "specs/001-x"); code != 1 || !strings.Contains(out, "CHECK_NOT_RUNNABLE live") {
		t.Errorf("BEHAVIORAL_RED: check of 005/T055 admitted without spec 005 read: exit %d\n%s", code, out)
	}
	if code, out := lzCheck(root, "ci-source", "specs/001-x", "specs/005-y", "specs/005-y"); code != 2 || !strings.Contains(out, "SPEC_DUPLICATE") {
		t.Errorf("BEHAVIORAL_RED: spec 005 read twice: exit %d\n%s", code, out)
	}
	if code, out := lzCheck(root, "ci-source", "specs/001-x", "specs/y"); code != 2 || !strings.Contains(out, "SPEC_NUMBER") {
		t.Errorf("BEHAVIORAL_RED: unnumbered spec dir accepted: exit %d\n%s", code, out)
	}
	if code, out := lzCheck(root, "ci-source", "specs/005-y"); code != 2 || !strings.Contains(out, "SPEC_FOUNDATION") {
		t.Errorf("BEHAVIORAL_RED: spec 001 not read, so its checks could not be required: exit %d\n%s", code, out)
	}
	withTargets(`["pins", "live", "later"]`)
	if code, out := lzCheck(root, "ci-source", "specs/001-x", "specs/005-y"); code != 1 || !strings.Contains(out, "CHECK_NOT_RUNNABLE later") {
		t.Errorf("BEHAVIORAL_RED: check of open 005/T056 admitted: exit %d\n%s", code, out)
	}
	withTargets(`["pins"]`)
	if code, out := lzCheck(root, "ci-source", "specs/001-x", "specs/005-y"); code != 0 || out != "FOUNDATION_CI_OK targets=1 deferred=0\n" {
		t.Errorf("BEHAVIORAL_RED: another spec's closed check made required: exit %d\n%s", code, out)
	}
}

const (
	capturedRuns = "../../internal/checks/testdata/foundation-runs"
	prHead       = "20b408842ae4d3a09b89d0639f61a6dd218cbc48"
	redHead      = "da612461f1dbc20a422043e4171e141ab0c62d33"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// runTree is the run heads' tree as far as the digests see it: the captured
// source and a registry defining its nine targets over tools/.
func runTree(t *testing.T, source []byte) map[string]string {
	t.Helper()
	parsed, err := checks.ParseFoundationSource(source)
	if err != nil {
		t.Fatal(err)
	}
	var defs []string
	for _, id := range parsed.Targets {
		defs = append(defs, `{"id": "`+id+`", "requirements": ["FR-001"], "command": "task `+id+`", "creator": "T001", "kind": "behavioral", "scope": ["tools"]}`)
	}
	return map[string]string{
		"pipelines/github/foundation-source.json": string(source),
		"harness/checks.yaml": `{"schema_version": 1, "requirements": {"FR-001": {"adrs": ["0011"]}}, "evaluators": [], "producers": [], "procedures": {},
 "checks": [` + strings.Join(defs, ",\n") + `]}`,
		"tools/pin.go": "package tools\n",
	}
}

// capture copies a captured run and writes the head's tree beside it.
func capture(t *testing.T, name string, tree map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{}
	for _, f := range []string{"run.json", "jobs.json", "workflow.sha", "source.sha", "run.log"} {
		data, err := os.ReadFile(filepath.Join(capturedRuns, name, f))
		if err != nil {
			t.Fatal(err)
		}
		files[f] = string(data)
	}
	for path, content := range tree {
		files["tree/"+path] = content
	}
	writeFiles(t, dir, files)
	return dir
}

// ci-observe turns captured GitHub output and the head's tree into the
// record; ci-runs judges the committed record against the committed source
// and tree, offline.
func TestFoundationRunCommands(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(capturedRuns, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	tree := runTree(t, source)
	code, record := lzCheck(repo(t, nil), "ci-observe",
		"head", prHead, capture(t, "head", tree), "red-control", redHead, capture(t, "red", tree))
	if code != 0 || !strings.Contains(record, `"run_id": 37278768943`) || !strings.Contains(record, `"run_id": 37279007980`) {
		t.Fatalf("BEHAVIORAL_RED: ci-observe exit %d:\n%s", code, record)
	}
	judged := func(files map[string]string) string {
		root := repo(t, nil)
		writeFiles(t, root, tree)
		writeFiles(t, root, files)
		return root
	}
	root := judged(map[string]string{"pipelines/github/foundation-runs.json": record})
	want := "RUN_OK head run=37278768943 check=111661662616 event=pull_request head=" + prHead + " targets=9\n" +
		"RUN_OK red-control run=37279007980 check=111662442027 event=pull_request head=" + redHead + " failed=test:traceability\n" +
		"FOUNDATION_RUNS_OK runs=2\n"
	if code, out := lzCheck(root, "ci-runs"); code != 0 || out != want {
		t.Errorf("BEHAVIORAL_RED: recorded runs refused: exit %d\n%s", code, out)
	}
	// A reviewed change to the source makes the recorded runs stale.
	stale := judged(map[string]string{
		"pipelines/github/foundation-source.json": strings.Replace(string(source), `"timeout_minutes": 10`, `"timeout_minutes": 9`, 1),
		"pipelines/github/foundation-runs.json":   record,
	})
	if code, out := lzCheck(stale, "ci-runs"); code != 1 || !strings.Contains(out, "RUN_STALE_SOURCE") || !strings.Contains(out, "FOUNDATION_RUNS_FAIL") {
		t.Errorf("BEHAVIORAL_RED: stale runs accepted: exit %d\n%s", code, out)
	}
	// A change to what decides a target after the run makes the run stale
	// for the judged tree, whatever the record says.
	changed := judged(map[string]string{"pipelines/github/foundation-runs.json": record, "tools/pin.go": "package tools // changed\n"})
	if code, out := lzCheck(changed, "ci-runs"); code != 1 || !strings.Contains(out, "RUN_STALE_HEAD") {
		t.Errorf("BEHAVIORAL_RED: changed tree accepted: exit %d\n%s", code, out)
	}
	// No record is no evidence: a failure, not a pass.
	absent := judged(nil)
	if code, out := lzCheck(absent, "ci-runs"); code != 1 || !strings.Contains(out, "RUNS_MISSING") {
		t.Errorf("BEHAVIORAL_RED: absent record accepted: exit %d\n%s", code, out)
	}
	for _, args := range [][]string{
		{"ci-observe"}, {"ci-observe", "head", prHead}, {"ci-observe", "head", prHead, filepath.Join(capturedRuns, "absent")},
		{"ci-runs", "extra"},
	} {
		if code, _ := lzCheck(root, args...); code != 2 {
			t.Errorf("BEHAVIORAL_RED: %v accepted (code %d)", args, code)
		}
	}
}

func TestFoundationCICommandsWithoutSource(t *testing.T) {
	root := repo(t, nil)
	for _, args := range [][]string{{"ci-workflow"}, {"ci-source", "specs/001-x"}} {
		if code, out := lzCheck(root, args...); code != 2 || !strings.Contains(out, "SOURCE_MISSING") {
			t.Errorf("BEHAVIORAL_RED: %v without a source: exit %d\n%s", args, code, out)
		}
	}
	injected := repo(t, map[string]string{"pipelines/github/foundation-source.json": strings.Replace(foundationSource, `["pins"]`, `["pins\n      BASH_ENV: x"]`, 1)})
	if code, out := lzCheck(injected, "ci-workflow"); code != 2 || !strings.Contains(out, "CHECK_NAME") || strings.Contains(out, "BASH_ENV: x\n") {
		t.Errorf("BEHAVIORAL_RED: ci-workflow rendered an injected target: exit %d\n%s", code, out)
	}
	malformed := repo(t, map[string]string{"pipelines/github/foundation-source.json": "{"})
	if code, out := lzCheck(malformed, "ci-workflow"); code != 2 || !strings.Contains(out, "SOURCE_SYNTAX") {
		t.Errorf("BEHAVIORAL_RED: ci-workflow rendered a malformed source: exit %d\n%s", code, out)
	}
}
