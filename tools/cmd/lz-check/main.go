// lz-check checks a feature's traceability and a path's definition of done
// against the check registry, harness/checks.yaml.
//
//	lz-check [-root <repo>] specs <spec-dir>
//	lz-check [-root <repo>] [-evidence <dir>] dod <path>
//	lz-check [-root <repo>] deps
//	lz-check [-root <repo>] select <changed-path>
//	lz-check [-root <repo>] [-tofu <bin>] [-tflint <bin>] lint <module-dir>
//	lz-check [-root <repo>] [-tofu <bin>] unit <module-dir>
//	lz-check [-root <repo>] [-tofu <bin>] [-tflint <bin>] slice
//	lz-check [-root <repo>] ci-workflow
//	lz-check [-root <repo>] ci-source <spec-dir>...
//	lz-check [-root <repo>] ci-runs
//	lz-check ci-observe (<role> <candidate> <capture-dir>)...
//
// specs follows requirement → ADR → paths → check → evidence for every task in
// <spec-dir>/tasks.md and exits 1 on any broken link. dod judges each registered
// check whose scope covers <path> from <evidence>/<check-id>.json and exits 1
// unless every one passes or is a docs exemption. deps checks the module graph
// of ADR-0002 and exits 1 on any finding; select prints the directories a
// changed path requires to be checked. lint runs the pinned fmt, init, validate
// and TFLint clauses on one module directory and exits 1 unless all pass.
// ci-workflow prints the foundation workflow rendered from its approved source;
// ci-source judges that source against the registry and the tasks.md of each
// <spec-dir> (spec 001 among them; a creator NNN/Txxx resolves against spec
// NNN) and the committed workflow against the rendered one, and exits 1 on any
// finding. ci-runs judges the recorded GitHub runs in
// pipelines/github/foundation-runs.json against that source and exits 1 on
// any finding or an absent record; ci-observe prints that record from
// captured GitHub output. Usage and input errors exit 2.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/report"
)

var (
	adrFile = regexp.MustCompile(`^(\d{4})-.+\.md$`)
	// specNumber is the leading number of a spec directory's name, which
	// selects the spec's registry keys.
	specNumber = regexp.MustCompile(`^(\d{3})-`)
)

func run(args []string, out io.Writer) int {
	flags := flag.NewFlagSet("lz-check", flag.ContinueOnError)
	flags.SetOutput(out)
	root := flags.String("root", ".", "repository root")
	evidenceDir := flags.String("evidence", ".local/evidence/checks", "evidence directory, relative to the root")
	tofu := flags.String("tofu", "/tcb/tofu", "pinned tofu binary")
	tflint := flags.String("tflint", "/tcb/tflint", "pinned TFLint binary")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	switch {
	case flags.NArg() == 2 && flags.Arg(0) == "lint":
		tools := checks.StaticTools{Tofu: *tofu, TFLint: *tflint, TFLintConfig: filepath.Join(*root, ".tflint.hcl")}
		return lint(out, *root, flags.Arg(1), tools)
	case flags.NArg() == 2 && flags.Arg(0) == "unit":
		return unit(out, *root, flags.Arg(1), checks.StaticTools{Tofu: *tofu})
	case flags.NArg() == 1 && flags.Arg(0) == "slice":
		tools := checks.StaticTools{Tofu: *tofu, TFLint: *tflint, TFLintConfig: filepath.Join(*root, ".tflint.hcl")}
		return slice(out, *root, tools)
	case flags.NArg() == 1 && flags.Arg(0) == "deps":
		return deps(out, *root)
	case flags.NArg() == 2 && flags.Arg(0) == "select":
		return selectChanged(out, *root, flags.Arg(1))
	case flags.NArg() == 1 && flags.Arg(0) == "ci-workflow":
		return ciWorkflow(out, *root)
	case flags.NArg() == 1 && flags.Arg(0) == "ci-runs":
		return ciRuns(out, *root)
	case flags.NArg() > 1 && (flags.NArg()-1)%3 == 0 && flags.Arg(0) == "ci-observe":
		return ciObserve(out, flags.Args()[1:])
	case flags.NArg() >= 2 && flags.Arg(0) == "ci-source":
		// One or more spec dirs; judged with the registry below.
	case flags.NArg() != 2 || flags.Arg(0) == "deps":
		fmt.Fprintln(out, "usage: lz-check [-root dir] specs <spec-dir> | [-evidence dir] dod <path> | deps | select <path> | lint <dir> | unit <dir> | slice | ci-workflow | ci-source <spec-dir>... | ci-runs | ci-observe (<role> <candidate> <capture-dir>)...")
		return 2
	}
	registry, err := loadRegistry(*root)
	if err != nil {
		fmt.Fprintln(out, err)
		return 2
	}
	switch flags.Arg(0) {
	case "specs":
		return specs(out, *root, flags.Arg(1), registry)
	case "dod":
		return dod(out, *root, filepath.Join(*root, *evidenceDir), flags.Arg(1), registry)
	case "ci-source":
		return ciSource(out, *root, flags.Args()[1:], registry)
	}
	fmt.Fprintf(out, "unknown command %q\n", flags.Arg(0))
	return 2
}

