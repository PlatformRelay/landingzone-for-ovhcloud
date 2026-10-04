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
// an alias spelling, an example, components with external sources, this
// repository's module addressed as a released package in two spellings, a
// singleton component, a library with a generated file, test configuration
// that uses another module, and a generated instance whose configuration lives
// in inherited *.tm.hcl files.
func TestDependenciesGraph(t *testing.T) {
	g, findings := ScanDependencies(validDependencies)
	if len(findings) != 0 {
		t.Fatalf("BEHAVIORAL_RED: valid fixture rejected: %+v", findings)
	}
	wantLayers := map[string]string{
		"modules/naming":                     LayerLibrary,
		"modules/net":                        LayerLibrary,
		"modules/net/sub":                    LayerLibrary,
		"modules/net/examples/basic":         LayerExample,
		"modules/dns":                        LayerLibrary,
		"components/runtime/kube":            LayerLibrary,
		"components/account-baseline":        LayerLibrary,
		"components/account-baseline/policy": LayerLibrary,
		"stages/platform":                    LayerStage,
		"examples/solo":                      LayerExample,
		"instances/prod":                     LayerInstance,
	}
	if !reflect.DeepEqual(g.Layers, wantLayers) {
		t.Errorf("BEHAVIORAL_RED: layers\n got %v\nwant %v", g.Layers, wantLayers)
	}
	wantUses := map[string][]string{
		"modules/net":                 {"modules/naming", "modules/net/sub"},
		"modules/net/sub":             {"modules/naming"},
		"modules/net/examples/basic":  {"modules/net"},
		"modules/dns":                 {"modules/naming"},
		"components/runtime/kube":     {"modules/naming", "modules/net"},
		"components/account-baseline": {"components/account-baseline/policy", "modules/naming"},
		"stages/platform":             {"components/account-baseline", "components/runtime/kube"},
		"examples/solo":               {"components/runtime/kube"},
		"instances/prod":              {"stages/platform"},
	}
	if !reflect.DeepEqual(g.Uses, wantUses) {
		t.Errorf("BEHAVIORAL_RED: uses\n got %v\nwant %v", g.Uses, wantUses)
	}
	wantTestUses := map[string][]string{"modules/dns": {"modules/net"}}
	if !reflect.DeepEqual(g.TestUses, wantTestUses) {
		t.Errorf("BEHAVIORAL_RED: test uses\n got %v\nwant %v", g.TestUses, wantTestUses)
	}
	wantExternal := map[string][]string{
		"components/runtime/kube": {"git::https://github.com/example/other.git//modules/x?ref=v1.0.0", "ovh/thing/ovh"},
	}
	if !reflect.DeepEqual(g.External, wantExternal) {
		t.Errorf("BEHAVIORAL_RED: external\n got %v\nwant %v", g.External, wantExternal)
	}
	wantConfigs := map[string][]string{
		"instances/prod": {"instances/config.tm.hcl", "instances/prod/stack.tm.hcl"},
		"modules/dns":    {"modules/dns/versions.tm.hcl"},
	}
	if !reflect.DeepEqual(g.Configs, wantConfigs) {
		t.Errorf("BEHAVIORAL_RED: generator configs\n got %v\nwant %v", g.Configs, wantConfigs)
	}
}

