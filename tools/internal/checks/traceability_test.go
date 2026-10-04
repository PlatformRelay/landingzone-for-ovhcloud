package checks

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const specText = `# Feature
- **FR-001**: MUST pin tools.
  - **C001.1**: Reject a missing tool (V001).
- **FR-002**: MUST trace requirements.
- **FR-003**: MUST explain the checks.
Body text mentions FR-009 and SC-009 without defining them.
- **SC-001**: Traces stay complete (V002).
`

const tasksText = "# Tasks\n\n## Setup\n\n" +
	"- [x] T001 Write pin checks in tools/internal/pin.go and Taskfile.yml\n" +
	"  - Requirements: FR-001; ADRs: 0011. Depends on: none.\n" +
	"  - Verify: `task verify:pins`: a missing tool is rejected.\n" +
	"  - Evidence: `evidence/T001.md`; GREEN.\n\n" +
	"- [ ] T002 [P] [US1] Implement trace rules in tools/internal/{trace,dod}.go, harness/checks.yaml\n" +
	"  - Requirements: FR-002 active clauses C002.1–C002.2/C002.4, SC-001; ADRs: 0008, 0019. Depends on: T001.\n" +
	"  - Verify: `task test:trace`: blanket lists fail.\n" +
	"  - Evidence: `.local/evidence/001/t002.json`; initial status `not-run`.\n\n" +
	"- [ ] T003 [US3] Write the router in AGENTS.md and harness/guides/{checks,naming}.md\n" +
	"  - Requirements: FR-003; ADRs: 0019. Depends on: T002.\n" +
	"  - Verify: exempt — docs-only; content review confirms the commands.\n" +
	"  - Evidence: `.local/evidence/001/t003-docs-review.json`; initial status `not-run`.\n\n" +
	"## Exit\n\n" +
	"- [ ] T004 Run exit checks through harness/checks.yaml\n" +
	"  - Requirements: FR-001, FR-002, FR-003, SC-001; ADRs: 0002, 0008, 0011, 0019. Depends on: T003.\n" +
	"  - Verify: Run every check and `task check`; absent evidence prevents exit.\n" +
	"  - Aggregate ADR rationale: 0002 paths; 0008 evidence; 0011 pins; 0019 guides.\n" +
	"  - Evidence: `.local/evidence/001/t004-exit.json`; initial status `not-run`.\n\n" +
	"| Task target | Creating task |\n| --- | --- |\n| `task check` | T004 |\n"

func validTasks() []Task {
	return []Task{
		{ID: "T001", Paths: []string{"tools/internal/pin.go", "Taskfile.yml"}, Requirements: []string{"FR-001"}, ADRs: []string{"0011"},
			Verify: "`task verify:pins`: a missing tool is rejected.", Evidence: "`evidence/T001.md`; GREEN."},
		{ID: "T002", Paths: []string{"tools/internal/{trace,dod}.go", "harness/checks.yaml"}, Requirements: []string{"FR-002", "SC-001"}, ADRs: []string{"0008", "0019"},
			Verify: "`task test:trace`: blanket lists fail.", Evidence: "`.local/evidence/001/t002.json`; initial status `not-run`."},
		{ID: "T003", Paths: []string{"AGENTS.md", "harness/guides/{checks,naming}.md"}, Requirements: []string{"FR-003"}, ADRs: []string{"0019"},
			Exempt: true, Verify: "content review confirms the commands.", Evidence: "`.local/evidence/001/t003-docs-review.json`; initial status `not-run`."},
		{ID: "T004", Paths: []string{"harness/checks.yaml"}, Requirements: []string{"FR-001", "FR-002", "FR-003", "SC-001"}, ADRs: []string{"0002", "0008", "0011", "0019"},
			Verify: "Run every check and `task check`; absent evidence prevents exit.", Evidence: "`.local/evidence/001/t004-exit.json`; initial status `not-run`.",
			Rationale: map[string]string{"0002": "paths", "0008": "evidence", "0011": "pins", "0019": "guides"}},
	}
}