func loadRegistry(root string) (checks.Registry, error) {
	data, err := os.ReadFile(filepath.Join(root, "harness/checks.yaml"))
	if err != nil {
		return checks.Registry{}, fmt.Errorf("REGISTRY_MISSING: %w", err)
	}
	return checks.ParseRegistry(data)
}

// specDirNumber returns a spec directory's three-digit number.
func specDirNumber(out io.Writer, dir string) (string, bool) {
	number := specNumber.FindStringSubmatch(filepath.Base(filepath.Clean(dir)))
	if number == nil {
		fmt.Fprintf(out, "SPEC_NUMBER: %s does not start with a three-digit spec number\n", dir)
		return "", false
	}
	return number[1], true
}

// specTasks reads and parses a spec directory's tasks.md.
func specTasks(out io.Writer, root, dir string) ([]checks.Task, bool) {
	tasksText, err := os.ReadFile(filepath.Join(root, dir, "tasks.md"))
	if err != nil {
		fmt.Fprintln(out, "TASKS_MISSING:", err)
		return nil, false
	}
	tasks, err := checks.ParseTasks(string(tasksText))
	if err != nil {
		fmt.Fprintln(out, err)
		return nil, false
	}
	return tasks, true
}

func specs(out io.Writer, root, dir string, registry checks.Registry) int {
	number, ok := specDirNumber(out, dir)
	if !ok {
		return 2
	}
	specText, err := os.ReadFile(filepath.Join(root, dir, "spec.md"))
	if err != nil {
		fmt.Fprintln(out, "SPEC_MISSING:", err)
		return 2
	}
	requirements := checks.ParseSpec(string(specText))
	if len(requirements) == 0 {
		fmt.Fprintf(out, "NO_REQUIREMENTS: %s/spec.md defines no FR or SC\n", dir)
		return 2
	}
	tasks, ok := specTasks(out, root, dir)
	if !ok {
		return 2
	}
	entries, err := os.ReadDir(filepath.Join(root, "docs/adr"))
	if err != nil {
		fmt.Fprintln(out, "ADR_INDEX_MISSING:", err)
		return 2
	}
	var adrs []string
	for _, e := range entries {
		if m := adrFile.FindStringSubmatch(e.Name()); m != nil && e.Type().IsRegular() {
			adrs = append(adrs, m[1])
		}
	}
	findings := checks.CheckTrace(checks.Trace{Spec: number, Requirements: requirements, Tasks: tasks, Registry: registry, ADRs: adrs})
	for _, f := range findings {
		fmt.Fprintf(out, "%s %s: %s\n", f.Rule, f.Subject, f.Detail)
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "TRACE_FAIL %s findings=%d\n", dir, len(findings))
		return 1
	}
	fmt.Fprintf(out, "TRACE_OK %s requirements=%d tasks=%d checks=%d\n", dir, len(requirements), len(tasks), checks.SpecChecks(number, registry))
	return 0
}

