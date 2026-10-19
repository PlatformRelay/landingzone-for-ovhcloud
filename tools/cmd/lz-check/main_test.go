package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
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
		"tools/harness/checks.yaml":       registry,
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
		"malformed registry": {map[string]string{"tools/harness/checks.yaml": "checks: []\n"}, 2, "REGISTRY_SYNTAX"},
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
		"specs/005-y/spec.md":       spec005,
		"specs/005-y/tasks.md":      tasks005,
		"tools/harness/checks.yaml": registry005,
		"docs/adr/0002-x.md":        "# ADR\n",
		"modules/naming/main.tf":    "locals {}\n",
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
		"registered only for spec 001": {map[string]string{"tools/harness/checks.yaml": strings.Replace(strings.Replace(registry005,
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
	// T109: the check registry wires every check, so a change to it runs the full suite.
	if code, out := lzCheck(root, "select", "tools/harness/checks.yaml"); code != 0 || !strings.Contains(out, "SELECT_FULL SHARED_TOOL") {
		t.Errorf("BEHAVIORAL_RED: check registry change not widened to the full suite: code=%d\n%s", code, out)
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

// update-lane prints CheckUpdateLane's findings and exits 1 on any (006
// FR-015, data-model *Fixed signatures*); this repository's configuration
// passes; it takes no argument.
func TestUpdateLaneCommand(t *testing.T) {
	if code, out := lzCheck(repo(t, nil), "update-lane"); code != 1 ||
		!strings.Contains(out, "UPDATE_UNCOVERED .github/renovate.json") || !strings.Contains(out, "UPDATE_LANE_FAIL findings=1") {
		t.Errorf("BEHAVIORAL_RED: a tree without a configuration passed or was not reported: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(filepath.Join("..", "..", ".."), "update-lane"); code != 0 || !strings.Contains(out, "UPDATE_LANE_OK") {
		t.Errorf("BEHAVIORAL_RED: this repository's update lane refused: code=%d\n%s", code, out)
	}
	if code, out := lzCheck(repo(t, nil), "update-lane", "x"); code != 2 || !strings.HasPrefix(out, "usage:") {
		t.Errorf("BEHAVIORAL_RED: update-lane with an argument not refused with usage: code=%d\n%s", code, out)
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
		"specs/001-x/tasks.md":      strings.Replace(tasks, "- [ ] T001", "- [x] T001", 1),
		"specs/005-y/tasks.md":      spec005,
		"tools/harness/checks.yaml": scoped,
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
		"tools/harness/checks.yaml": `{"schema_version": 1, "requirements": {"FR-001": {"adrs": ["0011"]}}, "evaluators": [], "producers": [], "procedures": {},
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
	files["workflow.sha"] = renderedBlob(t)
	for path, content := range tree {
		files["tree/"+path] = content
	}
	writeFiles(t, dir, files)
	return dir
}

// renderedBlob is the git blob of the workflow the current renderer makes
// from the captured source. The captured runs executed the one rendered by
// the renderer of their day; the judge expects today's, so a capture's
// workflow blob is moved to it. A changed source still makes them stale.
func renderedBlob(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(capturedRuns, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := checks.ParseFoundationSource(data)
	if err != nil {
		t.Fatal(err)
	}
	workflow := checks.RenderFoundationWorkflow(source)
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(workflow))
	h.Write([]byte(workflow))
	return hex.EncodeToString(h.Sum(nil)) + "\n"
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

// registryReads is each check's scope, inputs and presence paths in the
// committed registry.
func registryReads(t *testing.T, root string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "tools", "harness", "checks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := checks.ParseRegistry(data)
	if err != nil {
		t.Fatal(err)
	}
	reads := map[string][]string{}
	for _, c := range reg.Checks {
		reads[c.ID] = append(append(append([]string{}, c.Scope...), c.Inputs...), c.Presence...)
	}
	return reads
}

// covered says whether path is one of entries or lies below one of them.
func covered(entries []string, path string) bool {
	for _, e := range entries {
		if path == e || strings.HasPrefix(path, e+"/") {
			return true
		}
	}
	return false
}

// A check's registry scope and inputs name the trees its tests and commands
// read outside tools/ (005 T111; read sets traced through the entry,
// evidence/T111.md), so a change to one of them alone reselects the check and
// changes its digest. The test reads only the registry, so its own check
// needs no further input. test:outputs' stage closures are pinned beside
// their list, in tools/internal/stacks (TestOutputsRegistryInputs).
func TestRegistryInputsCoverReads(t *testing.T) {
	reads := registryReads(t, filepath.Join("..", "..", ".."))
	want := map[string][]string{
		"test:stack-plans": {"stacks", "terramate.tm.hcl", "tests/fixtures/manifests", "tests/fixtures/outputs/envelopes",
			"stages", "components", "modules", "mise.toml"},
		"test:stacks": {"stacks", "stages", "schemas/deployments.schema.json", "specs/005-first-landing-zone-slice/data-model.md",
			"tests/fixtures/manifests", "tests/fixtures/terramate", "tests/fixtures/outputs/captures", "tests/fixtures/tofu-probes/protect",
			"tests/live/probes/storage-iam/main.tf", "components", "modules", "mise.toml", "terramate.tm.hcl"},
		"test:live-lane": {"tests/fixtures/ovhcloud", "tests/fixtures/tofu-probes/locked-recovery", "stacks/deployments.yaml"},
		// 007 T008: the project-alias tests add a tenant to the checkout's sandbox manifest text.
		"test:bootstrap": {"stacks/deployments.yaml"},
		// 006 T037: the update-lane check reads the configuration, every pin source under the
		// infrastructure trees and the schema snapshot.
		"test:update-lane": {".github/renovate.json", "mise.toml", "mise.live.toml", "tools/go.mod", "stages", "components",
			"modules", "stacks", "tests/fixtures/provider-schema", "tests/check/fixtures/updatelane"},
		// 006 T041: the docs gates read the tool pins and the lock trees, and the
		// superseded-ADR scan every file the entry admits (.gitignore and
		// .sonarcloud.properties are host-only and absent inside it).
		// The CI run record is the one file the scan skips (review round 2).
		"docs:check": {"mise.toml", "mise.live.toml", "stages", "modules", "components", ".github", ".specify",
			".tflint.hcl", "AGENTS.md", "LICENSE", "NOTICE", "README.md", "Taskfile.yml", "docs", "pipelines/README.md",
			"pipelines/github/foundation-source.json", "schemas", "specs", "stacks", "terramate.tm.hcl", "tests", "tools"},
		// 006 T053: test:traceability's real-tree tests read the CI source and
		// workflow, the update-lane pins, the README walk's trees, the ADRs,
		// the repository map and the task lists (traced through the entry,
		// evidence/T053.md); go list reads the whole tools module. The update
		// lane also looks for .github/renovate.json5, absent here.
		"test:traceability": {"Taskfile.yml", "README.md", "mise.toml", "mise.live.toml", ".github/renovate.json", ".github/renovate.json5",
			".github/workflows/foundation.yml", "pipelines/README.md", "pipelines/github/foundation-source.json",
			"components", "modules", "stages", "stacks", "schemas", "specs", "tests", "tools", "docs/adr",
			"docs/reference/repository-map.md"},
	}
	// docs:check is a foundation target: if its digest covered the run
	// record, recording a run would change the digest the run recorded, and
	// ci:foundation would find every head run stale (review round 2).
	if covered(reads["docs:check"], checks.FoundationRunsPath) {
		t.Errorf("BEHAVIORAL_RED: docs:check's scope or inputs cover %s", checks.FoundationRunsPath)
	}
	for id, paths := range want {
		entries, ok := reads[id]
		if !ok {
			t.Errorf("BEHAVIORAL_RED: %s is not in the registry", id)
			continue
		}
		for _, p := range paths {
			if !covered(entries, p) {
				t.Errorf("BEHAVIORAL_RED: %s reads %s, but its registry scope and inputs do not name it", id, p)
			}
		}
	}
	mapReadsCovered(t, reads["docs:check"])
	readmesCovered(t, reads["test:traceability"])
	admittedCovered(t, "docs:check", reads["docs:check"], false)
	admittedCovered(t, "test:traceability", reads["test:traceability"], true)
	recordOutsideFoundation(t, reads)
}

// 006 T053: test:traceability's README walk (TestDocsRegistryScopeREADMEs)
// reads which READMEs exist; its reads cover each one it finds.
func readmesCovered(t *testing.T, entries []string) {
	t.Helper()
	readmes, _, err := readmePages(filepath.Join("..", "..", ".."))
	if err != nil || len(readmes) == 0 {
		t.Fatalf("readmePages: %v %v", readmes, err)
	}
	for _, p := range readmes {
		if !covered(entries, p) {
			t.Errorf("BEHAVIORAL_RED: test:traceability reads %s, but its registry reads do not name it", p)
		}
	}
}

// 006 T053: a check that reads the top level reads each name the offline
// entry admits, present or not: creating one changes what it judges (a map
// row due, a file to scan, a README to find). Its registry reads cover each
// such name, so its digest sees the name appear; the directory holding the
// CI run record is covered file by file instead, the record excepted.
// walked keeps the names readmePages walks (readsTopName). The rule asks the
// tree nothing about a covered name, so of the top level this test (part of
// test:traceability) reads only the names under pipelines/, walked whole
// (review round 1).
func admittedCovered(t *testing.T, id string, entries []string, walked bool) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	recordTop, _, _ := strings.Cut(checks.FoundationRunsPath, "/")
	for _, name := range checks.EntryAdmitted() {
		switch {
		case !readsTopName(walked, name), covered(entries, name):
		case name == recordTop:
			for _, rel := range uncoveredFiles(t, root, name, entries) {
				t.Errorf("BEHAVIORAL_RED: %s reads %s, but its registry reads do not cover it", id, rel)
			}
		default:
			t.Errorf("BEHAVIORAL_RED: %s reads the admitted top-level name %s, but its registry reads do not cover it", id, name)
		}
	}
}

// readsTopName says whether a check reads the admitted top-level name: every
// one, or with walked those readmePages lists by kind (os.ReadDir of the
// root, e.IsDir): any name neither hidden nor docs, file or directory,
// present or absent, since a file turned directory is walked.
func readsTopName(walked bool, name string) bool {
	return !walked || !strings.HasPrefix(name, ".") && name != "docs"
}

// readsTopName keeps for the README walk every non-hidden name but docs, and
// for a check reading the whole top level every name (review round 2).
func TestRegistryReadsTopName(t *testing.T) {
	for _, row := range []struct {
		walked bool
		name   string
		want   bool
	}{{true, ".github", false}, {true, "docs", false}, {true, "LICENSE", true}, {true, "profiles", true}, {false, ".github", true}, {false, "docs", true}} {
		if got := readsTopName(row.walked, row.name); got != row.want {
			t.Errorf("BEHAVIORAL_RED: readsTopName(%t, %s) = %t, want %t", row.walked, row.name, got, row.want)
		}
	}
}

// uncoveredFiles are the files under dir, other than the CI run record, that
// entries do not cover.
func uncoveredFiles(t *testing.T, root, dir string, entries []string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if err == nil && rel != checks.FoundationRunsPath && !covered(entries, rel) {
			files = append(files, rel)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// uncoveredFiles names a file the entries miss and passes the run record and
// the files they cover.
func TestRegistryUncoveredFiles(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"pipelines/README.md", "pipelines/github/foundation-source.json", checks.FoundationRunsPath, "pipelines/gitlab/offline.yml"} {
		writePrivate(t, filepath.Join(root, filepath.FromSlash(f)))
	}
	got := uncoveredFiles(t, root, "pipelines", []string{"pipelines/README.md", "pipelines/github/foundation-source.json"})
	if strings.Join(got, " ") != "pipelines/gitlab/offline.yml" {
		t.Errorf("BEHAVIORAL_RED: uncovered %q, want [pipelines/gitlab/offline.yml]", got)
	}
}

// 006 T053: no foundation target's registry reads cover the CI run record;
// recording a run would change the digest the run recorded (T041).
func recordOutsideFoundation(t *testing.T, reads map[string][]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", checks.FoundationSourcePath))
	if err != nil {
		t.Fatal(err)
	}
	source, err := checks.ParseFoundationSource(data)
	if err != nil || len(source.Targets) == 0 {
		t.Fatalf("foundation source: %v, targets %v", err, source.Targets)
	}
	for _, id := range source.Targets {
		if covered(reads[id], checks.FoundationRunsPath) {
			t.Errorf("BEHAVIORAL_RED: the foundation target %s's registry reads cover %s", id, checks.FoundationRunsPath)
		}
	}
}

// 006 T053: creating a top-level name the entry admits changes the digest of
// each check that reads the top level, as an empty directory, once a file is
// in it, and as a file; a name the entry does not admit leaves it unchanged.
// The fixture holds one file per scope and input entry of the check; the
// rows are the admitted names it binds by presence.
func TestRegistryDigestSeesAdmittedNames(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, "tools", "harness", "checks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := checks.ParseRegistry(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"docs:check", "test:traceability"} {
		i := slices.IndexFunc(reg.Checks, func(c checks.CheckDefinition) bool { return c.ID == id })
		if i < 0 {
			t.Fatalf("%s is not in the registry", id)
		}
		c := reg.Checks[i]
		fixture := digestFixture(t, c)
		base := checkDigest(t, fixture, c)
		names := presenceTopNames(c)
		for _, name := range names {
			t.Run(id+"/"+name, func(t *testing.T) { nameChangesDigest(t, fixture, c, name, base) })
		}
		if len(names) == 0 {
			t.Fatalf("BEHAVIORAL_RED: %s binds no admitted top-level name by presence; no row judges a new one", id)
		}
		t.Run(id+"/not admitted", func(t *testing.T) { otherNameKeepsDigest(t, fixture, c, base) })
	}
}

// otherNameKeepsDigest creates a top-level directory the entry does not admit
// and checks the digest is unchanged.
func otherNameKeepsDigest(t *testing.T, fixture string, c checks.CheckDefinition, base string) {
	t.Helper()
	const other = "agent-context"
	if slices.Contains(checks.EntryAdmitted(), other) {
		t.Fatalf("%s is admitted; the control needs a name that is not", other)
	}
	writePrivate(t, filepath.Join(fixture, other, "x.md"))
	defer os.RemoveAll(filepath.Join(fixture, other))
	if got := checkDigest(t, fixture, c); got != base {
		t.Errorf("BEHAVIORAL_RED: %s's digest changed when %s/ appeared, which the entry does not admit", c.ID, other)
	}
}

// 006 T053: a presence path is held to the rules of scope and inputs: the
// registry refuses one that is not a clean relative path, and the digest
// refuses one that is a link (even a dangling one) instead of reading it as
// absent.
func TestRegistryPresenceRefusals(t *testing.T) {
	c := checks.CheckDefinition{ID: "pins", Command: "task pins", Kind: "behavioral", Scope: []string{"tools"}, Presence: []string{"profiles"}}
	for name, target := range map[string]string{"link": "tools", "dangling link": "gone"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writePrivate(t, filepath.Join(root, "tools", "x"))
			if err := os.Symlink(target, filepath.Join(root, "profiles")); err != nil {
				t.Fatal(err)
			}
			if d, err := checks.CheckDigest(root, c); err == nil {
				t.Errorf("BEHAVIORAL_RED: a linked presence path was digested: %s", d)
			}
		})
	}
	t.Run("unclean path", func(t *testing.T) {
		text := strings.Replace(registry, `"scope": ["tools"]`, `"scope": ["tools"], "presence": ["../profiles"]`, 1)
		if _, err := checks.ParseRegistry([]byte(text)); err == nil || !strings.Contains(err.Error(), "REGISTRY_SCOPE") {
			t.Errorf("BEHAVIORAL_RED: an unclean presence path was admitted: %v", err)
		}
		control := strings.Replace(registry, `"scope": ["tools"]`, `"scope": ["tools"], "presence": ["profiles"]`, 1)
		if _, err := checks.ParseRegistry([]byte(control)); err != nil {
			t.Errorf("BEHAVIORAL_RED: a clean presence path was refused: %v", err)
		}
		root := t.TempDir()
		writePrivate(t, filepath.Join(root, "tools", "x"))
		unclean := c
		unclean.Presence = []string{"../profiles"}
		if d, err := checks.CheckDigest(root, unclean); err == nil {
			t.Errorf("BEHAVIORAL_RED: an unclean presence path was digested: %s", d)
		}
	})
}

// presenceTopNames are the admitted top-level names c binds by presence;
// admittedCovered requires every such name c reads and its inputs miss
// (each one absent here among them) to be there.
func presenceTopNames(c checks.CheckDefinition) []string {
	var names []string
	for _, name := range checks.EntryAdmitted() {
		if slices.Contains(c.Presence, name) {
			names = append(names, name)
		}
	}
	return names
}

// nameChangesDigest creates name in the fixture as an empty directory, then
// with a file in it, then as a file, and checks each changes the digest.
func nameChangesDigest(t *testing.T, fixture string, c checks.CheckDefinition, name, base string) {
	t.Helper()
	full := filepath.Join(fixture, name)
	defer os.RemoveAll(full)
	if err := os.Mkdir(full, 0o700); err != nil {
		t.Fatal(err)
	}
	empty := checkDigest(t, fixture, c)
	if empty == base {
		t.Errorf("BEHAVIORAL_RED: %s's digest did not change when the admitted directory %s/ appeared", c.ID, name)
	}
	writePrivate(t, filepath.Join(full, "x.md"))
	if got := checkDigest(t, fixture, c); got == empty {
		t.Errorf("BEHAVIORAL_RED: %s's digest did not change when a file appeared in %s/", c.ID, name)
	}
	if err := os.RemoveAll(full); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, full)
	if got := checkDigest(t, fixture, c); got == base || got == empty {
		t.Errorf("BEHAVIORAL_RED: %s's digest did not tell the admitted file %s from its absence or a directory", c.ID, name)
	}
}

// digestFixture lays entry/x for each scope and input entry of c. Every entry
// is a directory here, whatever its kind in this repository, so the test
// reads nothing of the tree beyond the registry (review round 1); the digest
// walks a directory entry as it walks a file entry.
func digestFixture(t *testing.T, c checks.CheckDefinition) string {
	t.Helper()
	fixture := t.TempDir()
	for _, e := range append(append([]string{}, c.Scope...), c.Inputs...) {
		writePrivate(t, filepath.Join(fixture, filepath.FromSlash(e), "x"))
	}
	return fixture
}

// writePrivate writes a one-line file readable by its owner only, with its
// parents.
func writePrivate(t *testing.T, file string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkDigest(t *testing.T, root string, c checks.CheckDefinition) string {
	t.Helper()
	d, err := checks.CheckDigest(root, c)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// 006 T042: the repository map check reads the map and whether each path it
// names under an admitted top-level name exists. docs:check's reads cover
// each such path, or a file below it whose digest binds its existence
// (pipelines/github is bound through foundation-source.json, never through
// the CI run record).
func mapReadsCovered(t *testing.T, entries []string) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(mapPage)))
	if err != nil {
		t.Fatal(err)
	}
	if !covered(entries, mapPage) {
		t.Errorf("BEHAVIORAL_RED: docs:check reads %s, but its registry scope and inputs do not name it", mapPage)
	}
	admitted := map[string]bool{}
	for _, name := range checks.EntryAdmitted() {
		admitted[name] = true
	}
	for _, p := range mapPaths(string(text)) {
		top, _, _ := strings.Cut(p, "/")
		if admitted[top] && !covered(entries, p) && !slices.ContainsFunc(entries, func(e string) bool { return strings.HasPrefix(e, p+"/") }) {
			t.Errorf("BEHAVIORAL_RED: docs:check judges the map path %s, but its registry scope and inputs cover neither it nor a file below it", p)
		}
	}
}

// Lines read by TestTaskfileTracesEverySpec: active (uncommented) lines only,
// and the ci-source arguments stop at an inline YAML comment.
var (
	specDirsLine = regexp.MustCompile(`(?m)^ +SPEC_DIRS:[^\n]*default "([^"]*)"`)
	ciSourceLine = regexp.MustCompile(`(?m)^ +- go -C tools run \./cmd/lz-check -root \.\. ci-source ([^#\n]*)`)
)

// taskfileSpecDirs returns the specs/NNN-… arguments on the active line of
// task's block in the Taskfile that line matches; a commented-out line or
// one in another task is never read. It fails the test when the task or the
// line is missing, so a reworded Taskfile cannot pass by matching nothing.
func taskfileSpecDirs(t *testing.T, taskfile, task string, line *regexp.Regexp) []string {
	t.Helper()
	block, ok := taskBlock(taskfile, task)
	if !ok {
		t.Fatalf("BEHAVIORAL_RED: Taskfile.yml has no task %s", task)
	}
	m := line.FindStringSubmatch(block)
	if m == nil {
		t.Fatalf("BEHAVIORAL_RED: task %s has no line matching %s", task, line)
	}
	return regexp.MustCompile(`specs/[0-9]{3}-[^ "'\n]+`).FindAllString(m[1], -1)
}

// taskBlock returns task's block in the Taskfile, from its two-space-indented
// header up to the next task's header.
func taskBlock(taskfile, task string) (string, bool) {
	start := strings.Index(taskfile, "\n  "+task+":\n")
	if start < 0 {
		return "", false
	}
	block := taskfile[start+1:]
	if end := regexp.MustCompile(`\n  [^ \n#][^\n]*:\n`).FindStringIndex(block[1:]); end != nil {
		block = block[:end[0]+1]
	}
	return block, true
}

// specNumbers maps specs/NNN-… directories to NNN.
func specNumbers(dirs []string) []string {
	var numbers []string
	for _, d := range dirs {
		numbers = append(numbers, strings.TrimPrefix(d, "specs/")[:3])
	}
	return numbers
}

// registrySpecs returns the specs that own registry keys: NNN for an NNN/…
// key, 001 for spec 001's unprefixed keys. The keys are requirement ids,
// each check's requirements and creator, and procedure task ids.
func registrySpecs(reg checks.Registry) map[string]bool {
	owners := map[string]bool{}
	add := func(key string) {
		if number, _, ok := strings.Cut(key, "/"); ok {
			owners[number] = true
		} else {
			owners["001"] = true
		}
	}
	for key := range reg.Requirements {
		add(key)
	}
	for key := range reg.Procedures {
		add(key)
	}
	for _, c := range reg.Checks {
		add(c.Creator)
		for _, key := range c.Requirements {
			add(key)
		}
	}
	return owners
}

// Every spec with registry keys is traced by `task check:specs` with no
// argument and read by the CI source check, and the CI source check's
// digest changes with each tasks.md it reads (006 FR-022, V004).
func TestTaskfileTracesEverySpec(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	taskfile := string(data)
	reg, err := os.ReadFile(filepath.Join(root, "tools", "harness", "checks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := checks.ParseRegistry(reg)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for number := range registrySpecs(registry) {
		want = append(want, number)
	}
	sort.Strings(want)
	if len(want) == 0 {
		t.Fatal("BEHAVIORAL_RED: the registry names no spec keys")
	}
	missing := func(listed []string) []string {
		got := map[string]bool{}
		for _, n := range listed {
			got[n] = true
		}
		var out []string
		for _, n := range want {
			if !got[n] {
				out = append(out, n)
			}
		}
		return out
	}

	t.Run("check:specs default", func(t *testing.T) {
		listed := specNumbers(taskfileSpecDirs(t, taskfile, "check:specs", specDirsLine))
		if gap := missing(listed); len(gap) > 0 {
			t.Errorf("BEHAVIORAL_RED: task check:specs with no argument does not trace spec(s) %v that own registry keys; it traces %v", gap, listed)
		}
	})
	t.Run("ci-source specs", func(t *testing.T) {
		listed := specNumbers(taskfileSpecDirs(t, taskfile, "test:foundation-ci", ciSourceLine))
		if gap := missing(listed); len(gap) > 0 {
			t.Errorf("BEHAVIORAL_RED: test:foundation-ci's ci-source does not read spec(s) %v that own registry keys; it reads %v", gap, listed)
		}
	})
	t.Run("foundation-ci inputs", func(t *testing.T) {
		reads := registryReads(t, root)["test:foundation-ci"]
		if reads == nil {
			t.Fatal("BEHAVIORAL_RED: test:foundation-ci is not in the registry")
		}
		for _, dir := range taskfileSpecDirs(t, taskfile, "test:foundation-ci", ciSourceLine) {
			if !covered(reads, dir+"/tasks.md") {
				t.Errorf("BEHAVIORAL_RED: test:foundation-ci's ci-source reads %s/tasks.md, but its registry scope and inputs do not name it", dir)
			}
		}
	})
}

// entryTargets are the checks lz-offline runs with its own trusted code; the
// Taskfile never defines them (Taskfile.yml header; trustedTargets in
// tools/cmd/lz-offline/main.go).
var entryTargets = map[string]bool{"verify:toolchain": true, "test:offline-boundary": true}

// shellWords splits a Taskfile command line into words, honouring single and
// double quotes. An unquoted command separator (& ; |) is a word of its own,
// prefixed with NUL so it never equals an argument; an unquoted redirection
// (<, >, >>, >&, with a leading descriptor number such as 2>&1) is one word
// prefixed with \x01, its target being the next word. An unquoted # at the
// start of a word ends the line.
func shellWords(line string) []string {
	var words []string
	var word strings.Builder
	var quote byte
	inWord := false
	flush := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			word.WriteByte(c)
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == ' ' || c == '\t':
			flush()
		case c == '<' || c == '>':
			if inWord && strings.Trim(word.String(), "0123456789") == "" {
				word.Reset()
				inWord = false
			}
			flush()
			op := string(c)
			for i+1 < len(line) && (line[i+1] == '>' || line[i+1] == '&') {
				i++
				op += string(line[i])
			}
			words = append(words, "\x01"+op)
		case strings.IndexByte("&;|", c) >= 0:
			flush()
			words = append(words, "\x00"+string(c))
		case c == '#' && !inWord:
			return words
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return words
}

// goRun is one `go -C tools test|run|build` invocation of a Taskfile command:
// its verb, build tags and the packages it names.
type goRun struct {
	verb, tags string
	pkgs       []string
}

// goWord finds the word go in a shell word, alone or inside a quoted one;
// goRuns uses it to fail on a go command it cannot read rather than skip it.
var goWord = regexp.MustCompile(`(^|[\s'"|>&;(])go(\s|$)`)

// goValueFlags are the go test|run|build flags whose value is the next word.
var goValueFlags = map[string]bool{"-run": true, "-skip": true, "-o": true, "-timeout": true, "-count": true, "-p": true,
	"-bench": true, "-parallel": true, "-ldflags": true, "-gcflags": true, "-coverprofile": true}

// goRuns returns the Go invocations of a Taskfile task block. Every line but
// the task's header, its desc and comments is read, so a command in a YAML
// block scalar is read too; a leading `- ` or `cmd:` is dropped. A go word in
// command position starts an invocation; its packages are the words that are
// neither flags, flag values nor redirections and their targets, and a go run
// names one package, the words after it being the program's arguments. A go
// command of any other form, and every go word not read as an invocation (in
// a quoted YAML scalar, an argument, a redirection target), is returned with
// no packages, so the caller fails on it rather than skip it.
func goRuns(block string) []goRun {
	var runs []goRun
	for _, line := range strings.Split(block, "\n")[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "desc:") {
			continue
		}
		line = strings.TrimPrefix(strings.TrimPrefix(line, "- "), "cmd:")
		words := shellWords(line)
		read, seen := 0, 0
		for _, w := range words {
			if goWord.MatchString(w) {
				seen++
			}
		}
		for i := 0; i < len(words); i++ {
			if words[i] != "go" || (i > 0 && !strings.HasPrefix(words[i-1], "\x00")) {
				continue
			}
			read++
			if i+3 >= len(words) || words[i+1] != "-C" || words[i+2] != "tools" || (words[i+3] != "test" && words[i+3] != "run" && words[i+3] != "build") {
				runs = append(runs, goRun{verb: strings.Join(words[i+1:min(i+4, len(words))], " ")})
				continue
			}
			verb := words[i+3]
			r := goRun{verb: verb}
			for j := i + 4; j < len(words) && !strings.HasPrefix(words[j], "\x00"); j++ {
				w := words[j]
				switch {
				case w == "-tags" && j+1 < len(words):
					j++
					r.tags = words[j]
				case strings.HasPrefix(w, "-tags="):
					r.tags = strings.TrimPrefix(w, "-tags=")
				case strings.HasPrefix(w, "\x01"):
					j++
				case goValueFlags[w]:
					j++
				case strings.HasPrefix(w, "-"):
				default:
					r.pkgs = append(r.pkgs, w)
				}
				if verb == "run" && len(r.pkgs) > 0 {
					break
				}
			}
			runs = append(runs, r)
		}
		if seen > read {
			runs = append(runs, goRun{verb: fmt.Sprintf("in %q", line)})
		}
	}
	return runs
}

// goDeps returns the tools-module package directories, relative to the
// repository root, that r's packages are or import (test imports included
// for go test), as `go list -deps` resolves them in the vendored module
// (premise P1). Vendored and standard-library packages are left out.
func goDeps(repoRoot string, r goRun) ([]string, error) {
	args := []string{"list", "-deps", "-f", "{{.Dir}}"}
	if r.verb == "test" {
		args = append(args, "-test")
	}
	if r.tags != "" {
		args = append(args, "-tags", r.tags)
	}
	args = append(args, r.pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join(repoRoot, "tools")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var dirs []string
	for _, dir := range strings.Split(string(out), "\n") {
		if dir == "" {
			continue
		}
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
		rel, err := filepath.Rel(root, dir)
		rel = filepath.ToSlash(rel)
		if err != nil || (rel != "tools" && !strings.HasPrefix(rel, "tools/")) || rel == "tools/vendor" || strings.HasPrefix(rel, "tools/vendor/") || seen[rel] {
			continue
		}
		seen[rel] = true
		dirs = append(dirs, rel)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// creatorTaskList names the tasks.md that owns a check's creator (a bare id
// is spec 001's, NNN/Tnnn spec NNN's) and the creator's id in it.
func creatorTaskList(t *testing.T, root string, c checks.CheckDefinition) (path, id string) {
	t.Helper()
	spec, id, ok := strings.Cut(c.Creator, "/")
	if !ok {
		spec, id = "001", c.Creator
	}
	found, _ := filepath.Glob(filepath.Join(root, "specs", spec+"-*", "tasks.md"))
	if len(found) != 1 {
		t.Fatalf("BEHAVIORAL_RED: creator %s of %s: want one specs/%s-*/tasks.md, found %d", c.Creator, c.ID, spec, len(found))
	}
	return found[0], id
}

// creatorsDone reports, per registry creator, whether its task is ticked in
// the tasks.md of the spec that owns it (creatorTaskList). An id that
// tasks.md does not list is open.
func creatorsDone(t *testing.T, root string, reg checks.Registry) map[string]bool {
	t.Helper()
	ticks := map[string]map[string]bool{}
	done := map[string]bool{}
	for _, c := range reg.Checks {
		file, id := creatorTaskList(t, root, c)
		if ticks[file] == nil {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			list, err := checks.ParseTasks(string(data))
			if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			ticks[file] = map[string]bool{}
			for _, task := range list {
				ticks[file][task.ID] = task.Done
			}
		}
		done[c.Creator] = ticks[file][id]
	}
	return done
}

// judgeGoDeps judges the registry under root against the Go its checks run
// (006 FR-012): every in-module directory a behavioural check's Taskfile
// target depends on lies in the check's scope or inputs. A check without a
// target is planned while its creator is open and a failure once it is
// ticked; the entry's own targets have none by design. It returns the
// planned checks and one failure per check and directory.
func judgeGoDeps(t *testing.T, root string) (planned, failures []string) {
	t.Helper()
	taskfile, err := os.ReadFile(filepath.Join(root, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "tools", "harness", "checks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := checks.ParseRegistry(data)
	if err != nil {
		t.Fatal(err)
	}
	reads := registryReads(t, root)
	done := creatorsDone(t, root, reg)
	deps := map[string][]string{}
	for _, c := range reg.Checks {
		if c.Kind != checks.KindBehavioral {
			continue
		}
		block, ok := taskBlock(string(taskfile), c.ID)
		switch {
		case !ok && entryTargets[c.ID]:
			continue
		case !ok && !done[c.Creator]:
			planned = append(planned, c.ID)
			continue
		case !ok:
			failures = append(failures, fmt.Sprintf("%s: its creator %s is ticked, but Taskfile.yml has no target %s", c.ID, c.Creator, c.ID))
			continue
		}
		reported := map[string]bool{}
		for _, r := range goRuns(block) {
			if len(r.pkgs) == 0 {
				failures = append(failures, fmt.Sprintf("%s: go %s: the test reads only go -C tools test|run|build naming packages", c.ID, r.verb))
				continue
			}
			key := fmt.Sprintf("%t %s %s", r.verb == "test", r.tags, strings.Join(r.pkgs, " "))
			dirs, seen := deps[key]
			if !seen {
				if dirs, err = goDeps(root, r); err != nil {
					t.Fatalf("BEHAVIORAL_RED: premise P1: %v", err)
				}
				deps[key] = dirs
			}
			for _, dir := range dirs {
				if !reported[dir] && !covered(reads[c.ID], dir) {
					reported[dir] = true
					failures = append(failures, fmt.Sprintf("%s runs Go that depends on %s (go %s %s), but its registry scope and inputs do not name it", c.ID, dir, r.verb, strings.Join(r.pkgs, " ")))
				}
			}
		}
	}
	return planned, failures
}

// goDepsFixture is a tree whose registry has a check with a target, one
// without a target and an open creator (planned), one without a target and a
// ticked creator, and an entry target; no target runs Go.
func goDepsFixture(t *testing.T) string {
	t.Helper()
	task := func(box, id string) string {
		return "- [" + box + "] " + id + " Write pins in tools/pin.go\n" +
			"  - Requirements: FR-001; ADRs: 0011. Depends on: none.\n" +
			"  - Verify: `task pins`: missing tool rejected.\n" +
			"  - Evidence: `evidence/" + id + ".md`.\n"
	}
	check := func(id, creator string) string {
		return `{"id": "` + id + `", "requirements": ["FR-001"], "command": "task ` + id + `", "creator": "` + creator + `", "kind": "behavioral", "scope": ["tools/pin.go"]}`
	}
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"Taskfile.yml":         "version: '3'\ntasks:\n  test:present:\n    cmds:\n      - echo present\n",
		"specs/001-x/tasks.md": task("x", "T001"),
		"specs/006-y/tasks.md": task("x", "T001") + task(" ", "T002"),
		"tools/harness/checks.yaml": `{"schema_version": 1, "requirements": {"FR-001": {"adrs": ["0011"]}}, "evaluators": [], "producers": [], "procedures": {},
 "checks": [` + strings.Join([]string{check("test:present", "T001"), check("test:planned", "006/T002"),
			check("test:orphan", "006/T001"), check("verify:toolchain", "T001")}, ",\n  ") + `]}`,
	})
	return root
}

// Every check that runs Go reselects on a change to any tools-module package
// it builds, and the check that judges the registry reads it (006 FR-012,
// V004; research R11).
func TestRegistryInputsCoverGoDeps(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("go deps covered", func(t *testing.T) {
		live, err := goDeps(root, goRun{verb: "test", pkgs: []string{"./internal/live"}})
		if err != nil {
			t.Fatalf("BEHAVIORAL_RED: premise P1: %v", err)
		}
		t.Logf("P1: go list -deps -test ./internal/live: in-module %v", live)
		planned, failures := judgeGoDeps(t, root)
		t.Logf("planned: %v", planned)
		for _, f := range failures {
			t.Errorf("BEHAVIORAL_RED: %s", f)
		}
	})
	t.Run("traceability reads the registry", func(t *testing.T) {
		if !covered(registryReads(t, root)["test:traceability"], "tools/harness/checks.yaml") {
			t.Error("BEHAVIORAL_RED: test:traceability judges tools/harness/checks.yaml, but its registry scope and inputs do not name it")
		}
	})
	t.Run("traceability reads the task lists", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(root, "tools", "harness", "checks.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		reg, err := checks.ParseRegistry(data)
		if err != nil {
			t.Fatal(err)
		}
		reads := registryReads(t, root)["test:traceability"]
		seen := map[string]bool{}
		for _, c := range reg.Checks {
			file, _ := creatorTaskList(t, root, c)
			rel, err := filepath.Rel(root, file)
			if err != nil {
				t.Fatal(err)
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				continue
			}
			seen[rel] = true
			if !covered(reads, rel) {
				t.Errorf("BEHAVIORAL_RED: test:traceability judges creator status in %s, but its registry scope and inputs do not name it", rel)
			}
		}
		if len(seen) == 0 {
			t.Fatal("BEHAVIORAL_RED: the registry names no creators, so the judge reads no task list")
		}
	})
	t.Run("planned check skipped", func(t *testing.T) {
		planned, failures := judgeGoDeps(t, goDepsFixture(t))
		if strings.Join(planned, " ") != "test:planned" {
			t.Errorf("BEHAVIORAL_RED: planned %v, want [test:planned]", planned)
		}
		for _, f := range failures {
			if strings.HasPrefix(f, "test:planned") || strings.HasPrefix(f, "verify:toolchain") {
				t.Errorf("BEHAVIORAL_RED: a planned check or an entry target failed: %s", f)
			}
		}
	})
	t.Run("closed creator without target", func(t *testing.T) {
		_, failures := judgeGoDeps(t, goDepsFixture(t))
		if len(failures) != 1 || !strings.HasPrefix(failures[0], "test:orphan: ") {
			t.Errorf("BEHAVIORAL_RED: want one failure naming test:orphan, got %q", failures)
		}
	})
}
