package checks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
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

// job is one job of a source, as JSON.
func job(name string, targets ...string) map[string]any {
	list := []any{}
	for _, target := range targets {
		list = append(list, target)
	}
	return map[string]any{"name": name, "targets": list}
}

// oneJob is a source's jobs with every target in one job.
func oneJob(targets ...string) []any { return []any{job("foundation-1", targets...)} }

func foundationSource() map[string]any {
	return map[string]any{
		"runner":          "ubuntu-24.04",
		"timeout_minutes": 10,
		"manifest":        "sha256:" + strings.Repeat("a", 64),
		"layer":           "sha256:" + strings.Repeat("b", 64),
		"entry_sha256":    strings.Repeat("c", 64),
		"bwrap_sha256":    strings.Repeat("d", 64),
		"jobs":            []any{job("foundation-1", "test:a"), job("foundation-2", "test:ci")},
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

// Each job's budget is 15 minutes again (owner decision D126, part A: the
// targets run in parallel jobs, each judged against the timeout on its own;
// the 20-minute bridge of the one sequential job is withdrawn). 15 is accepted
// and rendered on every target job; 16 is refused (TestFoundationSourceRejected).
func TestFoundationTimeoutAtBudget(t *testing.T) {
	registry, tasks := foundationRegistry()
	value := foundationSource()
	value["timeout_minutes"] = 15
	source := encode(t, value)
	workflow := renderSource(t, source)
	if findings := CheckFoundationCI(source, workflow, registry, tasks); len(findings) != 0 {
		t.Errorf("BEHAVIORAL_RED: 15-minute budget refused: %+v", findings)
	}
	if got := strings.Count(string(workflow), "\n    timeout-minutes: 15\n"); got != 2 {
		t.Errorf("BEHAVIORAL_RED: %d jobs carry the 15-minute timeout, want both target jobs", got)
	}
}

// The rendered workflow is the only one accepted, so its properties are the
// CI's properties: no Action, no token permission, no secret, main-only push
// and pull-request triggers, the exact PR head, bounded hosted jobs, the
// pinned runtime once for all of them, each job's targets run through the
// entry, and the gate named foundation after them.
func TestFoundationWorkflowShape(t *testing.T) {
	workflow := string(renderSource(t, encode(t, foundationSource())))
	steps := "    steps:\n      - name: Locate the work directories\n"
	for _, want := range []string{
		"\non:\n  pull_request:\n    branches: [main]\n  push:\n    branches: [main]\npermissions: {}\nenv:\n",
		"\n  LZ_HEAD: ${{ github.event.pull_request.head.sha || github.sha }}\n",
		"\n  LZ_IMAGE: " + RuntimeImage + "\n",
		"\n  LZ_MANIFEST: sha256:" + strings.Repeat("a", 64) + "\n",
		"\n  LZ_LAYER: sha256:" + strings.Repeat("b", 64) + "\n",
		"\n  LZ_ENTRY_SHA256: " + strings.Repeat("c", 64) + "\n",
		"\n  LZ_BWRAP_SHA256: " + strings.Repeat("d", 64) + "\n",
		"\njobs:\n  foundation-1:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 10\n    env:\n      LZ_TARGETS: test:a\n" + steps,
		"\n  foundation-2:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 10\n    env:\n      LZ_TARGETS: test:ci\n" + steps,
		"\n  foundation:\n    needs: [foundation-1, foundation-2]\n    if: ${{ always() }}\n    runs-on: ubuntu-24.04\n    timeout-minutes: 5\n" +
			"    env:\n      LZ_JOB_RESULTS: foundation-1=${{ needs.foundation-1.result }} foundation-2=${{ needs.foundation-2.result }}\n" +
			"    steps:\n      - name: Require every foundation job to succeed\n",
		`test "$LZ_JOB_RESULTS" = "foundation-1=success foundation-2=success"`,
		`test "$(git -C "$LZ_CANDIDATE" rev-parse HEAD)" = "$LZ_HEAD"`,
		`echo "LZ_CANDIDATE=$RUNNER_TEMP/candidate" >> "$GITHUB_ENV"`,
		`echo "LZ_RUNTIME=$RUNNER_TEMP/runtime" >> "$GITHUB_ENV"`,
		`echo "${LZ_LAYER#sha256:}  $RUNNER_TEMP/layer.tgz" | sha256sum -c -`,
		`echo "${LZ_MANIFEST#sha256:}  $RUNNER_TEMP/manifest.json" | sha256sum -c -`,
		`test "$(jq -r '.layers | length' "$RUNNER_TEMP/manifest.json")" = 1`,
		`test "$(jq -r '.layers[0].digest' "$RUNNER_TEMP/manifest.json")" = "$LZ_LAYER"`,
		`echo "$LZ_ENTRY_SHA256  $LZ_RUNTIME/runtime/lz-offline" | sha256sum -c -`,
		`echo "$LZ_BWRAP_SHA256  $LZ_RUNTIME/host/bwrap" | sha256sum -c -`,
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("BEHAVIORAL_RED: rendered workflow lacks %q", want)
		}
	}
	// Each target job runs its own targets through the entry, once.
	if got := strings.Count(workflow, `"$LZ_RUNTIME/runtime/lz-offline" --candidate "$LZ_CANDIDATE" -- task "$target"`); got != 2 {
		t.Errorf("BEHAVIORAL_RED: %d jobs run targets through the entry, want 2", got)
	}
}

// The rendered jobs are the source's, in order, and the gate after them; the
// gate's always() is the one condition, and the only expressions are the
// head, that condition and each job's result.
func TestFoundationWorkflowJobs(t *testing.T) {
	workflow := string(renderSource(t, encode(t, foundationSource())))
	names := []string{}
	if _, jobs, ok := strings.Cut(workflow, "\njobs:\n"); ok {
		for _, m := range regexp.MustCompile(`(?m)^  ([a-z0-9-]+):$`).FindAllStringSubmatch(jobs, -1) {
			names = append(names, m[1])
		}
	}
	if want := []string{"foundation-1", "foundation-2", "foundation"}; !slices.Equal(names, want) {
		t.Errorf("BEHAVIORAL_RED: jobs %v, want %v", names, want)
	}
	for _, banned := range []string{"uses:", "secrets", "github.token", "pull_request_target", "workflow_run", "continue-on-error",
		"self-hosted", "GITHUB_WORKSPACE", "cancelled()", "success()", "failure()", "fail-fast"} {
		if strings.Contains(workflow, banned) {
			t.Errorf("BEHAVIORAL_RED: rendered workflow contains %q", banned)
		}
	}
	// The gate's is the one condition: it runs whatever its needs concluded,
	// so it is never skipped, and judges them itself.
	if got := regexp.MustCompile(`(?m)^\s*if:.*$`).FindAllString(workflow, -1); !slices.Equal(got, []string{"    if: ${{ always() }}"}) {
		t.Errorf("BEHAVIORAL_RED: conditions %q, want only the gate's always()", got)
	}
	// The workflow env may not use the runner context, so the head is the
	// only expression there; the gate reads each job's result and nothing
	// else; the work directories come from $RUNNER_TEMP at run time.
	allowed := []string{"${{ github.event.pull_request.head.sha || github.sha }}", "${{ always() }}",
		"${{ needs.foundation-1.result }}", "${{ needs.foundation-2.result }}"}
	for _, expression := range regexp.MustCompile(`\$\{\{[^}]*\}\}`).FindAllString(workflow, -1) {
		if !slices.Contains(allowed, expression) {
			t.Errorf("BEHAVIORAL_RED: unexpected expression %s", expression)
		}
	}
}

// The candidate path appears only where git fetches it as data and where the
// entry receives it.
func TestFoundationWorkflowCandidateAsData(t *testing.T) {
	workflow := string(renderSource(t, encode(t, foundationSource())))
	for _, line := range strings.Split(workflow, "\n") {
		if strings.Contains(line, "$LZ_CANDIDATE") && !strings.Contains(line, `git -C "$LZ_CANDIDATE"`) &&
			!strings.Contains(line, `git init -q "$LZ_CANDIDATE"`) && !strings.Contains(line, `--candidate "$LZ_CANDIDATE"`) &&
			!strings.Contains(line, `-C "$LZ_CANDIDATE" checkout`) && !strings.Contains(line, `echo "LZ_CANDIDATE=`) {
			t.Errorf("BEHAVIORAL_RED: candidate used on the host: %s", line)
		}
	}
}

// gateScript is the gate step's script and its env template, as rendered.
func gateScript(t *testing.T, workflow string) (string, string) {
	t.Helper()
	_, rest, ok := strings.Cut(workflow, "      - name: "+gateStep+"\n        run: |\n")
	if !ok {
		t.Fatal("no gate step")
	}
	var script []string
	for _, line := range strings.Split(rest, "\n") {
		body, ok := strings.CutPrefix(line, "          ")
		if !ok {
			break
		}
		script = append(script, body)
	}
	_, template, ok := strings.Cut(workflow, "\n      LZ_JOB_RESULTS: ")
	if !ok {
		t.Fatal("no LZ_JOB_RESULTS")
	}
	template, _, _ = strings.Cut(template, "\n")
	return strings.Join(script, "\n"), template
}

// The gate named foundation, the check the main ruleset requires, passes
// only when every target job succeeded: a failed, cancelled or skipped job,
// one with no result and one left out all fail it. GitHub substitutes each
// job's result into LZ_JOB_RESULTS; the rendered script then runs as GitHub
// runs it (bash -e), here under the offline entry's POSIX sh with -e and -u:
// the script has no pipe, so pipefail changes nothing.
func TestFoundationWorkflowGate(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	script, template := gateScript(t, string(renderSource(t, encode(t, foundationSource()))))
	first, rest, _ := strings.Cut(script, "\n")
	if first != "set -euo pipefail" || strings.Contains(rest, "|") {
		t.Fatalf("gate script changed shape: %q", script)
	}
	script = "set -eu\n" + rest
	results := func(r map[string]string) string {
		value := template
		for _, name := range []string{"foundation-1", "foundation-2"} {
			value = strings.ReplaceAll(value, "${{ needs."+name+".result }}", r[name])
		}
		return value
	}
	for name, c := range map[string]struct {
		value string
		pass  bool
	}{
		"every job succeeded":   {results(map[string]string{"foundation-1": "success", "foundation-2": "success"}), true},
		"a job failed":          {results(map[string]string{"foundation-1": "failure", "foundation-2": "success"}), false},
		"a job was cancelled":   {results(map[string]string{"foundation-1": "success", "foundation-2": "cancelled"}), false},
		"a job was skipped":     {results(map[string]string{"foundation-1": "skipped", "foundation-2": "success"}), false},
		"every job was skipped": {results(map[string]string{"foundation-1": "skipped", "foundation-2": "skipped"}), false},
		"a job without result":  {results(map[string]string{"foundation-1": "success"}), false},
		"a job left out":        {"foundation-1=success", false},
		"no results":            {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(sh, "-e", "-c", script)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LZ_JOB_RESULTS=" + c.value}
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err := cmd.Run()
			if passed := err == nil; passed != c.pass {
				t.Errorf("BEHAVIORAL_RED: gate on %q passed=%v, want %v: %s", c.value, passed, c.pass, out.String())
			}
		})
	}
}