func dod(out io.Writer, root, evidenceDir, target string, registry checks.Registry) int {
	evidence := map[string]checks.Evidence{}
	for _, c := range registry.Checks {
		data, err := os.ReadFile(filepath.Join(evidenceDir, c.ID+".json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var e checks.Evidence
		if err == nil {
			err = checks.DecodeStrict(data, &e)
		}
		if err != nil {
			fmt.Fprintf(out, "EVIDENCE_SYNTAX: %s: %v\n", c.ID, err)
			return 2
		}
		evidence[c.ID] = e
	}
	d := checks.EvaluateDoD(registry, root, target, evidence)
	commands := map[string]string{}
	for _, c := range registry.Checks {
		commands[c.ID] = c.Command
	}
	items := append([]checks.Item{}, d.Items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Check < items[j].Check })
	for _, item := range items {
		fmt.Fprintf(out, "%s %s %s (%s)\n", item.Status, item.Check, item.Reason, commands[item.Check])
	}
	switch {
	case len(d.Items) == 0:
		fmt.Fprintf(out, "DOD_FAIL %s: no registered check applies\n", target)
		return 1
	case !d.Pass:
		fmt.Fprintf(out, "DOD_FAIL %s\n", target)
		return 1
	}
	fmt.Fprintf(out, "DOD_PASS %s\n", target)
	return 0
}

// lint runs the static clauses on one module directory of the repository. A
// directory that does not exist yet is not run; a missing tool blocks; both
// exit non-zero, like any failure.
func lint(out io.Writer, root, dir string, tools checks.StaticTools) int {
	clean, code, ok := moduleDir(out, root, dir, "lint", "LINT")
	if !ok {
		return code
	}
	o := checks.RunStatic(tools, filepath.Join(root, clean))
	for _, r := range o.Results {
		printResult(out, r)
	}
	switch status := staticStatus(o); {
	case o.Pass:
		fmt.Fprintf(out, "LINT_PASS %s files=%d\n", clean, len(o.Discovered))
		return 0
	case status == checks.StatusBlocked:
		fmt.Fprintf(out, "LINT_BLOCKED %s\n", clean)
	default:
		fmt.Fprintf(out, "LINT_FAIL %s\n", clean)
	}
	return 1
}

// moduleDir cleans a directory argument of command: one inside the
// repository, or a usage error (exit 2); a directory that does not exist yet
// is not run (exit 1).
func moduleDir(out io.Writer, root, dir, command, prefix string) (clean string, code int, ok bool) {
	clean = filepath.ToSlash(filepath.Clean(dir))
	if filepath.IsAbs(dir) || clean == ".." || strings.HasPrefix(clean, "../") {
		fmt.Fprintf(out, "%s needs a directory inside the repository, not %q\n", command, dir)
		return "", 2, false
	}
	if info, err := os.Stat(filepath.Join(root, clean)); err != nil || !info.IsDir() {
		fmt.Fprintf(out, "%s_NOT_RUN %s: the module directory does not exist yet\n", prefix, clean)
		return "", 1, false
	}
	return clean, 0, true
}

func printResult(out io.Writer, r checks.StaticResult) {
	fmt.Fprintf(out, "%s %s %s\n", r.Status, r.Clause, r.Reason)
	for _, detail := range append(append([]string{}, r.Files...), r.Messages...) {
		fmt.Fprintf(out, "  %s\n", detail)
	}
}

// staticStatus is pass, blocked when only missing tools stand in the way, or
// fail.
func staticStatus(o checks.StaticObservation) string {
	switch {
	case o.Pass:
		return checks.StatusPass
	case !slices.ContainsFunc(o.Results, func(r checks.StaticResult) bool { return r.Status == checks.StatusFail }):
		return checks.StatusBlocked
	}
	return checks.StatusFail
}

// counts is a unit result's test counts from the adapter; tests that never
// ran count zero.
func counts(o checks.UnitObservation) string {
	var r report.Observation
	if o.Report != nil {
		r = *o.Report
	}
	return fmt.Sprintf("tests=%d passed=%d failed=%d errored=%d skipped=%d", r.Discovered, r.Passed, r.Failed, r.Errored, r.Skipped)
}

// unit runs the L1 tests of one module directory of the repository.
func unit(out io.Writer, root, dir string, tools checks.StaticTools) int {
	clean, code, ok := moduleDir(out, root, dir, "unit", "UNIT")
	if !ok {
		return code
	}
	o := checks.RunUnit(tools, filepath.Join(root, clean))
	if o.Init.Clause != "" {
		printResult(out, o.Init)
	}
	if r := o.Report; r != nil {
		fmt.Fprintf(out, "%s report %s %s\n", r.Status, strings.Join(r.Reasons, ","), counts(o))
		for _, d := range r.Diagnostics {
			fmt.Fprintf(out, "  %s: %s: %s\n", d.Severity, d.Summary, d.Detail)
		}
	}
	switch o.Status {
	case checks.StatusPass:
		fmt.Fprintf(out, "UNIT_PASS %s %s\n", clean, counts(o))
		return 0
	case checks.StatusBlocked:
		fmt.Fprintf(out, "UNIT_BLOCKED %s %s\n", clean, o.Reason)
	default:
		fmt.Fprintf(out, "UNIT_FAIL %s %s\n", clean, o.Reason)
	}
	return 1
}

// slice runs lint and unit on every library and stage directory of the
// module graph. A broken graph cannot discover safely and zero directories
// prove nothing; both fail before any tool runs.
func slice(out io.Writer, root string, tools checks.StaticTools) int {
	g, ok := scan(out, root)
	if !ok {
		fmt.Fprintln(out, "SLICE_FAIL DEPENDENCIES")
		return 1
	}
	dirs := checks.SliceDirs(g)
	paths := make([]string, len(dirs))
	for i, d := range dirs {
		paths[i] = filepath.Join(root, d)
	}
	o := checks.RunSlice(tools, paths)
	if o.Reason == "NO_DISCOVERY" {
		fmt.Fprintln(out, "SLICE_FAIL NO_DISCOVERY dirs=0")
		return 1
	}
	for i, e := range o.Entries {
		status := checks.StatusFail
		switch lint := staticStatus(e.Static); {
		case lint == checks.StatusPass && e.Unit.Pass:
			status = checks.StatusPass
		case lint != checks.StatusFail && e.Unit.Status != checks.StatusFail:
			status = checks.StatusBlocked
		}
		fmt.Fprintf(out, "%s %s lint=%s %s %s\n", status, dirs[i], staticStatus(e.Static), counts(e.Unit), e.Unit.Reason)
		for _, r := range e.Static.Results {
			if r.Status != checks.StatusPass {
				fmt.Fprintf(out, "  lint %s %s %s\n", r.Status, r.Clause, r.Reason)
			}
		}
		if e.Unit.Report != nil && len(e.Unit.Report.Reasons) > 0 {
			fmt.Fprintf(out, "  unit %s\n", strings.Join(e.Unit.Report.Reasons, ","))
		}
	}
	if !o.Pass {
		fmt.Fprintf(out, "SLICE_FAIL dirs=%d\n", len(o.Entries))
		return 1
	}
	fmt.Fprintf(out, "SLICE_PASS dirs=%d\n", len(o.Entries))
	return 0
}

// scan reports every dependency finding and whether the graph is usable.
func scan(out io.Writer, root string) (checks.DependencyGraph, bool) {
	g, findings := checks.ScanDependencies(root)
	for _, f := range findings {
		fmt.Fprintf(out, "%s %s: %s\n", f.Rule, f.Subject, f.Detail)
	}
	return g, len(findings) == 0
}

// readFoundationSource reads the approved CI source, reporting its absence as
// an input error.
func readFoundationSource(out io.Writer, root string) ([]byte, bool) {
	data, err := os.ReadFile(filepath.Join(root, checks.FoundationSourcePath))
	if err != nil {
		fmt.Fprintln(out, "SOURCE_MISSING:", err)
		return nil, false
	}
	return data, true
}

func ciWorkflow(out io.Writer, root string) int {
	data, ok := readFoundationSource(out, root)
	if !ok {
		return 2
	}
	source, err := checks.ParseFoundationSource(data)
	if err != nil {
		fmt.Fprintln(out, err)
		return 2
	}
	// A source that fails validation is never rendered: a malformed name
	// could otherwise write itself into the workflow.
	if findings := checks.ValidateFoundationSource(source); len(findings) > 0 {
		for _, f := range findings {
			fmt.Fprintf(out, "%s %s: %s\n", f.Rule, f.Subject, f.Detail)
		}
		return 2
	}
	fmt.Fprint(out, checks.RenderFoundationWorkflow(source))
	return 0
}

// ciSource judges the source and the committed workflow; an absent workflow
// is judged as an empty one, so it is drift, not an input error. A registry
// creator resolves against the tasks of the spec dirs given, by spec number.
func ciSource(out io.Writer, root string, dirs []string, registry checks.Registry) int {
	data, ok := readFoundationSource(out, root)
	if !ok {
		return 2
	}
	tasks := map[string][]checks.Task{}
	for _, dir := range dirs {
		number, ok := specDirNumber(out, dir)
		if !ok {
			return 2
		}
		if _, seen := tasks[number]; seen {
			fmt.Fprintf(out, "SPEC_DUPLICATE: spec %s given twice\n", number)
			return 2
		}
		if tasks[number], ok = specTasks(out, root, dir); !ok {
			return 2
		}
	}
	if _, ok := tasks["001"]; !ok {
		fmt.Fprintln(out, "SPEC_FOUNDATION: spec 001 not given; its checks could not be required")
		return 2
	}
	workflow, err := os.ReadFile(filepath.Join(root, checks.FoundationWorkflow))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(out, "WORKFLOW_UNREADABLE:", err)
		return 2
	}
	findings := checks.CheckFoundationCI(data, workflow, registry, tasks)
	for _, f := range findings {
		fmt.Fprintf(out, "%s %s: %s\n", f.Rule, f.Subject, f.Detail)
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "FOUNDATION_CI_FAIL findings=%d\n", len(findings))
		return 1
	}
	source, _ := checks.ParseFoundationSource(data)
	fmt.Fprintf(out, "FOUNDATION_CI_OK targets=%d deferred=%d\n", len(source.Targets), len(source.Deferred))
	return 0
}