// Each change selects exactly its owning directory and every transitive
// consumer, including modules whose tests use it; shared tooling and unknown
// paths widen to the full suite; documentation selects nothing, and says so.
func TestDependenciesSelection(t *testing.T) {
	g, findings := ScanDependencies(validDependencies)
	if len(findings) != 0 {
		t.Fatalf("BEHAVIORAL_RED: valid fixture rejected: %+v", findings)
	}
	// modules/dns is selected through its tests, not through module calls.
	netConsumers := []string{"components/runtime/kube", "examples/solo", "instances/prod", "modules/dns", "modules/net", "modules/net/examples/basic", "stages/platform"}
	for name, c := range map[string]struct {
		changed []string
		want    Selection
	}{
		"shared leaf through aliases and released-package spellings": {[]string{"modules/naming/main.tf"}, Selection{Dirs: []string{
			"components/account-baseline", "components/runtime/kube", "examples/solo", "instances/prod", "modules/dns", "modules/naming",
			"modules/net", "modules/net/examples/basic", "modules/net/sub", "stages/platform"}}},
		"nested subdirectory":                     {[]string{"modules/net/sub/main.tf"}, Selection{Dirs: []string{"components/runtime/kube", "examples/solo", "instances/prod", "modules/dns", "modules/net", "modules/net/examples/basic", "modules/net/sub", "stages/platform"}}},
		"test file owned by its module":           {[]string{"modules/net/tests/unit.tftest.hcl"}, Selection{Dirs: netConsumers}},
		"module documentation":                    {[]string{"modules/net/README.md"}, Selection{Dirs: netConsumers}},
		"test configuration of another module":    {[]string{"modules/dns/tests/integration.tftest.hcl"}, Selection{Dirs: []string{"modules/dns"}}},
		"generated file in a library":             {[]string{"modules/dns/versions.tf"}, Selection{Dirs: []string{"modules/dns"}}},
		"library generator configuration":         {[]string{"modules/dns/versions.tm.hcl"}, Selection{Dirs: []string{"modules/dns"}}},
		"leaf and consumers":                      {[]string{"components/runtime/kube/main.tf"}, Selection{Dirs: []string{"components/runtime/kube", "examples/solo", "instances/prod", "stages/platform"}}},
		"singleton component subdirectory":        {[]string{"components/account-baseline/policy/main.tf"}, Selection{Dirs: []string{"components/account-baseline", "components/account-baseline/policy", "instances/prod", "stages/platform"}}},
		"stage":                                   {[]string{"stages/platform/main.tf"}, Selection{Dirs: []string{"instances/prod", "stages/platform"}}},
		"example only":                            {[]string{"examples/solo/main.tf"}, Selection{Dirs: []string{"examples/solo"}}},
		"generated instance":                      {[]string{"instances/prod/main.tf"}, Selection{Dirs: []string{"instances/prod"}}},
		"inherited generator configuration":       {[]string{"instances/config.tm.hcl"}, Selection{Dirs: []string{"instances/prod"}}},
		"generator configuration nobody inherits": {[]string{"orphan.tm.hcl"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"shared tool":                             {[]string{"tools/internal/checks/x.go"}, Selection{Full: true, Reason: "SHARED_TOOL"}},
		"task configuration":                      {[]string{"Taskfile.yml"}, Selection{Full: true, Reason: "SHARED_TOOL"}},
		"linter configuration":                    {[]string{".tflint.hcl"}, Selection{Full: true, Reason: "SHARED_TOOL"}},
		"documentation only":                      {[]string{"docs/guide.md"}, Selection{Reason: "DOCS_ONLY"}},
		"root documentation":                      {[]string{"README.md"}, Selection{Reason: "DOCS_ONLY"}},
		"script under a documentation root":       {[]string{"docs/check.sh"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"data under the specs root":               {[]string{"specs/001-x/data.json"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"documentation and an example":            {[]string{"docs/guide.md", "examples/solo/main.tf"}, Selection{Dirs: []string{"examples/solo"}}},
		"unknown path":                            {[]string{"misc/thing.txt"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"removed module":                          {[]string{"modules/gone/main.tf"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"unknown path wins over a known one":      {[]string{"examples/solo/main.tf", "misc/thing.txt"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"escaping path":                           {[]string{"../outside.tf"}, Selection{Full: true, Reason: "UNKNOWN_PATH"}},
		"no changes":                              {nil, Selection{Reason: "NO_CHANGES"}},
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

const generated = "// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT\n"

// Each control breaks one dependency rule and expects exactly its findings.
func TestDependenciesRejected(t *testing.T) {
	none := `variable "x" {}`
	for name, c := range map[string]struct {
		files map[string]string
		want  []string
	}{
		// the ADR-0002 edge matrix
		"naming uses another module": {map[string]string{
			"modules/naming/main.tf": `module "a" { source = "../a" }`, "modules/a/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"module uses a peer module": {map[string]string{
			"modules/a/main.tf": `module "b" { source = "../b" }`, "modules/b/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"module uses a component": {map[string]string{
			"modules/a/main.tf": `module "k" { source = "../../components/runtime/kube" }`, "components/runtime/kube/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"component uses another component": {map[string]string{
			"components/runtime/kube/main.tf": `module "b" { source = "../../account-baseline" }`, "components/account-baseline/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"component uses a stage": {map[string]string{
			"components/runtime/kube/main.tf": `module "p" { source = "../../../stages/platform" }`, "stages/platform/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"stage uses a module directly": {map[string]string{
			"stages/platform/main.tf": `module "n" { source = "../../modules/naming" }`, "modules/naming/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"stage uses another stage": {map[string]string{
			"stages/a/main.tf": `module "b" { source = "../b" }`, "stages/b/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"example uses a stage": {map[string]string{
			"examples/solo/main.tf": `module "p" { source = "../../stages/platform" }`, "stages/platform/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"test configuration uses a stage": {map[string]string{
			"tests/contracts/main.tf": `module "p" { source = "../../stages/platform" }`, "stages/platform/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"library uses an example": {map[string]string{
			"modules/a/main.tf": `module "e" { source = "./examples/basic" }`, "modules/a/examples/basic/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"stage uses a generated instance": {map[string]string{
			"stages/platform/main.tf": `module "i" { source = "../../instances/prod" }`, "instances/prod/main.tf": generated + none,
		}, []string{"LAYER_VIOLATION"}},
		"instance uses a component": {map[string]string{
			"components/runtime/kube/main.tf": none, "instances/prod/main.tf": generated + `module "k" { source = "../../components/runtime/kube" }`,
		}, []string{"LAYER_VIOLATION"}},
		"generated file does not make a library an instance": {map[string]string{
			"modules/a/main.tf": `module "p" { source = "../../stages/platform" }`, "modules/a/versions.tf": generated + "terraform {}\n", "stages/platform/main.tf": none,
		}, []string{"LAYER_VIOLATION"}},
		"cycle inside a package": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "./sub" }`, "modules/a/sub/main.tf": `module "a" { source = "../" }`,
		}, []string{"CYCLE"}},
		// references
		"missing local module": {map[string]string{
			"modules/a/main.tf": `module "m" { source = "../missing" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"directory without configuration": {map[string]string{
			"modules/a/main.tf": `module "m" { source = "../empty" }`, "modules/empty/x.txt": "not configuration",
		}, []string{"UNRESOLVED_REFERENCE"}},
		"computed source": {map[string]string{
			"modules/a/main.tf": "locals { p = \"../naming\" }\nmodule \"m\" { source = local.p }\n",
		}, []string{"UNRESOLVED_REFERENCE"}},
		"source outside the repository": {map[string]string{
			"modules/a/main.tf": `module "m" { source = "../../../outside" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own released module that does not exist": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "git::https://github.com/PlatformRelay/ovh-landing-zone-accelerator.git//modules/gone?ref=v1" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository without a subdirectory": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "github.com/PlatformRelay/landingzone-for-ovhcloud?ref=v1" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository through ssh without a subdirectory": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "git::ssh://git@github.com/PlatformRelay/ovh-landing-zone-accelerator.git?ref=v1" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository in scp form to a missing module": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "git::git@github.com:platformrelay/OVH-Landing-Zone-Accelerator.git//modules/gone" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository in an unrecognised form": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "s3::https://bucket/ovh-landing-zone-accelerator/modules/naming.zip" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository under its current name in an unrecognised form": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "s3::https://bucket/LandingZone-for-OVHcloud/modules/naming.zip" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository as a .git path in an unrecognised form": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "s3::https://bucket/landingzone-for-ovhcloud.git/modules/naming" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository under its current name in scp form to a missing module": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "git::git@github.com:PlatformRelay/landingzone-for-ovhcloud.git//modules/gone" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"test configuration with a missing module": {map[string]string{
			"modules/a/main.tf": none, "modules/a/tests/unit.tftest.hcl": "run \"x\" {\n  module {\n    source = \"../missing\"\n  }\n}\n",
		}, []string{"UNRESOLVED_REFERENCE"}},
		"test run block uses a stage": {map[string]string{
			"modules/a/main.tf": none, "stages/platform/main.tf": none,
			"modules/a/tests/unit.tftest.hcl": "run \"x\" {\n  module {\n    source = \"../../stages/platform\"\n  }\n}\n",
		}, []string{"LAYER_VIOLATION"}},
		"absolute source": {map[string]string{
			"modules/a/main.tf": `module "m" { source = "/srv/checkout/modules/naming" }`, "modules/naming/main.tf": none,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"malformed test configuration": {map[string]string{
			"modules/a/main.tf": none, "modules/a/tests/unit.tftest.hcl": `run "x" {`,
		}, []string{"PARSE_ERROR"}},
		// classification and syntax
		"unclassified directory": {map[string]string{
			"misc/thing/main.tf": none,
		}, []string{"UNCLASSIFIED"}},
		"component family without a variant": {map[string]string{
			"components/runtime/main.tf": none,
		}, []string{"UNCLASSIFIED"}},
		"malformed configuration": {map[string]string{
			"modules/a/main.tf": `module "m" { source = `,
		}, []string{"PARSE_ERROR"}},
		"JSON configuration": {map[string]string{
			"modules/a/main.tf.json": `{"module": {"m": {"source": "../naming"}}}`,
		}, []string{"UNSUPPORTED_CONFIG"}},
		"JSON test configuration": {map[string]string{
			"modules/a/main.tf": none, "modules/a/tests/unit.tftest.json": `{}`,
		}, []string{"UNSUPPORTED_CONFIG"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ScanDependencies(writeModules(t, c.files))
			if got := ruleNames(findings); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v (%+v)", got, c.want, findings)
			}
		})
	}
}

// A repository whose name only contains this repository's name, under either
// the current or the former name, is external, not a mention of this one.
func TestDependenciesNameCollisionsAreExternal(t *testing.T) {
	for _, source := range []string{
		"git::https://github.com/example/my-landingzone-for-ovhcloud.git//modules/x?ref=v1",
		"git::https://github.com/example/landingzone-for-ovhcloud-fork.git//modules/x?ref=v1",
		"git::ssh://git@github.com/example/ovh-landing-zone-accelerator2.git//modules/x?ref=v1",
		"s3::https://bucket/old-ovh-landing-zone-accelerator/modules/naming.zip",
	} {
		t.Run(source, func(t *testing.T) {
			graph, findings := ScanDependencies(writeModules(t, map[string]string{"modules/a/main.tf": `module "m" { source = "` + source + `" }`}))
			if len(findings) != 0 || !reflect.DeepEqual(graph.External["modules/a"], []string{source}) {
				t.Errorf("BEHAVIORAL_RED: collision not external: %+v %v", findings, graph.External)
			}
		})
	}
}

// An absolute source naming a directory inside the scanned tree is refused as
// well: it does not survive a checkout elsewhere and would otherwise bypass the
// layer rules as an external source.
func TestDependenciesAbsoluteSourceInsideRoot(t *testing.T) {
	root := writeModules(t, map[string]string{"stages/platform/main.tf": `variable "x" {}`})
	if err := os.MkdirAll(filepath.Join(root, "modules/a"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.ToSlash(filepath.Join(root, "stages/platform"))
	if err := os.WriteFile(filepath.Join(root, "modules/a/main.tf"), []byte(`module "p" { source = "`+source+`" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, findings := ScanDependencies(root); !reflect.DeepEqual(ruleNames(findings), []string{"UNRESOLVED_REFERENCE"}) {
		t.Errorf("BEHAVIORAL_RED: absolute source inside the root accepted: %+v", findings)
	}
}

// A changed test file selects the module that runs it, even when its tests/
// directory also holds configuration of its own.
func TestDependenciesTestOwnerSelection(t *testing.T) {
	root := writeModules(t, map[string]string{
		"modules/a/main.tf":               `variable "x" {}`,
		"modules/a/tests/main.tf":         `variable "helper" {}`,
		"modules/a/tests/unit.tftest.hcl": `run "x" {}`,
	})
	g, findings := ScanDependencies(root)
	if len(findings) != 0 {
		t.Fatalf("valid tree rejected: %+v", findings)
	}
	if got := SelectChanged(g, []string{"modules/a/tests/unit.tftest.hcl"}); !reflect.DeepEqual(got, Selection{Dirs: []string{"modules/a"}}) {
		t.Errorf("BEHAVIORAL_RED: changed test file selects %+v, want modules/a", got)
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

// A configuration or test file that cannot be read is reported, not skipped.
func TestDependenciesUnreadableFiles(t *testing.T) {
	for _, name := range []string{"modules/a/main.tf", "modules/a/tests/unit.tftest.hcl"} {
		root := writeModules(t, map[string]string{"modules/a/main.tf": `variable "x" {}`, "modules/a/tests/unit.tftest.hcl": `run "x" {}`})
		if err := os.Chmod(filepath.Join(root, name), 0); err != nil {
			t.Fatal(err)
		}
		if _, findings := ScanDependencies(root); !reflect.DeepEqual(ruleNames(findings), []string{"UNREADABLE"}) {
			t.Errorf("BEHAVIORAL_RED: unreadable %s not reported: %+v", name, findings)
		}
	}
}

func TestDependenciesRulesListed(t *testing.T) {
	want := []string{"CYCLE", "LAYER_VIOLATION", "PARSE_ERROR", "UNCLASSIFIED", "UNREADABLE", "UNRESOLVED_REFERENCE", "UNSUPPORTED_CONFIG"}
	got := append([]string{}, DependencyRules...)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BEHAVIORAL_RED: DependencyRules = %v, want %v", got, want)
	}
}
