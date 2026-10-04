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
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(repo(t, c.files), "specs", "specs/001-x")
			if code != c.code || !strings.Contains(out, c.want) {
				t.Errorf("BEHAVIORAL_RED: code=%d, want %d with %q:\n%s", code, c.code, c.want, out)
			}
		})
	}
}

func writeEvidence(t *testing.T, root, status string, discovered int) {
	t.Helper()
	d, err := checks.Digest(root, []string{"tools"})
	if err != nil {
		t.Fatal(err)
	}
	e := `{"check": "pins", "status": "` + status + `", "input_digest": "` + d + `", "discovered": ` + string(rune('0'+discovered)) + `, "failed": 0}`
	if err := os.WriteFile(filepath.Join(root, ".local/evidence/checks/pins.json"), []byte(e), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoD(t *testing.T) {
	root := repo(t, nil)
	if code, out := lzCheck(root, "dod", "tools"); code != 1 || !strings.Contains(out, "not-run pins NO_EVIDENCE") || !strings.Contains(out, "DOD_FAIL tools") {
		t.Errorf("BEHAVIORAL_RED: missing evidence not reported as not-run: code=%d\n%s", code, out)
	}
	writeEvidence(t, root, "pass", 2)
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
		"not JSON":      "pass\n",
		"unknown field": `{"check": "pins", "status": "pass", "verdict": "ok"}`,
		"duplicate key": `{"check": "pins", "status": "fail", "status": "pass"}`,
		"trailing data": `{"check": "pins", "status": "pass"} {}`,
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
