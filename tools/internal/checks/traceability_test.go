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
	"- [x] T001 Write pin checks in tools/internal/pin.go and Taskfile.yml — closed 2026-10-02, evidence: evidence/T001.md\n" +
	"  - Requirements: FR-001; ADRs: 0011. Depends on: none, confirmed 2026-10-01.\n" +
	"  - Verify: `task verify:pins`: a missing tool is rejected and the task fails closed.\n" +
	"  - Evidence: `evidence/T001.md`; GREEN.\n\n" +
	"- [ ] T002 [P] [US1] Implement trace/DoD rules in tools/internal/{trace,dod}.go, harness/checks.yaml (pinned 1.13.0)\n" +
	"  - Requirements: FR-002 active clauses C002.1–C002.2/C002.4, SC-001; ADRs: 0008, 0019. Depends on: T001.\n" +
	"  - Verify: `task test:trace`: blanket lists fail.\n" +
	"  - Evidence: `.local/evidence/001/t002.json`.\n\n" +
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
			Verify: "`task verify:pins`: a missing tool is rejected and the task fails closed.", Evidence: "`evidence/T001.md`; GREEN."},
		{ID: "T002", Paths: []string{"trace/DoD", "tools/internal/{trace,dod}.go", "harness/checks.yaml"}, Requirements: []string{"FR-002", "SC-001"}, ADRs: []string{"0008", "0019"},
			Verify: "`task test:trace`: blanket lists fail.", Evidence: "`.local/evidence/001/t002.json`."},
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
			{ID: "verify:pins", Requirements: []string{"FR-001"}, Command: "task verify:pins", Creator: "T001", Kind: KindBehavioral, Scope: []string{"tools/internal"}},
			{ID: "test:trace", Requirements: []string{"FR-002", "SC-001"}, Command: "task test:trace", Creator: "T002", Kind: KindBehavioral, Scope: []string{"tools/internal"}},
			{ID: "review:guides", Requirements: []string{"FR-003"}, Command: "content review", Creator: "T003", Kind: KindDocs, Scope: []string{"AGENTS.md"}},
		},
		Evaluators: []string{"check"},
		Procedures: map[string]string{},
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
	if tasks, err := ParseTasks("- [ ] T009 Write the guide in AGENTS.md.\n"); err != nil || !reflect.DeepEqual(tasks[0].Paths, []string{"AGENTS.md"}) {
		t.Errorf("BEHAVIORAL_RED: path ending a sentence: %+v %v", tasks, err)
	}
	for name, text := range map[string]string{
		"empty":          "",
		"no task lines":  "# Tasks\n\nNothing planned.\n",
		"repeated field": strings.Replace(tasksText, "  - Evidence: `evidence/T001.md`; GREEN.\n", "  - Evidence: `evidence/T001.md`; GREEN.\n  - Evidence: other.json\n", 1),
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
	"task without ADRs": {func(tr *Trace) {
		task(tr, "T001").ADRs = nil
	}, []string{"MISSING_ADR"}},
	"ADRs cover only some requirements": {func(tr *Trace) {
		task(tr, "T002").ADRs = []string{"0019"}
	}, []string{"MISSING_ADR"}},
	"rationale misses a listed ADR": {func(tr *Trace) {
		delete(task(tr, "T004").Rationale, "0002")
	}, []string{"INCOMPLETE_RATIONALE"}},
	"rationale without text": {func(tr *Trace) {
		task(tr, "T004").Rationale["0002"] = " "
	}, []string{"INCOMPLETE_RATIONALE"}},
	"rationale for an unlisted ADR": {func(tr *Trace) {
		task(tr, "T001").Rationale = map[string]string{"0008": "evidence"}
	}, []string{"INCOMPLETE_RATIONALE"}},
	"task without requirements": {func(tr *Trace) {
		tr.Tasks = append(tr.Tasks, Task{ID: "T005", Paths: []string{"tools/x.go"}, Verify: "`go test ./tools`", Evidence: "`x.json`"})
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
	"requirement registered with no ADRs": {func(tr *Trace) {
		tr.Registry.Requirements["FR-003"] = Requirement{ADRs: []string{}}
	}, []string{"UNREGISTERED_REQUIREMENT", "UNRELATED_ADR"}},
	"requirement without a check": {func(tr *Trace) {
		addRequirement(tr)
		tr.Registry.Requirements["FR-004"] = Requirement{ADRs: []string{"0008"}}
		task(tr, "T002").Requirements = append(task(tr, "T002").Requirements, "FR-004")
	}, []string{"UNCHECKED_REQUIREMENT"}},
	"check created by no task": {func(tr *Trace) {
		tr.Registry.Checks[0].Creator = "T009"
	}, []string{"UNKNOWN_CREATOR"}},
	"verify runs an unregistered target": {func(tr *Trace) {
		task(tr, "T001").Verify = "`task verify:pins; task nonexistent`: a missing tool is rejected."
	}, []string{"UNKNOWN_CHECK"}},
	"creator never runs its check": {func(tr *Trace) {
		task(tr, "T001").Verify = "`go -C tools test ./internal -count=1`: a missing tool is rejected."
	}, []string{"CHECK_NOT_VERIFIED"}},
	"verify runs no check": {func(tr *Trace) {
		task(tr, "T004").Verify = "`true`: nothing runs."
	}, []string{"NO_APPLICABLE_CHECK"}},
	"verify runs a check of other requirements": {func(tr *Trace) {
		task(tr, "T002").Verify = "`task test:trace; task verify:pins`: blanket lists fail."
	}, []string{"UNRELATED_CHECK"}},
	"printed target is not a run": {func(tr *Trace) {
		task(tr, "T004").Verify = "`echo task check`: prints only."
	}, []string{"NO_APPLICABLE_CHECK"}},
	"evaluator over checks of other requirements": {func(tr *Trace) {
		tr.Tasks = append(tr.Tasks, Task{ID: "T005", Paths: []string{"tools/x.go"}, Requirements: []string{"FR-001"}, ADRs: []string{"0011"},
			Verify: "`task check -- AGENTS.md`", Evidence: "`x.json`"})
	}, []string{"NO_APPLICABLE_CHECK"}},
	"evaluator over checks of the task's requirements": {func(tr *Trace) {
		tr.Tasks = append(tr.Tasks, Task{ID: "T005", Paths: []string{"tools/x.go"}, Requirements: []string{"FR-001"}, ADRs: []string{"0011"},
			Verify: "`task check -- tools/internal`", Evidence: "`x.json`"})
	}, nil},
	"go test of the parent package only": {func(tr *Trace) {
		task(tr, "T004").Paths = []string{"harness/sub/checks.yaml"}
		task(tr, "T004").Verify = "`go test ./harness -count=1`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"go test of a package tree": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./... -count=1`"
	}, nil},
	"commands inside quotes are printed, not run": {func(tr *Trace) {
		task(tr, "T004").Verify = "`echo 'x; task check; x'`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"double-quoted commands are printed, not run": {func(tr *Trace) {
		task(tr, "T004").Verify = "`echo \"x && task check\"`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"unbalanced quote": {func(tr *Trace) {
		task(tr, "T004").Verify = "`echo 'x; task check`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"valid-looking command before an unbalanced quote": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check '`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"expansion inside double quotes": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness -run \"$PATTERN\"`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"comment hides the following commands": {func(tr *Trace) {
		task(tr, "T004").Verify = "`echo ignored # ; task check`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"unsupported syntax anywhere voids the span": {func(tr *Trace) {
		task(tr, "T004").Verify = "`echo $(x); task check`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"directory path is not in its parent package": {func(tr *Trace) {
		task(tr, "T004").Paths = []string{"harness/sub/"}
		task(tr, "T004").Verify = "`go test ./harness -count=1`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"extensionless path is not in its parent package": {func(tr *Trace) {
		task(tr, "T004").Paths = []string{"harness/sub"}
		task(tr, "T004").Verify = "`go test ./harness -count=1`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"dotted directory is not in its parent package": {func(tr *Trace) {
		task(tr, "T004").Paths = []string{"harness/sub.v1/"}
		task(tr, "T004").Verify = "`go test ./harness -count=1`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"dotted directory selected exactly": {func(tr *Trace) {
		task(tr, "T004").Paths = []string{"harness/sub.v1/"}
		task(tr, "T004").Verify = "`go test ./harness/sub.v1 -count=1`"
	}, nil},
	"double semicolon": {func(tr *Trace) {
		task(tr, "T004").Verify = "`echo ignored ;; task check`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"leading operator": {func(tr *Trace) {
		task(tr, "T004").Verify = "`&& task check`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"leading semicolon": {func(tr *Trace) {
		task(tr, "T004").Verify = "`; task check`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"dangling operator": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check &&`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"trailing semicolon is valid": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check;`"
	}, nil},
	"directory path selected exactly": {func(tr *Trace) {
		task(tr, "T004").Paths = []string{"harness/sub/"}
		task(tr, "T004").Verify = "`go test ./harness/sub -count=1`"
	}, nil},
	"command after &&": {func(tr *Trace) {
		task(tr, "T004").Verify = "`true && task check`"
	}, nil},
	"command after ||": {func(tr *Trace) {
		task(tr, "T004").Verify = "`false || task check`"
	}, nil},
	"substitution": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check -- $(pwd)`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"redirection": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness > /dev/null`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"background operator": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check & true`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"zero test repetitions": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness -count=0`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"non-numeric repetitions": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness -count x`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"empty run pattern": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness -run=`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"evaluator name printed by another command": {func(tr *Trace) {
		task(tr, "T004").Verify = "`echo check`: prints only."
	}, []string{"NO_APPLICABLE_CHECK"}},
	"task flag instead of a target": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task --list-all`: lists only."
	}, []string{"NO_APPLICABLE_CHECK"}},
	"tofu command that is not a test": {func(tr *Trace) {
		task(tr, "T004").Verify = "`mise exec -- tofu -chdir=harness plan`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"go test flag without its value": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness -run`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"dry run is not a run": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check --dry`: prints only."
	}, []string{"NO_APPLICABLE_CHECK"}},
	"piped target is not a plain run": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check | true`: masks the exit status."
	}, []string{"NO_APPLICABLE_CHECK"}},
	"go test of another package": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go -C tools test ./internal/other -count=1`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"go test that only lists": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness -list .`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"go test that runs nothing": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness -run '^$'`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"go list is not a test": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go list ./harness`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"go test of the task's own package": {func(tr *Trace) {
		task(tr, "T004").Verify = "`go test ./harness -run \"TestA|TestB\" -count=1 -v`"
	}, nil},
	"tofu test of the task's own module": {func(tr *Trace) {
		task(tr, "T004").Verify = "`mise exec -- tofu -chdir=harness test`"
	}, nil},
	"target through the offline entry": {func(tr *Trace) {
		task(tr, "T001").Verify = "`<approved-absolute-path>/lz-offline --candidate <checkout> -- task verify:pins`"
	}, nil},
	"target with an argument": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check -- tools/internal`"
	}, nil},
	"evaluator over a path no check covers": {func(tr *Trace) {
		task(tr, "T004").Verify = "`task check -- modules/naming`"
	}, []string{"NO_APPLICABLE_CHECK"}},
	"registered procedure": {func(tr *Trace) {
		tr.Registry.Procedures = map[string]string{"T004": "approved external procedure"}
		task(tr, "T004").Verify = "Procedure: run `go list -m` and compare."
	}, nil},
	"procedure for an unknown task": {func(tr *Trace) {
		tr.Registry.Procedures = map[string]string{"T009": "approved external procedure"}
	}, []string{"UNKNOWN_PROCEDURE"}},
	"docs check created by a behavioural task": {func(tr *Trace) {
		tr.Registry.Checks[2].Creator = "T002"
	}, []string{"INVALID_EXEMPTION"}},
	"task without paths": {func(tr *Trace) {
		task(tr, "T001").Paths = nil
	}, []string{"MISSING_PATHS"}},
	"verify without a command": {func(tr *Trace) {
		task(tr, "T002").Verify = "run the trace checks"
	}, []string{"CHECK_NOT_VERIFIED", "MISSING_VERIFY"}},
	"empty verify": {func(tr *Trace) {
		task(tr, "T004").Verify = ""
	}, []string{"MISSING_VERIFY"}},
	"missing evidence": {func(tr *Trace) {
		task(tr, "T001").Evidence = ""
	}, []string{"MISSING_EVIDENCE"}},
	"evidence names no path": {func(tr *Trace) {
		task(tr, "T002").Evidence = "none"
	}, []string{"MISSING_EVIDENCE"}},
	"evidence starts with prose that has slashes": {func(tr *Trace) {
		task(tr, "T002").Evidence = "none; 96/96 mutants killed for modules/naming."
	}, []string{"MISSING_EVIDENCE"}},
	"evidence names a directory": {func(tr *Trace) {
		task(tr, "T002").Evidence = "`.local/evidence/001/`; pending."
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

// The exemption is judged on every path the title names, whatever its
// extension, from the parsed text rather than a hand-built Task.
func TestTraceabilityParsedExemption(t *testing.T) {
	for _, extra := range []string{"tools/guide.js", "tools/bin/guide", "guide.py", "main.c", "build.gradle", "guide.typescript"} {
		text := strings.Replace(tasksText, "harness/guides/{checks,naming}.md\n", "harness/guides/{checks,naming}.md and "+extra+"\n", 1)
		tasks, err := ParseTasks(text)
		if err != nil {
			t.Fatal(err)
		}
		tr := validTrace()
		tr.Tasks = tasks
		if got := ruleSet(CheckTrace(tr)); !reflect.DeepEqual(got, []string{"INVALID_EXEMPTION"}) {
			t.Errorf("BEHAVIORAL_RED: docs-only task naming %s: rules %v", extra, got)
		}
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
  "evaluators": ["check"],
  "producers": [],
  "procedures": {},
  "checks": [{"id": "verify:pins", "requirements": ["FR-001"], "command": "task verify:pins -- tools", "creator": "T001",
              "kind": "behavioral", "scope": ["tools"], "inputs": ["Taskfile.yml"], "review": true}]}
`
	reg, err := ParseRegistry([]byte(valid))
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: valid registry refused: %v", err)
	}
	want := Registry{
		Requirements: map[string]Requirement{"FR-001": {ADRs: []string{"0011"}}},
		Checks: []CheckDefinition{{ID: "verify:pins", Requirements: []string{"FR-001"}, Command: "task verify:pins -- tools", Creator: "T001",
			Kind: KindBehavioral, Scope: []string{"tools"}, Inputs: []string{"Taskfile.yml"}, Review: true}},
		Evaluators: []string{"check"},
		Procedures: map[string]string{},
		Producers:  []string{},
	}
	if !reflect.DeepEqual(reg, want) {
		t.Errorf("BEHAVIORAL_RED: registry\n got %#v\nwant %#v", reg, want)
	}
	for name, text := range map[string]string{
		"unknown field":            strings.Replace(valid, `"review": true`, `"review": true, "owner": "x"`, 1),
		"duplicate key":            strings.Replace(valid, `"kind": "behavioral"`, `"kind": "behavioral", "kind": "docs"`, 1),
		"case-variant duplicate":   strings.Replace(valid, `"kind": "behavioral"`, `"kind": "behavioral", "KIND": "docs"`, 1),
		"case-variant field":       strings.Replace(valid, `"kind": "behavioral"`, `"Kind": "behavioral"`, 1),
		"null field":               strings.Replace(valid, `"review": true`, `"review": null`, 1),
		"missing required field":   strings.Replace(valid, `"creator": "T001",`, ``, 1),
		"missing evaluators":       strings.Replace(valid, `"evaluators": ["check"],`, ``, 1),
		"procedure without reason": strings.Replace(valid, `"procedures": {}`, `"procedures": {"T001": " "}`, 1),
		"wrong value type":         strings.Replace(valid, `"schema_version": 1`, `"schema_version": "1"`, 1),
		"duplicate requirement":    strings.Replace(valid, `{"FR-001": {"adrs": ["0011"]}}`, `{"FR-001": {"adrs": ["0011"]}, "FR-001": {"adrs": []}}`, 1),
		"duplicate check id":       strings.Replace(valid, `"review": true}]`, `"review": true}, {"id": "verify:pins", "requirements": ["FR-001"], "command": "c", "creator": "T001", "kind": "docs", "scope": ["AGENTS.md"]}]`, 1),
		"command runs another":     strings.Replace(valid, `"command": "task verify:pins -- tools"`, `"command": "task verify:other"`, 1),
		"command extends the id":   strings.Replace(valid, `"command": "task verify:pins -- tools"`, `"command": "task verify:pinsx"`, 1),
		"unknown kind":             strings.Replace(valid, `"behavioral"`, `"manual"`, 1),
		"wrong schema version":     strings.Replace(valid, `"schema_version": 1`, `"schema_version": 2`, 1),
		"trailing document":        valid + "{}\n",
		"no checks":                `{"schema_version": 1, "requirements": {}, "evaluators": [], "producers": [], "procedures": {}, "checks": []}`,
		"check without scope":      strings.Replace(valid, `"scope": ["tools"]`, `"scope": []`, 1),
		"absolute scope":           strings.Replace(valid, `"scope": ["tools"]`, `"scope": ["/etc"]`, 1),
		"escaping scope":           strings.Replace(valid, `"scope": ["tools"]`, `"scope": ["tools/../.."]`, 1),
		"parent scope":             strings.Replace(valid, `"scope": ["tools"]`, `"scope": ["../x"]`, 1),
		"parent input":             strings.Replace(valid, `"inputs": ["Taskfile.yml"]`, `"inputs": ["../Taskfile.yml"]`, 1),
		"not JSON-compatible":      "checks:\n  - id: V001\n",
	} {
		if _, err := ParseRegistry([]byte(text)); err == nil {
			t.Errorf("BEHAVIORAL_RED: %s accepted", name)
		}
	}
}

