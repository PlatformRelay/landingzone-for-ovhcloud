package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Tests of 006 T036 (FR-016, SC-006) for Manifest, written before it (T038). Every case runs on
// a fixture repository built here with the runtime's git: the entry's snapshot carries no .git.
// The fixture is deterministic (fixed identities and dates, SHA-1 objects), so its object ids,
// and with them testdata/manifest.golden, are the same on every git.

const (
	releaseSchemaPath = "../../../schemas/release-manifest.schema.json"
	releaseGoldenPath = "testdata/manifest.golden"
	trainTag          = "2026.09.0" // the fixture's one train tag, on c2
	renderVersion     = "2026.10.0"
)

var releaseStrict = jsonv2.RejectUnknownMembers(true)

// releaseCommit is one commit of the fixture: files written (content) or removed (nil), then
// committed with the message; tags are annotated tags put on it afterwards.
type releaseCommit struct {
	label   string
	message string
	files   map[string]*string
	tags    []string
}

func fileText(s string) *string { return &s }

const releaseMise = `# Toolchain pins of the fixture.
[tools]
go = "1.27.1"
opentofu = "1.13.0"
"aqua:terramate-io/terramate" = "0.17.3"
# tflint = "0.63.0"

[env]
LZ_OFFLINE_IMAGE = "example.invalid/base@sha256:0000000000000000000000000000000000000000000000000000000000000000"
`

const releaseLock = `provider "registry.opentofu.org/ovh/ovh" {
  version = "%s"
}
`

// releaseHistory: c1 is the root, c2 carries the train tag, c3–c8 follow it (c8 is the commit the
// tests render), c9 comes after the rendered commit and is HEAD. c5 carries a module tag, which is
// no train tag. Every type but chore has a subject after the tag (c5a–c5e: ci, style, test,
// refactor, build), so a group left empty is seen.
func releaseHistory() []releaseCommit {
	return []releaseCommit{
		{"c1", ":tada: chore(repo): start the repository", map[string]*string{
			"README.md": fileText("fixture\n"), "mise.toml": fileText(releaseMise),
			"modules/README.md": fileText("modules\n"), "schemas/README.md": fileText("schemas\n"),
		}, nil},
		{"c2", ":sparkles: feat(naming): add the naming module", map[string]*string{
			"modules/naming/main.tf":                            fileText("# naming v1\n"),
			"modules/naming/tests/unit.tftest.hcl":              fileText("# unit\n"),
			"modules/naming/examples/basic/.terraform.lock.hcl": fileText(fmt.Sprintf(releaseLock, "2.20.0")),
			"schemas/deployments.schema.json":                   fileText("{\"title\": \"deployments\"}\n"),
		}, []string{trainTag}},
		{"c3", ":bug: fix(naming): trim the prefix", map[string]*string{
			"modules/naming/main.tf": fileText("# naming v2\n"),
		}, nil},
		{"c4", "Update README", map[string]*string{"README.md": fileText("fixture, updated\n")}, nil},
		{"c5", ":memo: docs: describe the stages", map[string]*string{
			"docs/stages.md": fileText("stages\n"),
		}, []string{"modules/naming/v0.2.0"}},
		{"c5a", ":construction_worker: ci: run the train checks", map[string]*string{"docs/ci.md": fileText("ci\n")}, nil},
		{"c5b", ":art: style(naming): align the arguments", map[string]*string{"docs/style.md": fileText("style\n")}, nil},
		{"c5c", ":white_check_mark: test(naming): cover the prefix", map[string]*string{"docs/test.md": fileText("test\n")}, nil},
		{"c5d", ":recycle: refactor(state): split the backend", map[string]*string{"docs/refactor.md": fileText("refactor\n")}, nil},
		{"c5e", ":package: build(tools): pin the renderer", map[string]*string{"docs/build.md": fileText("build\n")}, nil},
		{"c6", ":sparkles: feat(state): add the state backend component\n\n:bug: fix(body): a body line, not a subject\n", map[string]*string{
			"components/state-backend/main.tf":             fileText("# state backend\n"),
			"components/state-backend/.terraform.lock.hcl": fileText(fmt.Sprintf(releaseLock, "2.21.0")),
			"schemas/outputs/envelope.schema.json":         fileText("{\"title\": \"envelope\"}\n"),
			"stacks/bootstrap/.terraform.lock.hcl":         fileText(fmt.Sprintf(releaseLock, "2.21.0")),
			"tests/fixtures/lock/.terraform.lock.hcl":      fileText(fmt.Sprintf(releaseLock, "1.0.0")),
		}, nil},
		{"c7", "✨ feat(stages): add the bootstrap stage", map[string]*string{
			"stages/bootstrap/main.tf":             fileText("# bootstrap\n"),
			"stages/bootstrap/.terraform.lock.hcl": fileText(fmt.Sprintf(releaseLock, "2.21.0")),
			"mise.toml":                            fileText(strings.Replace(releaseMise, "[env]", "task = \"3.53.1\"\n\n[env]", 1)),
		}, nil},
		{"c8", ":wrench: release(train): prepare the train", map[string]*string{
			"modules/object-storage-user/main.tf":  fileText("# user\n"),
			"modules/naming/tests/unit.tftest.hcl": nil,
		}, nil},
		{"c9", ":recycle: refactor(naming): after the rendered commit", map[string]*string{
			"modules/naming/main.tf":          fileText("# naming v3\n"),
			"schemas/late.schema.json":        fileText("{}\n"),
			"stages/late/.terraform.lock.hcl": fileText(fmt.Sprintf(releaseLock, "2.22.0")),
			"mise.toml":                       fileText(strings.Replace(releaseMise, "1.27.1", "1.27.2", 1)),
		}, nil},
	}
}

