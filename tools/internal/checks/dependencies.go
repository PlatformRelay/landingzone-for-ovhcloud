package checks

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
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
var DependencyRules = []string{"CYCLE", "LAYER_VIOLATION", "PARSE_ERROR", "UNCLASSIFIED", "UNREADABLE", "UNRESOLVED_REFERENCE"}

// DependencyGraph is the classified module graph of a repository. Keys are
// directories relative to the repository root. Uses holds local module
// dependencies, External the sources outside this repository, and Configs the
// Terramate configuration files a generated directory inherits.
type DependencyGraph struct {
	Layers   map[string]string
	Uses     map[string][]string
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

// selfSources are this repository's own module addresses. A source under
// one of them with a "//<dir>" subdirectory is a released copy of a local
// directory, so it is resolved to that directory rather than treated as
// external.
var selfSources = []string{
	"git::https://github.com/PlatformRelay/ovh-landing-zone-accelerator.git",
	"github.com/PlatformRelay/ovh-landing-zone-accelerator",
}

// sharedTools are paths every check depends on; a change to them selects the
// full suite.
var sharedTools = []string{"tools", "harness", "schemas", "policies", "pipelines", ".github", "Taskfile.yml", "mise.toml", ".tflint.hcl"}

// docRoots hold documentation only; a change there selects no check.
var docRoots = []string{"docs", "specs"}

func under(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }

// classify places a directory holding *.tf files in its ADR-0002 layer.
func classify(dir string, generated bool) string {
	parts := strings.Split(dir, "/")
	switch {
	case parts[0] == "tests" || slices.Contains(parts, "tests"):
		return LayerTest
	case generated:
		return LayerInstance
	case parts[0] == "examples" || slices.Contains(parts, "examples"):
		return LayerExample
	case parts[0] == "modules" && len(parts) >= 2, parts[0] == "components" && len(parts) >= 3:
		return LayerLibrary
	case parts[0] == "stages" && len(parts) >= 2:
		return LayerStage
	}
	return ""
}

// rank orders the library layers: modules/naming, other modules, components,
// stages. Dependencies point to a lower rank only.
func rank(dir string) int {
	switch {
	case under(dir, "modules/naming"):
		return 0
	case under(dir, "modules"):
		return 1
	case under(dir, "components"):
		return 2
	}
	return 3
}

// pkg is the package root a library or stage directory belongs to.
func pkg(dir string) string {
	parts := strings.Split(dir, "/")
	if parts[0] == "components" && len(parts) >= 3 {
		return strings.Join(parts[:3], "/")
	}
	return strings.Join(parts[:min(2, len(parts))], "/")
}

func allowed(from, to string, layers map[string]string) bool {
	lf, lt := layers[from], layers[to]
	switch {
	case lt == LayerTest:
		return lf == LayerTest
	case lt == LayerExample || lt == LayerInstance:
		return false
	case lf == LayerExample || lf == LayerTest:
		return true
	case lf == LayerInstance:
		return lt == LayerStage
	}
	return pkg(from) == pkg(to) || rank(to) < rank(from)
}

// moduleSources returns the literal source of every module block in a file,
// and whether any source is not a literal string.
func moduleSources(body *hclsyntax.Body) (sources []string, computed bool) {
	for _, block := range body.Blocks {
		if block.Type != "module" {
			continue
		}
		attr, ok := block.Body.Attributes["source"]
		if !ok {
			computed = true
			continue
		}
		value, diags := attr.Expr.Value(nil)
		if diags.HasErrors() || !value.IsKnown() || value.IsNull() || value.Type() != cty.String {
			computed = true
			continue
		}
		sources = append(sources, value.AsString())
	}
	return sources, computed
}

// resolve maps a module source to a local directory, or reports it external.
// ok is false when the source should be local but cannot be resolved.
func resolve(dir, source string, dirs map[string]bool) (local string, external bool, ok bool) {
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
		target := path.Join(dir, source)
		return target, false, !strings.HasPrefix(target, "../") && target != ".." && dirs[target]
	}
	for _, self := range selfSources {
		rest, found := strings.CutPrefix(source, self)
		if !found || rest != "" && !strings.HasPrefix(rest, "//") && !strings.HasPrefix(rest, "?") {
			continue
		}
		sub, isSub := strings.CutPrefix(rest, "//")
		if !isSub {
			return "", false, false
		}
		sub, _, _ = strings.Cut(sub, "?")
		target := path.Clean(sub)
		return target, false, cleanRelative(target) && dirs[target]
	}
	return "", true, true
}

