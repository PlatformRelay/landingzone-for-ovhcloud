package checks

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
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
var DependencyRules = []string{"CYCLE", "HANDWRITTEN_INSTANCE", "LAYER_VIOLATION", "LIBRARY_BACKEND", "LIBRARY_PROVIDER_CONFIG", "NONSENSITIVE_CALL", "PARSE_ERROR", "REMOTE_STATE", "RETAINED_UNPROTECTED", "STAGE_COMPONENT_CALLS", "STAGE_RESOURCE", "STATE_BUCKET_UNPROTECTED", "UNCLASSIFIED", "UNREADABLE", "UNRESOLVED_REFERENCE", "UNSUPPORTED_CONFIG"}

// purity holds the FR-003 facts of configuration: whether it declares a state
// backend (a backend or cloud block), configures a provider, or reads another
// configuration's state.
type purity struct{ backend, provider, remoteState bool }

func (p purity) or(q purity) purity {
	return purity{p.backend || q.backend, p.provider || q.provider, p.remoteState || q.remoteState}
}

// purityOf reads the FR-003 facts of one parsed configuration file. A check
// block may hold a scoped data source, so it is searched for remote state.
func purityOf(body *hclsyntax.Body) purity {
	var p purity
	for _, block := range body.Blocks {
		switch block.Type {
		case "terraform":
			for _, inner := range block.Body.Blocks {
				if inner.Type == "backend" || inner.Type == "cloud" {
					p.backend = true
				}
			}
		case "provider":
			p.provider = true
		case "data":
			if len(block.Labels) > 0 && block.Labels[0] == "terraform_remote_state" {
				p.remoteState = true
			}
		case "check":
			p.remoteState = p.remoteState || purityOf(block.Body).remoteState
		}
	}
	return p
}

// unmarks reports whether one parsed configuration file calls nonsensitive,
// by its plain name or a namespaced one (core::nonsensitive, or a provider
// function of that name), in any expression, nested block or template.
func unmarks(body *hclsyntax.Body) bool {
	found := false
	hclsyntax.VisitAll(body, func(node hclsyntax.Node) hcl.Diagnostics {
		if call, ok := node.(*hclsyntax.FunctionCallExpr); ok && (call.Name == "nonsensitive" || strings.HasSuffix(call.Name, "::nonsensitive")) {
			found = true
		}
		return nil
	})
	return found
}

// retainedPackage says what a retained library package keeps (contracts/checks.md
// G7). kept lists the resource types that carry a literal prevent_destroy; nil
// means every resource. single, when set, is a type the package declares at
// exactly one address, in its root directory and without for_each, every block
// of it with a literal deletion_protection = true. Every retained package refuses removed blocks and non-relative module
// sources.
type retainedPackage struct {
	kept   []string
	single string
}

// retainedModules are the library packages whose resources outlive every run.
// The state bucket package keeps every resource. The project package keeps
// only its project (spec 005 T026): the budget alert stays switchable, and
// destroying the IAM tags removes only the configured keys
// (iam_resource_tags.md Notes).
var retainedModules = map[string]retainedPackage{
	"modules/object-storage-protected": {},
	"modules/cloud-project":            {kept: []string{"ovh_cloud_project"}, single: "ovh_cloud_project"},
}

// retainedOf returns the retained package a directory lies in.
func retainedOf(dir string) (string, retainedPackage, bool) {
	for m, p := range retainedModules {
		if under(dir, m) {
			return m, p, true
		}
	}
	return "", retainedPackage{}, false
}

// literalTrue reports whether an attribute is the constant bool true, evaluated
// without variables: OpenTofu admits no expression in prevent_destroy, and
// deletion_protection must not depend on an input a run could vary.
func literalTrue(attr *hclsyntax.Attribute) bool {
	value, diags := attr.Expr.Value(nil)
	return !diags.HasErrors() && value.Type() == cty.Bool && value.IsKnown() && !value.IsNull() && value.True()
}

