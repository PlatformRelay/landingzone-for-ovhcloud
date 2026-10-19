package release

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Tests of 006 T038 (FR-016) for what T036 left open: the commit argument, the toolchain table
// the renderer must read exactly, empty subjects, shallow repositories, and git's environment.

// fixtureCommitOnTop commits files (content, or nil to remove) on top of HEAD with the message
// and returns the new commit's id.
func fixtureCommitOnTop(t *testing.T, repo string, env []string, message string, files map[string]*string) string {
	t.Helper()
	writeReleaseFiles(t, repo, files)
	releaseGit(t, repo, env, "add", "-A")
	releaseGit(t, repo, env, "commit", "-q", "--no-verify", "--allow-empty", "--allow-empty-message", "-m", message)
	return releaseGit(t, repo, env, "rev-parse", "HEAD")
}

func wantRefusal(t *testing.T, rule string, out []byte, err error) {
	t.Helper()
	if refusalRule(err) != rule || out != nil {
		t.Errorf("%d bytes, error %v; want only a %s refusal", len(out), err, rule)
	}
}

// The commit argument is a full lowercase 40-hex id of a commit; anything else is refused.
func TestManifestRefusesCommit(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	c8 := ids["c8"]
	for name, commit := range map[string]string{
		"abbreviated": c8[:12],
		"ref":         "HEAD",
		"branch":      "main",
		"upper-case":  strings.ToUpper(c8),
		"tag-object":  releaseGit(t, repo, env, "rev-parse", "refs/tags/"+trainTag),
		"tree":        releaseGit(t, repo, env, "rev-parse", c8+"^{tree}"),
		"blob":        releaseGit(t, repo, env, "rev-parse", c8+":mise.toml"),
		"unknown":     strings.Repeat("ab", 20),
		"option":      "--output=" + filepath.Join(t.TempDir(), "x"),
		"suffixed":    c8 + "^",
		"empty":       "",
	} {
		t.Run("refused/"+name, func(t *testing.T) {
			out, err := Manifest(repo, commit, renderVersion)
			wantRefusal(t, "RELEASE_COMMIT", out, err)
		})
	}
}

// The [tools] table of mise.toml is read exactly or not at all: a value the renderer cannot read
// as one string is refused, never dropped or mangled.
func TestManifestRefusesToolchain(t *testing.T) {
	repo, env, _ := releaseFixture(t)
	head := "# pins\n[tools]\n"
	tail := "\n[env]\nX = \"y\"\n"
	for name, mise := range map[string]*string{
		"array":              fileText(head + "go = [\"1.27.1\"]" + tail),
		"inline-table":       fileText(head + "go = { version = \"1.27.1\" }" + tail),
		"dotted-key":         fileText(head + "go.version = \"1.27.1\"" + tail),
		"subtable":           fileText(head + "go = \"1.27.1\"\n[tools.node]\nversion = \"22\"" + tail),
		"top-level-tools":    fileText("tools = { go = \"1.27.1\" }\n" + head + "task = \"3.53.1\"" + tail),
		"multi-line-string":  fileText(head + "go = \"\"\"1.27.1\"\"\"" + tail),
		"multi-line-env":     fileText(head + "go = \"1.27.1\"\n[env]\nX = \"\"\"\n[tools]\ntask = \"9\"\n[env]\n\"\"\"\n"),
		"multi-line-literal": fileText(head + "go = \"1.27.1\"\n[env]\nX = '''\n[tools]\ntask = \"9\"\n[env]\n'''\n"),
		"header-text":        fileText("[tools] go = \"1.27.1\"\ngo = \"1.27.1\"" + tail),
		"escape":             fileText(head + "go = \"1.27\\u002e1\"" + tail),
		"duplicate":          fileText(head + "go = \"1.27.1\"\ngo = \"1.27.2\"" + tail),
		"whitespace":         fileText(head + "go = \"1.27 .1\"" + tail),
		"empty-version":      fileText(head + "go = \"\"" + tail),
		"unquoted":           fileText(head + "go = 1.27" + tail),
		"trailing-text":      fileText(head + "go = \"1.27.1\" latest" + tail),
		"no-mise":            nil,
		"escaped-header":     fileText("[\"\\u0074ools\"]\ngo = \"1.27.1\"" + tail),
		"escaped-root-key":   fileText("\"\\u0074ools\" = { go = \"1.27.1\" }\n" + head + "task = \"3.53.1\"" + tail),
	} {
		t.Run("refused/"+name, func(t *testing.T) {
			commit := fixtureCommitOnTop(t, repo, env, ":wrench: chore(mise): "+name, map[string]*string{"mise.toml": mise})
			out, err := Manifest(repo, commit, renderVersion)
			wantRefusal(t, "RELEASE_TOOLCHAIN", out, err)
		})
	}
	for name, c := range map[string]struct {
		mise string
		want map[string]any
	}{
		"inline-comment":      {head + "go = \"1.27.1\" # the pin" + tail, map[string]any{"go": "1.27.1"}},
		"literal-string":      {head + "'aqua:x/y' = '0.1.0'" + tail, map[string]any{"aqua:x/y": "0.1.0"}},
		"header-comment":      {"[tools] # pins\ngo = \"1.27.1\"" + tail, map[string]any{"go": "1.27.1"}},
		"header-spaces":       {"[ tools ]\ngo = \"1.27.1\"" + tail, map[string]any{"go": "1.27.1"}},
		"quoted-tools-header": {"[\"tools\"]\ngo = \"1.27.1\"" + tail, map[string]any{"go": "1.27.1"}},
		"quoted-other-table":  {head + "go = \"1.27.1\"\n[\"t ools\"]\ntask = \"9\"" + tail, map[string]any{"go": "1.27.1"}},
		"no-tools-table":      {"[env]\nX = \"y\"\n", map[string]any{}},
		"other-tables":        {"[settings]\ngo = \"9\"\n" + head + "go = \"1.27.1\"\n[tasks.go]\nrun = \"x\"\n", map[string]any{"go": "1.27.1"}},
	} {
		t.Run("accepted/"+name, func(t *testing.T) {
			commit := fixtureCommitOnTop(t, repo, env, ":wrench: chore(mise): "+name, map[string]*string{"mise.toml": &c.mise})
			if got := decodeManifest(t, mustRender(t, repo, commit, renderVersion))["toolchain"]; !reflect.DeepEqual(got, c.want) {
				t.Errorf("toolchain %v, want %v", got, c.want)
			}
		})
	}
}

