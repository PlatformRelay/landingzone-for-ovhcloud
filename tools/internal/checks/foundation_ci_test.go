package checks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// foundationRegistry has one check of each kind the source must account for:
// argument-free checks of a closed task and of T023, a T023 check that
// judges the run afterwards, a check that takes an argument and one of an
// open task.
func foundationRegistry() (Registry, map[string][]Task) {
	check := func(id, creator, command string) CheckDefinition {
		return CheckDefinition{ID: id, Creator: creator, Command: command, Kind: KindBehavioral}
	}
	return Registry{Checks: []CheckDefinition{
		check("test:a", "T001", "task test:a"),
		check("test:ci", "T023", "task test:ci"),
		check("ci:foundation", "T023", "task ci:foundation"),
		check("lint", "T001", "task lint -- modules/x"),
		check("test:later", "T050", "task test:later"),
	}}, map[string][]Task{"001": {
		{ID: "T001", Done: true}, {ID: "T023"}, {ID: "T050"},
	}}
}

func foundationSource() map[string]any {
	return map[string]any{
		"runner":          "ubuntu-24.04",
		"timeout_minutes": 10,
		"manifest":        "sha256:" + strings.Repeat("a", 64),
		"layer":           "sha256:" + strings.Repeat("b", 64),
		"entry_sha256":    strings.Repeat("c", 64),
		"bwrap_sha256":    strings.Repeat("d", 64),
		"targets":         []any{"test:a", "test:ci"},
		"deferred":        map[string]any{"ci:foundation": "judges this workflow's own run after it completes"},
	}
}

func encode(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func renderSource(t *testing.T, source []byte) []byte {
	t.Helper()
	parsed, err := ParseFoundationSource(source)
	if err != nil {
		t.Fatalf("valid source refused: %v", err)
	}
	return []byte(RenderFoundationWorkflow(parsed))
}

func TestFoundationCIValid(t *testing.T) {
	registry, tasks := foundationRegistry()
	source := encode(t, foundationSource())
	if findings := CheckFoundationCI(source, renderSource(t, source), registry, tasks); len(findings) != 0 {
		t.Errorf("BEHAVIORAL_RED: valid source and workflow refused: %+v", findings)
	}
}

// The rendered workflow is the only one accepted, so its properties are the
// CI's properties: no Action, no token permission, no secret, main-only push
// and pull-request triggers, the exact PR head, a bounded hosted job, the
// pinned runtime, and every target run through the entry.
func TestFoundationWorkflowShape(t *testing.T) {
	workflow := string(renderSource(t, encode(t, foundationSource())))
	for _, want := range []string{
		"\non:\n  pull_request:\n    branches: [main]\n  push:\n    branches: [main]\npermissions: {}\n",
		"\n    runs-on: ubuntu-24.04\n    timeout-minutes: 10\n",
		"\n      LZ_HEAD: ${{ github.event.pull_request.head.sha || github.sha }}\n",
		"\n      LZ_LAYER: sha256:" + strings.Repeat("b", 64) + "\n",
		"\n      LZ_ENTRY_SHA256: " + strings.Repeat("c", 64) + "\n",
		"\n      LZ_BWRAP_SHA256: " + strings.Repeat("d", 64) + "\n",
		"\n      LZ_TARGETS: test:a test:ci\n",
		`test "$(git -C "$LZ_CANDIDATE" rev-parse HEAD)" = "$LZ_HEAD"`,
		`echo "LZ_CANDIDATE=$RUNNER_TEMP/candidate" >> "$GITHUB_ENV"`,
		`echo "LZ_RUNTIME=$RUNNER_TEMP/runtime" >> "$GITHUB_ENV"`,
		`echo "${LZ_LAYER#sha256:}  $RUNNER_TEMP/layer.tgz" | sha256sum -c -`,
		"\n      LZ_MANIFEST: sha256:" + strings.Repeat("a", 64) + "\n",
		`echo "${LZ_MANIFEST#sha256:}  $RUNNER_TEMP/manifest.json" | sha256sum -c -`,
		`test "$(jq -r '.layers | length' "$RUNNER_TEMP/manifest.json")" = 1`,
		`test "$(jq -r '.layers[0].digest' "$RUNNER_TEMP/manifest.json")" = "$LZ_LAYER"`,
		`echo "$LZ_ENTRY_SHA256  $LZ_RUNTIME/runtime/lz-offline" | sha256sum -c -`,
		`echo "$LZ_BWRAP_SHA256  $LZ_RUNTIME/host/bwrap" | sha256sum -c -`,
		`"$LZ_RUNTIME/runtime/lz-offline" --candidate "$LZ_CANDIDATE" -- task "$target"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("BEHAVIORAL_RED: rendered workflow lacks %q", want)
		}
	}
	for _, banned := range []string{"uses:", "secrets", "github.token", "pull_request_target", "workflow_run", "continue-on-error", "if:", "self-hosted", "GITHUB_WORKSPACE"} {
		if strings.Contains(workflow, banned) {
			t.Errorf("BEHAVIORAL_RED: rendered workflow contains %q", banned)
		}
	}
	// Job-level env may not use the runner context, so the head is the only
	// expression; the work directories come from $RUNNER_TEMP at run time.
	allowed := []string{"${{ github.event.pull_request.head.sha || github.sha }}"}
	for _, expression := range regexp.MustCompile(`\$\{\{[^}]*\}\}`).FindAllString(workflow, -1) {
		if !slices.Contains(allowed, expression) {
			t.Errorf("BEHAVIORAL_RED: unexpected expression %s", expression)
		}
	}
	// The candidate path appears only where git fetches it as data and where
	// the entry receives it.
	for _, line := range strings.Split(workflow, "\n") {
		if strings.Contains(line, "$LZ_CANDIDATE") && !strings.Contains(line, `git -C "$LZ_CANDIDATE"`) &&
			!strings.Contains(line, `git init -q "$LZ_CANDIDATE"`) && !strings.Contains(line, `--candidate "$LZ_CANDIDATE"`) &&
			!strings.Contains(line, `-C "$LZ_CANDIDATE" checkout`) && !strings.Contains(line, `echo "LZ_CANDIDATE=`) {
			t.Errorf("BEHAVIORAL_RED: candidate used on the host: %s", line)
		}
	}
}

