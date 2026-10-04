package checks

import (
	"encoding/json"
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
func foundationRegistry() (Registry, []Task) {
	check := func(id, creator, command string) CheckDefinition {
		return CheckDefinition{ID: id, Creator: creator, Command: command, Kind: KindBehavioral}
	}
	return Registry{Checks: []CheckDefinition{
		check("test:a", "T001", "task test:a"),
		check("test:ci", "T023", "task test:ci"),
		check("ci:run", "T023", "task ci:run"),
		check("lint", "T001", "task lint -- modules/x"),
		check("test:later", "T050", "task test:later"),
	}}, []Task{
		{ID: "T001", Done: true}, {ID: "T023"}, {ID: "T050"},
	}
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
		"deferred":        map[string]any{"ci:run": "judges this workflow's own run after it completes"},
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
		`echo "${LZ_LAYER#sha256:}  $RUNNER_TEMP/layer.tgz" | sha256sum -c -`,
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
	allowed := []string{"${{ github.event.pull_request.head.sha || github.sha }}", "${{ runner.temp }}"}
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
			!strings.Contains(line, `-C "$LZ_CANDIDATE" checkout`) {
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
		"deferral without a reason": {func(s map[string]any) { s["deferred"] = map[string]any{"ci:run": " "} }, []string{"CHECK_DEFERRED"}},
		"deferred and run": {func(s map[string]any) {
			s["targets"] = []any{"test:a", "test:ci", "ci:run"}
		}, []string{"CHECK_DEFERRED"}},
		"unknown deferral": {func(s map[string]any) {
			s["deferred"] = map[string]any{"ci:run": "afterwards", "test:ghost": "absent"}
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

// A repeated key is refused rather than resolved to its last value.
func TestFoundationSourceDuplicateKey(t *testing.T) {
	registry, tasks := foundationRegistry()
	source := encode(t, foundationSource())
	duplicated := []byte(strings.Replace(string(source), `"runner":"ubuntu-24.04"`, `"runner":"ubuntu-24.04","runner":"self-hosted"`, 1))
	if got := ruleSet(CheckFoundationCI(duplicated, renderSource(t, source), registry, tasks)); !reflect.DeepEqual(got, []string{"SOURCE_SYNTAX"}) {
		t.Errorf("BEHAVIORAL_RED: duplicate key gave %v", got)
	}
}