// ciObserve prints the record of the captured runs: for each role, candidate
// and capture directory (run.json, jobs.json, workflow.sha, source.sha and
// run.log, as GitHub returned them, and tree/, a checkout of the run's head),
// the run's observation. It reads files only; capturing them is a separate,
// read-only step on the host.
func ciObserve(out io.Writer, args []string) int {
	record := checks.FoundationRuns{Runs: []checks.FoundationRun{}}
	for i := 0; i < len(args); i += 3 {
		var c checks.RunCapture
		for _, f := range []struct {
			name string
			into *[]byte
		}{{"run.json", &c.Run}, {"jobs.json", &c.Jobs}, {"workflow.sha", &c.WorkflowBlob}, {"source.sha", &c.SourceBlob}, {"run.log", &c.Log}} {
			data, err := os.ReadFile(filepath.Join(args[i+2], f.name))
			if err != nil {
				fmt.Fprintln(out, "CAPTURE_MISSING:", err)
				return 2
			}
			*f.into = data
		}
		digests, err := foundationDigests(filepath.Join(args[i+2], "tree"))
		if err != nil {
			fmt.Fprintf(out, "%s: CAPTURE_TREE: %v\n", args[i+2], err)
			return 2
		}
		c.Digests = digests
		run, err := checks.ObserveFoundationRun(args[i], args[i+1], c)
		if err != nil {
			fmt.Fprintf(out, "%s: %v\n", args[i+2], err)
			return 2
		}
		record.Runs = append(record.Runs, run)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		fmt.Fprintln(out, err)
		return 2
	}
	fmt.Fprintf(out, "%s\n", data)
	return 0
}

