package checks

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Layers a directory can belong to (ADR-0002).
const (
	LayerLibrary  = "library"
	LayerStage    = "stage"
	LayerInstance = "instance"
	LayerExample  = "example"
	LayerTest     = "test"
)

// DependencyRules lists every rule ScanDependencies can report.
var DependencyRules = []string{"CYCLE", "LAYER_VIOLATION", "PARSE_ERROR", "UNCLASSIFIED", "UNREADABLE", "UNRESOLVED_REFERENCE", "UNSUPPORTED_CONFIG"}

// DependencyGraph is the classified module graph of a repository. Keys are
// directories relative to the repository root. Uses holds local module
// dependencies, TestUses the modules a directory's test configuration runs,
// External the sources outside this repository, and Configs the Terramate
// configuration files a directory with generated files inherits.
type DependencyGraph struct {
	Layers   map[string]string
	Uses     map[string][]string
	TestUses map[string][]string
	External map[string][]string
	Configs  map[string][]string
}

// Selection is the set of directories a change requires to be checked. Full
// means every check runs; Reason explains a full or empty selection.
type Selection struct {
	Full   bool
	Dirs   []string
	Reason string
}

// generatedHeader marks a file Terramate generated from inherited *.tm.hcl
// configuration.
const generatedHeader = "// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT"

// selfRepositories are this repository's addresses after normalisation: its
// current name and the name it had before the rename, which GitHub still
// redirects. A source naming one with a "//<dir>" subdirectory is a released
// copy of that local directory.
var selfRepositories = []string{"github.com/platformrelay/landingzone-for-ovhcloud", "github.com/platformrelay/ovh-landing-zone-accelerator"}

// singletonComponents are the component packages without a family directory.
var singletonComponents = []string{"account-baseline", "project-factory", "guardrails", "state-backend"}

// sharedTools are paths every check depends on; a change to them selects the
// full suite.
var sharedTools = []string{"tools", "harness", "schemas", "policies", "pipelines", ".github", "Taskfile.yml", "mise.toml", ".tflint.hcl"}

// docRoots hold documentation; a Markdown change there selects no check.
var docRoots = []string{"docs", "specs"}

func under(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }

// role names a directory's place in the ADR-0002 edge matrix.
func role(dir, layer string) string {
	switch {
	case layer != LayerLibrary:
		return layer
	case under(dir, "modules/naming"):
		return "naming"
	case under(dir, "modules"):
		return "module"
	}
	return "component"
}

// classify places a directory holding *.tf files in its ADR-0002 layer. The
// layout decides; a Terramate-generated file only marks a directory that has
// no other place as a generated deployment instance.
func classify(dir string, generated bool) string {
	parts := strings.Split(dir, "/")
	switch {
	case parts[0] == "tests" || slices.Contains(parts, "tests"):
		return LayerTest
	case parts[0] == "examples" || slices.Contains(parts, "examples"):
		return LayerExample
	case parts[0] == "modules" && len(parts) >= 2:
		return LayerLibrary
	case parts[0] == "components" && len(parts) >= 2 && slices.Contains(singletonComponents, parts[1]),
		parts[0] == "components" && len(parts) >= 3:
		return LayerLibrary
	case parts[0] == "stages" && len(parts) >= 2:
		return LayerStage
	case generated:
		return LayerInstance
	}
	return ""
}

// pkg is the package root a library or stage directory belongs to.
func pkg(dir string) string {
	parts := strings.Split(dir, "/")
	if parts[0] == "components" && !slices.Contains(singletonComponents, parts[1]) && len(parts) >= 3 {
		return strings.Join(parts[:3], "/")
	}
	return strings.Join(parts[:min(2, len(parts))], "/")
}

// mayUse is the ADR-0002 edge matrix: modules use modules/naming, components
// use modules, stages use components, and only generated deployment instances
// use stages. Examples and tests use library code; only tests use tests.
// Within one package any edge is allowed; the cycle check still applies.
var mayUse = map[string][]string{
	"naming":      nil,
	"module":      {"naming"},
	"component":   {"naming", "module"},
	LayerStage:    {"component"},
	LayerInstance: {LayerStage},
	LayerExample:  {"naming", "module", "component"},
	LayerTest:     {"naming", "module", "component", LayerTest},
}