// stateBackend creates the state buckets; it takes them only from
// protectedBuckets, the retained module that pins a literal prevent_destroy
// and versioning (G7, component part; spec 005 T016).
const (
	stateBackend     = "components/state-backend"
	protectedBuckets = "modules/object-storage-protected"
)

// bucketResources are the prefixes of the resource types that create a
// bucket or change one (versioning, lifecycle, policy, objects): OVHcloud
// Object Storage, and the S3 and Swift resources of providers that reach the
// same service. A lifecycle rule outside the protected module could expire
// the state versions its versioning keeps.
var bucketResources = []string{"ovh_cloud_project_storage", "aws_s3_bucket", "openstack_objectstorage_"}

// buckets lists the bucket resources one parsed file declares.
func buckets(body *hclsyntax.Body) []string {
	var out []string
	for _, block := range body.Blocks {
		if block.Type == "resource" && len(block.Labels) > 0 && slices.ContainsFunc(bucketResources, func(p string) bool { return strings.HasPrefix(block.Labels[0], p) }) {
			out = append(out, strings.Join(block.Labels, "."))
		}
	}
	return out
}

// resources lists the resource blocks one parsed file declares. A stage
// composes components and declares none (ADR-0002); data sources are not
// resources.
func resources(body *hclsyntax.Body) []string {
	var out []string
	for _, block := range body.Blocks {
		if block.Type == "resource" {
			out = append(out, strings.Join(block.Labels, "."))
		}
	}
	return out
}

// singleComponentStages maps a stage to the one component it calls, exactly
// once and unrepeated, and calls no other module: tenant-state reaches its one
// tenant bucket through the state-backend component (G6; T021 decision request
// 1), account-governance its deployers, policies and groups through the
// identity component (G5; T023 decision request 2).
var singleComponentStages = map[string]string{
	"stages/tenant-state":       stateBackend,
	"stages/account-governance": "components/identity/ovh-native",
}

// moduleCall is one module block of a configuration file: its name, its
// source if literal, and whether count or for_each repeats it.
type moduleCall struct {
	name, source      string
	literal, repeated bool
}

// moduleCalls lists the module blocks of one parsed configuration file.
func moduleCalls(body *hclsyntax.Body) []moduleCall {
	var out []moduleCall
	for _, block := range body.Blocks {
		if block.Type != "module" || len(block.Labels) == 0 {
			continue
		}
		source, literal := literalSource(block.Body)
		_, count := block.Body.Attributes["count"]
		_, forEach := block.Body.Attributes["for_each"]
		out = append(out, moduleCall{name: block.Labels[0], source: source, literal: literal, repeated: count || forEach})
	}
	return out
}