func validRegistry() Registry {
	return Registry{
		Requirements: map[string]Requirement{
			"FR-001": {ADRs: []string{"0011"}},
			"FR-002": {ADRs: []string{"0008", "0019"}},
			"FR-003": {ADRs: []string{"0019"}},
			"SC-001": {ADRs: []string{"0002", "0008"}},
		},
		Checks: []CheckDefinition{
			{ID: "V001", Requirements: []string{"FR-001"}, Command: "task verify:pins", Creator: "T001", Kind: KindBehavioral, Scope: []string{"tools/internal"}},
			{ID: "V002", Requirements: []string{"FR-002", "SC-001"}, Command: "task test:trace", Creator: "T002", Kind: KindBehavioral, Scope: []string{"tools/internal"}},
			{ID: "V003", Requirements: []string{"FR-003"}, Command: "content review", Creator: "T003", Kind: KindDocs, Scope: []string{"AGENTS.md"}},
		},
	}
}

func validTrace() Trace {
	return Trace{
		Requirements: []string{"FR-001", "FR-002", "FR-003", "SC-001"},
		Tasks:        validTasks(),
		Registry:     validRegistry(),
		ADRs:         []string{"0002", "0008", "0011", "0019", "0021"},
	}
}

func ruleSet(findings []Finding) []string {
	seen := map[string]bool{}
	var rules []string
	for _, f := range findings {
		if !seen[f.Rule] {
			seen[f.Rule] = true
			rules = append(rules, f.Rule)
		}
	}
	sort.Strings(rules)
	return rules
}

func TestTraceabilityParse(t *testing.T) {
	if got := ParseSpec(specText); !reflect.DeepEqual(got, []string{"FR-001", "FR-002", "FR-003", "SC-001"}) {
		t.Errorf("BEHAVIORAL_RED: spec requirements = %v", got)
	}
	tasks, err := ParseTasks(tasksText)
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: valid tasks refused: %v", err)
	}
	if want := validTasks(); !reflect.DeepEqual(tasks, want) {
		t.Errorf("BEHAVIORAL_RED: parsed tasks\n got %#v\nwant %#v", tasks, want)
	}
	for name, text := range map[string]string{
		"empty":         "",
		"no task lines": "# Tasks\n\nNothing planned.\n",
	} {
		if _, err := ParseTasks(text); err == nil {
			t.Errorf("BEHAVIORAL_RED: %s: zero discovered tasks accepted", name)
		}
	}
}

// A focused trace, a justified phase aggregate and a docs-only exemption are
// all accepted together.
func TestTraceabilityAccepted(t *testing.T) {
	if findings := CheckTrace(validTrace()); len(findings) != 0 {
		t.Errorf("valid trace rejected: %+v", findings)
	}
}

func task(tr *Trace, id string) *Task {
	for i := range tr.Tasks {
		if tr.Tasks[i].ID == id {
			return &tr.Tasks[i]
		}
	}
	panic("no task " + id)
}

// addRequirement defines FR-004 in the spec; each case then wires it partly.
func addRequirement(tr *Trace) { tr.Requirements = append(tr.Requirements, "FR-004") }

