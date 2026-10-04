package checks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validDependencies = "../../../tests/check/fixtures/dependencies/valid"

// The valid fixture covers every class ADR-0002 names: a nested subdirectory,
// an alias spelling, an example, a component with external sources, a stage
// using this repository's module as a released package, and a generated
// instance whose configuration lives in *.tm.hcl files.
func TestDependenciesGraph(t *testing.T) {
	g, findings := ScanDependencies(validDependencies)
	if len(findings) != 0 {
		t.Fatalf("BEHAVIORAL_RED: valid fixture rejected: %+v", findings)
	}
	wantLayers := map[string]string{
		"modules/naming":             LayerLibrary,
		"modules/net":                LayerLibrary,
		"modules/net/sub":            LayerLibrary,
		"modules/net/examples/basic": LayerExample,
		"components/runtime/kube":    LayerLibrary,
		"stages/platform":            LayerStage,
		"examples/solo":              LayerExample,
		"instances/prod":             LayerInstance,
	}
	if !reflect.DeepEqual(g.Layers, wantLayers) {
		t.Errorf("BEHAVIORAL_RED: layers\n got %v\nwant %v", g.Layers, wantLayers)
	}
	wantUses := map[string][]string{
		"modules/net":                {"modules/naming", "modules/net/sub"},
		"modules/net/sub":            {"modules/naming"},
		"modules/net/examples/basic": {"modules/net"},
		"components/runtime/kube":    {"modules/net"},
		"stages/platform":            {"components/runtime/kube", "modules/naming"},
		"examples/solo":              {"stages/platform"},
		"instances/prod":             {"stages/platform"},
	}
	if !reflect.DeepEqual(g.Uses, wantUses) {
		t.Errorf("BEHAVIORAL_RED: uses\n got %v\nwant %v", g.Uses, wantUses)
	}
	wantExternal := map[string][]string{
		"components/runtime/kube": {"git::https://github.com/example/other.git//modules/x?ref=v1.0.0", "ovh/thing/ovh"},
	}
	if !reflect.DeepEqual(g.External, wantExternal) {
		t.Errorf("BEHAVIORAL_RED: external\n got %v\nwant %v", g.External, wantExternal)
	}
	wantConfigs := map[string][]string{
		"instances/prod": {"instances/config.tm.hcl", "instances/prod/stack.tm.hcl"},
	}
	if !reflect.DeepEqual(g.Configs, wantConfigs) {
		t.Errorf("BEHAVIORAL_RED: generator configs\n got %v\nwant %v", g.Configs, wantConfigs)
	}
}