// Every change to the committed workflow is drift from the rendered one,
// whichever authority it widens.
func TestFoundationWorkflowDrift(t *testing.T) {
	registry, tasks := foundationRegistry()
	source := encode(t, foundationSource())
	workflow := string(renderSource(t, source))
	for name, change := range map[string][2]string{
		"unpinned action":    {"    steps:\n", "    steps:\n      - uses: actions/checkout@v4\n"},
		"pinned action":      {"    steps:\n", "    steps:\n      - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955\n"},
		"widened permission": {"permissions: {}", "permissions: write-all"},
		"widened trigger":    {"  push:\n", "  pull_request_target:\n  push:\n"},
		"any branch":         {"  push:\n    branches: [main]", "  push:\n    branches: ['**']"},
		"secret":             {"      LZ_HEAD:", "      TOKEN: ${{ secrets.GITHUB_TOKEN }}\n      LZ_HEAD:"},
		"stale head":         {"${{ github.event.pull_request.head.sha || github.sha }}", "${{ github.sha }}"},
		"host execution":     {"    steps:\n", "    steps:\n      - run: bash \"$LZ_CANDIDATE/build.sh\"\n"},
		"skipped check":      {"LZ_TARGETS: test:a test:ci", "LZ_TARGETS: test:a"},
		"tolerated failure":  {"    timeout-minutes: 10\n", "    timeout-minutes: 10\n    continue-on-error: true\n"},
		"longer timeout":     {"timeout-minutes: 10", "timeout-minutes: 60"},
		"self-hosted runner": {"runs-on: ubuntu-24.04", "runs-on: self-hosted"},
		"other runtime":      {"LZ_LAYER: sha256:" + strings.Repeat("b", 64), "LZ_LAYER: sha256:" + strings.Repeat("e", 64)},
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(workflow, change[0]) {
				t.Fatalf("fixture lacks %q", change[0])
			}
			changed := strings.Replace(workflow, change[0], change[1], 1)
			if got := ruleSet(CheckFoundationCI(source, []byte(changed), registry, tasks)); !reflect.DeepEqual(got, []string{"WORKFLOW_DRIFT"}) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v", name, got)
			}
		})
	}
}