// The lists are sorted by path as bytes, not in git's tree order, which sorts a directory as if
// its name ended in "/" (modules/naming-x before modules/naming).
func TestManifestSortsByPath(t *testing.T) {
	repo, env, _ := releaseFixture(t)
	commit := fixtureCommitOnTop(t, repo, env, ":sparkles: feat(naming): a neighbour", map[string]*string{
		"modules/naming-x/main.tf":               fileText("# neighbour\n"),
		"modules/naming-x/.terraform.lock.hcl":   fileText("# lock\n"),
		"schemas/outputs-x.schema.json":          fileText("{}\n"),
		"schemas/outputs/zz/nested.schema.json":  fileText("{}\n"),
		"modules/naming/x/.terraform.lock.hcl":   fileText("# lock\n"),
		"modules/naming/x-y/.terraform.lock.hcl": fileText("# lock\n"),
	})
	doc := decodeManifest(t, mustRender(t, repo, commit, renderVersion))
	for _, key := range []string{"closure", "schemas", "lockfiles"} {
		var paths []string
		for _, e := range doc[key].([]any) {
			paths = append(paths, e.(map[string]any)["path"].(string))
		}
		if !slices.IsSorted(paths) || !slices.Contains(paths, map[string]string{
			"closure": "modules/naming-x", "schemas": "schemas/outputs-x.schema.json", "lockfiles": "modules/naming-x/.terraform.lock.hcl",
		}[key]) {
			t.Errorf("%s paths %v: want sorted as bytes, with the neighbour", key, paths)
		}
	}
}

// A partial clone misses blobs its promisor remote holds: the render reads no object it would have
// to fetch, so it fails and leaves the clone as it was.
func TestManifestNoLazyFetch(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	releaseGit(t, repo, env, "config", "uploadpack.allowFilter", "true")
	partial := filepath.Join(t.TempDir(), "partial")
	releaseGit(t, repo, env, "clone", "-q", "--no-checkout", "--filter=blob:none", "--no-local", "file://"+repo, partial)
	before := repoState(t, partial, env)
	if out, err := Manifest(partial, ids["c8"], renderVersion); err == nil || out != nil {
		t.Errorf("Manifest on a partial clone: %d bytes, error %v; want a failure", len(out), err)
	}
	if after := repoState(t, partial, env); !slices.Equal(before, after) {
		t.Errorf("the render changed the partial clone: before %v, after %v", before, after)
	}
}