// foundationDigests is checks.FoundationDigests of the source targets of the
// tree at root, with that tree's own registry and source.
func foundationDigests(root string) (map[string]string, error) {
	registry, err := loadRegistry(root)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, checks.FoundationSourcePath))
	if err != nil {
		return nil, fmt.Errorf("SOURCE_MISSING: %w", err)
	}
	source, err := checks.ParseFoundationSource(data)
	if err != nil {
		return nil, err
	}
	return checks.FoundationDigests(root, registry, source.Targets)
}

// ciRuns judges the committed run record against the committed source. An
// absent record is missing evidence, a failure; an absent source is an input
// error.
func ciRuns(out io.Writer, root string) int {
	source, ok := readFoundationSource(out, root)
	if !ok {
		return 2
	}
	record, err := os.ReadFile(filepath.Join(root, checks.FoundationRunsPath))
	if err != nil {
		fmt.Fprintln(out, "RUNS_MISSING:", err)
		fmt.Fprintln(out, "FOUNDATION_RUNS_FAIL findings=1")
		return 1
	}
	current, err := foundationDigests(root)
	if err != nil {
		fmt.Fprintln(out, err)
		return 2
	}
	findings := checks.JudgeFoundationRuns(source, record, current)
	for _, f := range findings {
		fmt.Fprintf(out, "%s %s: %s\n", f.Rule, f.Subject, f.Detail)
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "FOUNDATION_RUNS_FAIL findings=%d\n", len(findings))
		return 1
	}
	var runs checks.FoundationRuns
	_ = checks.DecodeStrict(record, &runs)
	for _, r := range runs.Runs {
		outcome := fmt.Sprintf("targets=%d", len(r.Targets))
		if r.Role == checks.RoleRedControl {
			outcome = "failed=" + r.Targets[len(r.Targets)-1].Name
		}
		fmt.Fprintf(out, "RUN_OK %s run=%d check=%d event=%s head=%s %s\n", r.Role, r.RunID, r.CheckRunID, r.Event, r.HeadSHA, outcome)
	}
	fmt.Fprintf(out, "FOUNDATION_RUNS_OK runs=%d\n", len(runs.Runs))
	return 0
}

func deps(out io.Writer, root string) int {
	g, ok := scan(out, root)
	if !ok {
		fmt.Fprintln(out, "DEPENDENCIES_FAIL")
		return 1
	}
	fmt.Fprintf(out, "DEPENDENCIES_OK dirs=%d\n", len(g.Layers))
	return 0
}

// selectChanged prints the directories a changed path requires to be
// checked. A broken graph cannot select safely, so it fails instead.
func selectChanged(out io.Writer, root, changed string) int {
	g, ok := scan(out, root)
	if !ok {
		fmt.Fprintln(out, "SELECT_FAIL")
		return 1
	}
	s := checks.SelectChanged(g, []string{changed})
	switch {
	case s.Full:
		fmt.Fprintf(out, "SELECT_FULL %s\n", s.Reason)
	case len(s.Dirs) == 0:
		fmt.Fprintf(out, "SELECT_NONE %s\n", s.Reason)
	default:
		fmt.Fprintf(out, "SELECT %s\n", strings.Join(s.Dirs, " "))
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