// The source pins a hosted runner, a bounded timeout and well-formed digests,
// and accounts for exactly the argument-free checks of closed tasks and T023:
// each runs or is deferred with a reason.
func TestFoundationSourceRejected(t *testing.T) {
	registry, tasks := foundationRegistry()
	for name, c := range map[string]struct {
		change func(map[string]any)
		want   []string
	}{
		"deferral of an implemented check": {func(s map[string]any) {
			s["targets"] = []any{"test:ci"}
			s["deferred"] = map[string]any{"ci:foundation": "afterwards", "test:a": "later"}
		}, []string{"CHECK_DEFERRED"}},
		"target name that is not a plain Task target": {func(s map[string]any) {
			s["targets"] = []any{"test:a", "test:ci", "Test A"}
		}, []string{"CHECK_NAME", "CHECK_UNKNOWN"}},
		"self-hosted runner":        {func(s map[string]any) { s["runner"] = "self-hosted" }, []string{"SOURCE_PIN"}},
		"timeout above budget":      {func(s map[string]any) { s["timeout_minutes"] = 11 }, []string{"SOURCE_PIN"}},
		"no timeout":                {func(s map[string]any) { s["timeout_minutes"] = 0 }, []string{"SOURCE_PIN"}},
		"layer not a digest":        {func(s map[string]any) { s["layer"] = "latest" }, []string{"SOURCE_PIN"}},
		"manifest not a digest":     {func(s map[string]any) { s["manifest"] = "sha256:" + strings.Repeat("A", 64) }, []string{"SOURCE_PIN"}},
		"short entry digest":        {func(s map[string]any) { s["entry_sha256"] = strings.Repeat("c", 63) }, []string{"SOURCE_PIN"}},
		"launcher digest prefixed":  {func(s map[string]any) { s["bwrap_sha256"] = "sha256:" + strings.Repeat("d", 64) }, []string{"SOURCE_PIN"}},
		"omitted check":             {func(s map[string]any) { s["targets"] = []any{"test:a"} }, []string{"CHECK_OMITTED"}},
		"no targets":                {func(s map[string]any) { s["targets"] = []any{} }, []string{"CHECK_OMITTED", "NO_TARGETS"}},
		"check of an open task":     {func(s map[string]any) { s["targets"] = []any{"test:a", "test:ci", "test:later"} }, []string{"CHECK_NOT_RUNNABLE"}},
		"check with an argument":    {func(s map[string]any) { s["targets"] = []any{"test:a", "test:ci", "lint"} }, []string{"CHECK_NOT_RUNNABLE"}},
		"unknown check":             {func(s map[string]any) { s["targets"] = []any{"test:a", "test:ci", "test:ghost"} }, []string{"CHECK_UNKNOWN"}},
		"repeated check":            {func(s map[string]any) { s["targets"] = []any{"test:a", "test:ci", "test:a"} }, []string{"CHECK_DUPLICATE"}},
		"deferral without a reason": {func(s map[string]any) { s["deferred"] = map[string]any{"ci:foundation": " "} }, []string{"CHECK_DEFERRED"}},
		"deferred and run": {func(s map[string]any) {
			s["targets"] = []any{"test:a", "test:ci", "ci:foundation"}
		}, []string{"CHECK_DEFERRED"}},
		"unknown deferral": {func(s map[string]any) {
			s["deferred"] = map[string]any{"ci:foundation": "afterwards", "test:ghost": "absent"}
		}, []string{"CHECK_UNKNOWN"}},
		"unknown field":   {func(s map[string]any) { s["permissions"] = "write-all" }, []string{"SOURCE_SYNTAX"}},
		"missing field":   {func(s map[string]any) { delete(s, "layer") }, []string{"SOURCE_SYNTAX"}},
		"null targets":    {func(s map[string]any) { s["targets"] = nil }, []string{"SOURCE_SYNTAX"}},
		"targets as text": {func(s map[string]any) { s["targets"] = "test:a test:ci" }, []string{"SOURCE_SYNTAX"}},
	} {
		t.Run(name, func(t *testing.T) {
			value := foundationSource()
			c.change(value)
			source := encode(t, value)
			// The workflow is rendered from the changed source where it
			// parses, so only the source rule under test can fire.
			workflow := []byte("")
			if parsed, err := ParseFoundationSource(source); err == nil {
				workflow = []byte(RenderFoundationWorkflow(parsed))
			}
			if got := ruleSet(CheckFoundationCI(source, workflow, registry, tasks)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v, want %v", name, got, c.want)
			}
		})
	}
}