func allowed(from, to string, layers map[string]string) bool {
	rf, rt := role(from, layers[from]), role(to, layers[to])
	if (layers[from] == LayerLibrary || layers[from] == LayerStage) && layers[to] == layers[from] && pkg(from) == pkg(to) {
		return true
	}
	return slices.Contains(mayUse[rf], rt)
}

// literalSource returns a block's source attribute if it is a literal string.
func literalSource(body *hclsyntax.Body) (string, bool) {
	attr, ok := body.Attributes["source"]
	if !ok {
		return "", false
	}
	value, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || !value.IsKnown() || value.IsNull() || value.Type() != cty.String {
		return "", false
	}
	return value.AsString(), true
}

// moduleSources returns the source of every module block, and of every module
// block nested in a test run block, and whether any is not a literal string.
func moduleSources(body *hclsyntax.Body) (sources []string, computed bool) {
	for _, block := range body.Blocks {
		switch block.Type {
		case "module":
			source, ok := literalSource(block.Body)
			if !ok {
				computed = true
				continue
			}
			sources = append(sources, source)
		case "run":
			nested, nestedComputed := moduleSources(block.Body)
			sources, computed = append(sources, nested...), computed || nestedComputed
		}
	}
	return sources, computed
}

// remote normalises a remote module address: forced getter, scheme, user and
// scp-style host separators, query and the ".git" suffix are removed, and the
// repository part is lower-cased. sub is the "//<dir>" subdirectory, if any.
func remote(source string) (repository, sub string, hasSub bool) {
	s := source
	if i := strings.Index(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s, _, _ = strings.Cut(s, "?")
	if at := strings.Index(s, "@"); at >= 0 && at < strings.IndexAny(s+"/", "/:") {
		s = s[at+1:]
	}
	if colon := strings.Index(s, ":"); colon >= 0 && colon < strings.Index(s+"/", "/") {
		s = s[:colon] + "/" + s[colon+1:]
	}
	repository, sub, hasSub = strings.Cut(s, "//")
	return strings.TrimSuffix(strings.ToLower(repository), ".git"), sub, hasSub
}

// resolve maps a module source to a local directory, or reports it external.
// ok is false when the source should be local but cannot be resolved,
// including any mention of this repository that is not a resolvable
// "//<dir>" address.
func resolve(dir, source string, dirs map[string]bool) (local string, external bool, ok bool) {
	if path.IsAbs(source) || filepath.IsAbs(source) {
		// An absolute path does not survive a checkout elsewhere.
		return "", false, false
	}
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
		target := path.Join(dir, source)
		return target, false, !strings.HasPrefix(target, "../") && target != ".." && dirs[target]
	}
	repository, sub, hasSub := remote(source)
	for _, self := range selfRepositories {
		if repository == self {
			target := path.Clean(sub)
			return target, false, hasSub && cleanRelative(target) && dirs[target]
		}
	}
	if mentionsSelf(source) {
		return "", false, false
	}
	return "", true, true
}

// mentionsSelf reports whether source names this repository: after decoding
// percent-escapes until nothing changes, some maximal run of repository-name
// characters, without a ".git" suffix, is exactly one of its names. Any other
// character separates, so no separator list has to be complete, and a
// repository whose name only contains one of the names is unrelated.
func mentionsSelf(source string) bool {
	decoded := strings.ToLower(source)
	// Each successful decode shortens the text, so the loop ends.
	for {
		next, err := url.PathUnescape(decoded)
		if err != nil || next == decoded {
			break
		}
		decoded = strings.ToLower(next)
	}
	runs := strings.FieldsFunc(decoded, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-')
	})
	for _, run := range runs {
		for _, self := range selfRepositories {
			if strings.TrimSuffix(run, ".git") == path.Base(self) {
				return true
			}
		}
	}
	return false
}

// testOwner is the module directory whose tests a test file belongs to: the
// parent of a tests/ directory, or the file's own directory.
func testOwner(file string) string {
	dir := path.Dir(file)
	if path.Base(dir) == "tests" {
		return path.Dir(dir)
	}
	return dir
}