// Evidence decoding refuses anything a missing, null or case-variant field
// could turn into a pass.
func TestDoDEvidenceStrict(t *testing.T) {
	valid := `{"check": "N1", "status": "pass", "input_digest": "sha256:x", "discovered": 1, "failed": 0, "producer": "lz-offline"}`
	var e Evidence
	if err := DecodeStrict([]byte(valid), &e); err != nil || e.Discovered != 1 || e.Producer != "lz-offline" {
		t.Fatalf("BEHAVIORAL_RED: valid evidence refused: %v %+v", err, e)
	}
	for name, text := range map[string]string{
		"missing failed":       strings.Replace(valid, `, "failed": 0`, ``, 1),
		"null failed":          strings.Replace(valid, `"failed": 0`, `"failed": null`, 1),
		"case-variant failed":  strings.Replace(valid, `"failed": 0`, `"failed": 1, "FAILED": 0`, 1),
		"fractional count":     strings.Replace(valid, `"discovered": 1`, `"discovered": 1.5`, 1),
		"missing producer":     strings.Replace(valid, `, "producer": "lz-offline"`, ``, 1),
		"null reviewer":        strings.Replace(valid, `"producer"`, `"reviewer": null, "producer"`, 1),
		"array instead of obj": `[` + valid + `]`,
	} {
		var e Evidence
		if err := DecodeStrict([]byte(text), &e); err == nil {
			t.Errorf("BEHAVIORAL_RED: %s accepted: %+v", name, e)
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
		"Taskfile.yml":                          "version: '3'\n",
		"modules/naming/main.tf":                "locals {}\n",
		"modules/naming/tests/names.tftest.hcl": "run \"x\" {}\n",
		"modules/naming/README.md":              "# naming\n",
		"tools/other/x.go":                      "package other\n",
	})
}