// A spec-scoped creator resolves against its own spec's tasks by bare id: a
// closed one admits its check as a target, without requiring it. An open one,
// an unread spec, an id the spec lacks, a bare id only another spec closes and
// a scoped id only spec 001 closes are still refused.
func TestFoundationSpecScopedCreators(t *testing.T) {
	registry, tasks := foundationRegistry()
	check := func(id, creator string) CheckDefinition {
		return CheckDefinition{ID: id, Creator: creator, Command: "task " + id, Kind: KindBehavioral}
	}
	registry.Checks = append(registry.Checks,
		check("test:live", "005/T055"),
		check("test:open", "005/T056"),
		check("test:unread", "007/T055"),
		check("test:missing", "005/T099"),
		check("test:bare", "T055"),
		check("test:shadow", "005/T001"),
		check("test:own", "001/T001"),
	)
	tasks["005"] = []Task{{ID: "T055", Done: true}, {ID: "T056"}, {ID: "T001"}}
	run := func(extra ...string) []string {
		targets := []any{"test:a", "test:ci"}
		for _, id := range extra {
			targets = append(targets, id)
		}
		value := foundationSource()
		value["targets"] = targets
		source := encode(t, value)
		return ruleSet(CheckFoundationCI(source, renderSource(t, source), registry, tasks))
	}
	if got := run("test:live"); len(got) != 0 {
		t.Errorf("BEHAVIORAL_RED: check of closed 005/T055 gave %v, want accepted", got)
	}
	if got := run(); len(got) != 0 {
		t.Errorf("BEHAVIORAL_RED: check of closed 005/T055 made required: %v", got)
	}
	for _, id := range []string{"test:open", "test:unread", "test:missing", "test:bare", "test:shadow", "test:own"} {
		if got := run(id); !reflect.DeepEqual(got, []string{"CHECK_NOT_RUNNABLE"}) {
			t.Errorf("BEHAVIORAL_RED: %s gave %v, want [CHECK_NOT_RUNNABLE]", id, got)
		}
	}
}

// A repeated key is refused rather than resolved to its last value.
func TestFoundationSourceDuplicateKey(t *testing.T) {
	registry, tasks := foundationRegistry()
	source := encode(t, foundationSource())
	duplicated := []byte(strings.Replace(string(source), `"runner":"ubuntu-24.04"`, `"runner":"ubuntu-24.04","runner":"self-hosted"`, 1))
	if got := ruleSet(CheckFoundationCI(duplicated, renderSource(t, source), registry, tasks)); !reflect.DeepEqual(got, []string{"SOURCE_SYNTAX"}) {
		t.Errorf("BEHAVIORAL_RED: duplicate key gave %v", got)
	}
}

// A registered check whose ID would inject YAML or an expression into the
// rendered workflow is refused by name before it reaches the workflow, even
// though the registry knows it and its creator is closed.
func TestFoundationSourceRefusesInjectedNames(t *testing.T) {
	for name, id := range map[string]string{
		"newline":    "test:x\n      BASH_ENV: /home/runner/work/_temp/candidate/probe.sh",
		"expression": "test:${{ secrets.GITHUB_TOKEN }}",
		"space":      "test:x --dry",
	} {
		t.Run(name, func(t *testing.T) {
			registry, tasks := foundationRegistry()
			registry.Checks = append(registry.Checks, CheckDefinition{ID: id, Creator: "T001", Command: "task " + id, Kind: KindBehavioral})
			value := foundationSource()
			value["targets"] = []any{"test:a", "test:ci", id}
			source := encode(t, value)
			parsed, err := ParseFoundationSource(source)
			if err != nil {
				t.Fatal(err)
			}
			if got := ruleSet(CheckFoundationCI(source, []byte(RenderFoundationWorkflow(parsed)), registry, tasks)); !reflect.DeepEqual(got, []string{"CHECK_NAME"}) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v", name, got)
			}
			if findings := ValidateFoundationSource(parsed); len(findings) == 0 {
				t.Errorf("BEHAVIORAL_RED: %s validates for rendering", name)
			}
		})
	}
}