var traceControls = map[string]struct {
	mutate func(*Trace)
	want   []string
}{
	"spec-wide blanket list on a focused task": {func(tr *Trace) {
		task(tr, "T001").ADRs = []string{"0002", "0008", "0011", "0019"}
	}, []string{"BLANKET_ADR_LIST", "UNRELATED_ADR"}},
	"aggregate without rationale": {func(tr *Trace) {
		task(tr, "T004").Rationale = nil
	}, []string{"BLANKET_ADR_LIST"}},
	"unrelated ADR": {func(tr *Trace) {
		task(tr, "T001").ADRs = []string{"0011", "0002"}
	}, []string{"UNRELATED_ADR"}},
	"ADR relevant to the spec but not to the task": {func(tr *Trace) {
		task(tr, "T002").ADRs = []string{"0008", "0011"}
	}, []string{"UNRELATED_ADR"}},
	"unknown ADR": {func(tr *Trace) {
		task(tr, "T001").ADRs = []string{"0011", "0099"}
	}, []string{"UNKNOWN_ADR"}},
	"rationale misses a listed ADR": {func(tr *Trace) {
		delete(task(tr, "T004").Rationale, "0002")
	}, []string{"INCOMPLETE_RATIONALE"}},
	"rationale without text": {func(tr *Trace) {
		task(tr, "T004").Rationale["0002"] = " "
	}, []string{"INCOMPLETE_RATIONALE"}},
	"rationale for an unlisted ADR": {func(tr *Trace) {
		task(tr, "T001").Rationale = map[string]string{"0011": "pins", "0008": "evidence"}
	}, []string{"INCOMPLETE_RATIONALE"}},
	"task without requirements": {func(tr *Trace) {
		tr.Tasks = append(tr.Tasks, Task{ID: "T005", Paths: []string{"tools/x.go"}, Verify: "`task x`", Evidence: "x.json"})
	}, []string{"UNMAPPED_TASK"}},
	"task names an undefined requirement": {func(tr *Trace) {
		task(tr, "T001").Requirements = []string{"FR-001", "FR-009"}
	}, []string{"UNKNOWN_REQUIREMENT"}},
	"check names an undefined requirement": {func(tr *Trace) {
		tr.Registry.Checks[0].Requirements = []string{"FR-001", "FR-009"}
	}, []string{"UNKNOWN_REQUIREMENT"}},
	"requirement no task maps": {func(tr *Trace) {
		addRequirement(tr)
		tr.Registry.Requirements["FR-004"] = Requirement{ADRs: []string{"0008"}}
		tr.Registry.Checks[1].Requirements = append(tr.Registry.Checks[1].Requirements, "FR-004")
	}, []string{"UNMAPPED_REQUIREMENT"}},
	"requirement without registered ADRs": {func(tr *Trace) {
		addRequirement(tr)
		task(tr, "T002").Requirements = append(task(tr, "T002").Requirements, "FR-004")
		tr.Registry.Checks[1].Requirements = append(tr.Registry.Checks[1].Requirements, "FR-004")
	}, []string{"UNREGISTERED_REQUIREMENT"}},
	"requirement without a check": {func(tr *Trace) {
		addRequirement(tr)
		tr.Registry.Requirements["FR-004"] = Requirement{ADRs: []string{"0008"}}
		task(tr, "T002").Requirements = append(task(tr, "T002").Requirements, "FR-004")
	}, []string{"UNCHECKED_REQUIREMENT"}},
	"check created by no task": {func(tr *Trace) {
		tr.Registry.Checks[0].Creator = "T009"
	}, []string{"UNKNOWN_CREATOR"}},
	"task without paths": {func(tr *Trace) {
		task(tr, "T001").Paths = nil
	}, []string{"MISSING_PATHS"}},
	"verify without a command": {func(tr *Trace) {
		task(tr, "T001").Verify = "run the pin checks"
	}, []string{"MISSING_VERIFY"}},
	"empty verify": {func(tr *Trace) {
		task(tr, "T002").Verify = ""
	}, []string{"MISSING_VERIFY"}},
	"missing evidence": {func(tr *Trace) {
		task(tr, "T001").Evidence = ""
	}, []string{"MISSING_EVIDENCE"}},
	"docs exemption still needs evidence": {func(tr *Trace) {
		task(tr, "T003").Evidence = ""
	}, []string{"MISSING_EVIDENCE"}},
	"exemption on a code path": {func(tr *Trace) {
		task(tr, "T003").Paths = append(task(tr, "T003").Paths, "tools/internal/guide.go")
	}, []string{"INVALID_EXEMPTION"}},
	"exemption without a reason": {func(tr *Trace) {
		task(tr, "T003").Verify = ""
	}, []string{"INVALID_EXEMPTION"}},
	"duplicate task": {func(tr *Trace) {
		tr.Tasks = append(tr.Tasks, validTasks()[0])
	}, []string{"DUPLICATE_TASK"}},
}

