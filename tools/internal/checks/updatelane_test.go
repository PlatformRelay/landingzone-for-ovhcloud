package checks

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The update-lane check (006 FR-015, SC-004) judges a repository tree: its
// dependency-update configuration (.github/renovate.json), the pin sources it
// must cover and the provider schema snapshot. Each row copies the base
// fixture, a tree whose configuration covers every pin source of data-model
// *Update-lane configuration*, into scratch and breaks one thing. Manager names
// are never judged (research R8).
const updateLaneFixtures = "../../../tests/check/fixtures/updatelane"

// updateLaneRow is one case: a configuration from configs/ replacing the base
// one, or a change to the copied tree, and the rule CheckUpdateLane must
// report with a subject or detail naming one of needles (any, when empty).
type updateLaneRow struct {
	name    string
	config  string
	change  func(t *testing.T, root string)
	rule    string
	needles []string
}

// updateLaneTree copies the base fixture into private scratch and applies the
// row's configuration and change.
func updateLaneTree(t *testing.T, row updateLaneRow) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	base := filepath.Join(updateLaneFixtures, "base")
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(root, rel), 0o700)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, rel), data, 0o600)
	})
	if err != nil {
		t.Fatalf("FIXTURE_MISSING: %v", err)
	}
	if row.config != "" {
		data, err := os.ReadFile(filepath.Join(updateLaneFixtures, "configs", row.config))
		if err != nil {
			t.Fatalf("FIXTURE_MISSING: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, ".github", "renovate.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if row.change != nil {
		row.change(t, root)
	}
	return root
}

// reportsUpdateLane says whether findings hold one with rule whose subject or
// detail names one of needles.
func reportsUpdateLane(findings []Finding, rule string, needles []string) bool {
	for _, f := range findings {
		if f.Rule != rule {
			continue
		}
		if len(needles) == 0 {
			return true
		}
		for _, n := range needles {
			if strings.Contains(f.Subject+" "+f.Detail, n) {
				return true
			}
		}
	}
	return false
}

// updateLaneMoveSnapshot renames the base snapshot and its record to version.
func updateLaneMoveSnapshot(version string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()
		dir := filepath.Join(root, "tests", "fixtures", "provider-schema")
		for _, ext := range []string{".json", ".command.txt"} {
			if err := os.Rename(filepath.Join(dir, "ovh-2.21.0"+ext), filepath.Join(dir, "ovh-"+version+ext)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// updateLaneRemove removes rel from the copied tree.
func updateLaneRemove(rel ...string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()
		for _, r := range rel {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(r))); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// updateLaneWrite replaces each rel in the copied tree with content.
func updateLaneWrite(content string, rel ...string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()
		for _, r := range rel {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(r)), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// updateLaneReplace replaces the first old in rel of the copied tree, which must hold it, with with.
func updateLaneReplace(rel, old, with string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), old) {
			t.Fatalf("FIXTURE_MISSING: %s does not hold %q: %v", rel, old, err)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, with, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// updateLaneMovedLock pins ovh/ovh 2.22.0 in every lock while the snapshot stays
// at 2.21.0.
const updateLaneMovedLock = `provider "registry.opentofu.org/ovh/ovh" {
  version     = "2.22.0"
  constraints = "~> 2.21"
  hashes = [
    "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
  ]
}
`

const updateLaneRecord = "tests/fixtures/provider-schema/ovh-2.21.0.command.txt"

// updateLaneRows are the refusal rows of T035, one per rule and pin source.
var updateLaneRows = []updateLaneRow{
	// An unwatched pin: each pin source without a rule naming its file.
	{name: "uncovered/mise-toml", config: "uncovered-mise-toml.json", rule: "UPDATE_UNCOVERED", needles: []string{"mise.toml"}},
	{name: "uncovered/mise-live-toml", config: "uncovered-mise-live.json", rule: "UPDATE_UNCOVERED", needles: []string{"mise.live.toml"}},
	{name: "uncovered/go-mod", config: "uncovered-go-mod.json", rule: "UPDATE_UNCOVERED", needles: []string{"go.mod"}},
	{name: "uncovered/lock-files", config: "uncovered-lock.json", rule: "UPDATE_UNCOVERED", needles: []string{".terraform.lock.hcl"}},
	{name: "uncovered/required-providers", config: "uncovered-required-providers.json", rule: "UPDATE_UNCOVERED", needles: []string{"versions.tf", "required_providers"}},
	{name: "uncovered/image-digest", config: "uncovered-image-digest.json", rule: "UPDATE_UNCOVERED", needles: []string{"LZ_OFFLINE_IMAGE"}},
	// No configuration at all: every pin source is unwatched.
	{name: "uncovered/no-configuration", change: updateLaneRemove(".github/renovate.json"), rule: "UPDATE_UNCOVERED", needles: []string{"renovate.json"}},
	// Unreviewed bumps: automerge anywhere.
	{name: "automerge/top-level", config: "automerge-top.json", rule: "UPDATE_AUTOMERGE"},
	{name: "automerge/package-rule", config: "automerge-rule.json", rule: "UPDATE_AUTOMERGE"},
	{name: "automerge/nested", config: "automerge-nested.json", rule: "UPDATE_AUTOMERGE"},
	// A bump that desynchronises the entry: a runtime-mirrored tool outside
	// the group labelled needs-entry-revision.
	{name: "runtime/tool-other-group", config: "runtime-tool-other-group.json", rule: "UPDATE_RUNTIME_GROUP", needles: []string{"tflint"}},
	{name: "runtime/image-other-group", config: "runtime-image-other-group.json", rule: "UPDATE_RUNTIME_GROUP", needles: []string{"wolfi-base", "LZ_OFFLINE_IMAGE"}},
	{name: "runtime/unlabelled", config: "runtime-unlabelled.json", rule: "UPDATE_RUNTIME_GROUP"},
	{name: "runtime/label-elsewhere", config: "runtime-label-elsewhere.json", rule: "UPDATE_RUNTIME_GROUP"},
	// An unreviewable provider bump: the provider group without the snapshot.
	{name: "provider/no-snapshot", config: "provider-no-snapshot.json", rule: "UPDATE_PROVIDER_GROUP"},
	{name: "provider/snapshot-other-group", config: "provider-snapshot-other-group.json", rule: "UPDATE_PROVIDER_GROUP"},
	// A stale or unprovenanced snapshot.
	{name: "schema/snapshot-older", change: updateLaneMoveSnapshot("2.20.0"), rule: "SCHEMA_VERSION", needles: []string{"2.20.0"}},
	{name: "schema/pin-moved", change: updateLaneWrite(updateLaneMovedLock, "stages/demo/.terraform.lock.hcl", "modules/demo/.terraform.lock.hcl"), rule: "SCHEMA_VERSION", needles: []string{"2.22.0"}},
	{name: "schema/snapshot-missing", change: updateLaneRemove("tests/fixtures/provider-schema/ovh-2.21.0.json", updateLaneRecord), rule: "SCHEMA_VERSION"},
	{name: "schema/record-missing", change: updateLaneRemove(updateLaneRecord), rule: "SCHEMA_RECORD", needles: []string{"ovh-2.21.0"}},
	{name: "schema/record-empty", change: updateLaneWrite("", updateLaneRecord), rule: "SCHEMA_RECORD", needles: []string{"ovh-2.21.0"}},
	// A record left from another capture: its provider version or the
	// snapshot's digest differs from the snapshot it sits beside.
	{name: "schema/record-other-version", change: updateLaneReplace(updateLaneRecord, "ovh/ovh 2.21.0", "ovh/ovh 2.20.0"), rule: "SCHEMA_RECORD", needles: []string{"ovh-2.21.0"}},
	{name: "schema/record-digest-differs", change: updateLaneReplace(updateLaneRecord, "sha256: 1421d788", "sha256: 0421d788"), rule: "SCHEMA_RECORD", needles: []string{"ovh-2.21.0"}},
	// A crash read as a pass: a configuration that is not the JSON object the
	// check reads.
	{name: "syntax/truncated", config: "syntax-truncated.json", rule: "UPDATE_SYNTAX"},
	{name: "syntax/top-level-array", config: "syntax-array.json", rule: "UPDATE_SYNTAX"},
	{name: "syntax/rules-not-a-list", config: "syntax-rules-type.json", rule: "UPDATE_SYNTAX"},
}

// TestUpdateLaneValid is the positive control: the base fixture covers every
// pin source and passes. It passes against the stub too.
func TestUpdateLaneValid(t *testing.T) {
	root := updateLaneTree(t, updateLaneRow{})
	if got := CheckUpdateLane(root); len(got) != 0 {
		t.Errorf("BEHAVIORAL_RED: a configuration covering every pin source reports %v, want no finding", got)
	}
}

// TestUpdateLaneRefusals pins one finding per rule and pin source.
func TestUpdateLaneRefusals(t *testing.T) {
	for _, row := range updateLaneRows {
		t.Run(row.name, func(t *testing.T) {
			got := CheckUpdateLane(updateLaneTree(t, row))
			if !reportsUpdateLane(got, row.rule, row.needles) {
				t.Errorf("BEHAVIORAL_RED: want a %s finding (naming one of %q when given), got %v", row.rule, row.needles, got)
			}
		})
	}
}

// TestUpdateLaneRepository judges this repository: it carries the
// configuration (FR-015) and passes the check (SC-004). Until T037 commits
// .github/renovate.json the check must report UPDATE_UNCOVERED for it, and the
// test stays red because the configuration is missing.
func TestUpdateLaneRepository(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	got := CheckUpdateLane(root)
	if _, err := os.Stat(filepath.Join(root, ".github", "renovate.json")); err != nil {
		if !reportsUpdateLane(got, "UPDATE_UNCOVERED", []string{"renovate.json"}) {
			t.Errorf("BEHAVIORAL_RED: the repository has no update-lane configuration, but the check reports %v, want UPDATE_UNCOVERED naming renovate.json", got)
		}
		t.Fatalf("BEHAVIORAL_RED: the repository carries no update-lane configuration (FR-015): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("BEHAVIORAL_RED: the repository's update lane reports %v, want no finding", got)
	}
}

// updateLaneAdd injects top-level keys (JSON members with a trailing comma) into the base configuration.
func updateLaneAdd(members string) func(t *testing.T, root string) {
	return updateLaneReplace(".github/renovate.json", `"automerge": false,`, `"automerge": false, `+members)
}

// updateLaneLock writes a lock pinning ovh/ovh 2.22.0 at rel, creating its directory.
func updateLaneLock(rel string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(updateLaneMovedLock), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// updateLaneLink places a dangling symbolic link at rel.
func updateLaneLink(rel string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()
		if err := os.Symlink(filepath.Join(root, "absent.tf"), filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
}

// TestUpdateLaneReadings pins T037's fail-closed reading of the configuration
// and of the pin trees (006 FR-015: every pin source watched, never
// auto-merged, the runtime group labelled): keys that drop or auto-merge
// updates the check cannot attribute are findings, a lock under stacks/ is a
// pin, and a lock in a fixture, hidden or tests/ tree is not.
func TestUpdateLaneReadings(t *testing.T) {
	refused := []updateLaneRow{
		{name: "ignore-paths-drops-a-pin", change: updateLaneAdd(`"ignorePaths": ["stages/**"],`), rule: "UPDATE_UNCOVERED", needles: []string{"stages/demo/.terraform.lock.hcl"}},
		{name: "ignore-paths-substring", change: updateLaneAdd(`"ignorePaths": ["tools/"],`), rule: "UPDATE_UNCOVERED", needles: []string{"tools/go.mod"}},
		{name: "ignore-paths-unreadable-glob", change: updateLaneAdd(`"ignorePaths": ["{mise,x}.toml"],`), rule: "UPDATE_UNCOVERED", needles: []string{"mise.toml"}},
		{name: "disabled", change: updateLaneAdd(`"enabled": false,`), rule: "UPDATE_UNCOVERED", needles: []string{"enabled"}},
		{name: "enabled-managers", change: updateLaneAdd(`"enabledManagers": ["gomod"],`), rule: "UPDATE_UNCOVERED", needles: []string{"enabledManagers"}},
		{name: "ignore-deps", change: updateLaneAdd(`"ignoreDeps": ["tflint"],`), rule: "UPDATE_UNCOVERED", needles: []string{"ignoreDeps"}},
		{name: "rule-disables-a-pin", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["mise.live.toml"]`, `"matchFileNames": ["mise.live.toml"], "enabled": false`), rule: "UPDATE_UNCOVERED", needles: []string{"mise.live.toml"}},
		{name: "preset", change: updateLaneAdd(`"extends": ["config:recommended"],`), rule: "UPDATE_AUTOMERGE", needles: []string{"extends"}},
		{name: "automerge-string", change: updateLaneReplace(".github/renovate.json", `"automerge": false,`, `"automerge": "true",`), rule: "UPDATE_AUTOMERGE"},
		{name: "later-labels-drop-the-label", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchFileNames": ["mise.toml"], "matchDepNames": ["task"], "labels": ["tools"]`), rule: "UPDATE_RUNTIME_GROUP", needles: []string{"task"}},
		{name: "stacks-lock-is-a-pin", change: updateLaneLock("stacks/demo/.terraform.lock.hcl"), rule: "SCHEMA_VERSION", needles: []string{"stacks/demo/.terraform.lock.hcl"}},
		// Review round 1.
		{name: "include-paths", change: updateLaneAdd(`"includePaths": ["stages/**"],`), rule: "UPDATE_UNCOVERED", needles: []string{"includePaths"}},
		{name: "manager-disabled", change: updateLaneAdd(`"terraform": {"enabled": false},`), rule: "UPDATE_UNCOVERED", needles: []string{"terraform"}},
		{name: "update-type-disabled", change: updateLaneAdd(`"major": {"enabled": false},`), rule: "UPDATE_UNCOVERED", needles: []string{"major"}},
		{name: "manager-files-redefined", change: updateLaneAdd(`"gomod": {"managerFilePatterns": ["/nomatch/"]},`), rule: "UPDATE_UNCOVERED", needles: []string{"gomod"}},
		{name: "disable-unreadable-glob", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchFileNames": ["{mise,mise.live}.toml"], "enabled": false`), rule: "UPDATE_UNCOVERED", needles: []string{"mise.toml"}},
		{name: "narrowed-provider-group", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchPackageNames": ["ovh/ovh"], "groupName": "separate-provider"`), rule: "UPDATE_PROVIDER_GROUP", needles: []string{"separate-provider"}},
		{name: "label-dropped-before-the-group", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchFileNames": ["mise.toml"], "matchDepNames": ["tflint"], "labels": []}, {"matchFileNames": ["mise.toml"], "groupName": "runtime-tools"`), rule: "UPDATE_RUNTIME_GROUP", needles: []string{"tflint"}},
		{name: "labels-replaced", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchFileNames": ["mise.toml"], "labels": ["tools"]`), rule: "UPDATE_RUNTIME_GROUP", needles: []string{"mise.toml"}},
		// Review round 2.
		{name: "nested-rule-switch", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchFileNames": ["mise.toml"], "major": {"enabled": false}`), rule: "UPDATE_UNCOVERED", needles: []string{"major"}},
		{name: "runtime-group-emptied", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchFileNames": ["mise.toml"], "groupName": ""`), rule: "UPDATE_RUNTIME_GROUP", needles: []string{"ungrouped"}},
		{name: "provider-group-emptied", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchFileNames": ["**/.terraform.lock.hcl"], "groupName": ""`), rule: "UPDATE_PROVIDER_GROUP", needles: []string{"ungrouped"}},
		{name: "regex-pattern-disable", change: updateLaneReplace(".github/renovate.json", `"matchFileNames": ["tools/go.mod"]`, `"matchFileNames": ["tools/go.mod"]}, {"matchFileNames": ["/^mise\\.toml$/"], "enabled": false`), rule: "UPDATE_UNCOVERED", needles: []string{"mise.toml"}},
		{name: "second-configuration", change: updateLaneWrite(`{"automerge": true}`, "renovate.json"), rule: "UPDATE_UNCOVERED", needles: []string{"second Renovate configuration"}},
		{name: "image-match-string-misses", change: updateLaneReplace(".github/renovate.json", `LZ_OFFLINE_IMAGE = `, `LZ_OFFLINE_IMAGE=`), rule: "UPDATE_UNCOVERED", needles: []string{"LZ_OFFLINE_IMAGE"}},
		{name: "dangling-link-in-a-tree", change: updateLaneLink("stages/demo/aaa.tf"), rule: "UPDATE_UNCOVERED", needles: []string{"stages"}},
		{name: "tofu-file-unwatched", change: updateLaneWrite("terraform {\n  required_providers {\n    ovh = { source = \"ovh/ovh\" }\n  }\n}\n", "stages/demo/versions.tofu"), rule: "UPDATE_UNCOVERED", needles: []string{"stages/demo/versions.tofu"}},
	}
	for _, row := range refused {
		t.Run("refused/"+row.name, func(t *testing.T) {
			got := CheckUpdateLane(updateLaneTree(t, row))
			if !reportsUpdateLane(got, row.rule, row.needles) {
				t.Errorf("BEHAVIORAL_RED: want a %s finding (naming one of %q when given), got %v", row.rule, row.needles, got)
			}
		})
	}
	accepted := []updateLaneRow{
		{name: "fixture-tree", change: updateLaneLock("stages/demo/fixtures/old/.terraform.lock.hcl")},
		{name: "hidden-tree", change: updateLaneLock("modules/demo/.terraform/modules/x/.terraform.lock.hcl")},
		{name: "tests-tree", change: updateLaneLock("tests/live/probes/x/.terraform.lock.hcl")},
		{name: "ignore-paths-tests", change: updateLaneAdd(`"ignorePaths": ["tests/**", "**/fixtures/**"],`)},
		{name: "narrowed-rule-before-the-group", change: updateLaneReplace(".github/renovate.json", `"packageRules": [`, `"packageRules": [{"groupName": "lint", "matchFileNames": ["mise.toml"], "matchDepNames": ["tflint"]},`)},
	}
	for _, row := range accepted {
		t.Run("accepted/"+row.name, func(t *testing.T) {
			if got := CheckUpdateLane(updateLaneTree(t, row)); len(got) != 0 {
				t.Errorf("BEHAVIORAL_RED: want no finding, got %v", got)
			}
		})
	}
}