// Every curl in the rendered workflow is limited to HTTPS, and one that
// follows redirects is limited to HTTPS on each redirect too, as the fetch
// script does: the digests already pin the content, so this keeps the token
// and the transfer off clear text.
func TestFoundationWorkflowCurlHTTPSOnly(t *testing.T) {
	workflow := string(renderSource(t, encode(t, foundationSource())))
	count := 0
	for _, line := range strings.Split(workflow, "\n") {
		if !strings.Contains(line, "curl ") {
			continue
		}
		count++
		if !strings.Contains(line, `curl --proto '=https' `) {
			t.Errorf("BEHAVIORAL_RED: curl not limited to HTTPS: %s", line)
		}
		if strings.Contains(line, "-fsSL") && !strings.Contains(line, `--proto-redir '=https'`) {
			t.Errorf("BEHAVIORAL_RED: curl follows redirects without limiting them to HTTPS: %s", line)
		}
	}
	if count != 6 {
		t.Errorf("BEHAVIORAL_RED: %d curl lines, want 3 (token, manifest, layer) in each of the 2 jobs", count)
	}
}

// Every change to the committed workflow is drift from the rendered one,
// whichever authority it widens.
func TestFoundationWorkflowDrift(t *testing.T) {
	registry, tasks := foundationRegistry()
	source := encode(t, foundationSource())
	workflow := string(renderSource(t, source))
	for name, change := range map[string][2]string{
		"unpinned action":        {"    steps:\n", "    steps:\n      - uses: actions/checkout@v4\n"},
		"pinned action":          {"    steps:\n", "    steps:\n      - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955\n"},
		"widened permission":     {"permissions: {}", "permissions: write-all"},
		"widened trigger":        {"  push:\n", "  pull_request_target:\n  push:\n"},
		"any branch":             {"  push:\n    branches: [main]", "  push:\n    branches: ['**']"},
		"secret":                 {"\n  LZ_HEAD:", "\n  TOKEN: ${{ secrets.GITHUB_TOKEN }}\n  LZ_HEAD:"},
		"stale head":             {"${{ github.event.pull_request.head.sha || github.sha }}", "${{ github.sha }}"},
		"host execution":         {"    steps:\n", "    steps:\n      - run: bash \"$LZ_CANDIDATE/build.sh\"\n"},
		"skipped check":          {"LZ_TARGETS: test:ci", "LZ_TARGETS: test:a"},
		"tolerated failure":      {"    timeout-minutes: 10\n", "    timeout-minutes: 10\n    continue-on-error: true\n"},
		"longer timeout":         {"timeout-minutes: 10", "timeout-minutes: 60"},
		"self-hosted runner":     {"runs-on: ubuntu-24.04", "runs-on: self-hosted"},
		"other runtime":          {"LZ_LAYER: sha256:" + strings.Repeat("b", 64), "LZ_LAYER: sha256:" + strings.Repeat("e", 64)},
		"gate only on success":   {"    if: ${{ always() }}\n", ""},
		"gate skippable":         {"    if: ${{ always() }}\n", "    if: ${{ !cancelled() }}\n"},
		"gate ignores a job":     {"needs: [foundation-1, foundation-2]", "needs: [foundation-1]"},
		"gate tolerates failure": {`test "$LZ_JOB_RESULTS" = "foundation-1=success foundation-2=success"`, "true"},
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
// splits the targets over a few plainly named jobs, none named as the gate,
// and accounts for exactly the argument-free checks of closed tasks and T023:
// each runs once, in one job, or is deferred with a reason.
func TestFoundationSourceRejected(t *testing.T) {
	registry, tasks := foundationRegistry()
	for name, c := range map[string]struct {
		change func(map[string]any)
		want   []string
	}{
		"deferral of an implemented check": {func(s map[string]any) {
			s["jobs"] = oneJob("test:ci")
			s["deferred"] = map[string]any{"ci:foundation": "afterwards", "test:a": "later"}
		}, []string{"CHECK_DEFERRED"}},
		"target name that is not a plain Task target": {func(s map[string]any) {
			s["jobs"] = oneJob("test:a", "test:ci", "Test A")
		}, []string{"CHECK_NAME", "CHECK_UNKNOWN"}},
		"self-hosted runner":        {func(s map[string]any) { s["runner"] = "self-hosted" }, []string{"SOURCE_PIN"}},
		"timeout above budget":      {func(s map[string]any) { s["timeout_minutes"] = 16 }, []string{"SOURCE_PIN"}},
		"the withdrawn bridge":      {func(s map[string]any) { s["timeout_minutes"] = 20 }, []string{"SOURCE_PIN"}},
		"no timeout":                {func(s map[string]any) { s["timeout_minutes"] = 0 }, []string{"SOURCE_PIN"}},
		"layer not a digest":        {func(s map[string]any) { s["layer"] = "latest" }, []string{"SOURCE_PIN"}},
		"manifest not a digest":     {func(s map[string]any) { s["manifest"] = "sha256:" + strings.Repeat("A", 64) }, []string{"SOURCE_PIN"}},
		"short entry digest":        {func(s map[string]any) { s["entry_sha256"] = strings.Repeat("c", 63) }, []string{"SOURCE_PIN"}},
		"launcher digest prefixed":  {func(s map[string]any) { s["bwrap_sha256"] = "sha256:" + strings.Repeat("d", 64) }, []string{"SOURCE_PIN"}},
		"omitted check":             {func(s map[string]any) { s["jobs"] = oneJob("test:a") }, []string{"CHECK_OMITTED"}},
		"no targets":                {func(s map[string]any) { s["jobs"] = oneJob() }, []string{"CHECK_OMITTED", "NO_TARGETS", "SOURCE_JOBS"}},
		"no jobs":                   {func(s map[string]any) { s["jobs"] = []any{} }, []string{"CHECK_OMITTED", "NO_TARGETS", "SOURCE_JOBS"}},
		"check of an open task":     {func(s map[string]any) { s["jobs"] = oneJob("test:a", "test:ci", "test:later") }, []string{"CHECK_NOT_RUNNABLE"}},
		"check with an argument":    {func(s map[string]any) { s["jobs"] = oneJob("test:a", "test:ci", "lint") }, []string{"CHECK_NOT_RUNNABLE"}},
		"unknown check":             {func(s map[string]any) { s["jobs"] = oneJob("test:a", "test:ci", "test:ghost") }, []string{"CHECK_UNKNOWN"}},
		"repeated check":            {func(s map[string]any) { s["jobs"] = oneJob("test:a", "test:ci", "test:a") }, []string{"CHECK_DUPLICATE"}},
		"deferral without a reason": {func(s map[string]any) { s["deferred"] = map[string]any{"ci:foundation": " "} }, []string{"CHECK_DEFERRED"}},
		"a target in two jobs": {func(s map[string]any) {
			s["jobs"] = []any{job("foundation-1", "test:a", "test:ci"), job("foundation-2", "test:a")}
		}, []string{"CHECK_DUPLICATE"}},
		"a job without targets": {func(s map[string]any) {
			s["jobs"] = []any{job("foundation-1", "test:a", "test:ci"), job("foundation-2")}
		}, []string{"SOURCE_JOBS"}},
		"two jobs of one name": {func(s map[string]any) {
			s["jobs"] = []any{job("foundation-1", "test:a"), job("foundation-1", "test:ci")}
		}, []string{"SOURCE_JOBS"}},
		"a job named as the gate": {func(s map[string]any) {
			s["jobs"] = []any{job("foundation-1", "test:a"), job("foundation", "test:ci")}
		}, []string{"SOURCE_JOBS"}},
		"a job name outside the family": {func(s map[string]any) {
			s["jobs"] = []any{job("foundation-1", "test:a"), job("build", "test:ci")}
		}, []string{"SOURCE_JOBS"}},
		"a job name that injects": {func(s map[string]any) {
			s["jobs"] = []any{job("foundation-1", "test:a"), job("foundation-2:\n    runs-on: self-hosted\n  x", "test:ci")}
		}, []string{"SOURCE_JOBS"}},
		"deferred and run": {func(s map[string]any) {
			s["jobs"] = oneJob("test:a", "test:ci", "ci:foundation")
		}, []string{"CHECK_DEFERRED"}},
		"unknown deferral": {func(s map[string]any) {
			s["deferred"] = map[string]any{"ci:foundation": "afterwards", "test:ghost": "absent"}
		}, []string{"CHECK_UNKNOWN"}},
		"unknown field":       {func(s map[string]any) { s["permissions"] = "write-all" }, []string{"SOURCE_SYNTAX"}},
		"the one-job targets": {func(s map[string]any) { s["targets"] = []any{"test:a", "test:ci"} }, []string{"SOURCE_SYNTAX"}},
		"missing field":       {func(s map[string]any) { delete(s, "layer") }, []string{"SOURCE_SYNTAX"}},
		"null jobs":           {func(s map[string]any) { s["jobs"] = nil }, []string{"SOURCE_SYNTAX"}},
		"null targets": {func(s map[string]any) {
			s["jobs"] = []any{map[string]any{"name": "foundation-1", "targets": nil}}
		}, []string{"SOURCE_SYNTAX"}},
		"targets as text": {func(s map[string]any) {
			s["jobs"] = []any{map[string]any{"name": "foundation-1", "targets": "test:a test:ci"}}
		}, []string{"SOURCE_SYNTAX"}},
		"a job without a name": {func(s map[string]any) {
			s["jobs"] = []any{map[string]any{"targets": []any{"test:a", "test:ci"}}}
		}, []string{"SOURCE_SYNTAX"}},
		"an unknown job field": {func(s map[string]any) {
			j := job("foundation-1", "test:a", "test:ci")
			j["if"] = "always()"
			s["jobs"] = []any{j}
		}, []string{"SOURCE_SYNTAX"}},
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

// At most four jobs: each costs a runner and the fetch of the runtime.
func TestFoundationSourceJobBound(t *testing.T) {
	s := FoundationSource{Runner: "ubuntu-24.04", TimeoutMinutes: 10, Manifest: "sha256:" + strings.Repeat("a", 64),
		Layer: "sha256:" + strings.Repeat("b", 64), EntrySHA256: strings.Repeat("c", 64), BwrapSHA256: strings.Repeat("d", 64)}
	for i := 1; i <= 5; i++ {
		s.Jobs = append(s.Jobs, FoundationJob{Name: fmt.Sprintf("foundation-%d", i), Targets: []string{fmt.Sprintf("test:t%d", i)}})
		got := ruleSet(ValidateFoundationSource(s))
		want := []string(nil)
		if i > 4 {
			want = []string{"SOURCE_JOBS"}
		}
		if !slices.Equal(got, want) {
			t.Errorf("BEHAVIORAL_RED: %d jobs gave %v, want %v", i, got, want)
		}
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
		value := foundationSource()
		value["jobs"] = oneJob(append([]string{"test:a", "test:ci"}, extra...)...)
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

// The committed source runs `stacks:check` in CI (005 T106), so the
// generated stack files are compared with the manifest on every pull request,
// and `test:configure` (004 T016), so a change to the stacks decoder, the
// schema or the manifest cannot break spec 004's answers decoding unseen;
// it runs its targets in parallel jobs at the 15-minute budget (D126 part A);
// the committed workflow carries each job's list (its drift from the rendered
// one is `ci-source`'s, under the same target).
func TestFoundationCommittedTargets(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", FoundationSourcePath))
	if err != nil {
		t.Fatal(err)
	}
	source, err := ParseFoundationSource(data)
	if err != nil {
		t.Fatalf("committed source refused: %v", err)
	}
	for _, id := range []string{"stacks:check", "test:configure"} {
		if !slices.Contains(source.Targets(), id) {
			t.Errorf("BEHAVIORAL_RED: committed source targets %v lack %s", source.Targets(), id)
		}
	}
	if len(source.Jobs) < 2 || source.TimeoutMinutes != 15 {
		t.Errorf("BEHAVIORAL_RED: committed source runs %d jobs at %d minutes, want parallel jobs at 15", len(source.Jobs), source.TimeoutMinutes)
	}
	workflow, err := os.ReadFile(filepath.Join("..", "..", "..", FoundationWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range source.Jobs {
		want := "\n  " + j.Name + ":\n    runs-on: " + source.Runner + "\n    timeout-minutes: 15\n    env:\n      LZ_TARGETS: " + strings.Join(j.Targets, " ") + "\n"
		if !strings.Contains(string(workflow), want) {
			t.Errorf("BEHAVIORAL_RED: committed workflow lacks %q", want)
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
			value["jobs"] = oneJob("test:a", "test:ci", id)
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
// captured read-only with gh; both ran the one sequential job of their day.
// See specs/001-offline-foundation/evidence/T023.md. head-split and red-split
// are those captures as the split workflow's would read: the job renamed
// foundation-1, and the gate job foundation added after it
// (TestFoundationSplitCaptures derives them). source.json is the approved
// source at both heads, its targets moved into the one job foundation-1.
const (
	runsDir = "testdata/foundation-runs"
	prHead  = "20b408842ae4d3a09b89d0639f61a6dd218cbc48"
	redHead = "da612461f1dbc20a422043e4171e141ab0c62d33"
	// splitJob is the job the split captures run the captured targets in.
	splitJob = "foundation-1"
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

// splitCapture derives the split capture of a captured one-job run: the job
// is renamed splitJob in the jobs and the log, and the gate job is added as
// GitHub reports it, its id the next one, its set-up the captured job's, its
// env the captured pins and the result GitHub substitutes for the job, its
// outcome that result's. The blobs are those of source.json and its
// rendering. Nothing else of the capture changes.
func splitCapture(t *testing.T, name string) map[string][]byte {
	t.Helper()
	c := captureRun(t, name)
	source := readRunFile(t, "source.json")
	parsed, err := ParseFoundationSource(source)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(c.Jobs))
	decoder.UseNumber()
	var jobs map[string]any
	if err := decoder.Decode(&jobs); err != nil {
		t.Fatal(err)
	}
	captured := jobs["jobs"].([]any)[0].(map[string]any)
	result := captured["conclusion"].(string)
	captured["name"] = splitJob
	id, err := captured["id"].(json.Number).Int64()
	if err != nil {
		t.Fatal(err)
	}
	gate := maps.Clone(captured)
	gate["name"] = FoundationCheck
	gate["id"] = json.Number(fmt.Sprint(id + 1))
	for _, key := range []string{"url", "html_url", "check_run_url"} {
		gate[key] = strings.ReplaceAll(gate[key].(string), fmt.Sprint(id), fmt.Sprint(id+1))
	}
	steps := captured["steps"].([]any)
	middle := maps.Clone(steps[len(steps)-2].(map[string]any))
	middle["name"], middle["conclusion"], middle["number"] = gateStep, result, json.Number("2")
	last := maps.Clone(steps[len(steps)-1].(map[string]any))
	last["number"] = json.Number("3")
	gate["steps"] = []any{steps[0], middle, last}
	jobs["jobs"], jobs["total_count"] = []any{captured, gate}, json.Number("2")
	jobsData, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}

	return map[string][]byte{
		"run.json":     c.Run,
		"jobs.json":    jobsData,
		"run.log":      splitCaptureLog(c.Log, result),
		"workflow.sha": []byte(gitBlob([]byte(RenderFoundationWorkflow(parsed))) + "\n"),
		"source.sha":   []byte(gitBlob(source) + "\n"),
	}
}

// splitCaptureLog is a captured one-job log with the job renamed splitJob
// and the gate's lines after it: the captured set-up, the gate step that saw
// result echoing the captured pins, and the clean-up, at the log's last time.
func splitCaptureLog(log []byte, result string) []byte {
	lines := strings.Split(strings.TrimSuffix(string(log), "\n"), "\n")
	var setUp, pins []string
	inEnv := false
	for i, line := range lines {
		fields := strings.SplitN(line, "\t", 3)
		fields[0] = splitJob
		lines[i] = strings.Join(fields, "\t")
		_, text, _ := strings.Cut(fields[2], " ")
		switch {
		case fields[1] == "Set up job":
			setUp = append(setUp, FoundationCheck+"\t"+fields[1]+"\t"+fields[2])
		case fields[1] == checksStep && text == "env:":
			inEnv = true
		case fields[1] == checksStep && inEnv && strings.HasPrefix(text, "  "):
			if key, _, _ := strings.Cut(strings.TrimSpace(text), ":"); !slices.Contains([]string{"LZ_TARGETS", "LZ_CANDIDATE", "LZ_RUNTIME"}, key) {
				pins = append(pins, text)
			}
		default:
			inEnv = false
		}
	}
	stamp, _, _ := strings.Cut(strings.SplitN(lines[len(lines)-1], "\t", 3)[2], " ")
	stamp = strings.TrimPrefix(stamp, "\uFEFF")
	results := splitJob + "=" + result
	body := []string{"##[group]Run set -euo pipefail", "set -euo pipefail", `echo "LZ_JOB_RESULTS $LZ_JOB_RESULTS"`,
		`test "$LZ_JOB_RESULTS" = "` + splitJob + `=success"`, "shell: /usr/bin/bash -e {0}", "env:"}
	body = append(append(body, pins...), "  LZ_JOB_RESULTS: "+results, "##[endgroup]", "LZ_JOB_RESULTS "+results)
	if result != "success" {
		body = append(body, "##[error]Process completed with exit code 1.")
	}
	gateLog := setUp
	for _, text := range body {
		gateLog = append(gateLog, FoundationCheck+"\t"+gateStep+"\t"+stamp+" "+text)
	}
	gateLog = append(gateLog, FoundationCheck+"\tComplete job\t"+stamp+" Cleaning up orphan processes")
	return []byte(strings.Join(append(lines, gateLog...), "\n") + "\n")
}

// The split captures are exactly what splitCapture derives from the
// captured runs, so a renderer or source change fails here until they are
// derived again (LZ_DERIVE_SPLIT_CAPTURES=1 writes them) or the split
// workflow's own runs are captured in their place.
func TestFoundationSplitCaptures(t *testing.T) {
	for _, name := range []string{"head", "red"} {
		for file, want := range splitCapture(t, name) {
			path := filepath.Join(runsDir, name+"-split", file)
			if os.Getenv("LZ_DERIVE_SPLIT_CAPTURES") == "1" {
				writeFile(t, path, want, 0o644)
				continue
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("BEHAVIORAL_RED: %s is not the split of the %s capture (%v)", path, name, err)
			}
		}
	}
}

// recordedRuns is the approved source and the record of the green PR head
// and the red control, as the split workflow ran them, observed afresh for
// every caller.
func recordedRuns(t *testing.T) ([]byte, FoundationRuns) {
	t.Helper()
	return readRunFile(t, "source.json"), FoundationRuns{Runs: []FoundationRun{
		observeRun(t, RoleHead, prHead, "head-split"),
		observeRun(t, RoleRedControl, redHead, "red-split"),
	}}
}

var foundationTargets = []string{"verify:toolchain", "test:offline-boundary", "test:reports", "test:traceability", "check:specs",
	"test:dependencies", "test:static", "test:runtime-image", "test:foundation-ci"}

// The observation is read from GitHub's output, not written by hand: the
// run, check run, heads, runner, token, executed identities and every
// target with what it discovered.
func TestFoundationObserve(t *testing.T) {
	head := observeRun(t, RoleHead, prHead, "head")
	if len(head.Jobs) != 1 {
		t.Fatalf("BEHAVIORAL_RED: head run observed with %d jobs", len(head.Jobs))
	}
	j := head.Jobs[0]
	for name, ok := range map[string]bool{
		"run":        head.RunID == 37278768943 && head.Attempt == 1 && j.CheckRunID == 111661662616 && j.Name == "foundation",
		"repository": head.Repository == FoundationRepository && head.HeadRepository == FoundationRepository,
		"event":      head.Event == "pull_request",
		"head":       head.Candidate == prHead && head.HeadSHA == prHead && j.FetchedHead == prHead,
		"workflow":   head.WorkflowPath == FoundationWorkflow && head.WorkflowBlob == "de1f3442b25ea94a490680e958103ad13cb141ea",
		"source":     head.SourceBlob == "ec8547ce87ddc7b1b07e3b47a99c9fec8057d226",
		"outcome":    head.Status == "completed" && head.Conclusion == "success" && j.Conclusion == "success",
		"runner": slices.Equal(j.RunnerLabels, []string{"ubuntu-24.04"}) && j.RunnerGroup == "GitHub Actions" &&
			j.RunnerImage == "ubuntu-24.04",
		"token": slices.Equal(j.Permissions, []string{"Metadata: read"}),
		"steps": len(j.Steps) == 7 && j.Steps[5] == RunStep{Name: "Run the foundation checks inside the offline entry", Conclusion: "success"},
		"image": j.Env["LZ_MANIFEST"] == "sha256:9425742e0c3e99b0a5d4f798ff796e441de78901ec7f7a6e110c74b5110c2614" &&
			j.Env["LZ_HEAD"] == prHead && len(j.Env) == 9,
		"targets": j.TargetsDone == 9 && len(j.Targets) == 9,
	} {
		if !ok {
			t.Errorf("BEHAVIORAL_RED: head run observed wrongly: %s: %+v", name, head)
		}
	}
	// test:runtime-image also printed a package without test files, which
	// is not a package that ran no test.
	for i, target := range j.Targets {
		if target.Name != foundationTargets[i] || target.Status != "ok" || target.Discovered == 0 || target.Empty != 0 {
			t.Errorf("BEHAVIORAL_RED: head target %d observed as %+v", i, target)
		}
	}
}

// The red control is observed failing in its fourth target, with failed
// assertions, before the done count.
func TestFoundationObserveRed(t *testing.T) {
	red := observeRun(t, RoleRedControl, redHead, "red")
	r := red.Jobs[0]
	if red.Conclusion != "failure" || r.TargetsDone != -1 || len(r.Targets) != 4 || r.FetchedHead != redHead {
		t.Fatalf("BEHAVIORAL_RED: red control observed wrongly: %+v", red)
	}
	if last := r.Targets[3]; last.Name != "test:traceability" || last.Status != "failed" || last.Failures == 0 {
		t.Errorf("BEHAVIORAL_RED: failing target observed as %+v", last)
	}
}

// A run of several jobs is observed job by job, each from its own lines of
// the log, ordered by name: the target job exactly as the one job of the
// sequential run was, the gate with its own steps, the results it judged and
// no target, and the run's check run is the gate's, the check named
// foundation.
func TestFoundationObserveSplit(t *testing.T) {
	for _, c := range []struct{ name, split, role, candidate, results string }{
		{"head", "head-split", RoleHead, prHead, "foundation-1=success"},
		{"red", "red-split", RoleRedControl, redHead, "foundation-1=failure"},
	} {
		one := observeRun(t, c.role, c.candidate, c.name)
		split := observeRun(t, c.role, c.candidate, c.split)
		if len(split.Jobs) != 2 || split.Jobs[0].Name != FoundationCheck || split.Jobs[1].Name != splitJob {
			t.Fatalf("BEHAVIORAL_RED: %s observed jobs %+v", c.split, split.Jobs)
		}
		want := one.Jobs[0]
		want.Name = splitJob
		if !reflect.DeepEqual(split.Jobs[1], want) {
			t.Errorf("BEHAVIORAL_RED: %s target job observed as %+v, want %+v", c.split, split.Jobs[1], want)
		}
		gate := split.Jobs[0]
		steps := []string{"Set up job", gateStep, "Complete job"}
		if !slices.Equal(stepNames(gate.Steps), steps) || gate.Env["LZ_JOB_RESULTS"] != c.results || len(gate.Targets) != 0 ||
			gate.TargetsDone != -1 || gate.Conclusion != one.Jobs[0].Conclusion || !slices.Equal(gate.Permissions, tokenPermissions) {
			t.Errorf("BEHAVIORAL_RED: %s gate observed as %+v", c.split, gate)
		}
		if split.CheckRunID != gate.CheckRunID || gate.CheckRunID != one.Jobs[0].CheckRunID+1 {
			t.Errorf("BEHAVIORAL_RED: %s check run %d, gate %d", c.split, split.CheckRunID, gate.CheckRunID)
		}
	}
}

// withLines inserts texts as job-log lines of the step in front of the line
// that holds anchor, so they belong to the same target.
func withLines(t *testing.T, log []byte, anchor string, texts ...string) []byte {
	t.Helper()
	lines := strings.Split(string(log), "\n")
	for i, line := range lines {
		if !strings.Contains(line, anchor) {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		stamp, _, _ := strings.Cut(fields[2], " ")
		var added []string
		for _, text := range texts {
			added = append(added, fields[0]+"\t"+fields[1]+"\t"+stamp+" "+text)
		}
		return []byte(strings.Join(slices.Concat(lines[:i], added, lines[i:]), "\n"))
	}
	t.Fatalf("anchor %q not in the log", anchor)
	return nil
}

// A target may split its packages over concurrent `go test` processes, one
// with -run and one with -skip of the same pattern (test:live-lane), so a
// package can print `[no tests to run]` in one half and run in the other.
// Discovery is judged per target: a package counts as tested when any of
// its lines ran tests, wherever that line falls, and a package that ran no
// test in every line is still a package that ran no test.
func TestFoundationObserveShardedTarget(t *testing.T) {
	const pkg = "github.com/PlatformRelay/landingzone-for-ovhcloud/tools/"
	first, last := "Z ok  \t"+pkg+"internal/report\t", "Z LZ_TARGET_OK test:reports"
	for name, c := range map[string]struct {
		anchor            string
		texts             []string
		discovered, empty int
	}{
		"empty half before the tested one": {first, []string{"ok  \t" + pkg + "internal/report\t0.010s [no tests to run]"}, 2, 0},
		"empty half after the tested one":  {last, []string{"ok  \t" + pkg + "internal/report\t0.010s [no tests to run]"}, 2, 0},
		"no half ran tests": {first, []string{"ok  \t" + pkg + "internal/other\t0.010s [no tests to run]",
			"ok  \t" + pkg + "internal/other\t0.020s [no tests to run]"}, 2, 1},
		"one package, one empty line": {last, []string{"ok  \t" + pkg + "internal/other\t0.010s [no tests to run]"}, 2, 1},
		// A target that is no Go test counts by its count line, as
		// stacks:check prints it (lz-stacks check).
		"a count line": {last, []string{"STACKS_CHECK_OK stacks=6 files=16"}, 3, 0},
	} {
		t.Run(name, func(t *testing.T) {
			capture := captureRun(t, "head")
			capture.Log = withLines(t, capture.Log, c.anchor, c.texts...)
			run, err := ObserveFoundationRun(RoleHead, prHead, capture)
			if err != nil {
				t.Fatal(err)
			}
			if got := run.Jobs[0].Targets[2]; got.Name != "test:reports" || got.Discovered != c.discovered || got.Empty != c.empty {
				t.Errorf("BEHAVIORAL_RED: %s observed as %+v, want discovered %d, empty %d", name, got, c.discovered, c.empty)
			}
		})
	}
}

// A capture that is not one run of uniquely named jobs, whose log names a
// job the run does not have, or whose blob is not a SHA, is refused rather
// than observed.
func TestFoundationObserveRefused(t *testing.T) {
	replace := func(data []byte, old, new string) []byte { return bytes.Replace(data, []byte(old), []byte(new), 1) }
	for name, c := range map[string]struct {
		capture string
		change  func(*RunCapture)
	}{
		"run not JSON":    {"head", func(c *RunCapture) { c.Run = []byte("{") }},
		"no jobs":         {"head", func(c *RunCapture) { c.Jobs = []byte(`{"total_count": 0, "jobs": []}`) }},
		"jobs miscounted": {"head-split", func(c *RunCapture) { c.Jobs = replace(c.Jobs, `"total_count":2`, `"total_count":3`) }},
		"a job named twice": {"head-split", func(c *RunCapture) {
			c.Jobs = replace(c.Jobs, `"name":"foundation-1"`, `"name":"foundation"`)
		}},
		"a job without a name": {"head-split", func(c *RunCapture) { c.Jobs = replace(c.Jobs, `"name":"foundation-1"`, `"name":""`) }},
		"log of another job": {"head-split", func(c *RunCapture) {
			c.Log = replace(c.Log, "foundation-1\tSet up job\t", "foundation-9\tSet up job\t")
		}},
		"no log":           {"head", func(c *RunCapture) { c.Log = nil }},
		"workflow not SHA": {"head", func(c *RunCapture) { c.WorkflowBlob = []byte("main\n") }},
		"source not SHA":   {"head", func(c *RunCapture) { c.SourceBlob = []byte("") }},
		"no head digests":  {"head", func(c *RunCapture) { c.Digests = nil }},
		"job of another run": {"head", func(c *RunCapture) {
			c.Jobs = replace(c.Jobs, `"run_id":37278768943`, `"run_id":37279007980`)
		}},
		"job at another head": {"head", func(c *RunCapture) {
			c.Jobs = replace(c.Jobs, `"head_sha":"`+prHead+`"`, `"head_sha":"`+redHead+`"`)
		}},
		"job of another attempt": {"head", func(c *RunCapture) { c.Jobs = replace(c.Jobs, `"run_attempt":1`, `"run_attempt":2`) }},
		"gate of another run": {"head-split", func(c *RunCapture) {
			i := bytes.LastIndex(c.Jobs, []byte(`"run_id":37278768943`))
			c.Jobs = slices.Concat(c.Jobs[:i], []byte(`"run_id":37279007980`), c.Jobs[i+len(`"run_id":37278768943`):])
		}},
	} {
		t.Run(name, func(t *testing.T) {
			capture := captureRun(t, c.capture)
			before := fmt.Sprint(capture)
			c.change(&capture)
			if fmt.Sprint(capture) == before {
				t.Fatal("fixture unchanged")
			}
			if _, err := ObserveFoundationRun(RoleHead, prHead, capture); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s observed", name)
			}
		})
	}
}

// unnamedHead is push run 38550248277 of main at this commit, captured with
// gh 2.98.0, which labels every line of a successful run's log UNKNOWN STEP.
const unnamedHead = "0e755067f64ef0cceabb7eeb70dacae2b3117e4e"

// unnameSteps rewrites a log the way gh prints it when it cannot name the
// steps: every step column UNKNOWN STEP, and the one byte-order mark at the
// head of each job's log, not one per step.
func unnameSteps(log []byte) []byte {
	lines := strings.Split(string(log), "\n")
	previous := ""
	for i, line := range lines {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		if fields[0] == previous {
			fields[2] = strings.TrimPrefix(fields[2], "\uFEFF")
		}
		previous = fields[0]
		lines[i] = fields[0] + "\t" + unknownStep + "\t" + fields[2]
	}
	return []byte(strings.Join(lines, "\n"))
}

// A log whose step names gh no longer prints is cut into steps at the
// `##[group]Run ` marker that opens each workflow step, job by job, and
// observed exactly as the same log with names.
func TestFoundationObserveUnnamedSteps(t *testing.T) {
	for _, c := range []struct{ name, role, candidate string }{
		{"head", RoleHead, prHead}, {"red", RoleRedControl, redHead},
		{"head-split", RoleHead, prHead}, {"red-split", RoleRedControl, redHead},
	} {
		named := observeRun(t, c.role, c.candidate, c.name)
		capture := captureRun(t, c.name)
		capture.Log = unnameSteps(capture.Log)
		unnamed, err := ObserveFoundationRun(c.role, c.candidate, capture)
		if err != nil {
			t.Fatalf("BEHAVIORAL_RED: %s run without step names refused: %v", c.name, err)
		}
		if !reflect.DeepEqual(unnamed, named) {
			t.Errorf("BEHAVIORAL_RED: %s run without step names observed as %+v, with names as %+v", c.name, unnamed, named)
		}
	}
	run := observeRun(t, RoleHead, unnamedHead, "head-unnamed")
	j := run.Jobs[0]
	targets := strings.Fields(j.Env["LZ_TARGETS"])
	names := []string{}
	for _, target := range j.Targets {
		names = append(names, target.Name)
		if target.Status != "ok" || target.Discovered == 0 {
			t.Errorf("BEHAVIORAL_RED: target observed as %+v", target)
		}
	}
	for name, ok := range map[string]bool{
		"token":   slices.Equal(j.Permissions, tokenPermissions),
		"image":   j.RunnerImage == "ubuntu-24.04",
		"head":    j.FetchedHead == unnamedHead && j.Env["LZ_HEAD"] == unnamedHead,
		"env":     len(j.Env) == 9 && j.Env["LZ_IMAGE"] == RuntimeImage,
		"targets": len(targets) == 17 && slices.Equal(names, targets) && j.TargetsDone == 17,
	} {
		if !ok {
			t.Errorf("BEHAVIORAL_RED: unnamed run observed wrongly: %s: %+v", name, run)
		}
	}
}

// Markers and steps that do not pair one to one are refused, never guessed
// at; so is a log that names some steps and not others.
func TestFoundationObserveUnnamedStepsRefused(t *testing.T) {
	marker := "Z ##[group]Run set -euo pipefail"
	for name, change := range map[string]func(*RunCapture){
		"a step marker missing": func(c *RunCapture) {
			c.Log = []byte(strings.Replace(string(c.Log), marker, "Z set -euo pipefail", 1))
		},
		"a marker in a step's output": func(c *RunCapture) {
			c.Log = []byte(strings.Replace(string(c.Log), "Z LZ_TARGET_OK docs:check", "Z ##[group]Run docs:check", 1))
		},
		"a step without a marker": func(c *RunCapture) {
			c.Jobs = []byte(strings.Replace(string(c.Jobs), `{"name":"Complete job"`,
				`{"name":"Post run","status":"completed","conclusion":"success"},{"name":"Complete job"`, 1))
		},
		// A skipped step prints no marker, so it pairs with none: a marker in
		// the checks' output cannot stand in for it.
		"a skipped step and a marker in a step's output": func(c *RunCapture) {
			c.Jobs = []byte(strings.Replace(string(c.Jobs), `{"name":"Install the pinned launcher","status":"completed","conclusion":"success"`,
				`{"name":"Install the pinned launcher","status":"completed","conclusion":"skipped"`, 1))
			c.Log = []byte(strings.Replace(string(c.Log), "21.9946033Z ##[group]Run set -euo pipefail", "21.9946033Z set -euo pipefail", 1))
			c.Log = []byte(strings.Replace(string(c.Log), "Z LZ_TARGET_OK docs:check", "Z ##[group]Run docs:check", 1))
		},
		"no set-up step": func(c *RunCapture) {
			c.Jobs = []byte(strings.Replace(string(c.Jobs), `"name":"Set up job"`, `"name":"Prepare"`, 1))
		},
		"names on some lines": func(c *RunCapture) {
			c.Log = []byte(strings.Replace(string(c.Log), "\t"+unknownStep+"\t", "\tSet up job\t", 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := captureRun(t, "head-unnamed")
			change(&c)
			if _, err := ObserveFoundationRun(RoleHead, unnamedHead, c); err == nil || !strings.HasPrefix(err.Error(), "CAPTURE_LOG: ") {
				t.Errorf("BEHAVIORAL_RED: %s gave %v, want CAPTURE_LOG", name, err)
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

// targetJob and gateJob are a run's job foundation-1 and its gate.
func targetJob(r *FoundationRun) *RunJob { return r.job(splitJob) }
func gateJob(r *FoundationRun) *RunJob   { return r.job(FoundationCheck) }

// withoutJob drops a run's job of that name.
func withoutJob(r *FoundationRun, name string) {
	r.Jobs = slices.DeleteFunc(r.Jobs, func(j RunJob) bool { return j.Name == name })
}

// Each way a run can fail to prove the candidate is refused: a missing role,
// a foreign or unexpected workflow, a stale head or source, other executed
// identities, a run that did not complete green, other jobs than the
// source's, a gate that did not judge every job's success, and omitted,
// zero or skipped checks; a red control must be red in the checks
// themselves, and its gate red for it.
func TestFoundationRunsRejected(t *testing.T) {
	other := strings.Repeat("e", 40)
	head := func(r *FoundationRuns) *FoundationRun { return &r.Runs[0] }
	red := func(r *FoundationRuns) *FoundationRun { return &r.Runs[1] }
	hj := func(r *FoundationRuns) *RunJob { return targetJob(head(r)) }
	hg := func(r *FoundationRuns) *RunJob { return gateJob(head(r)) }
	rj := func(r *FoundationRuns) *RunJob { return targetJob(red(r)) }
	rg := func(r *FoundationRuns) *RunJob { return gateJob(red(r)) }
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
		"repeated run":           {func(r *FoundationRuns) { r.Runs = append(r.Runs, r.Runs[0]) }, "RUN_DUPLICATE"},
		"foreign workflow":       {func(r *FoundationRuns) { head(r).WorkflowPath = ".github/workflows/other.yml" }, "RUN_FOREIGN"},
		"foreign repository":     {func(r *FoundationRuns) { head(r).Repository = "someone/fork" }, "RUN_FOREIGN"},
		"fork head":              {func(r *FoundationRuns) { head(r).HeadRepository = "someone/fork" }, "RUN_FOREIGN"},
		"manual event":           {func(r *FoundationRuns) { head(r).Event = "workflow_dispatch" }, "RUN_FOREIGN"},
		"privileged event":       {func(r *FoundationRuns) { head(r).Event = "pull_request_target" }, "RUN_FOREIGN"},
		"other steps":            {func(r *FoundationRuns) { hj(r).Steps[2].Name = "Run actions/checkout" }, "RUN_STEPS"},
		"extra step":             {func(r *FoundationRuns) { hj(r).Steps = append(hj(r).Steps, RunStep{"Post", "success"}) }, "RUN_STEPS"},
		"other gate steps":       {func(r *FoundationRuns) { hg(r).Steps[1].Name = "Run actions/checkout" }, "RUN_STEPS"},
		"stale head":             {func(r *FoundationRuns) { head(r).Candidate = other }, "RUN_STALE_HEAD"},
		"other head fetched":     {func(r *FoundationRuns) { hj(r).FetchedHead = other }, "RUN_STALE_HEAD"},
		"other head in env":      {func(r *FoundationRuns) { hj(r).Env["LZ_HEAD"] = other }, "RUN_STALE_HEAD"},
		"candidate not a SHA":    {func(r *FoundationRuns) { head(r).Candidate = "main"; head(r).HeadSHA = "main" }, "RUN_STALE_HEAD"},
		"stale source":           {func(r *FoundationRuns) { head(r).SourceBlob = other }, "RUN_STALE_SOURCE"},
		"stale workflow":         {func(r *FoundationRuns) { head(r).WorkflowBlob = other }, "RUN_STALE_SOURCE"},
		"stale red source":       {func(r *FoundationRuns) { red(r).SourceBlob = other }, "RUN_STALE_SOURCE"},
		"other image":            {func(r *FoundationRuns) { hj(r).Env["LZ_MANIFEST"] = "sha256:" + strings.Repeat("e", 64) }, "RUN_IDENTITY"},
		"other layer":            {func(r *FoundationRuns) { hj(r).Env["LZ_LAYER"] = "sha256:" + strings.Repeat("e", 64) }, "RUN_IDENTITY"},
		"other entry":            {func(r *FoundationRuns) { hj(r).Env["LZ_ENTRY_SHA256"] = strings.Repeat("e", 64) }, "RUN_IDENTITY"},
		"other launcher":         {func(r *FoundationRuns) { hj(r).Env["LZ_BWRAP_SHA256"] = strings.Repeat("e", 64) }, "RUN_IDENTITY"},
		"other targets":          {func(r *FoundationRuns) { hj(r).Env["LZ_TARGETS"] = "test:reports" }, "RUN_IDENTITY"},
		"extra environment":      {func(r *FoundationRuns) { hj(r).Env["BASH_ENV"] = "/tmp/x" }, "RUN_IDENTITY"},
		"missing environment":    {func(r *FoundationRuns) { delete(hj(r).Env, "LZ_IMAGE") }, "RUN_IDENTITY"},
		"extra gate environment": {func(r *FoundationRuns) { hg(r).Env["BASH_ENV"] = "/tmp/x" }, "RUN_IDENTITY"},
		"widened token":          {func(r *FoundationRuns) { hj(r).Permissions = append(hj(r).Permissions, "Contents: write") }, "RUN_IDENTITY"},
		"widened gate token":     {func(r *FoundationRuns) { hg(r).Permissions = append(hg(r).Permissions, "Contents: write") }, "RUN_IDENTITY"},
		"self-hosted runner":     {func(r *FoundationRuns) { hj(r).RunnerLabels = []string{"self-hosted"} }, "RUN_IDENTITY"},
		"self-hosted gate":       {func(r *FoundationRuns) { hg(r).RunnerLabels = []string{"self-hosted"} }, "RUN_IDENTITY"},
		"other runner group":     {func(r *FoundationRuns) { hj(r).RunnerGroup = "Default" }, "RUN_IDENTITY"},
		"other runner image":     {func(r *FoundationRuns) { hj(r).RunnerImage = "ubuntu-22.04" }, "RUN_IDENTITY"},
		"another check run":      {func(r *FoundationRuns) { head(r).CheckRunID = hj(r).CheckRunID }, "RUN_IDENTITY"},
		"incomplete run":         {func(r *FoundationRuns) { head(r).Status = "in_progress"; head(r).Conclusion = "" }, "RUN_INCOMPLETE"},
		"cancelled run":          {func(r *FoundationRuns) { head(r).Conclusion = "cancelled" }, "RUN_CANCELLED"},
		"cancelled job":          {func(r *FoundationRuns) { hj(r).Conclusion = "cancelled" }, "RUN_CANCELLED"},
		"skipped run":            {func(r *FoundationRuns) { head(r).Conclusion = "skipped" }, "RUN_SKIPPED"},
		"skipped job":            {func(r *FoundationRuns) { hj(r).Conclusion = "skipped" }, "RUN_SKIPPED"},
		"skipped step":           {func(r *FoundationRuns) { hj(r).Steps[5].Conclusion = "skipped" }, "RUN_SKIPPED"},
		"failed run":             {func(r *FoundationRuns) { head(r).Conclusion = "failure" }, "RUN_FAILED"},
		"timed out run":          {func(r *FoundationRuns) { head(r).Conclusion = "timed_out" }, "RUN_FAILED"},
		"failed target":          {func(r *FoundationRuns) { hj(r).Targets[2].Status = "failed" }, "RUN_FAILED"},
		"failed gate":            {func(r *FoundationRuns) { hg(r).Conclusion = "failure" }, "RUN_FAILED"},
		"cancelled gate":         {func(r *FoundationRuns) { hg(r).Conclusion = "cancelled" }, "RUN_CANCELLED"},
		"skipped gate":           {func(r *FoundationRuns) { hg(r).Conclusion = "skipped" }, "RUN_SKIPPED"},
		"gate passed a failure":  {func(r *FoundationRuns) { hg(r).Env["LZ_JOB_RESULTS"] = splitJob + "=failure" }, "RUN_FAILED"},
		"gate passed a skip":     {func(r *FoundationRuns) { hg(r).Env["LZ_JOB_RESULTS"] = splitJob + "=skipped" }, "RUN_FAILED"},
		"gate judged nothing":    {func(r *FoundationRuns) { delete(hg(r).Env, "LZ_JOB_RESULTS") }, "RUN_FAILED"},
		"no gate":                {func(r *FoundationRuns) { withoutJob(head(r), FoundationCheck) }, "RUN_JOBS"},
		"no target job":          {func(r *FoundationRuns) { withoutJob(head(r), splitJob) }, "RUN_JOBS"},
		"an extra job": {func(r *FoundationRuns) {
			extra := cloneJob(*hj(r))
			extra.Name = "foundation-9"
			head(r).Jobs = append(head(r).Jobs, extra)
		}, "RUN_JOBS"},
		"omitted check":         {func(r *FoundationRuns) { hj(r).Targets = slices.Delete(hj(r).Targets, 4, 5) }, "RUN_COUNT"},
		"reordered checks":      {func(r *FoundationRuns) { t := hj(r).Targets; t[0], t[1] = t[1], t[0] }, "RUN_COUNT"},
		"wrong done count":      {func(r *FoundationRuns) { hj(r).TargetsDone = 8 }, "RUN_COUNT"},
		"zero checks":           {func(r *FoundationRuns) { hj(r).Targets = []RunTarget{}; hj(r).TargetsDone = 0 }, "RUN_COUNT"},
		"zero discovery":        {func(r *FoundationRuns) { hj(r).Targets[2].Discovered = 0 }, "RUN_ZERO_DISCOVERY"},
		"package without tests": {func(r *FoundationRuns) { hj(r).Targets[3].Empty = 1 }, "RUN_ZERO_DISCOVERY"},
		"red control passed": {func(r *FoundationRuns) {
			red(r).Conclusion = "success"
			rj(r).Conclusion = "success"
			rg(r).Conclusion = "success"
		}, "RUN_NOT_RED"},
		"red before the checks": {func(r *FoundationRuns) {
			rj(r).Steps[3].Conclusion = "failure"
			rj(r).Steps[5].Conclusion = "skipped"
		}, "RUN_NOT_RED"},
		"red without a target":   {func(r *FoundationRuns) { rj(r).Targets = rj(r).Targets[:3] }, "RUN_NOT_RED"},
		"red after all targets":  {func(r *FoundationRuns) { rj(r).TargetsDone = 9 }, "RUN_NOT_RED"},
		"red in an unlisted one": {func(r *FoundationRuns) { rj(r).Targets[3].Name = "test:ghost" }, "RUN_NOT_RED"},
		"cancelled red control":  {func(r *FoundationRuns) { red(r).Conclusion = "cancelled" }, "RUN_CANCELLED"},
		"stale red head":         {func(r *FoundationRuns) { red(r).Candidate = other }, "RUN_STALE_HEAD"},
		"red without assertions": {func(r *FoundationRuns) { rj(r).Targets[3].Failures = 0 }, "RUN_NOT_RED"},
		"red gate passed": {func(r *FoundationRuns) {
			rg(r).Conclusion = "success"
			rg(r).Steps[1].Conclusion = "success"
		}, "RUN_NOT_RED"},
		"red gate cancelled":     {func(r *FoundationRuns) { rg(r).Conclusion = "cancelled" }, "RUN_NOT_RED"},
		"red gate saw a success": {func(r *FoundationRuns) { rg(r).Env["LZ_JOB_RESULTS"] = splitJob + "=success" }, "RUN_NOT_RED"},
		"red without a gate":     {func(r *FoundationRuns) { withoutJob(red(r), FoundationCheck) }, "RUN_JOBS"},
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

// cloneJob is a deep copy of a run's job.
func cloneJob(j RunJob) RunJob {
	j.RunnerLabels = slices.Clone(j.RunnerLabels)
	j.Permissions = slices.Clone(j.Permissions)
	j.Steps = slices.Clone(j.Steps)
	j.Env = maps.Clone(j.Env)
	j.Targets = slices.Clone(j.Targets)
	return j
}

// twoJobs is the recorded runs under a source whose first at targets run in
// foundation-1 and the rest in foundation-2, each run moved to that source's
// blobs: the head run's job split at at, and, at 4, the red control's
// failing job (its four targets) beside the head run's foundation-2 at the
// red head, which passed. The gates saw those results.
func twoJobs(t *testing.T, minutes, at int) ([]byte, FoundationRuns) {
	t.Helper()
	data, runs := recordedRuns(t)
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value["timeout_minutes"] = minutes
	value["jobs"] = []any{job("foundation-1", foundationTargets[:at]...), job("foundation-2", foundationTargets[at:]...)}
	source := encode(t, value)
	parsed, err := ParseFoundationSource(source)
	if err != nil {
		t.Fatal(err)
	}
	head, red := &runs.Runs[0], &runs.Runs[1]
	whole := cloneJob(*targetJob(head))
	first, second := cloneJob(whole), cloneJob(whole)
	first.Targets, first.TargetsDone, first.Env["LZ_TARGETS"] = whole.Targets[:at], at, strings.Join(foundationTargets[:at], " ")
	second.Name, second.CheckRunID = "foundation-2", whole.CheckRunID+2
	second.Targets, second.TargetsDone, second.Env["LZ_TARGETS"] = whole.Targets[at:], len(whole.Targets)-at, strings.Join(foundationTargets[at:], " ")
	head.Jobs = []RunJob{cloneJob(*gateJob(head)), first, second}
	gateJob(head).Env["LZ_JOB_RESULTS"] = "foundation-1=success foundation-2=success"
	if at == 4 {
		passed := cloneJob(second)
		passed.FetchedHead, passed.Env["LZ_HEAD"] = red.HeadSHA, red.HeadSHA
		failed := cloneJob(*targetJob(red))
		failed.Env["LZ_TARGETS"] = strings.Join(foundationTargets[:at], " ")
		red.Jobs = []RunJob{cloneJob(*gateJob(red)), failed, passed}
		gateJob(red).Env["LZ_JOB_RESULTS"] = "foundation-1=failure foundation-2=success"
	}
	for i := range runs.Runs {
		runs.Runs[i].SourceBlob = gitBlob(source)
		runs.Runs[i].WorkflowBlob = gitBlob([]byte(RenderFoundationWorkflow(parsed)))
	}
	return source, runs
}

// spendJob sets a job's seconds so that they add up to total: the first
// target takes what the others, one second each, leave, and no time is spent
// outside the targets.
func spendJob(j *RunJob, total int) {
	j.OutsideSeconds = 0
	for i := range j.Targets {
		j.Targets[i].Seconds = 1
	}
	j.Targets[0].Seconds = total - (len(j.Targets) - 1)
}

// With the targets in two jobs, each job is judged on its own: its order,
// count and budget. Two jobs that each fit the timeout pass even where their
// sum would not have fitted one job (the point of D126 part A); one over it
// is refused by name; a target moved to the other job is refused; a red
// control whose other job passed or also failed in its checks is red, one
// whose other job was cancelled, or whose gate misread it, is not.
func TestFoundationRunsTwoJobs(t *testing.T) {
	split := func(r *FoundationRuns, run int, name string) *RunJob { return r.Runs[run].job(name) }
	for name, c := range map[string]struct {
		at     int
		change func(*FoundationRuns)
		want   []string
	}{
		"split after five":        {5, func(*FoundationRuns) {}, nil},
		"the red control's split": {4, func(*FoundationRuns) {}, nil},
		"each job at the budget, together over it": {4, func(r *FoundationRuns) {
			spendJob(split(r, 0, "foundation-1"), 600-240)
			spendJob(split(r, 0, "foundation-2"), 600-240)
		}, nil},
		"one job a second over": {4, func(r *FoundationRuns) {
			spendJob(split(r, 0, "foundation-1"), 100)
			spendJob(split(r, 0, "foundation-2"), 600-240+1)
		}, []string{"RUN_OVER_BUDGET"}},
		"a target moved to the other job": {4, func(r *FoundationRuns) {
			first, second := split(r, 0, "foundation-1"), split(r, 0, "foundation-2")
			first.Targets, first.TargetsDone = append(first.Targets, second.Targets[0]), 5
			second.Targets, second.TargetsDone = second.Targets[1:], 4
		}, []string{"RUN_COUNT"}},
		"a target run twice": {4, func(r *FoundationRuns) {
			second := split(r, 0, "foundation-2")
			second.Targets, second.TargetsDone = append(second.Targets, second.Targets[0]), 6
		}, []string{"RUN_COUNT"}},
		"the other job skipped": {4, func(r *FoundationRuns) { split(r, 0, "foundation-2").Conclusion = "skipped" }, []string{"RUN_SKIPPED"}},
		"the gate saw one job skipped": {4, func(r *FoundationRuns) {
			gateJob(&r.Runs[0]).Env["LZ_JOB_RESULTS"] = "foundation-1=success foundation-2=skipped"
		}, []string{"RUN_FAILED"}},
		"red with both jobs red": {4, func(r *FoundationRuns) {
			second := split(r, 1, "foundation-2")
			second.Conclusion, second.Steps[5].Conclusion, second.TargetsDone = "failure", "failure", -1
			second.Targets[0].Status, second.Targets[0].Failures = "failed", 1
			second.Targets = second.Targets[:1]
			gateJob(&r.Runs[1]).Env["LZ_JOB_RESULTS"] = "foundation-1=failure foundation-2=failure"
		}, nil},
		"red with the other job cancelled": {4, func(r *FoundationRuns) {
			split(r, 1, "foundation-2").Conclusion = "cancelled"
			gateJob(&r.Runs[1]).Env["LZ_JOB_RESULTS"] = "foundation-1=failure foundation-2=cancelled"
		}, []string{"RUN_NOT_RED"}},
		"red whose gate misread the other job": {4, func(r *FoundationRuns) {
			gateJob(&r.Runs[1]).Env["LZ_JOB_RESULTS"] = "foundation-1=failure foundation-2=failure"
		}, []string{"RUN_NOT_RED"}},
	} {
		t.Run(name, func(t *testing.T) {
			source, runs := twoJobs(t, 10, c.at)
			if c.at != 4 {
				// Only the head run splits there; the red control is judged
				// in the cases split at 4.
				runs.Runs = runs.Runs[:1]
			}
			c.change(&runs)
			findings := JudgeFoundationRuns(source, encode(t, runs), treeDigests())
			got := ruleSet(findings)
			if c.at != 4 {
				got = slices.DeleteFunc(got, func(rule string) bool { return rule == "RUN_MISSING" })
			}
			if len(got) == 0 {
				got = nil
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v (%+v), want %v", name, got, findings, c.want)
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
		"a one-job record": {source, []byte(strings.Replace(string(record), `"jobs":`, `"job_conclusion":"success","jobs":`, 1)), "RUNS_SYNTAX"},
		"null jobs":        {source, regexp.MustCompile(`"jobs":\[.*?\],"check_digests"`).ReplaceAll(record, []byte(`"jobs":null,"check_digests"`)), "RUNS_SYNTAX"},
		"malformed source": {[]byte("{"), record, "SOURCE_SYNTAX"},
		"unpinned source":  {[]byte(strings.Replace(string(source), `"ubuntu-24.04"`, `"self-hosted"`, 1)), record, "SOURCE_PIN"},
	} {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(c.record, record) && bytes.Equal(c.source, source) {
				t.Fatal("fixture unchanged")
			}
			if got := ruleSet(JudgeFoundationRuns(c.source, c.record, treeDigests())); !slices.Contains(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v, want %s", name, got, c.want)
			}
		})
	}
}

// Each target's seconds are read from the job log's own timestamps: from the
// checks step's first line, or the previous target's end, to the target's
// LZ_TARGET_OK or Task failure line, rounded up. The values are an
// independent reading of the captured logs (evidence/T055.md).
func TestFoundationObserveSeconds(t *testing.T) {
	for _, c := range []struct {
		name, role, candidate string
		seconds               []int
		outside               int
	}{
		{"head", RoleHead, prHead, []int{2, 1, 10, 13, 10, 14, 14, 19, 13}, 11},
		{"red", RoleRedControl, redHead, []int{2, 1, 10, 13}, 12},
	} {
		run := observeRun(t, c.role, c.candidate, c.name)
		got := []int{}
		for _, target := range run.Jobs[0].Targets {
			got = append(got, target.Seconds)
		}
		if !slices.Equal(got, c.seconds) {
			t.Errorf("BEHAVIORAL_RED: %s run observed seconds %v, want %v", c.name, got, c.seconds)
		}
		// The job's time outside its targets: from the log's first line to the
		// checks step's, and from the last target's end to the log's last line.
		if run.Jobs[0].OutsideSeconds != c.outside {
			t.Errorf("BEHAVIORAL_RED: %s run observed %d s outside its targets, want %d", c.name, run.Jobs[0].OutsideSeconds, c.outside)
		}
	}
}

// Time after the last target counts too: a job whose last line comes a
// minute later has spent a minute more outside its targets.
func TestFoundationObserveSecondsAfterTargets(t *testing.T) {
	c := captureRun(t, "head")
	c.Log = []byte(strings.Replace(string(c.Log), "2026-10-05T07:39:09.5009198Z Cleaning up", "2026-10-05T07:40:09.5009198Z Cleaning up", 1))
	run, err := ObserveFoundationRun(RoleHead, prHead, c)
	if err != nil {
		t.Fatal(err)
	}
	if run.Jobs[0].OutsideSeconds != 71 {
		t.Errorf("BEHAVIORAL_RED: %d s outside the targets, want 71 (11 + 60)", run.Jobs[0].OutsideSeconds)
	}
}

// A line whose time cannot be read, or a target that ends before it began,
// is refused: a duration is observed, never guessed.
func TestFoundationObserveSecondsRefused(t *testing.T) {
	const reports = "2026-10-05T07:37:48.6844946Z LZ_TARGET_OK test:reports"
	for name, log := range map[string]func(string) string{
		"a target line without a time": func(l string) string {
			return strings.Replace(l, reports, "2026-10-05 07:37:48 LZ_TARGET_OK test:reports", 1)
		},
		"a set-up line without a time": func(l string) string {
			return strings.Replace(l, "Z Current runner version:", " Current runner version:", 1)
		},
		"a target ending before it began": func(l string) string {
			return strings.Replace(l, reports, "2026-10-05T07:30:00.0000000Z LZ_TARGET_OK test:reports", 1)
		},
		"a last line before the last target's end": func(l string) string {
			return strings.Replace(l, "2026-10-05T07:39:09.5009198Z Cleaning up", "2026-10-05T07:37:30.0000000Z Cleaning up", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := captureRun(t, "head")
			changed := log(string(c.Log))
			if changed == string(c.Log) {
				t.Fatal("fixture line not found")
			}
			c.Log = []byte(changed)
			if _, err := ObserveFoundationRun(RoleHead, prHead, c); err == nil || !strings.HasPrefix(err.Error(), "CAPTURE_LOG: ") {
				t.Errorf("BEHAVIORAL_RED: %s gave %v, want CAPTURE_LOG", name, err)
			}
		})
	}
}

// withTimeout is the recorded runs under a source whose job timeout is
// minutes, each run moved to that source's blobs, so only the budget differs.
func withTimeout(t *testing.T, minutes int) ([]byte, FoundationRuns) {
	t.Helper()
	source, runs := recordedRuns(t)
	changed := strings.Replace(string(source), `"timeout_minutes": 10`, fmt.Sprintf(`"timeout_minutes": %d`, minutes), 1)
	parsed, err := ParseFoundationSource([]byte(changed))
	if err != nil || parsed.TimeoutMinutes != minutes {
		t.Fatalf("timeout not set: %v", err)
	}
	for i := range runs.Runs {
		runs.Runs[i].SourceBlob = gitBlob([]byte(changed))
		runs.Runs[i].WorkflowBlob = gitBlob([]byte(RenderFoundationWorkflow(parsed)))
	}
	return []byte(changed), runs
}

// spend sets the head run's target job's seconds to add up to total.
func spend(r *FoundationRuns, total int) { spendJob(targetJob(&r.Runs[0]), total) }

// The head run's job's targets plus one child deadline (240 s) must fit the
// job's timeout: at it the run passes, a second over it is refused, at 10
// minutes (the fixture's source) and at 15 (D125, and each job's again since
// D126 part A); 20, the withdrawn bridge, is refused as a source. A target
// with no time, or a negative time that would hide another's, is refused; the
// red control stops early by design and is not budgeted; nor is the gate,
// which waits for the jobs before it starts.
func TestFoundationRunsBudget(t *testing.T) {
	for name, c := range map[string]struct {
		minutes int
		change  func(*FoundationRuns)
		want    []string
	}{
		"the captured run":           {10, func(*FoundationRuns) {}, nil},
		"at 10 minutes":              {10, func(r *FoundationRuns) { spend(r, 600-240) }, nil},
		"a second over 10 minutes":   {10, func(r *FoundationRuns) { spend(r, 600-240+1) }, []string{"RUN_OVER_BUDGET"}},
		"at 15 minutes":              {15, func(r *FoundationRuns) { spend(r, 900-240) }, nil},
		"a second over 15 minutes":   {15, func(r *FoundationRuns) { spend(r, 900-240+1) }, []string{"RUN_OVER_BUDGET"}},
		"over 10 fits 15":            {15, func(r *FoundationRuns) { spend(r, 600-240+1) }, nil},
		"the withdrawn bridge":       {20, func(r *FoundationRuns) { spend(r, 900-240+1) }, []string{"SOURCE_PIN"}},
		"one target over the budget": {15, func(r *FoundationRuns) { targetJob(&r.Runs[0]).Targets[8].Seconds = 900 }, []string{"RUN_OVER_BUDGET"}},
		"an untimed target":          {10, func(r *FoundationRuns) { targetJob(&r.Runs[0]).Targets[2].Seconds = 0 }, []string{"RUN_UNTIMED"}},
		"a negative time hides one": {10, func(r *FoundationRuns) {
			spend(r, 360)
			targetJob(&r.Runs[0]).Targets[0].Seconds += 100
			targetJob(&r.Runs[0]).Targets[1].Seconds = -99
		}, []string{"RUN_UNTIMED"}},
		"a slow red control": {10, func(r *FoundationRuns) {
			targetJob(&r.Runs[1]).Targets[0].Seconds = 10000
			targetJob(&r.Runs[1]).OutsideSeconds = 10000
		}, nil},
		"a slow gate": {10, func(r *FoundationRuns) { gateJob(&r.Runs[0]).OutsideSeconds = 10000 }, nil},
		// The timeout covers the whole job, not only its targets.
		"time outside the targets counts": {10, func(r *FoundationRuns) {
			spend(r, 600-240-2)
			targetJob(&r.Runs[0]).OutsideSeconds = 3
		}, []string{"RUN_OVER_BUDGET"}},
		"time outside the targets at the budget": {10, func(r *FoundationRuns) {
			spend(r, 600-240-2)
			targetJob(&r.Runs[0]).OutsideSeconds = 2
		}, nil},
		"negative time outside the targets": {10, func(r *FoundationRuns) {
			spend(r, 360)
			targetJob(&r.Runs[0]).Targets[0].Seconds += 50
			targetJob(&r.Runs[0]).OutsideSeconds = -50
		}, []string{"RUN_UNTIMED"}},
		// Two such times wrap an int sum to -2.
		"times that would wrap the sum": {15, func(r *FoundationRuns) {
			j := targetJob(&r.Runs[0])
			j.Targets[0].Seconds, j.Targets[1].Seconds = math.MaxInt, math.MaxInt
		}, []string{"RUN_OVER_BUDGET"}},
	} {
		t.Run(name, func(t *testing.T) {
			source, runs := withTimeout(t, c.minutes)
			c.change(&runs)
			findings := JudgeFoundationRuns(source, encode(t, runs), treeDigests())
			if got := ruleSet(findings); !slices.Equal(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v (%+v), want %v", name, got, findings, c.want)
			}
		})
	}
}

// The refusal names the job and says what it spent against what it has.
func TestFoundationRunsBudgetDetail(t *testing.T) {
	source, runs := withTimeout(t, 15)
	spend(&runs, 700)
	targetJob(&runs.Runs[0]).OutsideSeconds = 12
	findings := JudgeFoundationRuns(source, encode(t, runs), treeDigests())
	want := "job foundation-1: targets took 700 s and the job 12 s outside them; with one 240 s child deadline that is 952 s, over the 15-minute job timeout (900 s)"
	if len(findings) != 1 || findings[0].Detail != want {
		t.Errorf("BEHAVIORAL_RED: refusal %+v, want detail %q", findings, want)
	}
}

// A record written before targets were timed does not decode: its runs are
// no evidence of the job's time.
func TestFoundationRunsUntimedRecord(t *testing.T) {
	source, runs := recordedRuns(t)
	record := regexp.MustCompile(`,"seconds":-?\d+`).ReplaceAllString(string(encode(t, runs)), "")
	if strings.Contains(record, `"seconds"`) {
		t.Fatal("seconds not removed")
	}
	if got := ruleSet(JudgeFoundationRuns(source, []byte(record), treeDigests())); !slices.Equal(got, []string{"RUNS_SYNTAX"}) {
		t.Errorf("BEHAVIORAL_RED: untimed record gave %v, want [RUNS_SYNTAX]", got)
	}
}