// ScanDependencies parses every *.tf file under root with the HCL parser,
// classifies each directory holding configuration into its layer, resolves
// module sources and reports layer violations, cycles, unresolved and
// computed references, unclassified directories, parse errors and symlinks.
func ScanDependencies(root string) (DependencyGraph, []Finding) {
	g := DependencyGraph{Layers: map[string]string{}, Uses: map[string][]string{}, External: map[string][]string{}, Configs: map[string][]string{}}
	var findings []Finding
	add := func(rule, subject, format string, args ...any) {
		findings = append(findings, Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() {
		add("UNREADABLE", root, "repository root is not a directory")
		return g, findings
	}
	files := map[string][]string{}
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
		case strings.HasSuffix(rel, ".tm.hcl"):
			configs = append(configs, rel)
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

	for _, dir := range slices.Sorted(maps.Keys(files)) {
		generated := false
		uses, external := map[string]bool{}, map[string]bool{}
		for _, file := range files[dir] {
			src, err := os.ReadFile(file)
			if err != nil {
				add("UNREADABLE", dir, "%v", err)
				continue
			}
			generated = generated || bytes.HasPrefix(src, []byte(generatedHeader))
			parsed, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
			if diags.HasErrors() {
				add("PARSE_ERROR", dir, "%s", diags.Error())
				continue
			}
			sources, computed := moduleSources(parsed.Body.(*hclsyntax.Body))
			if computed {
				add("UNRESOLVED_REFERENCE", dir, "a module source is not a literal string")
			}
			for _, source := range sources {
				local, isExternal, ok := resolve(dir, source, dirs)
				switch {
				case !ok:
					add("UNRESOLVED_REFERENCE", dir, "module source %q resolves to no configuration in this repository", source)
				case isExternal:
					external[source] = true
				default:
					uses[local] = true
				}
			}
		}
		layer := classify(dir, generated)
		if layer == "" {
			add("UNCLASSIFIED", dir, "directory belongs to no layer of ADR-0002")
			continue
		}
		g.Layers[dir] = layer
		if len(uses) > 0 {
			g.Uses[dir] = slices.Sorted(maps.Keys(uses))
		}
		if len(external) > 0 {
			g.External[dir] = slices.Sorted(maps.Keys(external))
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

	for _, from := range slices.Sorted(maps.Keys(g.Uses)) {
		for _, to := range g.Uses[from] {
			if _, classified := g.Layers[to]; classified && !allowed(from, to, g.Layers) {
				add("LAYER_VIOLATION", from, "%s (%s) may not use %s (%s)", from, g.Layers[from], to, g.Layers[to])
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
// checked: each path's owning directory and every transitive consumer. Shared
// tooling widens to the full suite; a path no directory owns widens too, so a
// change is never silently dropped. Documentation selects nothing, explicitly.
func SelectChanged(g DependencyGraph, changed []string) Selection {
	if len(changed) == 0 {
		return Selection{Reason: "NO_CHANGES"}
	}
	consumers := map[string][]string{}
	for from, tos := range g.Uses {
		for _, to := range tos {
			consumers[to] = append(consumers[to], from)
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
		switch {
		case owner != "":
			selected[owner] = true
		case slices.ContainsFunc(docRoots, func(root string) bool { return under(p, root) }), !strings.Contains(p, "/") && strings.HasSuffix(p, ".md"):
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
	return Selection{Dirs: slices.Sorted(maps.Keys(selected))}
}
