// Package release renders the ADR-0022 release-train manifest (006 FR-016).
package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

var (
	// versionPattern is data-model's train version, YYYY.MM.N; `$` matches at the end of the text
	// only, so a trailing newline is refused.
	versionPattern = regexp.MustCompile(`^[0-9]{4}\.(0[1-9]|1[0-2])\.[0-9]+$`)
	// commitPattern is the schema's source_commit: a full lowercase SHA-1 object id.
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// subjectPattern is the commit convention `:gitmoji: type(scope): summary`.
	subjectPattern = regexp.MustCompile(`^:[a-z0-9_+-]+: (build|chore|ci|docs|feat|fix|refactor|style|test)(\([^)]+\))?: \S`)
	versionPin     = regexp.MustCompile(`^\S+$`)
	commitTypes    = []string{"build", "chore", "ci", "docs", "feat", "fix", "refactor", "style", "test"}
	closureRoots   = []string{"modules", "components", "stages"}
)

type treeEntry struct {
	Path string `json:"path"`
	Tree string `json:"tree"`
}

type blobEntry struct {
	Blob string `json:"blob"`
	Path string `json:"path"`
}

// manifest is the document schemas/release-manifest.schema.json describes. Its fields are
// declared in key order and its maps encode with sorted keys, so the bytes are sorted.
type manifest struct {
	APIVersion   string              `json:"apiVersion"`
	Changelog    map[string][]string `json:"changelog"`
	Closure      []treeEntry         `json:"closure"`
	Kind         string              `json:"kind"`
	Lockfiles    []blobEntry         `json:"lockfiles"`
	Schemas      []blobEntry         `json:"schemas"`
	SourceCommit string              `json:"source_commit"`
	Tested       []string            `json:"tested"`
	Toolchain    map[string]string   `json:"toolchain"`
	Version      string              `json:"version"`
}

// Manifest renders the release-train manifest of commit (a full object id) in the git repository
// at repo, for the train version (YYYY.MM.N), to bytes: JSON-compatible flow-form YAML, keys
// sorted, two-space indentation, a final newline, no timestamps, validated by
// schemas/release-manifest.schema.json (data-model *Release-train manifest*). It reads git
// objects only, never the working tree, the index or HEAD, through git under an environment built
// from scratch (no caller configuration, no replace refs or grafts, no lazy fetch), so the bytes depend on the commit and
// version alone. The changelog lists the subjects of the commits reachable from commit but not from
// the newest train tag (a tag named YYYY.MM.N, newest by version) reachable from it, or from the
// root when none is, oldest first, grouped by commit type; a
// subject that does not follow `:gitmoji: type(scope): summary` is listed under unclassified.
//
// Refusals start with a rule id: RELEASE_VERSION (malformed version), RELEASE_VERSION_TAKEN (a
// tag already carries it), RELEASE_COMMIT (not a full lowercase 40-hex id of a commit in the
// repository), RELEASE_SHALLOW (a shallow repository would cut the changelog), RELEASE_TOOLCHAIN
// (mise.toml absent at the commit, or a [tools] table not read exactly) and RELEASE_SUBJECT (an
// empty subject in the range). It claims no tested tuple (`tested: []`), creates no tag and
// writes no file.
func Manifest(repo, commit, version string) ([]byte, error) {
	if !versionPattern.MatchString(version) {
		return nil, fmt.Errorf("RELEASE_VERSION %q is not YYYY.MM.N", version)
	}
	if !commitPattern.MatchString(commit) {
		return nil, fmt.Errorf("RELEASE_COMMIT %q is not a full lowercase 40-hex commit id", commit)
	}
	for _, check := range []func(string, string, string) error{checkComplete, checkVersionFree, checkCommit} {
		if err := check(repo, commit, version); err != nil {
			return nil, err
		}
	}
	doc, err := render(repo, commit, version)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("release: encode the manifest: %w", err)
	}
	return out.Bytes(), nil
}

// gitBin is the runtime's pinned git (checks.offline: /usr/bin/git), run by absolute path so the
// caller's PATH cannot choose the binary.
const gitBin = "/usr/bin/git"

// tagRefs is the ref namespace of every tag the manifest reads.
const tagRefs = "refs/tags/"

// gitEnv is the whole environment of every git call: a fixed PATH and nothing of the caller's,
// so GIT_DIR, GIT_CONFIG_* and the like cannot point git elsewhere or configure it.
func gitEnv() []string {
	return []string{
		"PATH=/usr/bin:/bin", "HOME=" + os.DevNull, "LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_ATTR_NOSYSTEM=1",
		"GIT_NO_REPLACE_OBJECTS=1", "GIT_GRAFT_FILE=" + os.DevNull, "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0",
	}
}

// git runs a read-only git command in repo and returns its standard output.
func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command(gitBin, append([]string{"-C", repo}, args...)...)
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 512 {
			msg = msg[:512]
		}
		return nil, fmt.Errorf("release: git %s: %w: %s", args[0], err, msg)
	}
	return out, nil
}

// lines splits git output into its non-empty lines.
func lines(out []byte) []string {
	return slices.DeleteFunc(strings.Split(string(out), "\n"), func(s string) bool { return s == "" })
}