// The captured runs are GitHub's own output for the PR #20 head run
// 37278768943 and the red-control run 37279007980 (closed draft PR #21),
// captured read-only with gh; source.json is the approved source at both
// heads. See specs/001-offline-foundation/evidence/T023.md.
const (
	runsDir = "testdata/foundation-runs"
	prHead  = "20b408842ae4d3a09b89d0639f61a6dd218cbc48"
	redHead = "da612461f1dbc20a422043e4171e141ab0c62d33"
)

func readRunFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{runsDir}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func captureRun(t *testing.T, name string) RunCapture {
	t.Helper()
	return RunCapture{
		Run: readRunFile(t, name, "run.json"), Jobs: readRunFile(t, name, "jobs.json"),
		WorkflowBlob: readRunFile(t, name, "workflow.sha"), SourceBlob: readRunFile(t, name, "source.sha"),
		Log: readRunFile(t, name, "run.log"), Digests: treeDigests(),
	}
}

// treeDigests stands for FoundationDigests of the runs' head tree, which the
// CLI computes from a checkout (TestFoundationRunCommands); here the judged
// tree is that same tree unless a case changes it.
func treeDigests() map[string]string {
	digests := map[string]string{}
	for i, id := range foundationTargets {
		digests[id] = fmt.Sprintf("sha256:%064x", i+1)
	}
	return digests
}

func observeRun(t *testing.T, role, candidate, name string) FoundationRun {
	t.Helper()
	run, err := ObserveFoundationRun(role, candidate, captureRun(t, name))
	if err != nil {
		t.Fatalf("captured run %s refused: %v", name, err)
	}
	return run
}

// recordedRuns is the approved source and the record of the green PR head
// and the red control, observed afresh for every caller.
func recordedRuns(t *testing.T) ([]byte, FoundationRuns) {
	t.Helper()
	return readRunFile(t, "source.json"), FoundationRuns{Runs: []FoundationRun{
		observeRun(t, RoleHead, prHead, "head"),
		observeRun(t, RoleRedControl, redHead, "red"),
	}}
}

var foundationTargets = []string{"verify:toolchain", "test:offline-boundary", "test:reports", "test:traceability", "check:specs",
	"test:dependencies", "test:static", "test:runtime-image", "test:foundation-ci"}

// The observation is read from GitHub's output, not written by hand: the
// run, check run, heads, runner, token, executed identities and every
// target with what it discovered.
func TestFoundationObserve(t *testing.T) {
	head := observeRun(t, RoleHead, prHead, "head")
	for name, ok := range map[string]bool{
		"run":        head.RunID == 37278768943 && head.Attempt == 1 && head.CheckRunID == 111661662616,
		"repository": head.Repository == FoundationRepository && head.HeadRepository == FoundationRepository,
		"event":      head.Event == "pull_request",
		"head":       head.Candidate == prHead && head.HeadSHA == prHead && head.FetchedHead == prHead,
		"workflow":   head.WorkflowPath == FoundationWorkflow && head.WorkflowBlob == "de1f3442b25ea94a490680e958103ad13cb141ea",
		"source":     head.SourceBlob == "ec8547ce87ddc7b1b07e3b47a99c9fec8057d226",
		"outcome":    head.Status == "completed" && head.Conclusion == "success" && head.JobConclusion == "success",
		"runner": slices.Equal(head.RunnerLabels, []string{"ubuntu-24.04"}) && head.RunnerGroup == "GitHub Actions" &&
			head.RunnerImage == "ubuntu-24.04",
		"token": slices.Equal(head.Permissions, []string{"Metadata: read"}),
		"steps": len(head.Steps) == 7 && head.Steps[5] == RunStep{Name: "Run the foundation checks inside the offline entry", Conclusion: "success"},
		"image": head.Env["LZ_MANIFEST"] == "sha256:9425742e0c3e99b0a5d4f798ff796e441de78901ec7f7a6e110c74b5110c2614" &&
			head.Env["LZ_HEAD"] == prHead && len(head.Env) == 9,
		"targets": head.TargetsDone == 9 && len(head.Targets) == 9,
	} {
		if !ok {
			t.Errorf("BEHAVIORAL_RED: head run observed wrongly: %s: %+v", name, head)
		}
	}
	// test:runtime-image also printed a package without test files, which
	// is not a package that ran no test.
	for i, target := range head.Targets {
		if target.Name != foundationTargets[i] || target.Status != "ok" || target.Discovered == 0 || target.Empty != 0 {
			t.Errorf("BEHAVIORAL_RED: head target %d observed as %+v", i, target)
		}
	}
	red := observeRun(t, RoleRedControl, redHead, "red")
	if red.Conclusion != "failure" || red.TargetsDone != -1 || len(red.Targets) != 4 || red.FetchedHead != redHead {
		t.Fatalf("BEHAVIORAL_RED: red control observed wrongly: %+v", red)
	}
	if last := red.Targets[3]; last.Name != "test:traceability" || last.Status != "failed" || last.Failures == 0 {
		t.Errorf("BEHAVIORAL_RED: failing target observed as %+v", last)
	}
}