func sortedSet(set map[string]bool) []string { return slices.Sorted(maps.Keys(set)) }

// ScanDependencies parses every *.tf and *.tftest.hcl file under root with the
// HCL parser, classifies each directory holding configuration into its layer,
// resolves module sources and reports layer violations, cycles, unresolved and
// computed references, unclassified directories, unsupported JSON
// configuration, parse errors and symlinks.
func ScanDependencies(root string) (DependencyGraph, []Finding) {
	g := DependencyGraph{Layers: map[string]string{}, Uses: map[string][]string{}, TestUses: map[string][]string{}, External: map[string][]string{}, Configs: map[string][]string{}}
	var findings []Finding
	add := func(rule, subject, format string, args ...any) {
		findings = append(findings, Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() {
		add("UNREADABLE", root, "repository root is not a directory")
		return g, findings
	}
	files, tests := map[string][]string{}, map[string][]string{}
	var configs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			return fmt.Errorf("%s is a symlink", rel)
		case d.IsDir() && rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "fixtures"):
			// Hidden directories hold tool state; fixture trees are checker
			// input data, not configuration of this repository.
			return fs.SkipDir
		case d.IsDir():
			return nil
		case strings.HasSuffix(rel, ".tf.json"), strings.HasSuffix(rel, ".tftest.json"):
			add("UNSUPPORTED_CONFIG", path.Dir(rel), "%s: JSON configuration is not supported", rel)
		case strings.HasSuffix(rel, ".tm.hcl"):
			configs = append(configs, rel)
		case strings.HasSuffix(rel, ".tftest.hcl"):
			tests[testOwner(rel)] = append(tests[testOwner(rel)], p)
		case strings.HasSuffix(rel, ".tf"):
			files[path.Dir(rel)] = append(files[path.Dir(rel)], p)
		}
		return nil
	})
	if err != nil {
		add("UNREADABLE", root, "%v", err)
		return g, findings
	}
	dirs := map[string]bool{}
	for dir := range files {
		dirs[dir] = true
	}

	// parse reads one configuration file and returns its module sources
	// resolved against base.
	parse := func(subject, file, base string, uses, external map[string]bool) (generated bool) {
		src, err := os.ReadFile(file)
		if err != nil {
			add("UNREADABLE", subject, "%v", err)
			return false
		}
		parsed, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
		if diags.HasErrors() {
			add("PARSE_ERROR", subject, "%s", diags.Error())
			return false
		}
		sources, computed := moduleSources(parsed.Body.(*hclsyntax.Body))
		if computed {
			add("UNRESOLVED_REFERENCE", subject, "a module source is not a literal string")
		}
		for _, source := range sources {
			local, isExternal, ok := resolve(base, source, dirs)
			switch {
			case !ok:
				add("UNRESOLVED_REFERENCE", subject, "module source %q resolves to no configuration in this repository", source)
			case isExternal:
				external[source] = true
			default:
				uses[local] = true
			}
		}
		return bytes.HasPrefix(src, []byte(generatedHeader))
	}

	for _, dir := range slices.Sorted(maps.Keys(files)) {
		generated := false
		uses, external := map[string]bool{}, map[string]bool{}
		for _, file := range files[dir] {
			generated = parse(dir, file, dir, uses, external) || generated
		}
		layer := classify(dir, generated)
		if layer == "" {
			add("UNCLASSIFIED", dir, "directory belongs to no layer of ADR-0002")
			continue
		}
		g.Layers[dir] = layer
		if len(uses) > 0 {
			g.Uses[dir] = sortedSet(uses)
		}
		if len(external) > 0 {
			g.External[dir] = sortedSet(external)
		}
		if generated {
			for _, config := range configs {
				if under(dir, path.Dir(config)) || path.Dir(config) == "." {
					g.Configs[dir] = append(g.Configs[dir], config)
				}
			}
			slices.Sort(g.Configs[dir])
		}
	}
	for _, owner := range slices.Sorted(maps.Keys(tests)) {
		uses := map[string]bool{}
		for _, file := range tests[owner] {
			parse(owner, file, owner, uses, map[string]bool{})
		}
		if len(uses) > 0 {
			g.TestUses[owner] = sortedSet(uses)
		}
	}

	for _, from := range slices.Sorted(maps.Keys(g.Uses)) {
		for _, to := range g.Uses[from] {
			if _, classified := g.Layers[to]; classified && !allowed(from, to, g.Layers) {
				add("LAYER_VIOLATION", from, "%s (%s) may not use %s (%s)", from, role(from, g.Layers[from]), to, role(to, g.Layers[to]))
			}
		}
	}
	for _, owner := range slices.Sorted(maps.Keys(g.TestUses)) {
		for _, to := range g.TestUses[owner] {
			if _, classified := g.Layers[to]; classified && !slices.Contains(mayUse[LayerTest], role(to, g.Layers[to])) {
				add("LAYER_VIOLATION", owner, "tests of %s may not run %s (%s)", owner, to, role(to, g.Layers[to]))
			}
		}
	}
	if cycle := findCycle(g.Uses); cycle != nil {
		add("CYCLE", cycle[0], "%s", strings.Join(cycle, " → "))
	}
	return g, findings
}