// The range starts at the newest train tag reachable from the commit, by version compared as
// numbers: 2026.09.10 on a side branch from c1 is newer than 2026.09.9 (on c3) and 2026.09.0 (on
// c2), so c2 and c3 are in the range and the side commit is not.
func TestManifestRangeFromNewestTrain(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	releaseGit(t, repo, env, "tag", "-a", "-m", "tag 2026.09.9", "2026.09.9", ids["c3"])
	releaseGit(t, repo, env, "checkout", "-q", "-b", "side", ids["c1"])
	fixtureCommitOnTop(t, repo, env, ":bug: fix(side): on the side", map[string]*string{"docs/side.md": fileText("side\n")})
	releaseGit(t, repo, env, "tag", "-a", "-m", "tag 2026.09.10", "2026.09.10")
	releaseGit(t, repo, env, "checkout", "-q", "main")
	releaseGit(t, repo, env, "merge", "-q", "--no-ff", "-m", ":twisted_rightwards_arrows: chore(merge): the side branch", "side")
	merge := releaseGit(t, repo, env, "rev-parse", "HEAD")
	got := renderedChangelog(t, mustRender(t, repo, merge, renderVersion))
	if want := []any{":bug: fix(naming): trim the prefix"}; !reflect.DeepEqual(got["fix"], want) {
		t.Errorf("fix %v, want %v (c3 in the range, the side commit not)", got["fix"], want)
	}
	if feat, _ := got["feat"].([]any); !slices.Contains(feat, any(":sparkles: feat(naming): add the naming module")) {
		t.Errorf("feat %v: want c2's subject (c2 is not behind 2026.09.10)", got["feat"])
	}
	if want := []any{":twisted_rightwards_arrows: chore(merge): the side branch"}; !reflect.DeepEqual(got["chore"], want) {
		t.Errorf("chore %v, want the merge only", got["chore"])
	}
}

// A commit in the range with an empty subject cannot be listed (the schema refuses an empty
// subject) and is not dropped: the render is refused.
func TestManifestRefusesEmptySubject(t *testing.T) {
	repo, env, _ := releaseFixture(t)
	commit := fixtureCommitOnTop(t, repo, env, "", nil)
	out, err := Manifest(repo, commit, renderVersion)
	wantRefusal(t, "RELEASE_SUBJECT", out, err)
	if err != nil && !strings.Contains(err.Error(), commit) {
		t.Errorf("refusal %v does not name the commit %s", err, commit)
	}
}

// A shallow clone would cut the changelog silently: it is refused.
func TestManifestRefusesShallow(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	shallow := filepath.Join(t.TempDir(), "shallow")
	releaseGit(t, repo, env, "clone", "-q", "--depth", "1", "--no-local", "file://"+repo, shallow)
	head := releaseGit(t, shallow, env, "rev-parse", "HEAD")
	out, err := Manifest(shallow, head, renderVersion)
	wantRefusal(t, "RELEASE_SHALLOW", out, err)
	if out := mustRender(t, repo, ids["c9"], renderVersion); out == nil {
		t.Error("control: the full repository renders nothing")
	}
}

// The caller's git environment does not reach the renderer's git, and replace refs and a graft file do not change
// the history it reads.
func TestManifestIgnoresCallerGitEnv(t *testing.T) {
	repo, env, ids := releaseFixture(t)
	golden := readGolden(t)
	// A replacement for c3 with another subject, as `git replace` records it in the repository.
	other := releaseGit(t, repo, env, "commit-tree", ids["c3"]+"^{tree}", "-p", ids["c2"], "-m", ":bug: fix(naming): a replaced subject")
	releaseGit(t, repo, env, "replace", ids["c3"], other)
	// A graft file that makes c5 a root, cutting c2–c4 and the train tag out of c8's history.
	if err := os.WriteFile(filepath.Join(repo, ".git", "info", "grafts"), []byte(ids["c5"]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Mkdir(elsewhere, 0o700); err != nil {
		t.Fatal(err)
	}
	releaseGit(t, elsewhere, env, "init", "-q")
	for k, v := range map[string]string{
		"GIT_DIR": filepath.Join(elsewhere, ".git"), "GIT_WORK_TREE": elsewhere,
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.abbrev", "GIT_CONFIG_VALUE_0": "7",
		"GIT_CONFIG_PARAMETERS": "'log.showsignature'='true'",
	} {
		t.Setenv(k, v)
	}
	if out := mustRender(t, repo, ids["c8"], renderVersion); !bytes.Equal(out, golden) {
		t.Errorf("render under the caller's git environment and a replace ref differs from %s:\n%s", releaseGoldenPath, out)
	}
}