// unprotected lists the blocks of one parsed file that leave a retained
// package's resources destroyable: a resource of a kept type whose lifecycle
// does not set prevent_destroy to the literal true (an override file merges
// its own lifecycle), a block of the single type without a literal top-level
// deletion_protection = true, and any removed block.
func unprotected(body *hclsyntax.Body, p retainedPackage) []string {
	var out []string
	for _, block := range body.Blocks {
		switch block.Type {
		case "removed":
			out = append(out, "a removed block drops a retained resource from the configuration")
		case "resource":
			if len(block.Labels) == 0 {
				continue
			}
			address := strings.Join(block.Labels, ".")
			if p.kept == nil || slices.Contains(p.kept, block.Labels[0]) {
				kept := false
				for _, inner := range block.Body.Blocks {
					if attr, ok := inner.Body.Attributes["prevent_destroy"]; ok && inner.Type == "lifecycle" {
						kept = literalTrue(attr)
					}
				}
				if !kept {
					out = append(out, "resource "+address+" lacks a literal lifecycle { prevent_destroy = true }")
				}
			}
			if p.single != "" && block.Labels[0] == p.single {
				if attr, ok := block.Body.Attributes["deletion_protection"]; !ok || !literalTrue(attr) {
					out = append(out, "resource "+address+" lacks a literal deletion_protection = true")
				}
			}
		}
	}
	return out
}

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
// no other place as a generated deployment instance. A tests or examples
// segment makes test or example configuration only below a package root
// (D91): a package named tests or examples is library or stage code. The root
// tests/ and examples/ trees are roots of their own; outside these and the
// package trees a tests or examples segment does not count.
func classify(dir string, generated bool) string {
	parts := strings.Split(dir, "/")
	var below []string
	switch depth := packageDepth(parts); {
	case depth > 0:
		below = parts[depth:]
	case parts[0] == "tests" || parts[0] == "examples":
		below = parts[1:]
	}
	switch {
	case parts[0] == "tests" || slices.Contains(below, "tests"):
		return LayerTest
	case parts[0] == "examples" || slices.Contains(below, "examples"):
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

// packageDepth is the number of leading segments naming the package root a
// path lies in: modules/<m>, stages/<s>, components/<singleton> or
// components/<family>/<c>, cut short when the path is; 0 outside these trees.
func packageDepth(parts []string) int {
	switch {
	case parts[0] == "components" && len(parts) >= 2 && !slices.Contains(singletonComponents, parts[1]):
		return min(3, len(parts))
	case parts[0] == "modules" || parts[0] == "components" || parts[0] == "stages":
		return min(2, len(parts))
	}
	return 0
}

// pkg is the package root a library or stage directory belongs to.
func pkg(dir string) string {
	parts := strings.Split(dir, "/")
	return strings.Join(parts[:packageDepth(parts)], "/")
}

// hidden reports whether a path lies in a hidden directory.
func hidden(p string) bool {
	return slices.ContainsFunc(strings.Split(p, "/"), func(s string) bool { return strings.HasPrefix(s, ".") })
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
// valid percent-escapes until nothing changes, some maximal run of repository-name
// characters, without a ".git" suffix, is exactly one of its names. Any other
// character separates, so no separator list has to be complete, and a
// repository whose name only contains one of the names is unrelated.
func mentionsSelf(source string) bool {
	decoded := strings.ToLower(source)
	// Each pass that decodes something shortens the text, so the loop ends.
	for next := unescapeValid(decoded); next != decoded; next = unescapeValid(decoded) {
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

// unescapeValid decodes every valid %HH escape and keeps any other '%' as it
// is, so one malformed escape does not stop the others from decoding.
func unescapeValid(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			value, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			out.WriteByte(byte(value))
			i += 2
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

// isHex accepts lower-case digits only: mentionsSelf lower-cases before each pass.
func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' }

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
// computed references, unclassified directories, unsupported JSON, .tofu and
// .tofutest configuration, parse errors and symlinks. It also enforces FR-003:
// no backend or provider configuration in a library or stage, no
// terraform_remote_state in any configuration, and only generated *.tf files
// under stacks/ (test files are outside these rules). In a retained module
// (G7) every resource of a kept type keeps a literal prevent_destroy, a single
// type is declared once with a literal deletion_protection, and no removed
// block or external module is used; the state-backend component takes its buckets only from the
// protected module. No library, stage or instance calls nonsensitive (G2). A
// stage declares no resource, and each stage of singleComponentStages calls
// only one unrepeated instance of its component (G5, G6).
// Hidden directories are skipped unless a module call names one; fixture trees
// are skipped only under tests/ and tools/.
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
	// route sorts a file OpenTofu loads as module configuration: *.tf is
	// kept for its directory, the other formats are refused, anything else
	// is ignored.
	route := func(rel, p string) {
		switch {
		case strings.HasSuffix(rel, ".tf.json"), strings.HasSuffix(rel, ".tofu.json"):
			add("UNSUPPORTED_CONFIG", path.Dir(rel), "%s: JSON configuration is not supported", rel)
		case strings.HasSuffix(rel, ".tofu"):
			// OpenTofu loads *.tofu beside *.tf and lets it replace a
			// same-named *.tf file; refusing it keeps one file set to judge.
			add("UNSUPPORTED_CONFIG", path.Dir(rel), "%s: .tofu configuration is not supported, use .tf", rel)
		case strings.HasSuffix(rel, ".tf"):
			files[path.Dir(rel)] = append(files[path.Dir(rel)], p)
		}
	}
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
		case d.IsDir() && rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "fixtures" && (under(rel, "tests") || under(rel, "tools"))):
			// Hidden directories hold tool state such as .terraform and are
			// loaded only when a module call names one (load below); fixture
			// trees under tests/ and tools/ are checker input data (D91).
			return fs.SkipDir
		case d.IsDir():
			return nil
		case strings.HasSuffix(rel, ".tftest.json"):
			add("UNSUPPORTED_CONFIG", path.Dir(rel), "%s: JSON configuration is not supported", rel)
		case strings.HasSuffix(rel, ".tofutest.hcl"), strings.HasSuffix(rel, ".tofutest.json"):
			// OpenTofu runs these as test files too; refused like *.tofu.
			add("UNSUPPORTED_CONFIG", path.Dir(rel), "%s: .tofutest configuration is not supported, use .tftest.hcl", rel)
		case strings.HasSuffix(rel, ".tm.hcl"):
			configs = append(configs, rel)
		case strings.HasSuffix(rel, ".tftest.hcl"):
			tests[testOwner(rel)] = append(tests[testOwner(rel)], p)
		default:
			route(rel, p)
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

	// load reads a hidden directory a module call names, which the walk
	// skipped: OpenTofu loads it like any other module directory, so its
	// configuration files are routed as the walk routes them, and it is then
	// judged like a walked directory. Only that directory is read; a hidden
	// directory below it is loaded when a call names it in turn. A symlink on
	// the way to it or inside it is refused, as the walk refuses one.
	loaded := map[string]bool{}
	load := func(dir string) {
		if !cleanRelative(dir) || !hidden(dir) || loaded[dir] {
			return
		}
		loaded[dir] = true
		parts := strings.Split(dir, "/")
		for i := range parts {
			info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(strings.Join(parts[:i+1], "/"))))
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return
			case err != nil:
				add("UNREADABLE", dir, "%v", err)
				return
			case info.Mode()&fs.ModeSymlink != 0:
				add("UNREADABLE", dir, "%s is a symlink", strings.Join(parts[:i+1], "/"))
				return
			case !info.IsDir():
				return
			}
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			add("UNREADABLE", dir, "%v", err)
			return
		}
		for _, entry := range entries {
			rel := dir + "/" + entry.Name()
			switch {
			case entry.Type()&fs.ModeSymlink != 0:
				add("UNREADABLE", dir, "%s is a symlink", rel)
			case !entry.IsDir():
				route(rel, filepath.Join(root, filepath.FromSlash(rel)))
			}
		}
		dirs[dir] = len(files[dir]) > 0
	}

	// parse reads one configuration file, records its module sources resolved
	// against base, and returns whether Terramate generated it and its purity
	// facts.
	parse := func(subject, file, base string, uses, external map[string]bool) (generated bool, facts purity, body *hclsyntax.Body) {
		src, err := os.ReadFile(file)
		if err != nil {
			add("UNREADABLE", subject, "%v", err)
			return false, purity{}, nil
		}
		generated = bytes.HasPrefix(src, []byte(generatedHeader))
		parsed, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
		if diags.HasErrors() {
			add("PARSE_ERROR", subject, "%s", diags.Error())
			return generated, purity{}, nil
		}
		sources, computed := moduleSources(parsed.Body.(*hclsyntax.Body))
		if computed {
			add("UNRESOLVED_REFERENCE", subject, "a module source is not a literal string")
		}
		for _, source := range sources {
			local, isExternal, ok := resolve(base, source, dirs)
			if !ok && !isExternal {
				load(local)
				local, isExternal, ok = resolve(base, source, dirs)
			}
			switch {
			case !ok:
				add("UNRESOLVED_REFERENCE", subject, "module source %q resolves to no configuration in this repository", source)
			case isExternal:
				external[source] = true
			default:
				uses[local] = true
			}
		}
		body = parsed.Body.(*hclsyntax.Body)
		return generated, purityOf(body), body
	}

	// bucketDirs are the directories whose own code declares a bucket.
	bucketDirs := map[string]bool{}
	// remoteDirs are the directories that call a module by a non-relative
	// source.
	remoteDirs := map[string]bool{}
	// stageCalls are the module calls of the singleComponentStages' own
	// directories, by stage and calling directory.
	type stageCall struct {
		stage, dir string
		call       moduleCall
	}
	var stageCalls []stageCall
	// singles are the addresses (directory and name; an override block of the
	// same name is the same address) of each scanned retained package's
	// single type.
	singles := map[string]map[string]bool{}

	// judge parses one configuration directory, applies the FR-003 rules and
	// records its layer and edges.
	judge := func(dir string) {
		generated, handwritten := false, false
		var facts purity
		var bodies []*hclsyntax.Body
		uses, external := map[string]bool{}, map[string]bool{}
		for _, file := range files[dir] {
			fileGenerated, fileFacts, body := parse(dir, file, dir, uses, external)
			generated, handwritten = generated || fileGenerated, handwritten || !fileGenerated
			facts = facts.or(fileFacts)
			if body != nil {
				bodies = append(bodies, body)
			}
		}
		// FR-003: no configuration reads another's state, and stacks hold
		// only generated files, whatever layer the directory would get.
		if facts.remoteState {
			add("REMOTE_STATE", dir, "terraform_remote_state is not allowed")
		}
		if under(dir, "stacks") && handwritten {
			add("HANDWRITTEN_INSTANCE", dir, "a file under stacks/ lacks the Terramate generated header")
		}
		layer := classify(dir, generated)
		// FR-003: libraries and stages leave backend and provider to the
		// generated stack.
		if layer == LayerLibrary || layer == LayerStage {
			if facts.backend {
				add("LIBRARY_BACKEND", dir, "a %s declares a state backend", role(dir, layer))
			}
			if facts.provider {
				add("LIBRARY_PROVIDER_CONFIG", dir, "a %s configures a provider", role(dir, layer))
			}
		}
		// G2, configuration part: the envelope builder drops what tofu marks
		// sensitive, and a module's tests cannot enumerate its outputs, so no
		// code that is deployed may remove the mark (spec 005 T020).
		if layer == LayerLibrary || layer == LayerStage || layer == LayerInstance {
			for _, body := range bodies {
				if unmarks(body) {
					add("NONSENSITIVE_CALL", dir, "a %s calls nonsensitive(): a secret it unmarks is published as a plain output (G2)", role(dir, layer))
					break
				}
			}
		}
		// ADR-0002: a stage composes components; a resource of its own would
		// escape every component rule and test (T022).
		if layer == LayerStage {
			for _, body := range bodies {
				for _, r := range resources(body) {
					add("STAGE_RESOURCE", dir, "a stage declares resource %s; stages compose components, declare it in one", r)
				}
				for stage := range singleComponentStages {
					if under(dir, stage) {
						for _, c := range moduleCalls(body) {
							stageCalls = append(stageCalls, stageCall{stage, dir, c})
						}
					}
				}
			}
		}
		// G7: a retained package's library code keeps its retained resources.
		if pkg, retained, ok := retainedOf(dir); ok && layer == LayerLibrary {
			if singles[pkg] == nil {
				singles[pkg] = map[string]bool{}
			}
			for _, body := range bodies {
				for _, what := range unprotected(body, retained) {
					add("RETAINED_UNPROTECTED", dir, "retained module (G7): %s", what)
				}
				for _, block := range body.Blocks {
					if retained.single == "" || block.Type != "resource" || len(block.Labels) != 2 || block.Labels[0] != retained.single {
						continue
					}
					address := strings.Join(block.Labels, ".")
					singles[pkg][dir+"\x00"+block.Labels[1]] = true
					// One address must be one instance: a subdirectory can be
					// called more than once, and for_each multiplies the block.
					// count stays (adopt or not); the unit tests pin 0 or 1.
					if dir != pkg {
						add("RETAINED_UNPROTECTED", pkg, "retained module (G7): resource %s is declared in %s, outside the package root, where a repeated module call would multiply it", address, dir)
					}
					if _, ok := block.Body.Attributes["for_each"]; ok {
						add("RETAINED_UNPROTECTED", pkg, "retained module (G7): resource %s repeats by for_each; the package holds one instance", address)
					}
				}
			}
			// Only a relative source is the code scanned here: a remote one,
			// this repository's address with a ref included, is fetched from
			// elsewhere. Relative calls reach the package's own directories
			// (scanned) or naming (resource-free, and the only other module
			// the layer matrix admits).
			for _, body := range bodies {
				sources, _ := moduleSources(body)
				for _, source := range sources {
					if !strings.HasPrefix(source, "./") && !strings.HasPrefix(source, "../") {
						add("RETAINED_UNPROTECTED", dir, "retained module (G7): module source %q is not a relative path, its resources cannot be checked for prevent_destroy", source)
					}
				}
			}
		}
		// G7, component part: the state-backend component's own code
		// declares no bucket and calls modules by relative source only.
		var declared []string
		for _, body := range bodies {
			declared = append(declared, buckets(body)...)
		}
		if len(declared) > 0 {
			bucketDirs[dir] = true
		}
		// A non-relative source (an external module, or this repository's
		// address with a ref) is fetched code the scan does not see.
		var remoteSources []string
		for _, body := range bodies {
			sources, _ := moduleSources(body)
			for _, source := range sources {
				if !strings.HasPrefix(source, "./") && !strings.HasPrefix(source, "../") {
					remoteSources = append(remoteSources, source)
				}
			}
		}
		if len(remoteSources) > 0 {
			remoteDirs[dir] = true
		}
		if layer == LayerLibrary && under(dir, stateBackend) {
			for _, b := range declared {
				add("STATE_BUCKET_UNPROTECTED", dir, "state bucket (G7): resource %s is declared here, not through %s", b, protectedBuckets)
			}
			for _, source := range remoteSources {
				add("STATE_BUCKET_UNPROTECTED", dir, "state bucket (G7): module source %q is not a relative path, its buckets cannot be checked", source)
			}
		}
		if layer == "" {
			add("UNCLASSIFIED", dir, "directory belongs to no layer of ADR-0002")
			return
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
	// judgeAll judges every directory not judged yet, until a pass loads no
	// further hidden directory.
	judged := map[string]bool{}
	judgeAll := func() {
		for {
			pending := slices.DeleteFunc(slices.Sorted(maps.Keys(files)), func(dir string) bool { return judged[dir] })
			if len(pending) == 0 {
				return
			}
			for _, dir := range pending {
				judged[dir] = true
				judge(dir)
			}
		}
	}
	judgeAll()
	for _, owner := range slices.Sorted(maps.Keys(tests)) {
		uses := map[string]bool{}
		for _, file := range tests[owner] {
			parse(owner, file, owner, uses, map[string]bool{})
		}
		if len(uses) > 0 {
			g.TestUses[owner] = sortedSet(uses)
		}
	}
	// A test may run a hidden directory nothing else calls.
	judgeAll()

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
	// G7, component part: every bucket the state-backend component reaches
	// through its local module calls lies in the protected module, and no
	// reached module calls an external one (its code is not scanned). A
	// directory of the component itself is judged on its own (above).
	for _, dir := range slices.Sorted(maps.Keys(g.Layers)) {
		if g.Layers[dir] != LayerLibrary || !under(dir, stateBackend) {
			continue
		}
		seen := map[string]bool{dir: true}
		queue := slices.Clone(g.Uses[dir])
		for len(queue) > 0 {
			to := queue[0]
			queue = queue[1:]
			if seen[to] {
				continue
			}
			seen[to] = true
			queue = append(queue, g.Uses[to]...)
			if bucketDirs[to] && !under(to, stateBackend) && !under(to, protectedBuckets) {
				add("STATE_BUCKET_UNPROTECTED", dir, "state bucket (G7): %s declares a bucket outside %s", to, protectedBuckets)
			}
			// A non-relative call below a reached module escapes the walk.
			if remoteDirs[to] && !under(to, stateBackend) {
				add("STATE_BUCKET_UNPROTECTED", dir, "state bucket (G7): %s calls a module by a non-relative source, its buckets cannot be checked", to)
			}
		}
	}
	// G5/G6, stage part: the tenant-state stage's one bucket and two S3 users
	// come from one state-backend instance, the account-governance stage's
	// deployers, policies and groups from one identity instance. Every call is
	// relative (fetched code is not scanned), unrepeated, and reaches the
	// component's root (a subdirectory of it skips its naming, protection and
	// policies) or the stage's own directories; each target is called by at
	// most one module block, so it is instantiated at most once.
	// A block of the same name in the same directory (an override file) is
	// the same call.
	callers := map[string]map[string]map[string]bool{}
	for _, sc := range stageCalls {
		component := singleComponentStages[sc.stage]
		if !sc.call.literal {
			continue // UNRESOLVED_REFERENCE already
		}
		if !strings.HasPrefix(sc.call.source, "./") && !strings.HasPrefix(sc.call.source, "../") {
			add("STAGE_COMPONENT_CALLS", sc.stage, "%s: module %q (%s) is not a relative source; its code cannot be checked", sc.dir, sc.call.name, sc.call.source)
			continue
		}
		local, _, ok := resolve(sc.dir, sc.call.source, dirs)
		if !ok {
			continue // UNRESOLVED_REFERENCE already
		}
		if sc.call.repeated {
			add("STAGE_COMPONENT_CALLS", sc.stage, "%s: module %q repeats by count or for_each; the stage takes one %s instance", sc.dir, sc.call.name, component)
		}
		if local != component && !under(local, sc.stage) {
			add("STAGE_COMPONENT_CALLS", sc.stage, "%s: module %q (%s) is not %s, the only module this stage calls", sc.dir, sc.call.name, sc.call.source, component)
			continue
		}
		if callers[sc.stage] == nil {
			callers[sc.stage] = map[string]map[string]bool{}
		}
		if callers[sc.stage][local] == nil {
			callers[sc.stage][local] = map[string]bool{}
		}
		callers[sc.stage][local][sc.dir+"\x00"+sc.call.name] = true
	}
	for _, stage := range slices.Sorted(maps.Keys(callers)) {
		for _, target := range slices.Sorted(maps.Keys(callers[stage])) {
			if n := len(callers[stage][target]); n > 1 {
				add("STAGE_COMPONENT_CALLS", stage, "%d module blocks call %s; the stage takes one %s instance", n, target, singleComponentStages[stage])
			}
		}
	}
	// G7: a retained package with a single type declares it at exactly one
	// address of its root directory, without for_each (checked above); the
	// instance count of that address is the unit tests' (0 or 1). A package
	// with any configuration or test file left is held to the count, even
	// with no library code.
	for _, owners := range []map[string][]string{files, tests} {
		for dir := range owners {
			if pkg, _, ok := retainedOf(dir); ok && singles[pkg] == nil {
				singles[pkg] = map[string]bool{}
			}
		}
	}
	for _, pkg := range slices.Sorted(maps.Keys(singles)) {
		if single := retainedModules[pkg].single; single != "" {
			if n := len(singles[pkg]); n != 1 {
				add("RETAINED_UNPROTECTED", pkg, "retained module (G7): declares %d %s resources, want exactly one", n, single)
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
