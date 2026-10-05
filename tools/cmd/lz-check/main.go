// lz-check checks a feature's traceability and a path's definition of done
// against the check registry, harness/checks.yaml.
//
//	lz-check [-root <repo>] specs <spec-dir>
//	lz-check [-root <repo>] [-evidence <dir>] dod <path>
//	lz-check [-root <repo>] deps
//	lz-check [-root <repo>] select <changed-path>
//	lz-check [-root <repo>] [-tofu <bin>] [-tflint <bin>] lint <module-dir>
//	lz-check [-root <repo>] ci-workflow
//	lz-check [-root <repo>] ci-source <spec-dir>
//
// specs follows requirement → ADR → paths → check → evidence for every task in
// <spec-dir>/tasks.md and exits 1 on any broken link. dod judges each registered
// check whose scope covers <path> from <evidence>/<check-id>.json and exits 1
// unless every one passes or is a docs exemption. deps checks the module graph
// of ADR-0002 and exits 1 on any finding; select prints the directories a
// changed path requires to be checked. lint runs the pinned fmt, init, validate
// and TFLint clauses on one module directory and exits 1 unless all pass.
// ci-workflow prints the foundation workflow rendered from its approved source;
// ci-source judges that source against the registry and <spec-dir>/tasks.md
// and the committed workflow against the rendered one, and exits 1 on any
// finding. Usage and input errors exit 2.
package main

import (
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
)

var adrFile = regexp.MustCompile(`^(\d{4})-.+\.md$`)

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
	case flags.NArg() == 1 && flags.Arg(0) == "deps":
		return deps(out, *root)
	case flags.NArg() == 2 && flags.Arg(0) == "select":
		return selectChanged(out, *root, flags.Arg(1))
	case flags.NArg() == 1 && flags.Arg(0) == "ci-workflow":
		return ciWorkflow(out, *root)
	case flags.NArg() != 2 || flags.Arg(0) == "deps":
		fmt.Fprintln(out, "usage: lz-check [-root dir] specs <spec-dir> | [-evidence dir] dod <path> | deps | select <path> | ci-workflow | ci-source <spec-dir>")
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
		return ciSource(out, *root, flags.Arg(1), registry)
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

func specs(out io.Writer, root, dir string, registry checks.Registry) int {
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
	tasksText, err := os.ReadFile(filepath.Join(root, dir, "tasks.md"))
	if err != nil {
		fmt.Fprintln(out, "TASKS_MISSING:", err)
		return 2
	}
	tasks, err := checks.ParseTasks(string(tasksText))
	if err != nil {
		fmt.Fprintln(out, err)
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
	findings := checks.CheckTrace(checks.Trace{Requirements: requirements, Tasks: tasks, Registry: registry, ADRs: adrs})
	for _, f := range findings {
		fmt.Fprintf(out, "%s %s: %s\n", f.Rule, f.Subject, f.Detail)
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "TRACE_FAIL %s findings=%d\n", dir, len(findings))
		return 1
	}
	fmt.Fprintf(out, "TRACE_OK %s requirements=%d tasks=%d checks=%d\n", dir, len(requirements), len(tasks), len(registry.Checks))
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
	clean := filepath.ToSlash(filepath.Clean(dir))
	if filepath.IsAbs(dir) || clean == ".." || strings.HasPrefix(clean, "../") {
		fmt.Fprintf(out, "lint needs a directory inside the repository, not %q\n", dir)
		return 2
	}
	if info, err := os.Stat(filepath.Join(root, clean)); err != nil || !info.IsDir() {
		fmt.Fprintf(out, "LINT_NOT_RUN %s: the module directory does not exist yet\n", clean)
		return 1
	}
	o := checks.RunStatic(tools, filepath.Join(root, clean))
	blocked := false
	for _, r := range o.Results {
		fmt.Fprintf(out, "%s %s %s\n", r.Status, r.Clause, r.Reason)
		for _, detail := range append(append([]string{}, r.Files...), r.Messages...) {
			fmt.Fprintf(out, "  %s\n", detail)
		}
		blocked = blocked || r.Status == checks.StatusBlocked
	}
	switch {
	case o.Pass:
		fmt.Fprintf(out, "LINT_PASS %s files=%d\n", clean, len(o.Discovered))
		return 0
	case blocked && !slices.ContainsFunc(o.Results, func(r checks.StaticResult) bool { return r.Status == checks.StatusFail }):
		fmt.Fprintf(out, "LINT_BLOCKED %s\n", clean)
	default:
		fmt.Fprintf(out, "LINT_FAIL %s\n", clean)
	}
	return 1
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
// is judged as an empty one, so it is drift, not an input error.
func ciSource(out io.Writer, root, dir string, registry checks.Registry) int {
	data, ok := readFoundationSource(out, root)
	if !ok {
		return 2
	}
	tasksText, err := os.ReadFile(filepath.Join(root, dir, "tasks.md"))
	if err != nil {
		fmt.Fprintln(out, "TASKS_MISSING:", err)
		return 2
	}
	tasks, err := checks.ParseTasks(string(tasksText))
	if err != nil {
		fmt.Fprintln(out, err)
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