func dodRegistry() Registry {
	return Registry{
		Checks: []CheckDefinition{
			{ID: "N1", Requirements: []string{"FR-001"}, Command: "task test:naming", Creator: "T001", Kind: KindBehavioral, Scope: []string{"modules/naming"}, Inputs: []string{"Taskfile.yml"}},
			{ID: "N2", Requirements: []string{"FR-001"}, Command: "task snap:check", Creator: "T001", Kind: KindBehavioral, Scope: []string{"modules/naming/tests"}, Review: true},
			{ID: "N3", Requirements: []string{"FR-003"}, Command: "content review", Creator: "T003", Kind: KindDocs, Scope: []string{"modules/naming/README.md"}},
			{ID: "O1", Requirements: []string{"FR-002"}, Command: "task test:other", Creator: "T002", Kind: KindBehavioral, Scope: []string{"tools/other"}},
		},
		Producers: []string{"lz-offline"},
	}
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

func checkDigest(t *testing.T, root string, c CheckDefinition) string {
	t.Helper()
	d, err := CheckDigest(root, c)
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: check digest of %s: %v", c.ID, err)
	}
	if !strings.HasPrefix(d, "sha256:") || len(d) != len("sha256:")+64 {
		t.Fatalf("BEHAVIORAL_RED: check digest %q is not a sha256", d)
	}
	return d
}