// Each control breaks one link of the trace and expects exactly its rules.
func TestTraceabilityRejected(t *testing.T) {
	for name, c := range traceControls {
		t.Run(name, func(t *testing.T) {
			tr := validTrace()
			c.mutate(&tr)
			if got := ruleSet(CheckTrace(tr)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v", got, c.want)
			}
		})
	}
}

// Every trace rule has a control in which it is the only rule, so dropping any
// one guard turns a control red.
func TestTraceabilityRulesIsolated(t *testing.T) {
	isolated := map[string]bool{}
	for _, c := range traceControls {
		if len(c.want) == 1 {
			isolated[c.want[0]] = true
		}
	}
	for _, rule := range TraceRules {
		if !isolated[rule] {
			t.Errorf("rule %s has no isolated control", rule)
		}
	}
	if len(TraceRules) != len(isolated) {
		t.Errorf("BEHAVIORAL_RED: TraceRules %v, controls cover %d rules", TraceRules, len(isolated))
	}
}

func TestTraceabilityRegistryStrict(t *testing.T) {
	valid := `{"schema_version": 1,
  "requirements": {"FR-001": {"adrs": ["0011"]}},
  "checks": [{"id": "V001", "requirements": ["FR-001"], "command": "task verify:pins", "creator": "T001",
              "kind": "behavioral", "scope": ["tools"], "review": true}]}
`
	reg, err := ParseRegistry([]byte(valid))
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: valid registry refused: %v", err)
	}
	want := Registry{
		Requirements: map[string]Requirement{"FR-001": {ADRs: []string{"0011"}}},
		Checks: []CheckDefinition{{ID: "V001", Requirements: []string{"FR-001"}, Command: "task verify:pins", Creator: "T001",
			Kind: KindBehavioral, Scope: []string{"tools"}, Review: true}},
	}
	if !reflect.DeepEqual(reg, want) {
		t.Errorf("BEHAVIORAL_RED: registry\n got %#v\nwant %#v", reg, want)
	}
	for name, text := range map[string]string{
		"unknown field":         strings.Replace(valid, `"review": true`, `"review": true, "owner": "x"`, 1),
		"duplicate key":         strings.Replace(valid, `"kind": "behavioral"`, `"kind": "behavioral", "kind": "docs"`, 1),
		"duplicate requirement": strings.Replace(valid, `{"FR-001": {"adrs": ["0011"]}}`, `{"FR-001": {"adrs": ["0011"]}, "FR-001": {"adrs": []}}`, 1),
		"duplicate check id":    strings.Replace(valid, `"review": true}]`, `"review": true}, {"id": "V001", "requirements": ["FR-001"], "command": "c", "creator": "T001", "kind": "docs", "scope": ["AGENTS.md"]}]`, 1),
		"unknown kind":          strings.Replace(valid, `"behavioral"`, `"manual"`, 1),
		"wrong schema version":  strings.Replace(valid, `"schema_version": 1`, `"schema_version": 2`, 1),
		"trailing document":     valid + "{}\n",
		"no checks":             `{"schema_version": 1, "requirements": {}, "checks": []}`,
		"check without scope":   strings.Replace(valid, `"scope": ["tools"]`, `"scope": []`, 1),
		"absolute scope":        strings.Replace(valid, `"scope": ["tools"]`, `"scope": ["/etc"]`, 1),
		"escaping scope":        strings.Replace(valid, `"scope": ["tools"]`, `"scope": ["tools/../.."]`, 1),
		"not JSON-compatible":   "checks:\n  - id: V001\n",
	} {
		if _, err := ParseRegistry([]byte(text)); err == nil {
			t.Errorf("BEHAVIORAL_RED: %s accepted", name)
		}
	}
}

// writeTree creates files relative to a fresh root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
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

func dodTree(t *testing.T) string {
	return writeTree(t, map[string]string{
		"modules/naming/main.tf":                "locals {}\n",
		"modules/naming/tests/names.tftest.hcl": "run \"x\" {}\n",
		"modules/naming/README.md":              "# naming\n",
		"tools/other/x.go":                      "package other\n",
	})
}

