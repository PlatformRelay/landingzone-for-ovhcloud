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
	want := []string{"CYCLE", "HANDWRITTEN_INSTANCE", "LAYER_VIOLATION", "LIBRARY_BACKEND", "LIBRARY_PROVIDER_CONFIG", "PARSE_ERROR", "REMOTE_STATE", "UNCLASSIFIED", "UNREADABLE", "UNRESOLVED_REFERENCE", "UNSUPPORTED_CONFIG"}
	got := append([]string{}, DependencyRules...)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BEHAVIORAL_RED: DependencyRules = %v, want %v", got, want)
	}
}