// A capture that is not one run of one job, or whose blob is not a SHA, is
// refused rather than observed.
func TestFoundationObserveRefused(t *testing.T) {
	for name, change := range map[string]func(*RunCapture){
		"run not JSON":     func(c *RunCapture) { c.Run = []byte("{") },
		"two jobs":         func(c *RunCapture) { c.Jobs = []byte(`{"total_count": 2, "jobs": [{}, {}]}`) },
		"no log":           func(c *RunCapture) { c.Log = nil },
		"workflow not SHA": func(c *RunCapture) { c.WorkflowBlob = []byte("main\n") },
		"source not SHA":   func(c *RunCapture) { c.SourceBlob = []byte("") },
		"no head digests":  func(c *RunCapture) { c.Digests = nil },
		"job of another run": func(c *RunCapture) {
			c.Jobs = []byte(strings.Replace(string(c.Jobs), `"run_id":37278768943`, `"run_id":37279007980`, 1))
		},
		"job at another head": func(c *RunCapture) {
			c.Jobs = []byte(strings.Replace(string(c.Jobs), `"head_sha":"`+prHead+`"`, `"head_sha":"`+redHead+`"`, 1))
		},
		"job of another attempt": func(c *RunCapture) {
			c.Jobs = []byte(strings.Replace(string(c.Jobs), `"run_attempt":1`, `"run_attempt":2`, 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := captureRun(t, "head")
			change(&c)
			if _, err := ObserveFoundationRun(RoleHead, prHead, c); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s observed", name)
			}
		})
	}
}

func TestFoundationRunsValid(t *testing.T) {
	source, runs := recordedRuns(t)
	if findings := JudgeFoundationRuns(source, encode(t, runs), treeDigests()); len(findings) != 0 {
		t.Errorf("BEHAVIORAL_RED: the recorded runs refused: %+v", findings)
	}
}