func dodRegistry() Registry {
	return Registry{Checks: []CheckDefinition{
		{ID: "N1", Requirements: []string{"FR-001"}, Command: "task test:naming", Creator: "T001", Kind: KindBehavioral, Scope: []string{"modules/naming"}},
		{ID: "N2", Requirements: []string{"FR-001"}, Command: "task snap:check", Creator: "T001", Kind: KindBehavioral, Scope: []string{"modules/naming/tests"}, Review: true},
		{ID: "N3", Requirements: []string{"FR-003"}, Command: "content review", Creator: "T003", Kind: KindDocs, Scope: []string{"modules/naming/README.md"}},
		{ID: "O1", Requirements: []string{"FR-002"}, Command: "task test:other", Creator: "T002", Kind: KindBehavioral, Scope: []string{"tools/other"}},
	}}
}

func digest(t *testing.T, root string, scope ...string) string {
	t.Helper()
	d, err := Digest(root, scope)
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: digest of %v: %v", scope, err)
	}
	if !strings.HasPrefix(d, "sha256:") || len(d) != len("sha256:")+64 {
		t.Fatalf("BEHAVIORAL_RED: digest %q is not a sha256", d)
	}
	return d
}

func freshEvidence(t *testing.T, root string) map[string]Evidence {
	return map[string]Evidence{
		"N1": {Check: "N1", Status: StatusPass, InputDigest: digest(t, root, "modules/naming"), Discovered: 3},
		"N2": {Check: "N2", Status: StatusPass, InputDigest: digest(t, root, "modules/naming/tests"), Discovered: 1, Reviewer: "independent"},
	}
}

func statuses(d DoD) map[string]string {
	got := map[string]string{}
	for _, item := range d.Items {
		got[item.Check] = item.Status + " " + item.Reason
	}
	return got
}

func TestDoDAccepted(t *testing.T) {
	root := dodTree(t)
	d := EvaluateDoD(dodRegistry(), root, "modules/naming", freshEvidence(t, root))
	want := map[string]string{"N1": "pass ", "N2": "pass ", "N3": "exempt "}
	if got := statuses(d); !reflect.DeepEqual(got, want) || !d.Pass {
		t.Errorf("BEHAVIORAL_RED: fresh evidence not accepted: pass=%v %v", d.Pass, got)
	}
}

func TestDoDSelection(t *testing.T) {
	root := dodTree(t)
	for path, want := range map[string]string{
		"modules":                "N1 N2 N3",
		"modules/naming":         "N1 N2 N3",
		"modules/naming/main.tf": "N1",
		"modules/naming/tests":   "N1 N2",
		"tools/other/x.go":       "O1",
	} {
		d := EvaluateDoD(dodRegistry(), root, path, nil)
		var ids []string
		for _, item := range d.Items {
			ids = append(ids, item.Check)
		}
		if strings.Join(ids, " ") != want {
			t.Errorf("BEHAVIORAL_RED: %s selects %v, want %v", path, ids, want)
		}
	}
	if d := EvaluateDoD(dodRegistry(), root, "docs", nil); d.Pass || len(d.Items) != 0 {
		t.Errorf("BEHAVIORAL_RED: a path with no checks passes DoD: %+v", d)
	}
}