func checkComplete(repo, _, _ string) error {
	out, err := git(repo, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "false" {
		return errors.New("RELEASE_SHALLOW the repository is shallow, so the changelog would be cut")
	}
	return nil
}

// checkVersionFree refuses a version any tag carries, reachable from the commit or not.
func checkVersionFree(repo, _, version string) error {
	ref := tagRefs + version
	out, err := git(repo, "for-each-ref", "--format=%(refname)", ref)
	if err != nil {
		return err
	}
	if slices.Contains(lines(out), ref) {
		return fmt.Errorf("RELEASE_VERSION_TAKEN %s: the tag %s exists", version, ref)
	}
	return nil
}

// checkCommit refuses an id that does not name a commit itself (a tag, tree or blob id, or an
// unknown object).
func checkCommit(repo, commit, _ string) error {
	out, err := git(repo, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil || strings.TrimSpace(string(out)) != commit {
		return fmt.Errorf("RELEASE_COMMIT %s is not a commit in the repository", commit)
	}
	return nil
}

func render(repo, commit, version string) (manifest, error) {
	tree, err := readTree(repo, commit)
	if err != nil {
		return manifest{}, err
	}
	if tree.mise == "" {
		return manifest{}, fmt.Errorf("RELEASE_TOOLCHAIN mise.toml is absent at %s", commit)
	}
	mise, err := git(repo, "cat-file", "blob", tree.mise)
	if err != nil {
		return manifest{}, err
	}
	toolchain, err := parseTools(string(mise))
	if err != nil {
		return manifest{}, err
	}
	log, err := changelog(repo, commit)
	if err != nil {
		return manifest{}, err
	}
	return manifest{
		APIVersion: "lz.platformrelay.dev/v1alpha1", Kind: "ReleaseTrain", Version: version, SourceCommit: commit,
		Closure: tree.closure, Schemas: tree.schemas, Lockfiles: tree.lockfiles,
		Toolchain: toolchain, Tested: []string{}, Changelog: log,
	}, nil
}

// listing is what the manifest takes from the commit's tree.
type listing struct {
	closure            []treeEntry
	schemas, lockfiles []blobEntry
	mise               string // mise.toml's blob id, "" when the commit has none
}

// readTree lists the commit's whole tree (-z: paths unquoted) and sorts each list by path.
func readTree(repo, commit string) (listing, error) {
	out, err := git(repo, "ls-tree", "-r", "-t", "-z", "--full-tree", commit)
	if err != nil {
		return listing{}, err
	}
	l := listing{closure: []treeEntry{}, schemas: []blobEntry{}, lockfiles: []blobEntry{}}
	for _, record := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return listing{}, fmt.Errorf("release: unexpected ls-tree record %q", record)
		}
		l.add(fields[1], fields[2], path)
	}
	slices.SortFunc(l.closure, func(a, b treeEntry) int { return strings.Compare(a.Path, b.Path) })
	byPath := func(a, b blobEntry) int { return strings.Compare(a.Path, b.Path) }
	slices.SortFunc(l.schemas, byPath)
	slices.SortFunc(l.lockfiles, byPath)
	return l, nil
}

// add files one tree entry: a directory directly under a closure root, a *.json blob under
// schemas/, a .terraform.lock.hcl blob inside the closure, or mise.toml at the root.
func (l *listing) add(kind, id, path string) {
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	inClosure := len(parts) >= 2 && slices.Contains(closureRoots, parts[0])
	switch {
	case kind == "tree" && inClosure && len(parts) == 2:
		l.closure = append(l.closure, treeEntry{Path: path, Tree: id})
	case kind != "blob":
	case path == "mise.toml":
		l.mise = id
	case parts[0] == "schemas" && len(parts) >= 2 && len(name) > len(".json") && strings.HasSuffix(name, ".json"):
		l.schemas = append(l.schemas, blobEntry{Blob: id, Path: path})
	case inClosure && len(parts) >= 3 && name == ".terraform.lock.hcl":
		l.lockfiles = append(l.lockfiles, blobEntry{Blob: id, Path: path})
	}
}

// previousTrain is the newest train tag (by version, compared as numbers) reachable from commit,
// or "" when none is.
func previousTrain(repo, commit string) (string, error) {
	tags, err := git(repo, "for-each-ref", "--merged", commit, "--format=%(refname)", tagRefs)
	if err != nil {
		return "", err
	}
	newest := ""
	for _, ref := range lines(tags) {
		name, ok := strings.CutPrefix(ref, tagRefs)
		if ok && versionPattern.MatchString(name) && (newest == "" || newerVersion(name, newest)) {
			newest = name
		}
	}
	return newest, nil
}

// newerVersion compares two well-formed train versions part by part as numbers of any length;
// equal numbers written differently (2026.10.01, 2026.10.1) fall back to the names' order.
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range pa {
		x, y := strings.TrimLeft(pa[i], "0"), strings.TrimLeft(pb[i], "0")
		if len(x) != len(y) {
			return len(x) > len(y)
		}
		if x != y {
			return x > y
		}
	}
	return a > b
}

// changelog groups the subjects of the commits reachable from commit but not from the previous
// train tag (all of them when there is none), oldest first; every group is present, empty or not.
func changelog(repo, commit string) (map[string][]string, error) {
	previous, err := previousTrain(repo, commit)
	if err != nil {
		return nil, err
	}
	args := []string{"rev-list", "--topo-order", "--reverse", "--no-commit-header", "--encoding=UTF-8", "--format=%H %s", commit}
	if previous != "" {
		args = append(args, "^"+tagRefs+previous)
	}
	out, err := git(repo, append(args, "--")...)
	if err != nil {
		return nil, err
	}
	log := map[string][]string{"unclassified": {}}
	for _, t := range commitTypes {
		log[t] = []string{}
	}
	for _, line := range lines(out) {
		id, subject, _ := strings.Cut(line, " ")
		if strings.TrimSpace(subject) == "" {
			return nil, fmt.Errorf("RELEASE_SUBJECT commit %s has an empty subject", id)
		}
		group := "unclassified"
		if m := subjectPattern.FindStringSubmatch(subject); m != nil {
			group = m[1]
		}
		log[group] = append(log[group], subject)
	}
	return log, nil
}