// Each way a run can fail to prove the candidate is refused: a missing role,
// a foreign or unexpected workflow, a stale head or source, other executed
// identities, a run that did not complete green, and omitted, zero or
// skipped checks; a red control must be red in the checks themselves.
func TestFoundationRunsRejected(t *testing.T) {
	other := strings.Repeat("e", 40)
	head := func(r *FoundationRuns) *FoundationRun { return &r.Runs[0] }
	red := func(r *FoundationRuns) *FoundationRun { return &r.Runs[1] }
	for name, c := range map[string]struct {
		change func(*FoundationRuns)
		want   string
	}{
		"no head run":    {func(r *FoundationRuns) { r.Runs = r.Runs[1:] }, "RUN_MISSING"},
		"no red control": {func(r *FoundationRuns) { r.Runs = r.Runs[:1] }, "RUN_MISSING"},
		"unknown role": {func(r *FoundationRuns) {
			r.Runs = append(r.Runs, r.Runs[0])
			r.Runs[2].Role = "advisory"
			r.Runs[2].RunID = 1
		}, "RUN_ROLE"},
		"repeated run":          {func(r *FoundationRuns) { r.Runs = append(r.Runs, r.Runs[0]) }, "RUN_DUPLICATE"},
		"foreign workflow":      {func(r *FoundationRuns) { head(r).WorkflowPath = ".github/workflows/other.yml" }, "RUN_FOREIGN"},
		"foreign repository":    {func(r *FoundationRuns) { head(r).Repository = "someone/fork" }, "RUN_FOREIGN"},
		"fork head":             {func(r *FoundationRuns) { head(r).HeadRepository = "someone/fork" }, "RUN_FOREIGN"},
		"manual event":          {func(r *FoundationRuns) { head(r).Event = "workflow_dispatch" }, "RUN_FOREIGN"},
		"privileged event":      {func(r *FoundationRuns) { head(r).Event = "pull_request_target" }, "RUN_FOREIGN"},
		"other steps":           {func(r *FoundationRuns) { head(r).Steps[2].Name = "Run actions/checkout" }, "RUN_STEPS"},
		"extra step":            {func(r *FoundationRuns) { head(r).Steps = append(head(r).Steps, RunStep{"Post", "success"}) }, "RUN_STEPS"},
		"stale head":            {func(r *FoundationRuns) { head(r).Candidate = other }, "RUN_STALE_HEAD"},
		"other head fetched":    {func(r *FoundationRuns) { head(r).FetchedHead = other }, "RUN_STALE_HEAD"},
		"other head in env":     {func(r *FoundationRuns) { head(r).Env["LZ_HEAD"] = other }, "RUN_STALE_HEAD"},
		"candidate not a SHA":   {func(r *FoundationRuns) { head(r).Candidate = "main"; head(r).HeadSHA = "main" }, "RUN_STALE_HEAD"},
		"stale source":          {func(r *FoundationRuns) { head(r).SourceBlob = other }, "RUN_STALE_SOURCE"},
		"stale workflow":        {func(r *FoundationRuns) { head(r).WorkflowBlob = other }, "RUN_STALE_SOURCE"},
		"stale red source":      {func(r *FoundationRuns) { red(r).SourceBlob = other }, "RUN_STALE_SOURCE"},
		"other image":           {func(r *FoundationRuns) { head(r).Env["LZ_MANIFEST"] = "sha256:" + strings.Repeat("e", 64) }, "RUN_IDENTITY"},
		"other layer":           {func(r *FoundationRuns) { head(r).Env["LZ_LAYER"] = "sha256:" + strings.Repeat("e", 64) }, "RUN_IDENTITY"},
		"other entry":           {func(r *FoundationRuns) { head(r).Env["LZ_ENTRY_SHA256"] = strings.Repeat("e", 64) }, "RUN_IDENTITY"},
		"other launcher":        {func(r *FoundationRuns) { head(r).Env["LZ_BWRAP_SHA256"] = strings.Repeat("e", 64) }, "RUN_IDENTITY"},
		"other targets":         {func(r *FoundationRuns) { head(r).Env["LZ_TARGETS"] = "test:reports" }, "RUN_IDENTITY"},
		"extra environment":     {func(r *FoundationRuns) { head(r).Env["BASH_ENV"] = "/tmp/x" }, "RUN_IDENTITY"},
		"missing environment":   {func(r *FoundationRuns) { delete(head(r).Env, "LZ_IMAGE") }, "RUN_IDENTITY"},
		"widened token":         {func(r *FoundationRuns) { head(r).Permissions = append(head(r).Permissions, "Contents: write") }, "RUN_IDENTITY"},
		"self-hosted runner":    {func(r *FoundationRuns) { head(r).RunnerLabels = []string{"self-hosted"} }, "RUN_IDENTITY"},
		"other runner group":    {func(r *FoundationRuns) { head(r).RunnerGroup = "Default" }, "RUN_IDENTITY"},
		"other runner image":    {func(r *FoundationRuns) { head(r).RunnerImage = "ubuntu-22.04" }, "RUN_IDENTITY"},
		"incomplete run":        {func(r *FoundationRuns) { head(r).Status = "in_progress"; head(r).Conclusion = "" }, "RUN_INCOMPLETE"},
		"cancelled run":         {func(r *FoundationRuns) { head(r).Conclusion = "cancelled" }, "RUN_CANCELLED"},
		"cancelled job":         {func(r *FoundationRuns) { head(r).JobConclusion = "cancelled" }, "RUN_CANCELLED"},
		"skipped run":           {func(r *FoundationRuns) { head(r).Conclusion = "skipped" }, "RUN_SKIPPED"},
		"skipped step":          {func(r *FoundationRuns) { head(r).Steps[5].Conclusion = "skipped" }, "RUN_SKIPPED"},
		"failed run":            {func(r *FoundationRuns) { head(r).Conclusion = "failure" }, "RUN_FAILED"},
		"timed out run":         {func(r *FoundationRuns) { head(r).Conclusion = "timed_out" }, "RUN_FAILED"},
		"failed target":         {func(r *FoundationRuns) { head(r).Targets[2].Status = "failed" }, "RUN_FAILED"},
		"omitted check":         {func(r *FoundationRuns) { head(r).Targets = slices.Delete(head(r).Targets, 4, 5) }, "RUN_COUNT"},
		"reordered checks":      {func(r *FoundationRuns) { t := head(r).Targets; t[0], t[1] = t[1], t[0] }, "RUN_COUNT"},
		"wrong done count":      {func(r *FoundationRuns) { head(r).TargetsDone = 8 }, "RUN_COUNT"},
		"zero checks":           {func(r *FoundationRuns) { head(r).Targets = []RunTarget{}; head(r).TargetsDone = 0 }, "RUN_COUNT"},
		"zero discovery":        {func(r *FoundationRuns) { head(r).Targets[2].Discovered = 0 }, "RUN_ZERO_DISCOVERY"},
		"package without tests": {func(r *FoundationRuns) { head(r).Targets[3].Empty = 1 }, "RUN_ZERO_DISCOVERY"},
		"red control passed":    {func(r *FoundationRuns) { red(r).Conclusion = "success"; red(r).JobConclusion = "success" }, "RUN_NOT_RED"},
		"red before the checks": {func(r *FoundationRuns) {
			red(r).Steps[3].Conclusion = "failure"
			red(r).Steps[5].Conclusion = "skipped"
		}, "RUN_NOT_RED"},
		"red without a target":   {func(r *FoundationRuns) { red(r).Targets = red(r).Targets[:3] }, "RUN_NOT_RED"},
		"red after all targets":  {func(r *FoundationRuns) { red(r).TargetsDone = 9 }, "RUN_NOT_RED"},
		"red in an unlisted one": {func(r *FoundationRuns) { red(r).Targets[3].Name = "test:ghost" }, "RUN_NOT_RED"},
		"cancelled red control":  {func(r *FoundationRuns) { red(r).Conclusion = "cancelled" }, "RUN_CANCELLED"},
		"stale red head":         {func(r *FoundationRuns) { red(r).Candidate = other }, "RUN_STALE_HEAD"},
		"red without assertions": {func(r *FoundationRuns) { red(r).Targets[3].Failures = 0 }, "RUN_NOT_RED"},
		"tree changed since run": {func(r *FoundationRuns) { head(r).CheckDigests["test:reports"] = "sha256:" + other }, "RUN_STALE_HEAD"},
		"no digests recorded":    {func(r *FoundationRuns) { head(r).CheckDigests = map[string]string{} }, "RUN_STALE_HEAD"},
		"extra digest recorded":  {func(r *FoundationRuns) { head(r).CheckDigests["test:ghost"] = "sha256:" + other }, "RUN_STALE_HEAD"},
	} {
		t.Run(name, func(t *testing.T) {
			source, runs := recordedRuns(t)
			c.change(&runs)
			if got := ruleSet(JudgeFoundationRuns(source, encode(t, runs), treeDigests())); !reflect.DeepEqual(got, []string{c.want}) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v, want [%s]", name, got, c.want)
			}
		})
	}
}

// A record or source that does not decode strictly is refused, and so is a
// source the workflow could not have been rendered from.
func TestFoundationRunsSyntax(t *testing.T) {
	source, runs := recordedRuns(t)
	record := encode(t, runs)
	for name, c := range map[string]struct {
		source, record []byte
		want           string
	}{
		"malformed record": {source, []byte("{"), "RUNS_SYNTAX"},
		"null runs":        {source, []byte(`{"runs": null}`), "RUNS_SYNTAX"},
		"unknown field":    {source, []byte(strings.Replace(string(record), `"runs":`, `"verdict":"pass","runs":`, 1)), "RUNS_SYNTAX"},
		"malformed source": {[]byte("{"), record, "SOURCE_SYNTAX"},
		"unpinned source":  {[]byte(strings.Replace(string(source), `"ubuntu-24.04"`, `"self-hosted"`, 1)), record, "SOURCE_PIN"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ruleSet(JudgeFoundationRuns(c.source, c.record, treeDigests())); !slices.Contains(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v, want %s", name, got, c.want)
			}
		})
	}
}