// findCycle returns one dependency cycle, or nil.
func findCycle(uses map[string][]string) []string {
	const (
		unseen = iota
		active
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) []string
	visit = func(dir string) []string {
		state[dir] = active
		stack = append(stack, dir)
		for _, next := range uses[dir] {
			switch state[next] {
			case active:
				start := slices.Index(stack, next)
				return append(append([]string{}, stack[start:]...), next)
			case unseen:
				if cycle := visit(next); cycle != nil {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[dir] = done
		return nil
	}
	for _, dir := range slices.Sorted(maps.Keys(uses)) {
		if state[dir] == unseen {
			if cycle := visit(dir); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// SelectChanged returns the directories a set of changed paths requires to be
// checked: each path's owning directory and every transitive consumer,
// including directories whose tests run it. Shared tooling widens to the full
// suite; a path no directory owns widens too, so a change is never silently
// dropped. Markdown documentation selects nothing, explicitly.
func SelectChanged(g DependencyGraph, changed []string) Selection {
	if len(changed) == 0 {
		return Selection{Reason: "NO_CHANGES"}
	}
	consumers := map[string][]string{}
	for _, edges := range []map[string][]string{g.Uses, g.TestUses} {
		for from, tos := range edges {
			for _, to := range tos {
				consumers[to] = append(consumers[to], from)
			}
		}
	}
	selected := map[string]bool{}
	docs, full := false, ""
	for _, c := range changed {
		p := path.Clean(filepath.ToSlash(c))
		if !cleanRelative(p) {
			full = "UNKNOWN_PATH"
			continue
		}
		if slices.ContainsFunc(sharedTools, func(root string) bool { return under(p, root) }) {
			if full == "" {
				full = "SHARED_TOOL"
			}
			continue
		}
		if strings.HasSuffix(p, ".tm.hcl") {
			owned := false
			for dir, configs := range g.Configs {
				if slices.Contains(configs, p) {
					selected[dir], owned = true, true
				}
			}
			if !owned {
				full = "UNKNOWN_PATH"
			}
			continue
		}
		owner := ""
		for dir := range g.Layers {
			if under(p, dir) && len(dir) > len(owner) {
				owner = dir
			}
		}
		// A test file belongs to the module that runs it, as when parsing.
		if strings.HasSuffix(p, ".tftest.hcl") && g.Layers[testOwner(p)] != "" {
			owner = testOwner(p)
		}
		markdown := strings.HasSuffix(p, ".md")
		switch {
		case owner != "":
			selected[owner] = true
		case markdown && (!strings.Contains(p, "/") || slices.ContainsFunc(docRoots, func(root string) bool { return under(p, root) })):
			docs = true
		default:
			full = "UNKNOWN_PATH"
		}
	}
	if full != "" {
		return Selection{Full: true, Reason: full}
	}
	queue := slices.Collect(maps.Keys(selected))
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		for _, consumer := range consumers[dir] {
			if !selected[consumer] {
				selected[consumer] = true
				queue = append(queue, consumer)
			}
		}
	}
	if len(selected) == 0 && docs {
		return Selection{Reason: "DOCS_ONLY"}
	}
	return Selection{Dirs: sortedSet(selected)}
}