// Each control changes one fact; only an observed, fresh, consistent pass counts.
func TestDoDRejected(t *testing.T) {
	for name, c := range map[string]struct {
		mutate func(t *testing.T, root string, e map[string]Evidence)
		check  string
		want   string
	}{
		"missing evidence": {func(t *testing.T, root string, e map[string]Evidence) { delete(e, "N1") }, "N1", "not-run NO_EVIDENCE"},
		"stale after a source change": {func(t *testing.T, root string, e map[string]Evidence) {
			if err := os.WriteFile(filepath.Join(root, "modules/naming/main.tf"), []byte("locals { x = 1 }\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "N1", "not-run STALE_EVIDENCE"},
		"stale after a file is added": {func(t *testing.T, root string, e map[string]Evidence) {
			if err := os.WriteFile(filepath.Join(root, "modules/naming/tests/extra.tftest.hcl"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "N2", "not-run STALE_EVIDENCE"},
		"forged digest": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N1"]
			n.InputDigest = "sha256:" + strings.Repeat("0", 64)
			e["N1"] = n
		}, "N1", "not-run STALE_EVIDENCE"},
		"evidence for another check": {func(t *testing.T, root string, e map[string]Evidence) {
			e["N1"] = e["N2"]
		}, "N1", "fail EVIDENCE_CHECK_MISMATCH"},
		"pass with zero discovery": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N1"]
			n.Discovered = 0
			e["N1"] = n
		}, "N1", "fail ZERO_DISCOVERY"},
		"pass with failures": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N1"]
			n.Failed = 1
			e["N1"] = n
		}, "N1", "fail INCONSISTENT_PASS"},
		"reported failure": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N1"]
			n.Status, n.Failed = StatusFail, 1
			e["N1"] = n
		}, "N1", "fail REPORTED_FAIL"},
		"reported blocked": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N1"]
			n.Status = StatusBlocked
			e["N1"] = n
		}, "N1", "blocked REPORTED_BLOCKED"},
		"unknown status": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N1"]
			n.Status = "green"
			e["N1"] = n
		}, "N1", "fail INVALID_STATUS"},
		"self-declared exemption": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N1"]
			n.Status = StatusExempt
			e["N1"] = n
		}, "N1", "fail INVALID_STATUS"},
		"expert review outstanding": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N2"]
			n.Reviewer = ""
			e["N2"] = n
		}, "N2", "review-required REVIEW_REQUIRED"},
		"scope not created yet": {func(t *testing.T, root string, e map[string]Evidence) {
			if err := os.RemoveAll(filepath.Join(root, "modules/naming/tests")); err != nil {
				t.Fatal(err)
			}
		}, "N2", "not-run SCOPE_MISSING"},
	} {
		t.Run(name, func(t *testing.T) {
			root := dodTree(t)
			evidence := freshEvidence(t, root)
			c.mutate(t, root, evidence)
			d := EvaluateDoD(dodRegistry(), root, "modules/naming", evidence)
			if got := statuses(d)[c.check]; got != c.want || d.Pass {
				t.Errorf("BEHAVIORAL_RED: %s = %q (DoD pass=%v), want %q", c.check, got, d.Pass, c.want)
			}
		})
	}
}

func TestDoDDigest(t *testing.T) {
	root := dodTree(t)
	base := digest(t, root, "modules/naming")
	if digest(t, root, "modules/naming") != base {
		t.Fatal("BEHAVIORAL_RED: digest is not deterministic")
	}
	if err := os.WriteFile(filepath.Join(root, "tools/other/x.go"), []byte("package changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if digest(t, root, "modules/naming") != base {
		t.Error("BEHAVIORAL_RED: a change outside the scope changed the digest")
	}
	if err := os.Rename(filepath.Join(root, "modules/naming/main.tf"), filepath.Join(root, "modules/naming/other.tf")); err != nil {
		t.Fatal(err)
	}
	if digest(t, root, "modules/naming") == base {
		t.Error("BEHAVIORAL_RED: a rename inside the scope kept the digest")
	}
	if digest(t, root, "modules/naming/README.md") == digest(t, root, "modules/naming/tests") {
		t.Error("BEHAVIORAL_RED: different scopes share a digest")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "modules/naming/link.tf")); err != nil {
		t.Fatal(err)
	}
	if _, err := Digest(root, []string{"modules/naming"}); err == nil {
		t.Error("BEHAVIORAL_RED: a symlink inside the scope was hashed")
	}
	if _, err := Digest(root, []string{"modules/absent"}); err == nil {
		t.Error("BEHAVIORAL_RED: a missing scope was hashed")
	}
}