func freshEvidence(t *testing.T, root string) map[string]Evidence {
	checks := dodRegistry().Checks
	return map[string]Evidence{
		"N1": {Check: "N1", Status: StatusPass, InputDigest: checkDigest(t, root, checks[0]), Discovered: 3, Producer: "lz-offline"},
		"N2": {Check: "N2", Status: StatusPass, InputDigest: checkDigest(t, root, checks[1]), Discovered: 1, Producer: "lz-offline", Reviewer: "independent"},
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
		".":                      "N1 N2 N3 O1",
		"Taskfile.yml":           "",
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

// Each control changes one fact; only an observed, fresh, consistent and
// attested pass counts.
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
		"stale after an execution input changes": {func(t *testing.T, root string, e map[string]Evidence) {
			if err := os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte("version: '3'\ntasks: {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "N1", "not-run STALE_EVIDENCE"},
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
		"unattested producer": {func(t *testing.T, root string, e map[string]Evidence) {
			n := e["N1"]
			n.Producer = "written-by-hand"
			e["N1"] = n
		}, "N1", "review-required UNATTESTED_EVIDENCE"},
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
		"symlink in scope": {func(t *testing.T, root string, e map[string]Evidence) {
			if err := os.Symlink("/etc/passwd", filepath.Join(root, "modules/naming/tests/link.hcl")); err != nil {
				t.Fatal(err)
			}
		}, "N2", "fail SCOPE_UNREADABLE"},
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

// A docs exemption holds only while every file in its scope is Markdown.
func TestDoDDocsExemption(t *testing.T) {
	root := dodTree(t)
	reg := Registry{Checks: []CheckDefinition{
		{ID: "D1", Command: "review", Creator: "T003", Kind: KindDocs, Scope: []string{"modules/naming/README.md"}},
		{ID: "D2", Command: "review", Creator: "T003", Kind: KindDocs, Scope: []string{"modules/naming"}},
		{ID: "D3", Command: "review", Creator: "T003", Kind: KindDocs, Scope: []string{"docs/absent"}},
	}}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "Taskfile.md")); err != nil {
		t.Fatal(err)
	}
	reg.Checks = append(reg.Checks, CheckDefinition{ID: "D4", Command: "review", Creator: "T003", Kind: KindDocs, Scope: []string{"Taskfile.md"}})
	want := map[string]string{"D1": "exempt ", "D2": "fail INVALID_EXEMPTION", "D3": "not-run SCOPE_MISSING", "D4": "fail SCOPE_UNREADABLE"}
	if got := statuses(EvaluateDoD(reg, root, ".", nil)); !reflect.DeepEqual(got, want) {
		t.Errorf("BEHAVIORAL_RED: docs exemptions %v, want %v", got, want)
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
	n1 := dodRegistry().Checks[0]
	before := checkDigest(t, root, n1)
	changed := n1
	changed.Command = "task test:naming -- other"
	if checkDigest(t, root, changed) == before {
		t.Error("BEHAVIORAL_RED: a changed check definition kept its digest")
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
	if err := os.Symlink(filepath.Join(root, "modules/naming"), filepath.Join(root, "modules/alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := Digest(root, []string{"modules/naming/other.tf"}); err != nil {
		t.Fatalf("control: the renamed file is not hashable: %v", err)
	}
	if _, err := Digest(root, []string{"modules/alias/other.tf"}); err == nil {
		t.Error("BEHAVIORAL_RED: a scope reached through a symlink was hashed")
	}
	if _, err := Digest(root, []string{"modules/absent"}); err == nil {
		t.Error("BEHAVIORAL_RED: a missing scope was hashed")
	}
}
