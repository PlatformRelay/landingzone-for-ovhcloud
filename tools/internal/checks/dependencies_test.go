package checks

import (
	"maps"
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
		"own repository encoded in a query value": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "https://mirror.example/module?source=github.com%2FPlatformRelay%2Fovh-landing-zone-accelerator.git" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository under its current name, encoded twice": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "https://mirror.example/module?source=github.com%252FPlatformRelay%252Flandingzone-for-ovhcloud" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository encoded twice beside an escape that decodes to a malformed one": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "https://mirror.example/module?source=github.com%252FPlatformRelay%252Flandingzone-for-ovhcloud&label=100%25" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository encoded beside a malformed escape": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "https://mirror.example/module?label=100%&source=github.com%2Fplatformrelay%2Fovh-landing-zone-accelerator" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository with its last character escaped": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "https://mirror.example/module?source=platformrelay%2Flandingzone-for-ovhclou%64" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository with an escape that decodes to upper case": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "https://mirror.example/module?source=platformrelay%2F%4Candingzone-for-ovhcloud" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"own repository after another separator": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "https://mirror.example/x#landingzone-for-ovhcloud,ref" }`,
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
		"https://mirror.example/module?source=github.com%2Fexample%2Fmy-landingzone-for-ovhcloud.git",
		"git::https://github.com/example/landingzone-for-ovhcloud.v2.git//modules/x?ref=v1",
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

const purityDependencies = "../../../tests/check/fixtures/dependencies/purity"

// FR-003, ADR-0004: only generated stacks configure backend and provider. The
// purity fixture is the accepted shape: a generated stack with backend and
// provider calling a stage, the stage passing providers to a component, the
// component calling a module and naming, a module whose test files configure
// and mock providers, and an example and a live probe root (both their own
// roots) with backend and provider configuration.
func TestDependenciesPurityAccepted(t *testing.T) {
	g, findings := ScanDependencies(purityDependencies)
	if len(findings) != 0 {
		t.Fatalf("BEHAVIORAL_RED: purity fixture rejected: %+v", findings)
	}
	wantLayers := map[string]string{
		"stacks/account/bootstrap":                     LayerInstance,
		"stages/bootstrap":                             LayerStage,
		"components/state-backend":                     LayerLibrary,
		"modules/naming":                               LayerLibrary,
		"modules/object-storage-protected":             LayerLibrary,
		"modules/object-storage-protected/tests/setup": LayerTest,
		"examples/state-backend":                       LayerExample,
		"tests/live/probes/state-backend":              LayerTest,
	}
	if !reflect.DeepEqual(g.Layers, wantLayers) {
		t.Errorf("BEHAVIORAL_RED: layers\n got %v\nwant %v", g.Layers, wantLayers)
	}
	wantUses := map[string][]string{
		"stacks/account/bootstrap":         {"stages/bootstrap"},
		"stages/bootstrap":                 {"components/state-backend"},
		"components/state-backend":         {"modules/naming", "modules/object-storage-protected"},
		"modules/object-storage-protected": {"modules/naming"},
		"examples/state-backend":           {"components/state-backend"},
	}
	if !reflect.DeepEqual(g.Uses, wantUses) {
		t.Errorf("BEHAVIORAL_RED: uses\n got %v\nwant %v", g.Uses, wantUses)
	}
	wantTestUses := map[string][]string{"modules/object-storage-protected": {"modules/object-storage-protected/tests/setup"}}
	if !reflect.DeepEqual(g.TestUses, wantTestUses) {
		t.Errorf("BEHAVIORAL_RED: test uses\n got %v\nwant %v", g.TestUses, wantTestUses)
	}
}

// Each control breaks one purity rule of FR-003 (or several, named) in an
// otherwise valid tree, and expects exactly those rules, reported against the
// offending directory. A Terramate-generated file is held to the rules of the
// directory it sits in: generation does not make library code a stack.
func TestDependenciesPurityRejected(t *testing.T) {
	none := `variable "x" {}`
	backend := "terraform {\n  backend \"s3\" {\n    key = \"x.tfstate\"\n  }\n}\n"
	provider := "provider \"ovh\" {\n  endpoint = \"ovh-eu\"\n}\n"
	remoteState := "data \"terraform_remote_state\" \"up\" {\n  backend = \"s3\"\n  config  = {}\n}\n"
	stack := generated + "module \"stage\" {\n  source = \"../../../stages/platform\"\n}\n"
	for name, c := range map[string]struct {
		files   map[string]string
		subject string
		want    []string
	}{
		// backend
		"module declares a backend": {map[string]string{
			"modules/a/main.tf": backend,
		}, "modules/a", []string{"LIBRARY_BACKEND"}},
		"nested module directory declares a backend": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "./sub" }`, "modules/a/sub/main.tf": backend,
		}, "modules/a/sub", []string{"LIBRARY_BACKEND"}},
		"naming declares a local backend": {map[string]string{
			"modules/naming/main.tf": "terraform {\n  backend \"local\" {}\n}\n",
		}, "modules/naming", []string{"LIBRARY_BACKEND"}},
		"component declares a backend": {map[string]string{
			"components/runtime/kube/main.tf": backend,
		}, "components/runtime/kube", []string{"LIBRARY_BACKEND"}},
		"singleton component declares a backend": {map[string]string{
			"components/state-backend/main.tf": backend,
		}, "components/state-backend", []string{"LIBRARY_BACKEND"}},
		"stage declares a backend": {map[string]string{
			"stages/platform/main.tf": backend,
		}, "stages/platform", []string{"LIBRARY_BACKEND"}},
		"stage declares a cloud block": {map[string]string{
			"stages/platform/main.tf": "terraform {\n  cloud {\n    organization = \"x\"\n  }\n}\n",
		}, "stages/platform", []string{"LIBRARY_BACKEND"}},
		"backend in a second terraform block of a module": {map[string]string{
			"modules/a/main.tf": "terraform {\n  required_version = \">= 1.13.0\"\n}\n", "modules/a/backend.tf": backend,
		}, "modules/a", []string{"LIBRARY_BACKEND"}},
		"generated backend in a module": {map[string]string{
			"modules/a/main.tf": none, "modules/a/backend.tf": generated + backend,
		}, "modules/a", []string{"LIBRARY_BACKEND"}},
		// provider configuration
		"module configures a provider": {map[string]string{
			"modules/a/main.tf": provider,
		}, "modules/a", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"module configures an empty provider block": {map[string]string{
			"modules/a/main.tf": `provider "ovh" {}`,
		}, "modules/a", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"component configures an aliased provider": {map[string]string{
			"components/runtime/kube/main.tf": "provider \"ovh\" {\n  alias = \"admin\"\n}\n",
		}, "components/runtime/kube", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"singleton component subdirectory configures a provider": {map[string]string{
			"components/account-baseline/main.tf": `module "p" { source = "./policy" }`, "components/account-baseline/policy/main.tf": provider,
		}, "components/account-baseline/policy", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"stage configures a provider": {map[string]string{
			"stages/platform/main.tf": provider,
		}, "stages/platform", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"generated provider in a module": {map[string]string{
			"modules/a/main.tf": none, "modules/a/providers.tf": generated + provider,
		}, "modules/a", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"provider in a module's override file": {map[string]string{
			"modules/a/main.tf": none, "modules/a/override.tf": provider,
		}, "modules/a", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"stage with backend and provider": {map[string]string{
			"stages/platform/main.tf": backend + provider,
		}, "stages/platform", []string{"LIBRARY_BACKEND", "LIBRARY_PROVIDER_CONFIG"}},
		// terraform_remote_state, in any directory
		"module reads remote state": {map[string]string{
			"modules/a/main.tf": remoteState,
		}, "modules/a", []string{"REMOTE_STATE"}},
		"component reads remote state": {map[string]string{
			"components/runtime/kube/main.tf": remoteState,
		}, "components/runtime/kube", []string{"REMOTE_STATE"}},
		"stage reads remote state": {map[string]string{
			"stages/platform/main.tf": remoteState,
		}, "stages/platform", []string{"REMOTE_STATE"}},
		"stage reads remote state inside a check block": {map[string]string{
			"stages/platform/main.tf": "check \"upstream\" {\n" + remoteState + "  assert {\n    condition     = true\n    error_message = \"x\"\n  }\n}\n",
		}, "stages/platform", []string{"REMOTE_STATE"}},
		"stage reads remote state before a check block": {map[string]string{
			"stages/platform/main.tf": remoteState + "check \"later\" {\n  assert {\n    condition     = true\n    error_message = \"x\"\n  }\n}\n",
		}, "stages/platform", []string{"REMOTE_STATE"}},
		"generated stack reads remote state": {map[string]string{
			"stages/platform/main.tf": none, "stacks/account/platform/main.tf": stack, "stacks/account/platform/upstream.tf": generated + remoteState,
		}, "stacks/account/platform", []string{"REMOTE_STATE"}},
		"example reads remote state": {map[string]string{
			"examples/solo/main.tf": remoteState,
		}, "examples/solo", []string{"REMOTE_STATE"}},
		"live probe root reads remote state": {map[string]string{
			"tests/live/probes/x/main.tf": remoteState,
		}, "tests/live/probes/x", []string{"REMOTE_STATE"}},
		"module test helper reads remote state": {map[string]string{
			"modules/a/main.tf": none, "modules/a/tests/setup/main.tf": remoteState,
		}, "modules/a/tests/setup", []string{"REMOTE_STATE"}},
		// hand-written configuration in a generated stack
		"hand-written file beside generated ones in a stack": {map[string]string{
			"stages/platform/main.tf": none, "stacks/account/platform/main.tf": stack, "stacks/account/platform/extra.tf": none,
		}, "stacks/account/platform", []string{"HANDWRITTEN_INSTANCE"}},
		"hand-written override file in a stack": {map[string]string{
			"stages/platform/main.tf": none, "stacks/account/platform/main.tf": stack, "stacks/account/platform/override.tf": "module \"stage\" {\n  source = \"../../../stages/platform\"\n}\n",
		}, "stacks/account/platform", []string{"HANDWRITTEN_INSTANCE"}},
		"generator header not on the first line of a stack file": {map[string]string{
			"stages/platform/main.tf": none, "stacks/account/platform/main.tf": stack, "stacks/account/platform/extra.tf": "# edited by hand\n" + generated + none,
		}, "stacks/account/platform", []string{"HANDWRITTEN_INSTANCE"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ScanDependencies(writeModules(t, c.files))
			if got := ruleNames(findings); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v (%+v)", got, c.want, findings)
			}
			for _, f := range findings {
				if f.Subject != c.subject {
					t.Errorf("BEHAVIORAL_RED: %s reported against %q, want %q", f.Rule, f.Subject, c.subject)
				}
			}
		})
	}
}

// A stack directory with no generated file at all is hand-written: it is
// reported as such, against its own directory, whether or not it is also
// unclassified. A tests/, examples/ or fixtures/ segment under stacks/ does not
// exempt it.
func TestDependenciesHandwrittenStacks(t *testing.T) {
	for name, c := range map[string]struct {
		files   map[string]string
		subject string
	}{
		"hand-written stack":            {map[string]string{"stages/platform/main.tf": `variable "x" {}`, "stacks/account/platform/main.tf": `module "stage" { source = "../../../stages/platform" }`}, "stacks/account/platform"},
		"hand-written nested stack":     {map[string]string{"stages/platform/main.tf": `variable "x" {}`, "stacks/tenants/demo/dev/project/main.tf": `module "stage" { source = "../../../../../stages/platform" }`}, "stacks/tenants/demo/dev/project"},
		"hand-written file at the root": {map[string]string{"stacks/main.tf": `variable "x" {}`}, "stacks"},
		"hand-written tests directory in a stack": {map[string]string{
			"components/runtime/kube/main.tf": `variable "x" {}`, "stacks/account/platform/tests/probe/main.tf": `module "k" { source = "../../../../../components/runtime/kube" }`,
		}, "stacks/account/platform/tests/probe"},
		"hand-written examples directory under stacks": {map[string]string{
			"components/runtime/kube/main.tf": `variable "x" {}`, "stacks/examples/solo/main.tf": "provider \"ovh\" {}\nmodule \"k\" { source = \"../../../components/runtime/kube\" }\n",
		}, "stacks/examples/solo"},
		"hand-written fixtures directory under stacks": {map[string]string{
			"stages/platform/main.tf": `variable "x" {}`, "stacks/fixtures/platform/main.tf": `module "stage" { source = "../../../stages/platform" }`,
		}, "stacks/fixtures/platform"},
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ScanDependencies(writeModules(t, c.files))
			reported := false
			for _, f := range findings {
				switch {
				case f.Rule == "HANDWRITTEN_INSTANCE" && f.Subject == c.subject:
					reported = true
				case f.Rule != "HANDWRITTEN_INSTANCE" && f.Rule != "UNCLASSIFIED":
					t.Errorf("BEHAVIORAL_RED: unexpected rule %s: %+v", f.Rule, findings)
				}
			}
			if !reported {
				t.Errorf("BEHAVIORAL_RED: hand-written stack %s not reported: %+v", c.subject, findings)
			}
		})
	}
}

// OpenTofu also reads *.tofu (and *.tofu.json) configuration files, so they
// cannot be a way around the purity rules: each is either refused as
// unsupported, as JSON configuration is, or held to the same rule as a *.tf
// file in its place.
func TestDependenciesTofuFiles(t *testing.T) {
	backend := "terraform {\n  backend \"s3\" {}\n}\n"
	stack := generated + "module \"stage\" {\n  source = \"../../../stages/platform\"\n}\n"
	for name, c := range map[string]struct {
		files   map[string]string
		subject string
		rule    string
	}{
		"backend in a module's .tofu file": {map[string]string{
			"modules/a/main.tf": `variable "x" {}`, "modules/a/backend.tofu": backend,
		}, "modules/a", "LIBRARY_BACKEND"},
		"provider in a stage's only .tofu file": {map[string]string{
			"stages/platform/main.tofu": `provider "ovh" {}`,
		}, "stages/platform", "LIBRARY_PROVIDER_CONFIG"},
		"remote state in a component's .tofu file": {map[string]string{
			"components/runtime/kube/main.tf": `variable "x" {}`, "components/runtime/kube/up.tofu": "data \"terraform_remote_state\" \"up\" {\n  backend = \"s3\"\n}\n",
		}, "components/runtime/kube", "REMOTE_STATE"},
		"hand-written .tofu file in a stack": {map[string]string{
			"stages/platform/main.tf": `variable "x" {}`, "stacks/account/platform/main.tf": stack, "stacks/account/platform/extra.tofu": `variable "x" {}`,
		}, "stacks/account/platform", "HANDWRITTEN_INSTANCE"},
		"JSON .tofu file in a module": {map[string]string{
			"modules/a/main.tf": `variable "x" {}`, "modules/a/backend.tofu.json": `{"terraform": {"backend": {"s3": {}}}}`,
		}, "modules/a", "UNSUPPORTED_CONFIG"},
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ScanDependencies(writeModules(t, c.files))
			got := ruleNames(findings)
			if !reflect.DeepEqual(got, []string{c.rule}) && !reflect.DeepEqual(got, []string{"UNSUPPORTED_CONFIG"}) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want [%s] or [UNSUPPORTED_CONFIG] (%+v)", got, c.rule, findings)
			}
			for _, f := range findings {
				if f.Subject != c.subject {
					t.Errorf("BEHAVIORAL_RED: %s reported against %q, want %q", f.Rule, f.Subject, c.subject)
				}
			}
		})
	}
}

// A generated stack file that does not parse is a parse error, not also a
// hand-written file: the header decides, whether or not the rest parses.
func TestDependenciesGeneratedParseError(t *testing.T) {
	_, findings := ScanDependencies(writeModules(t, map[string]string{
		"stages/platform/main.tf":         `variable "x" {}`,
		"stacks/account/platform/main.tf": generated + "module \"stage\" {\n  source = \"../../../stages/platform\"\n}\n",
		"stacks/account/platform/bad.tf":  generated + "provider \"ovh\" {\n",
	}))
	if got := ruleNames(findings); !reflect.DeepEqual(got, []string{"PARSE_ERROR"}) {
		t.Errorf("BEHAVIORAL_RED: rules %v, want [PARSE_ERROR] (%+v)", got, findings)
	}
}

const escapeDependencies = "../../../tests/check/fixtures/dependencies/escapes"

// D91: the shapes the escape-route rules must keep accepting. Test helpers and
// examples below a package root of a module, component, singleton component
// and stage are their own roots and may configure backend and provider, also
// one level down; a generated instance outside stacks/ may too (ADR-0002
// tenant layout); fixture data under tests/ and tools/, at any depth, is not
// configuration and is not reported however broken.
func TestDependenciesEscapeRoutesAccepted(t *testing.T) {
	g, findings := ScanDependencies(escapeDependencies)
	if len(findings) != 0 {
		t.Fatalf("BEHAVIORAL_RED: escape-route fixture rejected: %+v", findings)
	}
	wantLayers := map[string]string{
		"modules/object-store":                    LayerLibrary,
		"modules/object-store/tests/setup":        LayerTest,
		"modules/object-store/examples/basic":     LayerExample,
		"modules/object-store/sub/tests/helper":   LayerTest,
		"components/runtime/kube":                 LayerLibrary,
		"components/runtime/kube/tests/setup":     LayerTest,
		"components/state-backend":                LayerLibrary,
		"components/state-backend/examples/basic": LayerExample,
		"stages/platform":                         LayerStage,
		"stages/platform/tests/setup":             LayerTest,
		"stages/platform/examples/solo":           LayerExample,
		"live/prod":                               LayerInstance,
	}
	if !reflect.DeepEqual(g.Layers, wantLayers) {
		t.Errorf("BEHAVIORAL_RED: layers\n got %v\nwant %v", g.Layers, wantLayers)
	}
}

// D91 closes two routes around the FR-003 purity rules and the layer matrix,
// found in T006's review: a directory named or nested under fixtures outside
// tests/ and tools/ was skipped, and a package named tests or examples took the
// test or example layer. A .tofutest.hcl or .tofutest.json file, which OpenTofu
// loads as test configuration, is refused as unsupported. Each control expects
// exactly its rules, reported against the offending directory.
func TestDependenciesEscapeRoutesRejected(t *testing.T) {
	none := `variable "x" {}`
	backend := "terraform {\n  backend \"s3\" {\n    key = \"x.tfstate\"\n  }\n}\n"
	provider := "provider \"ovh\" {\n  endpoint = \"ovh-eu\"\n}\n"
	remoteState := "data \"terraform_remote_state\" \"up\" {\n  backend = \"s3\"\n  config  = {}\n}\n"
	for name, c := range map[string]struct {
		files   map[string]string
		subject string
		want    []string
	}{
		// route 1: fixtures outside tests/ and tools/
		"module named fixtures declares a backend": {map[string]string{
			"modules/fixtures/main.tf": backend,
		}, "modules/fixtures", []string{"LIBRARY_BACKEND"}},
		"fixtures directory in a module configures a provider": {map[string]string{
			"modules/a/main.tf": none, "modules/a/fixtures/main.tf": provider,
		}, "modules/a/fixtures", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"component directory nested under fixtures declares a backend": {map[string]string{
			"components/runtime/kube/main.tf": none, "components/runtime/kube/fixtures/basic/main.tf": backend,
		}, "components/runtime/kube/fixtures/basic", []string{"LIBRARY_BACKEND"}},
		"component family named fixtures configures a provider": {map[string]string{
			"components/fixtures/kube/main.tf": provider,
		}, "components/fixtures/kube", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"stage directory nested under fixtures reads remote state": {map[string]string{
			"stages/platform/main.tf": none, "stages/platform/fixtures/x/main.tf": remoteState,
		}, "stages/platform/fixtures/x", []string{"REMOTE_STATE"}},
		"fixtures at the repository root read remote state": {map[string]string{
			"fixtures/x/main.tf": remoteState,
		}, "fixtures/x", []string{"REMOTE_STATE", "UNCLASSIFIED"}},
		"fixtures in a module package named tests declares a backend": {map[string]string{
			"modules/tests/fixtures/x/main.tf": backend,
		}, "modules/tests/fixtures/x", []string{"LIBRARY_BACKEND"}},
		"fixtures in a module package named tools declares a backend": {map[string]string{
			"modules/tools/fixtures/x/main.tf": backend,
		}, "modules/tools/fixtures/x", []string{"LIBRARY_BACKEND"}},
		// route 2: a package named tests or examples is library code
		"module named tests declares a backend": {map[string]string{
			"modules/tests/main.tf": backend,
		}, "modules/tests", []string{"LIBRARY_BACKEND"}},
		"module named examples configures a provider": {map[string]string{
			"modules/examples/main.tf": provider,
		}, "modules/examples", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"subdirectory of a module named tests declares a backend": {map[string]string{
			"modules/tests/main.tf": `module "s" { source = "./sub" }`, "modules/tests/sub/main.tf": backend,
		}, "modules/tests/sub", []string{"LIBRARY_BACKEND"}},
		"component named tests configures a provider": {map[string]string{
			"components/runtime/tests/main.tf": provider,
		}, "components/runtime/tests", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"component named examples declares a backend": {map[string]string{
			"components/runtime/examples/main.tf": backend,
		}, "components/runtime/examples", []string{"LIBRARY_BACKEND"}},
		"component family named tests declares a backend": {map[string]string{
			"components/tests/kube/main.tf": backend,
		}, "components/tests/kube", []string{"LIBRARY_BACKEND"}},
		"component family named tests without a variant": {map[string]string{
			"components/tests/main.tf": backend,
		}, "components/tests", []string{"UNCLASSIFIED"}},
		"stage named examples declares a backend": {map[string]string{
			"stages/examples/main.tf": backend,
		}, "stages/examples", []string{"LIBRARY_BACKEND"}},
		"stage named tests configures a provider": {map[string]string{
			"stages/tests/main.tf": provider,
		}, "stages/tests", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"stage named examples uses a module directly": {map[string]string{
			"stages/examples/main.tf": `module "n" { source = "../../modules/naming" }`, "modules/naming/main.tf": none,
		}, "stages/examples", []string{"LAYER_VIOLATION"}},
		"tests directory outside any package root declares a backend (T067 review)": {map[string]string{
			"ops/tests/main.tf": backend,
		}, "ops/tests", []string{"UNCLASSIFIED"}},
		"examples directory outside any package root configures a provider (T067 review)": {map[string]string{
			"misc/examples/x/main.tf": provider,
		}, "misc/examples/x", []string{"UNCLASSIFIED"}},
		"generated stack directory with a tests segment uses a component (T067 review)": {map[string]string{
			"components/runtime/kube/main.tf": none,
			"stacks/a/p/tests/probe/main.tf":  generated + "module \"k\" {\n  source = \"../../../../../components/runtime/kube\"\n}\n",
		}, "stacks/a/p/tests/probe", []string{"LAYER_VIOLATION"}},
		// OpenTofu test files
		".tofutest.hcl file runs a stage": {map[string]string{
			"modules/a/main.tf": none, "stages/platform/main.tf": none,
			"modules/a/tests/unit.tofutest.hcl": "run \"x\" {\n  module {\n    source = \"../../stages/platform\"\n  }\n}\n",
		}, "modules/a/tests", []string{"UNSUPPORTED_CONFIG"}},
		".tofutest.hcl file beside a module": {map[string]string{
			"modules/a/main.tf": none, "modules/a/unit.tofutest.hcl": `run "x" {}`,
		}, "modules/a", []string{"UNSUPPORTED_CONFIG"}},
		".tofutest.json file": {map[string]string{
			"modules/a/main.tf": none, "modules/a/tests/unit.tofutest.json": `{"run": {"x": {}}}`,
		}, "modules/a/tests", []string{"UNSUPPORTED_CONFIG"}},
		// route 4 (T067): a called hidden directory is loaded and judged
		"module calls a hidden directory that declares a backend": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "./.impl" }`, "modules/a/.impl/main.tf": backend,
		}, "modules/a/.impl", []string{"LIBRARY_BACKEND"}},
		"stage calls a hidden directory that uses a module directly": {map[string]string{
			"stages/platform/main.tf": `module "x" { source = "./.x" }`, "modules/naming/main.tf": none,
			"stages/platform/.x/main.tf": `module "n" { source = "../../../modules/naming" }`,
		}, "stages/platform/.x", []string{"LAYER_VIOLATION"}},
		"hidden directory called from a hidden directory reads remote state": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "./.impl" }`, "modules/a/.impl/main.tf": `module "d" { source = "./.deeper" }`,
			"modules/a/.impl/.deeper/main.tf": remoteState,
		}, "modules/a/.impl/.deeper", []string{"REMOTE_STATE"}},
		"released address of a hidden directory configures a provider": {map[string]string{
			"components/runtime/kube/main.tf": `module "m" { source = "git::https://github.com/PlatformRelay/landingzone-for-ovhcloud.git//modules/a/.impl?ref=v1" }`,
			"modules/a/main.tf":               none, "modules/a/.impl/main.tf": provider,
		}, "modules/a/.impl", []string{"LIBRARY_PROVIDER_CONFIG"}},
		"hidden directory at the repository root is unclassified": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "../../.shared/x" }`, ".shared/x/main.tf": none,
		}, ".shared/x", []string{"UNCLASSIFIED"}},
		".tofu file in a called hidden directory": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "./.impl" }`, "modules/a/.impl/main.tf": none, "modules/a/.impl/override.tofu": none,
		}, "modules/a/.impl", []string{"UNSUPPORTED_CONFIG"}},
		"test runs a stage in a hidden directory": {map[string]string{
			"modules/a/main.tf": none, "stages/platform/.x/main.tf": none,
			"modules/a/tests/unit.tftest.hcl": "run \"x\" {\n  module {\n    source = \"../../stages/platform/.x\"\n  }\n}\n",
		}, "modules/a", []string{"LAYER_VIOLATION"}},
		"stack calls a hand-written hidden directory": {map[string]string{
			"stacks/account/platform/main.tf":        generated + "module \"x\" {\n  source = \"./.extra\"\n}\n",
			"stacks/account/platform/.extra/main.tf": provider,
		}, "stacks/account/platform/.extra", []string{"HANDWRITTEN_INSTANCE", "UNCLASSIFIED"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ScanDependencies(writeModules(t, c.files))
			if got := ruleNames(findings); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v (%+v)", got, c.want, findings)
			}
			for _, f := range findings {
				if f.Subject != c.subject {
					t.Errorf("BEHAVIORAL_RED: %s reported against %q, want %q", f.Rule, f.Subject, c.subject)
				}
			}
		})
	}
}

// T067 route 4: a hidden directory nobody calls stays skipped (tool state such
// as .terraform holds downloaded modules with provider configuration), while a
// called one is loaded like any module directory: classified, its edges kept,
// and a symlink on the way to it or inside it refused.
func TestDependenciesHiddenDirectories(t *testing.T) {
	none := `variable "x" {}`
	backend := "terraform {\n  backend \"s3\" {\n    key = \"x.tfstate\"\n  }\n}\n"
	provider := "provider \"ovh\" {\n  endpoint = \"ovh-eu\"\n}\n"
	g, findings := ScanDependencies(writeModules(t, map[string]string{
		"modules/a/main.tf": none, "modules/a/.scratch/main.tf": backend,
		".terraform/modules/x/main.tf": provider, "modules/a/.terraform/modules/y/main.tf": backend,
		"modules/b/main.tf": `module "s" { source = "./.impl" }`, "modules/b/.impl/main.tf": none,
	}))
	if len(findings) != 0 {
		t.Errorf("BEHAVIORAL_RED: hidden directories rejected: %+v", findings)
	}
	wantLayers := map[string]string{"modules/a": LayerLibrary, "modules/b": LayerLibrary, "modules/b/.impl": LayerLibrary}
	if !reflect.DeepEqual(g.Layers, wantLayers) {
		t.Errorf("BEHAVIORAL_RED: layers\n got %v\nwant %v", g.Layers, wantLayers)
	}
	if want := map[string][]string{"modules/b": {"modules/b/.impl"}}; !reflect.DeepEqual(g.Uses, want) {
		t.Errorf("BEHAVIORAL_RED: uses\n got %v\nwant %v", g.Uses, want)
	}

	// Each link points outside the repository at configuration with a
	// backend: following it would report LIBRARY_BACKEND instead.
	outside := writeModules(t, map[string]string{"main.tf": backend, "impl/main.tf": backend})
	for name, c := range map[string]struct {
		source, existing, link, target string
		want                           []string
	}{
		"called directory below a hidden directory is a symlink": {"./.x/impl", "modules/a/.x/other.txt", "modules/a/.x/impl", "impl", []string{"UNREADABLE", "UNRESOLVED_REFERENCE"}},
		"hidden directory on the way is a symlink":               {"./.x/.y/impl", "modules/a/.x/other.txt", "modules/a/.x/.y", "", []string{"UNREADABLE", "UNRESOLVED_REFERENCE"}},
		"file in a called hidden directory is a symlink":         {"./.impl", "modules/a/.impl/other.tf", "modules/a/.impl/main.tf", "main.tf", []string{"UNREADABLE"}},
	} {
		root := writeModules(t, map[string]string{"modules/a/main.tf": "module \"s\" { source = \"" + c.source + "\" }", c.existing: none})
		if err := os.Symlink(filepath.Join(outside, c.target), filepath.Join(root, c.link)); err != nil {
			t.Fatal(err)
		}
		if _, findings := ScanDependencies(root); !reflect.DeepEqual(ruleNames(findings), c.want) {
			t.Errorf("BEHAVIORAL_RED: %s: rules %v, want %v (%+v)", name, ruleNames(findings), c.want, findings)
		}
	}

	// A called hidden directory without *.tf, or one that does not exist, is
	// no module, and a call into a (non-hidden) skipped fixture tree is not
	// loaded: each stays unresolved.
	for name, c := range map[string]struct {
		files map[string]string
		want  []string
	}{
		"called hidden directory holds only .tofu": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "./.impl" }`, "modules/a/.impl/main.tofu": none,
		}, []string{"UNRESOLVED_REFERENCE", "UNSUPPORTED_CONFIG"}},
		"called hidden directory does not exist": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "./.missing" }`,
		}, []string{"UNRESOLVED_REFERENCE"}},
		"call into a skipped fixture tree": {map[string]string{
			"modules/a/main.tf": `module "s" { source = "../../tests/fixtures/x" }`, "tests/fixtures/x/main.tf": backend,
		}, []string{"UNRESOLVED_REFERENCE"}},
	} {
		if _, findings := ScanDependencies(writeModules(t, c.files)); !reflect.DeepEqual(ruleNames(findings), c.want) {
			t.Errorf("BEHAVIORAL_RED: %s: rules %v, want %v (%+v)", name, ruleNames(findings), c.want, findings)
		}
	}
}

// The root tests/ and examples/ trees are roots of their own (T066): a test
// helper below an example, or an example below a test tree, keeps the inner
// role, as a helper below a module's tests/ does (T067 review round 2).
func TestDependenciesRootTreeHelpers(t *testing.T) {
	provider := "provider \"ovh\" {\n  endpoint = \"ovh-eu\"\n}\n"
	g, findings := ScanDependencies(writeModules(t, map[string]string{
		"examples/solo/main.tf":                  `variable "x" {}`,
		"examples/solo/tests/setup/main.tf":      provider,
		"examples/solo/tests/unit.tftest.hcl":    "run \"setup\" {\n  module {\n    source = \"./tests/setup\"\n  }\n}\n",
		"tests/contracts/examples/basic/main.tf": provider,
	}))
	if len(findings) != 0 {
		t.Errorf("BEHAVIORAL_RED: root-tree helpers rejected: %+v", findings)
	}
	want := map[string]string{"examples/solo": LayerExample, "examples/solo/tests/setup": LayerTest, "tests/contracts/examples/basic": LayerTest}
	if !reflect.DeepEqual(g.Layers, want) {
		t.Errorf("BEHAVIORAL_RED: layers\n got %v\nwant %v", g.Layers, want)
	}
}

// G7 (code part): a retained module keeps its resources through a literal
// `lifecycle { prevent_destroy = true }` on every resource block, in every
// file and library subdirectory of the package, and declares no `removed`
// block. tofu test cannot assert prevent_destroy (spec 005 T013), so this
// static scan is the sensor. Data sources, other modules and the package's
// test configuration are outside the rule.
func TestDependenciesRetainedProtected(t *testing.T) {
	const m = "modules/object-storage-protected"
	protected := "resource \"ovh_cloud_project_storage\" \"this\" {\n  name = var.name\n\n  lifecycle {\n    prevent_destroy = true\n  }\n}\n"
	bare := "resource \"ovh_cloud_project_storage\" \"this\" {\n  name = var.name\n}\n"
	withLifecycle := func(inner string) string {
		return "resource \"ovh_cloud_project_storage\" \"this\" {\n  name = var.name\n\n  lifecycle {\n" + inner + "  }\n}\n"
	}
	for name, c := range map[string]struct {
		files   map[string]string
		subject string
		want    []string
	}{
		"protected resource": {map[string]string{m + "/main.tf": protected}, "", nil},
		"protected resource with other lifecycle arguments": {map[string]string{m + "/main.tf": withLifecycle("    prevent_destroy = true\n    ignore_changes  = [tags]\n")}, "", nil},
		"module without a resource":                         {map[string]string{m + "/main.tf": `variable "x" {}`}, "", nil},
		"data source needs no lifecycle":                    {map[string]string{m + "/main.tf": protected + "data \"ovh_me\" \"me\" {}\n"}, "", nil},
		"plain bucket module is not retained":               {map[string]string{"modules/object-storage/main.tf": bare}, "", nil},
		"test helper of the retained module":                {map[string]string{m + "/main.tf": protected, m + "/tests/setup/main.tf": bare}, "", nil},
		"resource without lifecycle":                        {map[string]string{m + "/main.tf": bare}, m, []string{"RETAINED_UNPROTECTED"}},
		"lifecycle without prevent_destroy":                 {map[string]string{m + "/main.tf": withLifecycle("    ignore_changes = [tags]\n")}, m, []string{"RETAINED_UNPROTECTED"}},
		"prevent_destroy false":                             {map[string]string{m + "/main.tf": withLifecycle("    prevent_destroy = false\n")}, m, []string{"RETAINED_UNPROTECTED"}},
		"prevent_destroy from a variable":                   {map[string]string{m + "/main.tf": withLifecycle("    prevent_destroy = var.keep\n")}, m, []string{"RETAINED_UNPROTECTED"}},
		"prevent_destroy a string":                          {map[string]string{m + "/main.tf": withLifecycle("    prevent_destroy = \"true\"\n")}, m, []string{"RETAINED_UNPROTECTED"}},
		"external module call":                              {map[string]string{m + "/main.tf": protected + "module \"x\" {\n  source = \"ovh/bucket/ovh\"\n}\n"}, m, []string{"RETAINED_UNPROTECTED"}},
		"external module call in a subdirectory":            {map[string]string{m + "/main.tf": protected + "module \"s\" {\n  source = \"./sub\"\n}\n", m + "/sub/main.tf": "module \"x\" {\n  source = \"git::https://example.com/b.git\"\n}\n"}, m + "/sub", []string{"RETAINED_UNPROTECTED"}},
		"own package by repository address and ref":         {map[string]string{m + "/main.tf": protected + "module \"s\" {\n  source = \"github.com/PlatformRelay/landingzone-for-ovhcloud//modules/object-storage-protected/sub?ref=v0.0.1\"\n}\n", m + "/sub/main.tf": protected}, m, []string{"RETAINED_UNPROTECTED"}},
		"naming call is fine":                               {map[string]string{m + "/main.tf": protected + "module \"n\" {\n  source = \"../naming\"\n}\n", "modules/naming/main.tf": `variable "x" {}`}, "", nil},
		"prevent_destroy outside lifecycle":                 {map[string]string{m + "/main.tf": "resource \"ovh_cloud_project_storage\" \"this\" {\n  timeouts {\n    prevent_destroy = true\n  }\n}\n"}, m, []string{"RETAINED_UNPROTECTED"}},
		"second resource unprotected":                       {map[string]string{m + "/main.tf": protected + "resource \"ovh_cloud_project_user\" \"u\" {}\n"}, m, []string{"RETAINED_UNPROTECTED"}},
		"second file unprotected":                           {map[string]string{m + "/main.tf": protected, m + "/zz_extra.tf": "resource \"ovh_cloud_project_storage\" \"b\" {}\n"}, m, []string{"RETAINED_UNPROTECTED"}},
		"override file switches it off":                     {map[string]string{m + "/main.tf": protected, m + "/main_override.tf": withLifecycle("    prevent_destroy = false\n")}, m, []string{"RETAINED_UNPROTECTED"}},
		"removed block":                                     {map[string]string{m + "/main.tf": protected + "removed {\n  from = ovh_cloud_project_storage.old\n}\n"}, m, []string{"RETAINED_UNPROTECTED"}},
		"library subdirectory unprotected":                  {map[string]string{m + "/main.tf": protected + "module \"s\" {\n  source = \"./sub\"\n}\n", m + "/sub/main.tf": bare}, m + "/sub", []string{"RETAINED_UNPROTECTED"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ScanDependencies(writeModules(t, c.files))
			if got := ruleNames(findings); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v (%+v)", got, c.want, findings)
			}
			for _, f := range findings {
				if f.Subject != c.subject {
					t.Errorf("BEHAVIORAL_RED: %s reported against %q, want %q", f.Rule, f.Subject, c.subject)
				}
			}
		})
	}
}

// G7 (component part, spec 005 T016): components/state-backend creates its
// buckets only through modules/object-storage-protected, so every state bucket
// carries the literal prevent_destroy and versioning that module pins. tofu test
// cannot see a child module's lifecycle (spec 005 T015 gap 1), so this static
// scan is the sensor. The component's own code declares no resource that
// creates or changes a bucket (versioning, lifecycle), every such resource it reaches through local module calls lies in the
// protected module, it calls no module by a non-relative source, and no module
// it reaches calls an external one (a release or an external module cannot be
// scanned). Data sources, the component's test
// configuration and other components are outside the rule.
func TestDependenciesStateBucketProtected(t *testing.T) {
	const c = "components/state-backend"
	base := map[string]string{
		"modules/object-storage-protected/main.tf": "resource \"ovh_cloud_project_storage\" \"this\" {\n  lifecycle {\n    prevent_destroy = true\n  }\n}\n",
		"modules/object-storage/main.tf":           "resource \"ovh_cloud_project_storage\" \"this\" {}\n",
		"modules/object-storage-user/main.tf":      "resource \"ovh_cloud_project_user\" \"this\" {}\n",
		"modules/naming/main.tf":                   `variable "x" {}`,
	}
	call := func(name, source string) string {
		return "module \"" + name + "\" {\n  source = \"" + source + "\"\n}\n"
	}
	composed := call("bucket", "../../modules/object-storage-protected") + call("user", "../../modules/object-storage-user") + call("name", "../../modules/naming")
	bucket := "resource \"ovh_cloud_project_storage\" \"b\" {}\n"
	for name, k := range map[string]struct {
		files   map[string]string
		subject string
		want    []string
	}{
		"protected bucket, user and naming":                    {map[string]string{c + "/main.tf": composed}, "", nil},
		"bucket data source is fine":                           {map[string]string{c + "/main.tf": composed + "data \"ovh_cloud_project_storage\" \"b\" {}\n"}, "", nil},
		"other component may use the plain module":             {map[string]string{"components/runtime/managed-only/main.tf": call("b", "../../../modules/object-storage")}, "", nil},
		"test helper of the component":                         {map[string]string{c + "/main.tf": composed, c + "/tests/setup/main.tf": bucket}, "", nil},
		"direct bucket resource":                               {map[string]string{c + "/main.tf": composed + bucket}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"direct S3 bucket of another provider":                 {map[string]string{c + "/main.tf": composed + "resource \"aws_s3_bucket\" \"b\" {}\n"}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"bucket lifecycle resource":                            {map[string]string{c + "/main.tf": composed + "resource \"ovh_cloud_project_storage_object_bucket_lifecycle_configuration\" \"l\" {}\n"}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"bucket versioning of another provider":                {map[string]string{c + "/main.tf": composed + "resource \"aws_s3_bucket_versioning\" \"v\" {}\n"}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"module with a bucket lifecycle resource":              {map[string]string{c + "/main.tf": composed + call("l", "../../modules/lifecycle"), "modules/lifecycle/main.tf": "resource \"ovh_cloud_project_storage_object_bucket_lifecycle_configuration\" \"l\" {}\n"}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"direct bucket in an override file":                    {map[string]string{c + "/main.tf": composed, c + "/main_override.tf": bucket}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"plain bucket module":                                  {map[string]string{c + "/main.tf": composed + call("plain", "../../modules/object-storage")}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"plain bucket module only":                             {map[string]string{c + "/main.tf": call("plain", "../../modules/object-storage")}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"module with a bucket in its subdirectory":             {map[string]string{c + "/main.tf": composed + call("w", "../../modules/wrap"), "modules/wrap/main.tf": call("i", "./inner"), "modules/wrap/inner/main.tf": bucket}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"protected module by repository address":               {map[string]string{c + "/main.tf": call("bucket", "github.com/PlatformRelay/landingzone-for-ovhcloud//modules/object-storage-protected?ref=v0.0.1")}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"external module":                                      {map[string]string{c + "/main.tf": composed + call("x", "ovh/bucket/ovh")}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"module calling its own package by repository address": {map[string]string{c + "/main.tf": composed + call("w", "../../modules/wrap"), "modules/wrap/main.tf": call("i", "github.com/PlatformRelay/landingzone-for-ovhcloud//modules/wrap/inner?ref=v0.0.1"), "modules/wrap/inner/main.tf": `variable "x" {}`}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"module calling an external module":                    {map[string]string{c + "/main.tf": composed + call("w", "../../modules/wrap"), "modules/wrap/main.tf": call("x", "ovh/bucket/ovh")}, c, []string{"STATE_BUCKET_UNPROTECTED"}},
		"library subdirectory with a bucket":                   {map[string]string{c + "/main.tf": composed + call("s", "./sub"), c + "/sub/main.tf": bucket}, c + "/sub", []string{"STATE_BUCKET_UNPROTECTED"}},
		"hidden subdirectory with a bucket":                    {map[string]string{c + "/main.tf": composed + call("h", "./.b"), c + "/.b/main.tf": bucket}, c + "/.b", []string{"STATE_BUCKET_UNPROTECTED"}},
	} {
		t.Run(name, func(t *testing.T) {
			files := maps.Clone(base)
			maps.Copy(files, k.files)
			_, findings := ScanDependencies(writeModules(t, files))
			if got := ruleNames(findings); !reflect.DeepEqual(got, k.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v (%+v)", got, k.want, findings)
			}
			for _, f := range findings {
				if f.Subject != k.subject {
					t.Errorf("BEHAVIORAL_RED: %s reported against %q, want %q", f.Rule, f.Subject, k.subject)
				}
			}
		})
	}
}

// G2, configuration part (spec 005 T020; T013 gap 5, T019 gap 1): the
// envelope builder drops every output tofu marked sensitive, and a module's
// unit tests cannot enumerate its outputs, so an output added later that wraps
// a secret in nonsensitive() is seen by neither. No call to nonsensitive, by
// its plain or namespaced name, anywhere in the configuration of a library
// (modules, components) or of what deploys it (stages, generated instances):
// each one can hand an unmarked secret to the stack whose outputs are
// published. Test configuration (test files, helper modules under tests/)
// asserts on secrets and is not deployed; examples are not deployed either.
// A comment, a string or a name that only contains the word is no call.
func TestDependenciesNonsensitiveRefused(t *testing.T) {
	none := `variable "x" {}`
	unmark := "variable \"secret\" {\n  sensitive = true\n}\n\noutput \"leak\" {\n  value = nonsensitive(var.secret)\n}\n"
	for name, c := range map[string]struct {
		files   map[string]string
		subject string
		want    []string
	}{
		"module output":                   {map[string]string{"modules/a/main.tf": unmark}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"family component output":         {map[string]string{"components/identity/ovh-native/main.tf": unmark}, "components/identity/ovh-native", []string{"NONSENSITIVE_CALL"}},
		"singleton component output":      {map[string]string{"components/state-backend/main.tf": unmark}, "components/state-backend", []string{"NONSENSITIVE_CALL"}},
		"naming module":                   {map[string]string{"modules/naming/main.tf": unmark}, "modules/naming", []string{"NONSENSITIVE_CALL"}},
		"nested module directory":         {map[string]string{"modules/a/main.tf": `module "s" { source = "./sub" }`, "modules/a/sub/main.tf": unmark}, "modules/a/sub", []string{"NONSENSITIVE_CALL"}},
		"hidden directory a module calls": {map[string]string{"modules/a/main.tf": `module "h" { source = "./.h" }`, "modules/a/.h/main.tf": unmark}, "modules/a/.h", []string{"NONSENSITIVE_CALL"}},
		"override file":                   {map[string]string{"modules/a/main.tf": none, "modules/a/main_override.tf": unmark}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"namespaced core function":        {map[string]string{"modules/a/main.tf": "output \"o\" {\n  value = core::nonsensitive(var.s)\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"provider function of that name":  {map[string]string{"modules/a/main.tf": "output \"o\" {\n  value = provider::x::nonsensitive(var.s)\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"in a local":                      {map[string]string{"modules/a/main.tf": "locals {\n  plain = nonsensitive(var.s)\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"in a resource argument":          {map[string]string{"modules/a/main.tf": "resource \"ovh_me_identity_group\" \"g\" {\n  description = nonsensitive(var.s)\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"nested in a nested block":        {map[string]string{"modules/a/main.tf": "resource \"ovh_iam_policy\" \"p\" {\n  dynamic \"conditions\" {\n    for_each = [1]\n    content {\n      values = { k = nonsensitive(var.s) }\n    }\n  }\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"inside another call":             {map[string]string{"modules/a/main.tf": "output \"o\" {\n  value = upper(try(nonsensitive(var.s), \"\"))\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"in a template interpolation":     {map[string]string{"modules/a/main.tf": "output \"o\" {\n  value = \"id-${nonsensitive(var.s)}\"\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"in a heredoc":                    {map[string]string{"modules/a/main.tf": "output \"o\" {\n  value = <<-EOT\n    ${nonsensitive(var.s)}\n  EOT\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"in a for expression":             {map[string]string{"modules/a/main.tf": "output \"o\" {\n  value = [for s in var.l : nonsensitive(s)]\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"in a check block":                {map[string]string{"modules/a/main.tf": "check \"c\" {\n  assert {\n    condition     = nonsensitive(var.s) != \"\"\n    error_message = \"x\"\n  }\n}\n"}, "modules/a", []string{"NONSENSITIVE_CALL"}},
		"stage output":                    {map[string]string{"stages/platform/main.tf": unmark}, "stages/platform", []string{"NONSENSITIVE_CALL"}},
		"generated instance output":       {map[string]string{"stages/platform/main.tf": none, "stacks/prod/platform/main.tf": generated + "module \"stage\" {\n  source = \"../../../stages/platform\"\n}\n\noutput \"o\" {\n  value = nonsensitive(module.stage.x)\n}\n"}, "stacks/prod/platform", []string{"NONSENSITIVE_CALL"}},
		// out of scope or no call
		"module test file asserts on a secret":   {map[string]string{"modules/a/main.tf": none, "modules/a/tests/unit.tftest.hcl": "run \"r\" {\n  assert {\n    condition     = nonsensitive(output.s) == \"x\"\n    error_message = \"x\"\n  }\n}\n"}, "", nil},
		"helper module under the module's tests": {map[string]string{"modules/a/main.tf": none, "modules/a/tests/setup/main.tf": unmark}, "", nil},
		"root test configuration":                {map[string]string{"tests/live/probes/a/main.tf": unmark}, "", nil},
		"example":                                {map[string]string{"modules/a/main.tf": none, "examples/a/main.tf": "module \"a\" {\n  source = \"../../modules/a\"\n}\n" + unmark}, "", nil},
		"word in a comment, string and name":     {map[string]string{"modules/a/main.tf": "# nonsensitive(var.s) is refused here\nvariable \"nonsensitive\" {\n  description = \"not nonsensitive(x)\"\n}\n\noutput \"o\" {\n  value = issensitive(var.nonsensitive)\n}\n"}, "", nil},
		"other function with the word":           {map[string]string{"modules/a/main.tf": "output \"o\" {\n  value = provider::x::nonsensitive_name(var.s)\n}\n"}, "", nil},
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ScanDependencies(writeModules(t, c.files))
			if got := ruleNames(findings); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v (%+v)", got, c.want, findings)
			}
			for _, f := range findings {
				if f.Subject != c.subject {
					t.Errorf("BEHAVIORAL_RED: %s reported against %q, want %q", f.Rule, f.Subject, c.subject)
				}
			}
		})
	}
}

const nonsensitiveDependencies = "../../../tests/check/fixtures/dependencies/nonsensitive"

// The on-disk control of the same rule: a module, a component and a stage that
// unmark a secret are refused, each against its own directory; the module's
// test file, its test helper and an example doing the same are not.
func TestDependenciesNonsensitiveFixture(t *testing.T) {
	_, findings := ScanDependencies(nonsensitiveDependencies)
	subjects := map[string]bool{}
	for _, f := range findings {
		if f.Rule != "NONSENSITIVE_CALL" {
			t.Errorf("BEHAVIORAL_RED: unexpected rule %s: %+v", f.Rule, f)
		}
		subjects[f.Subject] = true
	}
	want := []string{"components/identity/ovh-native", "modules/credential", "stages/account-governance"}
	if got := sortedSet(subjects); !reflect.DeepEqual(got, want) {
		t.Errorf("BEHAVIORAL_RED: NONSENSITIVE_CALL reported against %v, want %v (%+v)", got, want, findings)
	}
}

// Stages compose components (ADR-0002; coordinator decision 2026-10-07 on
// T021's decision request 1): a resource block anywhere in a stage's own
// configuration is refused (STAGE_RESOURCE), data sources stay allowed. The
// tenant-state stage reaches its one bucket through exactly one, unrepeated
// components/state-backend call and calls nothing else (STAGE_COMPONENT_CALLS,
// G6): a second call, a count or for_each, or another module would add
// buckets or users no stage test can count. The first two rows are T021's
// probes, verbatim. Test configuration, other layers and other stages'
// component calls are outside the rules.
func TestDependenciesStageComposition(t *testing.T) {
	const ts = "stages/tenant-state"
	base := map[string]string{
		"components/state-backend/main.tf":       `variable "x" {}`,
		"components/state-backend/sub/main.tf":   `variable "x" {}`,
		"components/identity/ovh-native/main.tf": `variable "x" {}`,
		"modules/naming/main.tf":                 `variable "x" {}`,
	}
	call := func(name, source, extra string) string {
		return "module \"" + name + "\" {\n  source = \"" + source + "\"\n" + extra + "}\n"
	}
	one := call("state_backend", "../../components/state-backend", "")
	probeSecondCall := "module \"second\" {\n  source     = \"../../components/state-backend\"\n  project_id = \"x\"\n  region     = \"GRA\"\n  org        = \"lz\"\n  instance   = \"x\"\n  managed_in = \"x\"\n  s3_users   = []\n}\n"
	probeBucket := "resource \"ovh_cloud_project_storage\" \"extra\" {\n  service_name = \"x\"\n  region_name  = \"GRA\"\n  name         = \"lz-bkt-state\"\n}\n"
	for name, c := range map[string]struct {
		files   map[string]string
		subject string
		want    []string
	}{
		"probe: second component call":                 {map[string]string{ts + "/main.tf": one, ts + "/extra.tf": probeSecondCall}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"probe: direct bucket resource":                {map[string]string{ts + "/main.tf": one, ts + "/extra.tf": probeBucket}, ts, []string{"STAGE_RESOURCE"}},
		"resource in another stage":                    {map[string]string{"stages/platform/main.tf": "resource \"terraform_data\" \"x\" {}\n"}, "stages/platform", []string{"STAGE_RESOURCE"}},
		"resource in a stage's override file":          {map[string]string{ts + "/main.tf": one, ts + "/main_override.tf": probeBucket}, ts, []string{"STAGE_RESOURCE"}},
		"resource in a stage subdirectory":             {map[string]string{"stages/platform/main.tf": call("s", "./sub", ""), "stages/platform/sub/main.tf": probeBucket}, "stages/platform/sub", []string{"STAGE_RESOURCE"}},
		"resource in a hidden directory a stage calls": {map[string]string{"stages/platform/main.tf": call("h", "./.h", ""), "stages/platform/.h/main.tf": probeBucket}, "stages/platform/.h", []string{"STAGE_RESOURCE"}},
		"second call in the same file":                 {map[string]string{ts + "/main.tf": one + call("again", "../../components/state-backend", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"second call by repository address":            {map[string]string{ts + "/main.tf": one + call("again", "github.com/PlatformRelay/landingzone-for-ovhcloud//components/state-backend?ref=v0.0.1", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"only call to a component subdirectory":        {map[string]string{ts + "/main.tf": call("state_backend", "../../components/state-backend/sub", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"second call to a component subdirectory":      {map[string]string{ts + "/main.tf": one + call("again", "../../components/state-backend/sub", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"second call from a stage subdirectory":        {map[string]string{ts + "/main.tf": one + call("s", "./sub", ""), ts + "/sub/main.tf": call("again", "../../../components/state-backend", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"call with count":                              {map[string]string{ts + "/main.tf": call("state_backend", "../../components/state-backend", "  count = 2\n")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"call with for_each":                           {map[string]string{ts + "/main.tf": call("state_backend", "../../components/state-backend", "  for_each = toset([\"a\"])\n")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"repeated call to a stage subdirectory":        {map[string]string{ts + "/main.tf": call("s", "./sub", "  count = 1\n"), ts + "/sub/main.tf": call("state_backend", "../../../components/state-backend", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"call to another component":                    {map[string]string{ts + "/main.tf": one + call("identity", "../../components/identity/ovh-native", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"stage subdirectory called twice":              {map[string]string{ts + "/main.tf": call("a", "./sub", "") + call("b", "./sub", ""), ts + "/sub/main.tf": call("state_backend", "../../../components/state-backend", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"two stage subdirectories each calling it":     {map[string]string{ts + "/main.tf": call("a", "./a", "") + call("b", "./b", ""), ts + "/a/main.tf": call("s", "../../../components/state-backend", ""), ts + "/b/main.tf": call("s", "../../../components/state-backend", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"only call by repository address":              {map[string]string{ts + "/main.tf": call("state_backend", "github.com/PlatformRelay/landingzone-for-ovhcloud//components/state-backend?ref=v0.0.1", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		"call to an external module":                   {map[string]string{ts + "/main.tf": one + call("x", "ovh/bucket/ovh", "")}, ts, []string{"STAGE_COMPONENT_CALLS"}},
		// allowed
		"one component call":                        {map[string]string{ts + "/main.tf": one}, "", nil},
		"one component call through a subdirectory": {map[string]string{ts + "/main.tf": call("s", "./sub", ""), ts + "/sub/main.tf": call("state_backend", "../../../components/state-backend", "")}, "", nil},
		"override file merging the one call":        {map[string]string{ts + "/main.tf": one, ts + "/main_override.tf": call("state_backend", "../../components/state-backend", "")}, "", nil},
		"data source in a stage":                    {map[string]string{ts + "/main.tf": one + "data \"ovh_cloud_project_storage\" \"b\" {}\n"}, "", nil},
		"resource in a stage's test helper":         {map[string]string{ts + "/main.tf": one, ts + "/tests/setup/main.tf": probeBucket}, "", nil},
		"test run calling the component again":      {map[string]string{ts + "/main.tf": one, ts + "/tests/unit.tftest.hcl": "run \"r\" {\n  module {\n    source = \"../../components/state-backend\"\n  }\n}\n"}, "", nil},
		"resource in a component":                   {map[string]string{ts + "/main.tf": one, "components/identity/ovh-native/main.tf": probeBucket}, "", nil},
		"other stage calls a component twice":       {map[string]string{"stages/platform/main.tf": call("a", "../../components/state-backend", "") + call("b", "../../components/state-backend", "")}, "", nil},
	} {
		t.Run(name, func(t *testing.T) {
			files := maps.Clone(base)
			maps.Copy(files, c.files)
			_, findings := ScanDependencies(writeModules(t, files))
			if got := ruleNames(findings); !reflect.DeepEqual(got, c.want) {
				t.Errorf("BEHAVIORAL_RED: rules %v, want %v (%+v)", got, c.want, findings)
			}
			for _, f := range findings {
				if f.Subject != c.subject {
					t.Errorf("BEHAVIORAL_RED: %s reported against %q, want %q", f.Rule, f.Subject, c.subject)
				}
			}
		})
	}
}

// The purity rules add negatives; they never widen the ADR-0002 edge matrix.
func TestDependenciesLayerMatrixUnchanged(t *testing.T) {
	want := map[string][]string{
		"naming":      nil,
		"module":      {"naming"},
		"component":   {"naming", "module"},
		LayerStage:    {"component"},
		LayerInstance: {LayerStage},
		LayerExample:  {"naming", "module", "component"},
		LayerTest:     {"naming", "module", "component", LayerTest},
	}
	if !reflect.DeepEqual(mayUse, want) {
		t.Errorf("BEHAVIORAL_RED: mayUse changed\n got %v\nwant %v", mayUse, want)
	}
}

func TestDependenciesRulesListed(t *testing.T) {
	want := []string{"CYCLE", "HANDWRITTEN_INSTANCE", "LAYER_VIOLATION", "LIBRARY_BACKEND", "LIBRARY_PROVIDER_CONFIG", "NONSENSITIVE_CALL", "PARSE_ERROR", "REMOTE_STATE", "RETAINED_UNPROTECTED", "STAGE_COMPONENT_CALLS", "STAGE_RESOURCE", "STATE_BUCKET_UNPROTECTED", "UNCLASSIFIED", "UNREADABLE", "UNRESOLVED_REFERENCE", "UNSUPPORTED_CONFIG"}
	got := append([]string{}, DependencyRules...)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BEHAVIORAL_RED: DependencyRules = %v, want %v", got, want)
	}
}