// releaseGitEnv isolates git from the host: no system or global configuration, a private HOME,
// fixed identities and dates. Manifest runs under the same variables (t.Setenv).
func releaseGitEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	vars := map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull,
		"GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
		"GIT_COMMITTER_NAME": "Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
		"GIT_AUTHOR_DATE": "2026-09-01T00:00:00+00:00", "GIT_COMMITTER_DATE": "2026-09-01T00:00:00+00:00",
		"TZ": "UTC", "LC_ALL": "C",
	}
	// Variables that would point git at another repository or add configuration are removed for
	// Manifest too (t.Setenv restores them afterwards).
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CONFIG",
		"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_SYSTEM", "GIT_CEILING_DIRECTORIES"} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"PATH=" + os.Getenv("PATH")}
	for k, v := range vars {
		t.Setenv(k, v)
		env = append(env, k+"="+v)
	}
	return env
}

func releaseGit(t *testing.T, repo string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("git %v: %v: %s", args, err, exit.Stderr)
		}
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func writeReleaseFiles(t *testing.T, repo string, files map[string]*string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if content == nil {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(*content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// releaseFixture builds the fixture repository and returns it with the commit id of each label.
func releaseFixture(t *testing.T) (string, []string, map[string]string) {
	t.Helper()
	env := releaseGitEnv(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	releaseGit(t, repo, env, "init", "-q", "--object-format=sha1", "-b", "main")
	ids := map[string]string{}
	for _, c := range releaseHistory() {
		writeReleaseFiles(t, repo, c.files)
		releaseGit(t, repo, env, "add", "-A")
		releaseGit(t, repo, env, "commit", "-q", "--no-verify", "-m", c.message)
		ids[c.label] = releaseGit(t, repo, env, "rev-parse", "HEAD")
		for _, tag := range c.tags {
			releaseGit(t, repo, env, "tag", "-a", "-m", "tag "+tag, tag)
		}
	}
	return repo, env, ids
}

func fixtureObjectID(t *testing.T, repo string, env []string, commit, path string) string {
	t.Helper()
	return releaseGit(t, repo, env, "rev-parse", commit+":"+path)
}

func wantEntries(t *testing.T, repo string, env []string, commit, key string, paths ...string) []any {
	t.Helper()
	list := []any{}
	for _, p := range paths {
		list = append(list, map[string]any{"path": p, key: fixtureObjectID(t, repo, env, commit, p)})
	}
	return list
}

func wantChangelog(groups map[string][]string) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"build", "chore", "ci", "docs", "feat", "fix", "refactor", "style", "test", "unclassified"} {
		subjects := []any{}
		for _, s := range groups[k] {
			subjects = append(subjects, s)
		}
		out[k] = subjects
	}
	return out
}

// expectedC8 is the manifest of c8 at renderVersion, built from the fixture's own git objects:
// the closure is every directory directly under modules/, components/ and stages/ (not a file
// there); schemas every *.json under schemas/; lock files every .terraform.lock.hcl inside the
// closure (not stacks/ or tests/); the toolchain the [tools] table of c8's mise.toml; the
// changelog c3–c8, oldest first.
func expectedC8(t *testing.T, repo string, env []string, ids map[string]string) map[string]any {
	t.Helper()
	c8 := ids["c8"]
	return map[string]any{
		"apiVersion": "lz.platformrelay.dev/v1alpha1", "kind": "ReleaseTrain",
		"version": renderVersion, "source_commit": c8,
		"closure": wantEntries(t, repo, env, c8, "tree", "components/state-backend", "modules/naming",
			"modules/object-storage-user", "stages/bootstrap"),
		"schemas":   wantEntries(t, repo, env, c8, "blob", "schemas/deployments.schema.json", "schemas/outputs/envelope.schema.json"),
		"lockfiles": wantEntries(t, repo, env, c8, "blob", "components/state-backend/.terraform.lock.hcl", "modules/naming/examples/basic/.terraform.lock.hcl", "stages/bootstrap/.terraform.lock.hcl"),
		"toolchain": map[string]any{"aqua:terramate-io/terramate": "0.17.3", "go": "1.27.1", "opentofu": "1.13.0", "task": "3.53.1"},
		"tested":    []any{},
		"changelog": wantChangelog(map[string][]string{
			"fix":          {":bug: fix(naming): trim the prefix"},
			"ci":           {":construction_worker: ci: run the train checks"},
			"style":        {":art: style(naming): align the arguments"},
			"test":         {":white_check_mark: test(naming): cover the prefix"},
			"refactor":     {":recycle: refactor(state): split the backend"},
			"build":        {":package: build(tools): pin the renderer"},
			"docs":         {":memo: docs: describe the stages"},
			"feat":         {":sparkles: feat(state): add the state backend component"},
			"unclassified": {"Update README", "✨ feat(stages): add the bootstrap stage", ":wrench: release(train): prepare the train"},
		}),
	}
}

// decodeManifest reads a rendered manifest as JSON-compatible flow-form YAML; duplicate keys are
// refused.
func decodeManifest(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := jsonv2.Unmarshal(data, &doc); err != nil {
		t.Fatalf("manifest is not JSON-compatible flow-form YAML: %v\n%s", err, data)
	}
	return doc
}

func mustRender(t *testing.T, repo, commit, version string) []byte {
	t.Helper()
	out, err := Manifest(repo, commit, version)
	if err != nil {
		t.Fatalf("Manifest(%s, %s): %v", commit[:7], version, err)
	}
	return out
}

func readGolden(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(releaseGoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Case 1: a known commit and version render to the golden bytes twice, and they validate
// against the schema and carry the fixture's own object ids.
func TestManifestGolden(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	golden := readGolden(t)
	first := mustRender(t, repo, ids["c8"], renderVersion)
	second := mustRender(t, repo, ids["c8"], renderVersion)
	if !bytes.Equal(first, second) {
		t.Errorf("two renders differ:\n%s\n---\n%s", first, second)
	}
	if !bytes.Equal(first, golden) {
		t.Errorf("render differs from %s:\n%s", releaseGoldenPath, first)
	}
	if err := validateRelease(t, first); err != nil {
		t.Errorf("render does not validate against the schema: %v", err)
	}
	if got, want := decodeManifest(t, first), expectedC8(t, repo, env, ids); !reflect.DeepEqual(got, want) {
		t.Errorf("render's content:\n%v\nwant\n%v", got, want)
	}
}

// Control (green at the stub): the golden file is the fixture's c8 manifest and validates, so a
// render equal to it is right, not merely stable.
func TestManifestGoldenMatchesFixture(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	golden := readGolden(t)
	if err := validateRelease(t, golden); err != nil {
		t.Errorf("golden does not validate: %v", err)
	}
	if got, want := decodeManifest(t, golden), expectedC8(t, repo, env, ids); !reflect.DeepEqual(got, want) {
		t.Errorf("golden:\n%v\nwant (from the fixture's git objects)\n%v", got, want)
	}
}

// ruleOf is the refusal's rule id: the error text up to the first space or colon.
func refusalRule(err error) string {
	if err == nil {
		return ""
	}
	rule, _, _ := strings.Cut(strings.ReplaceAll(err.Error(), ":", " "), " ")
	return rule
}

// Case 2: a malformed version is refused with RELEASE_VERSION; well-formed versions render.
func TestManifestVersion(t *testing.T) {
	repo, _, ids := releaseFixture(t)
	for name, version := range map[string]string{
		"month-13": "2026.13.0", "month-00": "2026.00.0", "month-one-digit": "2026.1.0",
		"year-two-digits": "26.10.0", "year-five-digits": "20261.10.0", "no-patch": "2026.10",
		"prefixed": "v2026.10.0", "suffixed": "2026.10.0-rc.1", "trailing-newline": "2026.10.0\n",
		"patch-not-a-number": "2026.10.x", "empty": "",
	} {
		t.Run("refused/"+name, func(t *testing.T) {
			out, err := Manifest(repo, ids["c8"], version)
			if refusalRule(err) != "RELEASE_VERSION" || out != nil {
				t.Errorf("Manifest(%q): %d bytes, error %v; want only a RELEASE_VERSION refusal", version, len(out), err)
			}
		})
	}
	for _, version := range []string{"2026.01.0", "2026.12.17", "2027.10.0"} {
		t.Run("accepted/"+version, func(t *testing.T) {
			if doc := decodeManifest(t, mustRender(t, repo, ids["c8"], version)); doc["version"] != version {
				t.Errorf("version %v, want %s", doc["version"], version)
			}
		})
	}
}

// Case 3: the version an existing tag carries is refused with RELEASE_VERSION_TAKEN, also when
// the tag is not reachable from the commit; a neighbouring version is not taken.
func TestManifestVersionTaken(t *testing.T) {
	repo, _, ids := releaseFixture(t)
	for name, commit := range map[string]string{"tag-reachable": ids["c8"], "tag-not-reachable": ids["c1"]} {
		t.Run("refused/"+name, func(t *testing.T) {
			out, err := Manifest(repo, commit, trainTag)
			if refusalRule(err) != "RELEASE_VERSION_TAKEN" || out != nil || !strings.Contains(err.Error(), trainTag) {
				t.Errorf("Manifest(%s): %d bytes, error %v; want a RELEASE_VERSION_TAKEN refusal naming %s", trainTag, len(out), err, trainTag)
			}
		})
	}
	t.Run("accepted/neighbour", func(t *testing.T) {
		mustRender(t, repo, ids["c8"], "2026.09.1")
	})
}

func renderedChangelog(t *testing.T, data []byte) map[string]any {
	t.Helper()
	got, ok := decodeManifest(t, data)["changelog"].(map[string]any)
	if !ok {
		t.Fatalf("no changelog object:\n%s", data)
	}
	return got
}

// Case 4: non-conforming subjects since the train tag are listed under unclassified, never
// dropped; commits before the tag or after the commit, body lines and other tags do not move the
// range.
func TestManifestUnclassified(t *testing.T) {
	repo, _, ids := releaseFixture(t)
	got := renderedChangelog(t, mustRender(t, repo, ids["c8"], renderVersion))
	want := []any{"Update README", "✨ feat(stages): add the bootstrap stage", ":wrench: release(train): prepare the train"}
	if !reflect.DeepEqual(got["unclassified"], want) {
		t.Errorf("unclassified %v, want %v", got["unclassified"], want)
	}
	if !reflect.DeepEqual(got["fix"], []any{":bug: fix(naming): trim the prefix"}) || !reflect.DeepEqual(got["chore"], []any{}) {
		t.Errorf("fix %v, chore %v: want c3's subject only (c6's body line and c1 before the tag are no subjects)", got["fix"], got["chore"])
	}
	t.Run("from-the-root", func(t *testing.T) {
		got := renderedChangelog(t, mustRender(t, repo, ids["c1"], renderVersion))
		if want := wantChangelog(map[string][]string{"chore": {":tada: chore(repo): start the repository"}}); !reflect.DeepEqual(got, want) {
			t.Errorf("changelog of c1 (no train tag reachable): %v, want %v", got, want)
		}
	})
}

// Case 5: a working-tree change (tracked edits, a staged edit, a removed lock file, untracked
// files in the closure and schemas) leaves the render unchanged.
func TestManifestIgnoresWorkingTree(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	golden := readGolden(t)
	writeReleaseFiles(t, repo, map[string]*string{
		"modules/naming/main.tf":               fileText("# edited\n"),
		"mise.toml":                            fileText(strings.Replace(releaseMise, "[env]", "tflint = \"0.64.0\"\n\n[env]", 1)),
		"components/state-backend/main.tf":     fileText("# staged\n"),
		"stages/bootstrap/.terraform.lock.hcl": nil,
		"modules/untracked/main.tf":            fileText("# untracked\n"),
		"schemas/untracked.schema.json":        fileText("{}\n"),
	})
	releaseGit(t, repo, env, "add", "components/state-backend/main.tf")
	if out := mustRender(t, repo, ids["c8"], renderVersion); !bytes.Equal(out, golden) {
		t.Errorf("render with a changed working tree differs from %s:\n%s", releaseGoldenPath, out)
	}
}

// repoState lists every path under the repository (inside .git by name only, elsewhere with its
// content digest) and every ref with its object.
func repoState(t *testing.T, repo string, env []string) []string {
	t.Helper()
	var state []string
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, path)
		if d.IsDir() || rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			state = append(state, rel)
			return nil
		}
		data, err := os.ReadFile(path)
		sum := sha256.Sum256(data)
		state = append(state, rel+" "+hex.EncodeToString(sum[:]))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(state, strings.Split(releaseGit(t, repo, env, "for-each-ref", "--format=%(refname) %(objectname)"), "\n")...)
}

// Case 6: no render, accepted or refused, claims a tested tuple, creates a tag, or writes a file:
// not in the repository, not under releases/, not in the working directory.
func TestManifestClaimsNothing(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	before := repoState(t, repo, env)
	out := mustRender(t, repo, ids["c8"], renderVersion)
	if tested, ok := decodeManifest(t, out)["tested"].([]any); !ok || len(tested) != 0 {
		t.Errorf("tested is %v, want an empty list", decodeManifest(t, out)["tested"])
	}
	checkUnchanged(t, renderVersion, repo, env, cwd, before)
	for _, version := range []string{"2026.13.0", trainTag} {
		if _, err := Manifest(repo, ids["c8"], version); err == nil {
			t.Errorf("Manifest(%s) was not refused", version)
		}
		checkUnchanged(t, version, repo, env, cwd, before)
	}
}

func checkUnchanged(t *testing.T, version, repo string, env []string, cwd string, before []string) {
	t.Helper()
	if after := repoState(t, repo, env); !slices.Equal(before, after) {
		t.Errorf("render of %s changed the repository: before %v, after %v", version, before, after)
	}
	if _, err := os.Stat(filepath.Join(repo, "releases")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("render of %s: releases/ exists (%v)", version, err)
	}
	if left, _ := os.ReadDir(cwd); len(left) != 0 {
		t.Errorf("render of %s wrote %d entries into the working directory", version, len(left))
	}
}

// releaseSchemaNode is the part of JSON Schema the release schema may use. It is decoded with
// unknown members refused, so a keyword this validator does not implement fails the test instead
// of being ignored.
type releaseSchemaNode struct {
	Schema               string                        `json:"$schema"`
	ID                   string                        `json:"$id"`
	Ref                  string                        `json:"$ref"`
	Defs                 map[string]*releaseSchemaNode `json:"$defs"`
	Title                string                        `json:"title"`
	Description          string                        `json:"description"`
	Type                 string                        `json:"type"`
	Const                *string                       `json:"const"`
	Pattern              string                        `json:"pattern"`
	Required             []string                      `json:"required"`
	Properties           map[string]*releaseSchemaNode `json:"properties"`
	AdditionalProperties jsontext.Value                `json:"additionalProperties"`
	Items                *releaseSchemaNode            `json:"items"`
	MaxItems             *int                          `json:"maxItems"`
}

type releaseValidator struct{ root *releaseSchemaNode }

func (v releaseValidator) check(n *releaseSchemaNode, value any, path string) []string {
	if n.Ref != "" {
		def, ok := v.root.Defs[strings.TrimPrefix(n.Ref, "#/$defs/")]
		if !ok || !strings.HasPrefix(n.Ref, "#/$defs/") {
			return []string{path + ": unresolved $ref " + n.Ref}
		}
		n = def
	}
	if n.Const != nil && value != *n.Const {
		return []string{fmt.Sprintf("%s: %v is not the constant %s", path, value, *n.Const)}
	}
	switch n.Type {
	case "object":
		return v.object(n, value, path)
	case "array":
		return v.array(n, value, path)
	case "string":
		return v.str(n, value, path)
	case "":
		return nil
	}
	return []string{path + ": schema type " + n.Type + " not implemented by the validator"}
}

func (v releaseValidator) str(n *releaseSchemaNode, value any, path string) []string {
	s, ok := value.(string)
	if !ok {
		return []string{fmt.Sprintf("%s: %v is not a string", path, value)}
	}
	if n.Pattern != "" && !regexp.MustCompile(n.Pattern).MatchString(s) {
		return []string{fmt.Sprintf("%s: %q does not match %s", path, s, n.Pattern)}
	}
	return nil
}

func (v releaseValidator) array(n *releaseSchemaNode, value any, path string) []string {
	list, ok := value.([]any)
	if !ok {
		return []string{fmt.Sprintf("%s: %v is not an array", path, value)}
	}
	var errs []string
	if n.MaxItems != nil && len(list) > *n.MaxItems {
		errs = append(errs, fmt.Sprintf("%s: %d items, at most %d", path, len(list), *n.MaxItems))
	}
	for i, item := range list {
		if n.Items != nil {
			errs = append(errs, v.check(n.Items, item, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return errs
}

func (v releaseValidator) object(n *releaseSchemaNode, value any, path string) []string {
	m, ok := value.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("%s: %v is not an object", path, value)}
	}
	var errs []string
	for _, k := range n.Required {
		if _, ok := m[k]; !ok {
			errs = append(errs, path+"."+k+": required, missing")
		}
	}
	for k, member := range m {
		errs = append(errs, v.member(n, k, member, path+"."+k)...)
	}
	return errs
}

func (v releaseValidator) member(n *releaseSchemaNode, key string, value any, path string) []string {
	if sub, ok := n.Properties[key]; ok {
		return v.check(sub, value, path)
	}
	switch extra := strings.TrimSpace(string(n.AdditionalProperties)); extra {
	case "false":
		return []string{path + ": not a property of the schema"}
	case "", "true":
		return nil
	}
	var sub releaseSchemaNode
	if err := jsonv2.Unmarshal(n.AdditionalProperties, &sub, releaseStrict); err != nil {
		return []string{path + ": additionalProperties: " + err.Error()}
	}
	return v.check(&sub, value, path)
}

// validateRelease validates a rendered manifest against schemas/release-manifest.schema.json.
func validateRelease(t *testing.T, data []byte) error {
	t.Helper()
	raw, err := os.ReadFile(releaseSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var root releaseSchemaNode
	if err := jsonv2.Unmarshal(raw, &root, releaseStrict); err != nil {
		t.Fatalf("schema uses a keyword the validator does not implement, or is not JSON: %v", err)
	}
	var doc any
	if err := jsonv2.Unmarshal(data, &doc); err != nil {
		return err
	}
	if errs := (releaseValidator{root: &root}).check(&root, doc, "$"); len(errs) > 0 {
		slices.Sort(errs)
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Control (green at the stub): the schema refuses each way a manifest can be wrong, so "validates
// against the schema" in case 1 is not vacuous.
func TestManifestSchemaRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		edit func(doc map[string]any)
		path string
	}{
		"tested-claimed":    {func(d map[string]any) { d["tested"] = []any{"solo"} }, "$.tested"},
		"tested-missing":    {func(d map[string]any) { delete(d, "tested") }, "$.tested"},
		"extra-key":         {func(d map[string]any) { d["created"] = "2026-10-19" }, "$.created"},
		"version-malformed": {func(d map[string]any) { d["version"] = "2026.13.0" }, "$.version"},
		"kind-other":        {func(d map[string]any) { d["kind"] = "Release" }, "$.kind"},
		"commit-short":      {func(d map[string]any) { d["source_commit"] = "abc1234" }, "$.source_commit"},
		"closure-file": {func(d map[string]any) {
			d["closure"] = []any{map[string]any{"path": "modules/naming/main.tf", "tree": strings.Repeat("a", 40)}}
		}, "$.closure[0].path"},
		"schema-not-json": {func(d map[string]any) {
			d["schemas"] = []any{map[string]any{"path": "schemas/README.md", "blob": strings.Repeat("a", 40)}}
		}, "$.schemas[0].path"},
		"lockfile-in-stacks": {func(d map[string]any) {
			d["lockfiles"] = []any{map[string]any{"path": "stacks/x/.terraform.lock.hcl", "blob": strings.Repeat("a", 40)}}
		}, "$.lockfiles[0].path"},
		"toolchain-not-string": {func(d map[string]any) { d["toolchain"] = map[string]any{"go": 1.27} }, "$.toolchain.go"},
		"unclassified-missing": {func(d map[string]any) { delete(d["changelog"].(map[string]any), "unclassified") }, "$.changelog.unclassified"},
		"changelog-other-type": {func(d map[string]any) { d["changelog"].(map[string]any)["perf"] = []any{} }, "$.changelog.perf"},
		"subject-empty":        {func(d map[string]any) { d["changelog"].(map[string]any)["fix"] = []any{" "} }, "$.changelog.fix[0]"},
	} {
		t.Run(name, func(t *testing.T) {
			doc := decodeManifest(t, readGolden(t))
			c.edit(doc)
			data, err := jsonv2.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRelease(t, data); err == nil || !strings.Contains(err.Error(), c.path+":") {
				t.Errorf("schema admitted the edit, or refused it elsewhere: %v; want a refusal at %s", err, c.path)
			}
		})
	}
}