// Each change selects exactly its owning directory and every transitive
// consumer; shared tooling and unknown paths widen to the full suite; docs
// select nothing, and say so.
func TestDependenciesSelection(t *testing.T) {
	g, findings := ScanDependencies(validDependencies)
	if len(findings) != 0 {
		t.Fatalf("BEHAVIORAL_RED: valid fixture rejected: %+v", findings)
	}
	everything := []string{"components/runtime/kube", "examples/solo", "instances/prod", "modules/naming", "modules/net", "modules/net/examples/basic", "modules/net/sub", "stages/platform"}
	for name, c := range map[string]struct {
		changed []string
		want    Selection
	}{
		"shared leaf through alias and nested consumers": {[]string{"modules/naming/main.tf"}, Selection{Dirs: everything}},
		"nested subdirectory":                            {[]string{"modules/net/sub/main.tf"}, Selection{Dirs: []string{"components/runtime/kube", "examples/solo", "instances/prod", "modules/net", "modules/net/examples/basic", "modules/net/sub", "stages/platform"}}},
		"test file owned by its module":                  {[]string{"modules/net/tests/unit.tftest.hcl"}, Selection{Dirs: []string{"components/runtime/kube", "examples/solo", "instances/prod", "modules/net", "modules/net/examples/basic", "stages/platform"}}},
		"module documentation":                           {[]string{"modules/net/README.md"}, Selection{Dirs: []string{"components/runtime/kube", "examples/solo", "instances/prod", "modules/net", "modules/net/examples/basic", "stages/platform"}}},
		"leaf and consumers":                             {[]string{"components/runtime/kube/main.tf"}, Selection{Dirs: []string{"components/runtime/kube", "examples/solo", "instances/prod", "stages/platform"}}},
		"package boundary keeps its consumer":            {[]string{"stages/platform/main.tf"}, Selection{Dirs: []string{"examples/solo", "instances/prod", "stages/platform"}}},
		"example only":                                   {[]string{"examples/solo/main.tf"}, Selection{Dirs: []string{"examples/solo"}}},
		"generated instance":                             {[]string{"instances/prod/main.tf"}, Selection{Dirs: []string{"instances/prod"}}},
		"inherited generator configuration":              {[]string{"instances/config.tm.hcl"}, Selection{Dirs: []string{"instances/prod"}}},
		"generator configuration nobody inherits":        {[]string{"orphan.tm.hcl"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"shared tool":                                    {[]string{"tools/internal/checks/x.go"}, Selection{Full: true, Reason: "SHARED_TOOL"}},
		"task configuration":                             {[]string{"Taskfile.yml"}, Selection{Full: true, Reason: "SHARED_TOOL"}},
		"linter configuration":                           {[]string{".tflint.hcl"}, Selection{Full: true, Reason: "SHARED_TOOL"}},
		"documentation only":                             {[]string{"docs/guide.md"}, Selection{Reason: "DOCS_ONLY"}},
		"documentation and an example":                   {[]string{"docs/guide.md", "examples/solo/main.tf"}, Selection{Dirs: []string{"examples/solo"}}},
		"unknown path":                                   {[]string{"misc/thing.txt"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"removed module":                                 {[]string{"modules/gone/main.tf"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"unknown path wins over a known one":             {[]string{"examples/solo/main.tf", "misc/thing.txt"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"escaping path":                                  {[]string{"../outside.tf"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"no changes":                                     {nil, Selection{Reason: "NO_CHANGES"}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := SelectChanged(g, c.changed); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: %v selects %+v, want %+v", c.changed, got, c.want)
			}
		})
	}
}

// writeModules builds a small repository from path → HCL text.
func writeModules(t *testing.T, files map[string]string) string {
	t.Helper()
	return writeTree(t, files)
}

func ruleNames(findings []Finding) []string {
	return ruleSet(findings)
}

// Each control breaks one dependency rule and expects exactly its findings.
func TestDependenciesRejected(t *testing.T) {
	naming := `variable "name" { type = string }`
	for name, c := range map[string]struct {
		files map[string]string
		want  []string
	}{
		"module uses a component": {map[string]string{
			"modules/naming/main.tf":          `module "k" { source = "../../components/runtime/kube" }`,
			"components/runtime/kube/main.tf": `variable "x" {}`,
		}, []string{"LAYER_VIOLATION"}},
		"component uses a stage": {map[string]string{
			"components/runtime/kube/main.tf": `module "p" { source = "../../../stages/platform" }`,
			"stages/platform/main.tf":         `variable "x" {}`,
		}, []string{"LAYER_VIOLATION"}},
		"module uses a peer module": {map[string]string{
			"modules/naming/main.tf": naming,
			"modules/a/main.tf":      `module "b" { source = "../b" }`,
			"modules/b/main.tf":      `variable "x" {}`,
		}, []string{"LAYER_VIOLATION"}},
		"library uses an example": {map[string]string{
			"modules/a/main.tf":                `module "e" { source = "./examples/basic" }`,
			"modules/a/examples/basic/main.tf": `variable "x" {}`,
		}, []string{"LAYER_VIOLATION"}},
		"stage uses a generated instance": {map[string]string{
			"stages/platform/main.tf": `module "i" { source = "../../instances/prod" }`,
			"instances/prod/main.tf":  "// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT\nvariable \"x\" {}\n",
		}, []string{"LAYER_VIOLATION"}},
		"instance uses a component": {map[string]string{
			"components/runtime/kube/main.tf": `variable "x" {}`,
			"instances/prod/main.tf":          "// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT\nmodule \"k\" { source = \"../../components/runtime/kube\" }\n",
		}, []string{"LAYER_VIOLATION"}},
		"cycle inside a package": {map[string]string{
			"modules/a/main.tf":     `module "s" { source = "./sub" }`,
			"modules/a/sub/main.tf": `module "a" { source = "../" }`,
		}, []string{"CYCLE"}},
		"missing local module": {map[string]string{
			"modules/a/main.tf": `module "m" { source = "../missing" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"directory without configuration": {map[string]string{
			"modules/a/main.tf":   `module "m" { source = "../empty" }`,
			"modules/empty/x.txt": "not configuration",
		}, []string{"UNRESOLVED_REFERENCE"}},
		"computed source": {map[string]string{
			"modules/a/main.tf": "locals { p = \"../naming\" }\nmodule \"m\" { source = local.p }\n",
		}, []string{"UNRESOLVED_REFERENCE"}},
		"source outside the repository": {map[string]string{
			"modules/a/main.tf": `module "m" { source = "../../../outside" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own released module that does not exist": {map[string]string{
			"stages/platform/main.tf": `module "m" { source = "git::https://github.com/PlatformRelay/ovh-landing-zone-accelerator.git//modules/gone?ref=v1" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository without a subdirectory": {map[string]string{
			"stages/platform/main.tf": `module "m" { source = "github.com/PlatformRelay/ovh-landing-zone-accelerator?ref=v1" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"unclassified directory": {map[string]string{
			"misc/thing/main.tf": `variable "x" {}`,
		}, []string{"UNCLASSIFIED"}},
		"malformed configuration": {map[string]string{
			"modules/a/main.tf": `module "m" { source = `,
		}, []string{"PARSE_ERROR"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ScanDependencies(writeModules(t, c.files))
			if got := ruleNames(findings); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v (%+v)", got, c.want, findings)
			}
		})
	}
}

// Fixture trees are input data for checkers, not configuration of the
// repository that holds them, so a broken fixture is not reported.
func TestDependenciesIgnoreFixtures(t *testing.T) {
	root := writeModules(t, map[string]string{
		"modules/a/main.tf":                                  `variable "x" {}`,
		"tests/check/fixtures/bad/modules/x/main.tf":         `module "m" { source = "../missing" }`,
		"tools/cmd/lz-offline/fixtures/provider/main.tf":     `variable "x" {}`,
		"tests/check/fixtures/bad/misc/unclassified/main.tf": `variable "x" {}`,
	})
	g, findings := ScanDependencies(root)
	if len(findings) != 0 || !reflect.DeepEqual(g.Layers, map[string]string{"modules/a": LayerLibrary}) {
		t.Errorf("BEHAVIORAL_RED: fixture data scanned as configuration: %v %+v", g.Layers, findings)
	}
}

// A symlinked directory could alias a module under another layer, so it is
// refused rather than followed or skipped.
func TestDependenciesRefuseSymlinks(t *testing.T) {
	root := writeModules(t, map[string]string{"modules/a/main.tf": `variable "x" {}`})
	if err := os.Symlink(filepath.Join(root, "modules/a"), filepath.Join(root, "modules/alias")); err != nil {
		t.Fatal(err)
	}
	if _, findings := ScanDependencies(root); !reflect.DeepEqual(ruleNames(findings), []string{"UNREADABLE"}) {
		t.Errorf("BEHAVIORAL_RED: symlinked directory not refused: %+v", findings)
	}
	if _, findings := ScanDependencies(filepath.Join(root, "absent")); !reflect.DeepEqual(ruleNames(findings), []string{"UNREADABLE"}) {
		t.Errorf("BEHAVIORAL_RED: missing root not refused: %+v", findings)
	}
}

// Every dependency rule has a control in which it is the only rule.
func TestDependenciesRulesListed(t *testing.T) {
	want := []string{"CYCLE", "LAYER_VIOLATION", "PARSE_ERROR", "UNCLASSIFIED", "UNREADABLE", "UNRESOLVED_REFERENCE"}
	got := append([]string{}, DependencyRules...)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BEHAVIORAL_RED: DependencyRules = %v, want %v", got, want)
	}
}
