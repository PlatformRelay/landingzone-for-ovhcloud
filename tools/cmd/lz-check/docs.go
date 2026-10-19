package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
)

// The documentation rules of ADR-0013 that hold without a site build: every
// page under docs/ except the ADRs (history, written before the rule) carries
// front matter naming the repository files it describes with their git blob
// ids and the commit it was last checked against; a page is stale once a
// referenced file's blob differs or the file is gone. On every page, ADRs
// included, relative links and anchors resolve and no 32-hex id (an OVHcloud
// project or account id) appears; outside the ADRs, which name targets that
// were designed but not built, every `task <target>` named exists in
// Taskfile.yml. The root README.md, when present, and every README.md below
// the other top-level directories (outside hidden directories and fixtures,
// testdata and vendor trees) are held to the link and anchor rules only (T107);
// DOCS_OK counts them and says whether the root README was checked or absent
// (the offline entry's snapshot holds none).
//
// 006 FR-018 adds two anti-rot gates (versionGate, supersededScan): a page's
// verified_against is compared with the tool pins, and no file outside the
// ADRs and vendored code names or links a Superseded ADR. FR-019 adds the
// repository map check (repositoryMap).
//
// Blob ids are git's (sha1 over "blob <size>\x00" and the content), computed
// from the file on disk: on a clean checkout that is the id of HEAD:<path>,
// and the check needs neither git nor a .git directory.

const adrDir = "docs/adr/"

var (
	fullHex = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// hexID is 32 hex digits in either case not adjoining another letter or
	// digit; '_' adjoins (project_<id>), a 40-hex blob id does not match.
	hexID = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])[0-9a-f]{32}(?:$|[^0-9a-z])`)
	// taskfileTarget is a target key, indented once under tasks:.
	taskfileTarget = regexp.MustCompile(`^  ['"]?([a-z][\w:-]*)['"]?:\s*$`)
	// taskUse is a `task <target>` invocation; a target followed by '<', '{'
	// or '*', directly or after ':', is a placeholder (capture:<stage>-plan,
	// live:*) and not judged.
	taskUse = regexp.MustCompile(`\btask +([a-z][a-z0-9_-]*(?::[a-z0-9_-]+)*)`)
	// inlineLink is a link destination, with or without a title.
	inlineLink = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?\)`)
	// refLink is a reference definition; a footnote ([^1]:) is not a link.
	refLink     = regexp.MustCompile(`^ {0,3}\[[^\]^][^\]]*\]:\s*(\S+)`)
	codeSpan    = regexp.MustCompile("`+[^`]*`+")
	fence       = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")
	heading     = regexp.MustCompile(`^ {0,3}#{1,6}\s+(.+?)\s*#*\s*$`)
	htmlAnchor  = regexp.MustCompile(`<a\s+(?:id|name)="([^"]+)"`)
	headingLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	scheme      = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

type docReference struct{ path, blob string }

type docFinding struct{ rule, page, detail string }

type docChecker struct {
	root     string
	targets  map[string]bool
	anchors  map[string]map[string]bool
	findings []docFinding
	// rootReadme is whether the root README.md exists; without it a README's
	// link to it is counted in unjudgedRoot instead of judged (the entry's
	// snapshot).
	rootReadme   bool
	unjudgedRoot int
	// pins are the tool pins verified_against is judged against (FR-018).
	pins map[string]docPin
}

func docs(out io.Writer, root string) int {
	taskfile, err := os.ReadFile(filepath.Join(root, "Taskfile.yml"))
	if err != nil {
		fmt.Fprintln(out, "TASKFILE_MISSING:", err)
		return 2
	}
	var pages []string
	err = filepath.WalkDir(filepath.Join(root, "docs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && strings.HasSuffix(p, ".md") {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			pages = append(pages, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(out, "DOCS_MISSING:", err)
		return 2
	}
	sort.Strings(pages)
	readmes, rootReadme, err := readmePages(root)
	if err != nil {
		fmt.Fprintln(out, "README_UNREADABLE:", err)
		return 2
	}
	c := &docChecker{root: root, targets: taskfileTargets(string(taskfile)), anchors: map[string]map[string]bool{}, rootReadme: rootReadme}
	c.loadPins()
	headed, references := 0, 0
	for _, page := range pages {
		data, err := os.ReadFile(filepath.Join(root, page))
		if err != nil {
			fmt.Fprintln(out, "PAGE_UNREADABLE:", err)
			return 2
		}
		text := string(data)
		adr := strings.HasPrefix(page, adrDir)
		if !adr {
			headed++
			references += c.header(page, text)
		}
		c.body(page, text, bodyRules{ids: true, targets: !adr})
		c.versionGate(page, text)
	}
	if len(pages) == 0 {
		c.add("NO_PAGES", "docs", "no Markdown page under docs/")
	}
	for _, page := range readmes {
		data, err := os.ReadFile(filepath.Join(root, page))
		if err != nil {
			fmt.Fprintln(out, "PAGE_UNREADABLE:", err)
			return 2
		}
		c.body(page, string(data), bodyRules{readme: true})
	}
	c.supersededScan()
	unjudgedMap := c.repositoryMap()
	rootState := fmt.Sprintf("absent unjudged_root_links=%d", c.unjudgedRoot)
	if rootReadme {
		rootState = "checked"
	}
	for _, f := range c.findings {
		fmt.Fprintf(out, "%s %s: %s\n", f.rule, f.page, f.detail)
	}
	// The map paths outside the entry's admitted names, skipped (FR-019).
	fmt.Fprintf(out, "MAP_UNJUDGED=%d\n", unjudgedMap)
	if len(c.findings) > 0 {
		fmt.Fprintf(out, "DOCS_FAIL findings=%d\n", len(c.findings))
		return 1
	}
	fmt.Fprintf(out, "DOCS_OK pages=%d headed=%d references=%d readmes=%d root_readme=%s\n", len(pages), headed, references, len(readmes), rootState)
	return 0
}

// readmeSkip are the directory names whose READMEs are not read: a fixture's
// or a test's data, and third-party code.
var readmeSkip = map[string]bool{"fixtures": true, "testdata": true, "vendor": true}

// readmePages are the READMEs held to the link rules (T107), sorted: the root
// README.md when it exists (rootReadme), and every regular README.md below a
// top-level directory other than docs/ (whose pages have every rule), outside
// hidden directories and readmeSkip trees.
func readmePages(root string) (pages []string, rootReadme bool, err error) {
	// A root README that is not a regular file (a symbolic link, a directory)
	// is an input error, never followed out of the repository (review r2).
	if info, err := os.Lstat(filepath.Join(root, "README.md")); err == nil {
		if !info.Mode().IsRegular() {
			return nil, false, errors.New("README.md is not a regular file")
		}
		pages, rootReadme = append(pages, "README.md"), true
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false, err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "docs" || strings.HasPrefix(e.Name(), ".") || readmeSkip[e.Name()] {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, e.Name()), func(p string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case d.IsDir() && (strings.HasPrefix(d.Name(), ".") || readmeSkip[d.Name()]):
				return filepath.SkipDir
			case d.Name() != "README.md" || !d.Type().IsRegular():
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err == nil {
				pages = append(pages, filepath.ToSlash(rel))
			}
			return err
		})
		if err != nil {
			return nil, false, err
		}
	}
	sort.Strings(pages)
	return pages, rootReadme, nil
}

func (c *docChecker) add(rule, page, format string, args ...any) {
	c.findings = append(c.findings, docFinding{rule, page, fmt.Sprintf(format, args...)})
}

// taskfileTargets is the set of target names under the Taskfile's tasks: key.
func taskfileTargets(taskfile string) map[string]bool {
	targets := map[string]bool{}
	inTasks := false
	for _, line := range strings.Split(taskfile, "\n") {
		if line != "" && line[0] != ' ' && line[0] != '#' {
			inTasks = strings.TrimSpace(line) == "tasks:"
			continue
		}
		if m := taskfileTarget.FindStringSubmatch(line); inTasks && m != nil {
			targets[m[1]] = true
		}
	}
	return targets
}

// header judges a page's front matter and its references, returning how many
// references it records.
func (c *docChecker) header(page, text string) int {
	lines := strings.Split(text, "\n")
	if lines[0] != "---" {
		c.add("HEADER_MISSING", page, "no ADR-0013 front matter (references, last_verified)")
		return 0
	}
	end := closingLine(lines)
	if end < 0 {
		c.add("HEADER_MALFORMED", page, "front matter not closed by ---")
		return 0
	}
	refs, problem := parseHeader(lines[1:end])
	if problem != "" {
		c.add("HEADER_MALFORMED", page, "%s", problem)
		return 0
	}
	seen := map[string]bool{}
	for _, r := range refs {
		if path.IsAbs(r.path) || path.Clean(r.path) != r.path || r.path == ".." || strings.HasPrefix(r.path, "../") || r.path == ".git" || strings.HasPrefix(r.path, ".git/") || seen[r.path] {
			c.add("REFERENCE_PATH", page, "%s is not a distinct clean path inside the repository", r.path)
			continue
		}
		seen[r.path] = true
		full := filepath.Join(c.root, filepath.FromSlash(r.path))
		if !c.inside(filepath.Dir(full)) {
			c.add("REFERENCE_PATH", page, "%s is reached through a link leaving the repository", r.path)
			continue
		}
		blob, ok := blobID(full)
		switch {
		case !ok:
			c.add("REFERENCE_GONE", page, "%s is no longer a file; re-check the page and its references", r.path)
		case blob != r.blob:
			c.add("REFERENCE_STALE", page, "%s changed (recorded %s, now %s); re-check the page, then update its blob id and last_verified", r.path, r.blob, blob)
		}
	}
	return len(refs)
}

// closingLine is the index of the line closing front matter that opens at
// lines[0], or -1 when it is never closed.
func closingLine(lines []string) int {
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			return i
		}
	}
	return -1
}

// inside reports whether dir, symbolic links resolved, is in the repository;
// a directory that does not exist is judged later, as a gone reference.
func (c *docChecker) inside(dir string) bool {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return true
	}
	root, err := filepath.EvalSymlinks(c.root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, real)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// parseHeader reads the front matter's lines: exactly one `references:`
// (`[]`, or a list of path/blob pairs) and one `last_verified:` commit. Other
// top-level keys, with their indented lines, belong to other schemas (a known
// issue's id and verified block) and are left to them.
func parseHeader(lines []string) ([]docReference, string) {
	var refs []docReference
	var haveRefs, haveVerified bool
	for i := 0; i < len(lines); i++ {
		key, value, _ := strings.Cut(lines[i], ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case strings.TrimSpace(lines[i]) == "":
		case lines[i][0] == ' ' || lines[i][0] == '\t' || lines[i][0] == '-':
			return nil, fmt.Sprintf("unexpected line %q", lines[i])
		case key == "last_verified" && !haveVerified:
			if !fullHex.MatchString(value) {
				return nil, "last_verified is not a full commit id"
			}
			haveVerified = true
		case key == "references" && !haveRefs && value == "[]":
			haveRefs = true
		case key == "references" && !haveRefs && value == "":
			haveRefs = true
			for i+1 < len(lines) && strings.HasPrefix(lines[i+1], "  ") {
				p, ok := strings.CutPrefix(lines[i+1], "  - path: ")
				if !ok || i+2 >= len(lines) {
					return nil, fmt.Sprintf("reference entry %q is not `  - path: <path>` followed by `    blob: <id>`", lines[i+1])
				}
				b, ok := strings.CutPrefix(lines[i+2], "    blob: ")
				if !ok || !fullHex.MatchString(strings.TrimSpace(b)) {
					return nil, fmt.Sprintf("reference %s has no full blob id", strings.TrimSpace(p))
				}
				refs = append(refs, docReference{strings.TrimSpace(p), strings.TrimSpace(b)})
				i += 2
			}
			if len(refs) == 0 {
				return nil, "references lists nothing; write `references: []`"
			}
		case key == "references" || key == "last_verified":
			return nil, key + " given twice or malformed"
		default:
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
				i++
			}
		}
	}
	switch {
	case !haveRefs:
		return nil, "references missing"
	case !haveVerified:
		return nil, "last_verified missing"
	}
	return refs, ""
}

// blobID is git's blob id of the file at p (a symbolic link's blob is its
// target), and false when there is no file.
func blobID(p string) (string, bool) {
	info, err := os.Lstat(p)
	if err != nil {
		return "", false
	}
	var data []byte
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(p)
		if err != nil {
			return "", false
		}
		data = []byte(target)
	case info.Mode().IsRegular():
		if data, err = os.ReadFile(p); err != nil {
			return "", false
		}
	default:
		return "", false
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), true
}

// fenced tracks fenced code blocks: one opened by a backtick fence closes only
// on a backtick fence, one opened by tildes only on tildes, and (CommonMark)
// only on a bare fence at least as long as the opening one.
type fenced struct{ marker string }

// step reports whether line is a fence or inside a fenced block.
func (f *fenced) step(line string) bool {
	m := fence.FindStringSubmatch(line)
	switch {
	case m != nil && f.marker == "":
		f.marker = m[1]
		return true
	case m != nil && m[1][0] == f.marker[0] && len(m[1]) >= len(f.marker) && strings.TrimSpace(m[2]) == "":
		f.marker = ""
		return true
	}
	return f.marker != ""
}

// bodyRules are the body rules a page is held to besides links and anchors:
// no 32-hex id (ids) and only existing `task` targets (targets); readme marks a
// README, whose links to an absent root README are counted, not judged (a page
// under docs/ keeps judging them: review r1).
type bodyRules struct{ ids, targets, readme bool }

// body judges a page's links and anchors and, as rules say, its ids and its
// `task` invocations. Code blocks and code spans hold no links; a target in
// code is judged whatever its form, one in prose only when namespaced
// ("this task covers" is English, "task live:plan" is a command).
func (c *docChecker) body(page, text string, rules bodyRules) {
	targets := rules.targets
	var code fenced
	for n, line := range strings.Split(text, "\n") {
		if rules.ids && hexID.MatchString(line) {
			c.add("HEX_ID", page, "line %d carries a 32-hex id", n+1)
		}
		if code.step(line) {
			if targets {
				c.taskTargets(page, line, true)
			}
			continue
		}
		if targets {
			for _, span := range codeSpan.FindAllString(line, -1) {
				c.taskTargets(page, span, true)
			}
		}
		prose := codeSpan.ReplaceAllStringFunc(line, func(s string) string { return strings.Repeat(" ", len(s)) })
		if targets {
			c.taskTargets(page, prose, false)
		}
		for _, m := range inlineLink.FindAllStringSubmatch(prose, -1) {
			c.link(page, m[1], rules.readme)
		}
		if m := refLink.FindStringSubmatch(prose); m != nil {
			c.link(page, m[1], rules.readme)
		}
	}
}

func (c *docChecker) taskTargets(page, text string, code bool) {
	for _, m := range taskUse.FindAllStringSubmatchIndex(text, -1) {
		target := text[m[2]:m[3]]
		if rest := strings.TrimPrefix(text[m[3]:], ":"); rest != "" && strings.ContainsRune("<{*", rune(rest[0])) {
			continue
		}
		if (code || strings.Contains(target, ":")) && !c.targets[target] {
			c.add("TASK_TARGET", page, "%s is not a target in Taskfile.yml", target)
		}
	}
}

// link judges one link target of page: external schemes are not followed; a
// relative path must exist inside the repository (a leading / is the
// repository root), and a fragment on a Markdown page must be one of its
// anchors.
func (c *docChecker) link(page, target string, readme bool) {
	if scheme.MatchString(target) {
		return
	}
	raw, fragment, _ := strings.Cut(target, "#")
	if decoded, err := url.PathUnescape(fragment); err == nil {
		fragment = decoded
	}
	p, err := url.PathUnescape(raw)
	if err != nil {
		c.add("LINK_BROKEN", page, "%s does not decode", target)
		return
	}
	resolved := page
	switch {
	case p == "":
	case strings.HasPrefix(p, "/"):
		resolved = path.Clean(strings.TrimPrefix(p, "/"))
	default:
		resolved = path.Join(path.Dir(page), p)
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		c.add("LINK_BROKEN", page, "%s leaves the repository", target)
		return
	}
	if readme && resolved == "README.md" && !c.rootReadme {
		c.unjudgedRoot++ // the root README is absent (the entry's snapshot): counted, not judged
		return
	}
	full := filepath.Join(c.root, filepath.FromSlash(resolved))
	if !c.inside(full) {
		c.add("LINK_BROKEN", page, "%s leaves the repository through a symbolic link", target)
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		c.add("LINK_BROKEN", page, "%s does not exist", target)
		return
	}
	if fragment == "" || info.IsDir() || !strings.HasSuffix(resolved, ".md") {
		return
	}
	if !c.anchorsOf(resolved)[fragment] {
		c.add("ANCHOR_MISSING", page, "%s: no such heading or anchor", target)
	}
}

// anchorsOf is the set of anchors GitHub gives a Markdown file: one per
// heading outside code blocks (repeats numbered -1, -2, … as GitHub does) and every
// explicit <a id|name>.
func (c *docChecker) anchorsOf(page string) map[string]bool {
	if a, ok := c.anchors[page]; ok {
		return a
	}
	a := map[string]bool{}
	c.anchors[page] = a
	data, err := os.ReadFile(filepath.Join(c.root, filepath.FromSlash(page)))
	if err != nil {
		return a
	}
	counts := map[string]int{}
	var code fenced
	for _, line := range strings.Split(string(data), "\n") {
		if code.step(line) {
			continue
		}
		for _, m := range htmlAnchor.FindAllStringSubmatch(line, -1) {
			a[m[1]] = true
		}
		if m := heading.FindStringSubmatch(line); m != nil {
			// As github-slugger: a slug already given, by a heading or by an
			// earlier numbering, takes the next free -n suffix.
			s := slug(m[1])
			candidate := s
			if _, taken := counts[candidate]; taken {
				for {
					counts[s]++
					candidate = fmt.Sprintf("%s-%d", s, counts[s])
					if _, taken := counts[candidate]; !taken {
						break
					}
				}
			}
			counts[candidate] = 0
			a[candidate] = true
		}
	}
	return a
}

// slug is GitHub's heading anchor: the heading's text lower-cased, with
// everything but letters, digits, marks, '_' and '-' removed and each space
// turned into '-'.
func slug(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(headingLink.ReplaceAllString(h, "$1")) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

// 006 FR-018, the version gate. A page under docs/ outside the ADRs and the
// known-issue records may carry `verified_against: {tool: "major.minor", …}`
// (data-model *Docs front matter*), an inline mapping with quoted versions
// (unquoted, YAML reads 1.10 as 1.1); any other form is HEADER_MALFORMED.
// Each tool is compared with its pin: more than two minor versions behind,
// an older major or a version ahead of the pin is VERIFIED_AGAINST_STALE
// (ahead names a version the repository does not run); a tool with no pin
// is VERIFIED_AGAINST_UNKNOWN_TOOL. A page under docs/how-to/ or
// docs/reference/ naming a tofu, terramate, task or ovhcloud command without
// the mapping is VERIFIED_AGAINST_MISSING. A tool the page names but the
// mapping omits is not judged (FR-018 asks for the mapping, not for a
// complete one).

const knownIssuesDir = "docs/known-issues/"

// docPin is a tool's pinned version and where it was read.
type docPin struct{ version, source string }

// miseTools says where a verified_against key is pinned: a mise file's
// [tools] key. ovh is pinned by the provider lock files (lockTrees).
var miseTools = map[string]struct{ file, key string }{
	"tofu":      {"mise.toml", "opentofu"},
	"terramate": {"mise.toml", "aqua:terramate-io/terramate"},
	"task":      {"mise.toml", "task"},
	"tflint":    {"mise.toml", "tflint"},
	"go":        {"mise.toml", "go"},
	"ovhcloud":  {"mise.live.toml", "github:ovh/ovhcloud-cli"},
}

// lockTrees hold the stage, module and component lock files whose ovh/ovh
// version pins ovh (stacks/ holds copies of the stage locks, tests/ fixtures).
var lockTrees = []string{"stages", "modules", "components"}

var (
	// tomlTools is the [tools] table header, with an optional comment.
	tomlTools = regexp.MustCompile(`^\[\s*tools\s*\]\s*(?:#.*)?$`)
	// tomlString is a `key = "value"` line: the key bare, "quoted" or
	// 'literal', the value a string without escapes, an optional comment.
	tomlString = regexp.MustCompile(`^(?:"([^"\\]*)"|'([^']*)'|([A-Za-z0-9_-]+))\s*=\s*(?:"([^"\\]*)"|'([^']*)')\s*(?:#.*)?$`)
	// docLockProvider is one provider block of a lock file.
	docLockProvider = regexp.MustCompile(`(?ms)^provider\s+"([^"]+)"\s*\{(.*?)^\}`)
	docLockVersion  = regexp.MustCompile(`(?m)^\s*version\s*=\s*"([^"]+)"`)
	// version is major.minor with an optional patch.
	version = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?$`)
	// stampEntry is one `tool: "major.minor"` entry of verified_against.
	stampEntry = regexp.MustCompile(`^([a-z][a-z0-9_-]*)\s*:\s*(?:"([^"]*)"|'([^']*)')$`)
	// toolUse is a tool word followed by a flag (task --list), a subcommand
	// or a target.
	toolUse = regexp.MustCompile(`\b(tofu|terramate|task|ovhcloud)[ \t]+(--?[A-Za-z][A-Za-z0-9-]*|[a-z][a-z0-9_-]*(?::[a-z0-9_-]+)*)`)
)

// loadPins reads the pins of every miseTools key and of ovh. A missing pin
// file pins nothing; an unreadable one is a finding.
func (c *docChecker) loadPins() {
	c.pins = map[string]docPin{}
	for _, file := range []string{"mise.toml", "mise.live.toml"} {
		data, err := os.ReadFile(filepath.Join(c.root, file))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			c.add("INPUT_UNREADABLE", file, "%v", err)
			continue
		}
		tools := tomlToolsTable(string(data))
		for name, at := range miseTools {
			if v, ok := tools[at.key]; ok && at.file == file && version.MatchString(v) {
				c.pins[name] = docPin{v, file}
			}
		}
	}
	for _, tree := range lockTrees {
		if err := c.lockPins(tree); err != nil {
			c.add("INPUT_UNREADABLE", tree, "%v", err)
		}
	}
}

// tomlToolsTable is the [tools] table of a mise file: its `key = "value"`
// lines, other tables skipped. A line it cannot read pins nothing, so the
// tool reads as unpinned (fail closed).
func tomlToolsTable(text string) map[string]string {
	tools := map[string]string{}
	inTools := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") {
			inTools = tomlTools.MatchString(line)
			continue
		}
		if m := tomlString.FindStringSubmatch(line); inTools && m != nil {
			tools[m[1]+m[2]+m[3]] = m[4] + m[5]
		}
	}
	return tools
}

// lockPins raises the ovh pin to the highest ovh/ovh version among the lock
// files under tree (outside hidden and fixtures directories): lock files that
// disagree pin the newest, so no page is judged against an older lock.
func (c *docChecker) lockPins(tree string) error {
	dir := filepath.Join(c.root, tree)
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (strings.HasPrefix(d.Name(), ".") || d.Name() == "fixtures"):
			return filepath.SkipDir
		case d.Name() != ".terraform.lock.hcl" || !d.Type().IsRegular():
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range docLockProvider.FindAllStringSubmatch(string(data), -1) {
			v := docLockVersion.FindStringSubmatch(m[2])
			if strings.HasSuffix(m[1], "/ovh/ovh") && v != nil && version.MatchString(v[1]) && newer(v[1], c.pins["ovh"].version) {
				c.pins["ovh"] = docPin{v[1], "the ovh/ovh provider lock files"}
			}
		}
		return nil
	})
}

// versionParts is a version's major, minor and patch (0 when absent); ok is
// false when v is not a version.
func versionParts(v string) (parts [3]int, ok bool) {
	m := version.FindStringSubmatch(v)
	if m == nil {
		return parts, false
	}
	for i, s := range m[1:] {
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

// newer reports whether v is a higher version than than, which may be empty.
func newer(v, than string) bool {
	a, _ := versionParts(v)
	b, ok := versionParts(than)
	if !ok {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// versionGate judges one page's verified_against.
func (c *docChecker) versionGate(page, text string) {
	if strings.HasPrefix(page, adrDir) || strings.HasPrefix(page, knownIssuesDir) {
		return
	}
	stamp, present, problem := stampOf(text)
	switch {
	case problem != "":
		c.add("HEADER_MALFORMED", page, "%s", problem)
	case present:
		c.judgeStamp(page, stamp)
	case stampRequired(page):
		if command := toolCommand(text, c.targets); command != "" {
			c.add("VERIFIED_AGAINST_MISSING", page, "names the command %q but has no verified_against", command)
		}
	}
}

// stampRequired reports whether page must carry verified_against when it
// names a tool command.
func stampRequired(page string) bool {
	return strings.HasPrefix(page, "docs/how-to/") || strings.HasPrefix(page, "docs/reference/")
}

// stampOf reads the top-level verified_against key of a page's front matter:
// the mapping, whether the key is present, and a problem when it is
// malformed (given twice, block form, unquoted or not major.minor).
func stampOf(text string) (stamp map[string]string, present bool, problem string) {
	lines := strings.Split(text, "\n")
	end := closingLine(lines)
	if lines[0] != "---" || end < 0 {
		return nil, false, ""
	}
	for _, line := range lines[1:end] {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimRight(key, " \t") != "verified_against" {
			continue
		}
		if present {
			return nil, true, "verified_against given twice"
		}
		present = true
		if stamp, problem = parseStamp(strings.TrimSpace(value)); problem != "" {
			return nil, true, problem
		}
	}
	return stamp, present, ""
}

// parseStamp reads an inline mapping {tool: "major.minor", …}.
func parseStamp(value string) (map[string]string, string) {
	const form = "verified_against is not an inline mapping {tool: \"major.minor\", …}"
	inner, opened := strings.CutPrefix(value, "{")
	inner, closed := strings.CutSuffix(inner, "}")
	if !opened || !closed || strings.TrimSpace(inner) == "" {
		return nil, form
	}
	stamp := map[string]string{}
	for _, entry := range strings.Split(inner, ",") {
		m := stampEntry.FindStringSubmatch(strings.TrimSpace(entry))
		if m == nil || !version.MatchString(m[2]+m[3]) {
			return nil, fmt.Sprintf("%s: %q", form, strings.TrimSpace(entry))
		}
		if _, seen := stamp[m[1]]; seen {
			return nil, fmt.Sprintf("verified_against names %s twice", m[1])
		}
		stamp[m[1]] = m[2] + m[3]
	}
	return stamp, ""
}

// judgeStamp compares each stamped tool with its pin, in name order.
func (c *docChecker) judgeStamp(page string, stamp map[string]string) {
	tools := make([]string, 0, len(stamp))
	for tool := range stamp {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	for _, tool := range tools {
		pin, ok := c.pins[tool]
		if !ok {
			c.add("VERIFIED_AGAINST_UNKNOWN_TOOL", page, "%s has no pin in mise.toml, mise.live.toml or the ovh/ovh provider lock files", tool)
			continue
		}
		if why := staleness(stamp[tool], pin.version); why != "" {
			c.add("VERIFIED_AGAINST_STALE", page, "%s %q is %s its pin %s (%s); re-check the page, then update verified_against", tool, stamp[tool], why, pin.version, pin.source)
		}
	}
}

// staleness says how a stamped version fails its pin, or "" when it is at
// the pin or at most two minor versions behind it (ADR-0013).
func staleness(stamp, pin string) string {
	s, _ := versionParts(stamp)
	p, _ := versionParts(pin)
	switch {
	case newer(stamp, pin):
		return "ahead of"
	case s[0] < p[0]:
		return "a major version behind"
	case p[1]-s[1] > 2:
		return "more than two minor versions behind"
	}
	return ""
}

// toolCommand is the first tofu, terramate, task or ovhcloud command a page
// names, or "". In code (block or span) a tool word followed by a flag or a
// word is a command; in prose `task` is one only with a flag or a namespaced
// or existing target ("this task has" is English, as for TASK_TARGET), the
// other tools with any following word ("run tofu plan").
func toolCommand(text string, targets map[string]bool) string {
	var code fenced
	for _, line := range strings.Split(text, "\n") {
		if code.step(line) {
			if m := toolUse.FindString(line); m != "" {
				return m
			}
			continue
		}
		if m := lineCommand(line, targets); m != "" {
			return m
		}
	}
	return ""
}

// lineCommand is the first command a line outside code blocks names, in a
// code span or, under toolCommand's rule, in prose.
func lineCommand(line string, targets map[string]bool) string {
	for _, span := range codeSpan.FindAllString(line, -1) {
		if m := toolUse.FindString(span); m != "" {
			return m
		}
	}
	for _, m := range toolUse.FindAllStringSubmatch(codeSpan.ReplaceAllString(line, " "), -1) {
		if m[1] != "task" || strings.HasPrefix(m[2], "-") || strings.Contains(m[2], ":") || targets[m[2]] {
			return m[0]
		}
	}
	return ""
}

// 006 FR-018, the superseded-ADR scan. An ADR is Superseded when the first
// `- Status:` line of docs/adr/NNNN-*.md starts so (the ADR template,
// docs/adr/README.md); an ADR without a status line is not. Every regular
// text file outside docs/adr/ and tools/vendor/ that names `ADR-NNNN` or
// links `adr/NNNN-` for such an ADR is SUPERSEDED_ADR, once per file and
// ADR. "Tracked" is read without git (the entry's snapshot carries no .git):
// .git and what the root .gitignore names by a plain or wildcard name
// (`agent-context/`, `.terraform/`, `*.tfstate`) are skipped; a pattern with
// an inner slash or a negation is not read, and `name/*` skips all of name/.
// A file holding a NUL byte is binary and skipped.

var (
	adrStatus = regexp.MustCompile(`^- Status:\s*(.*)$`)
	adrCite   = regexp.MustCompile(`(?:\bADR-|\badr/)(\d{4})(?:[^0-9]|$)`)
)

// ignoreRule is one .gitignore pattern the scan honours.
type ignoreRule struct {
	pattern           string
	dirOnly, anchored bool
}

// supersededScan reports every citation of a Superseded ADR.
func (c *docChecker) supersededScan() {
	withdrawn, err := supersededADRs(c.root)
	if err != nil {
		c.add("INPUT_UNREADABLE", "docs/adr", "%v", err)
		return
	}
	ignored, err := ignoreRules(c.root)
	if err != nil {
		c.add("INPUT_UNREADABLE", ".gitignore", "%v", err)
		return
	}
	err = filepath.WalkDir(c.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return c.scanEntry(p, d, withdrawn, ignored)
	})
	if err != nil {
		c.add("INPUT_UNREADABLE", ".", "%v", err)
	}
}

// scanEntry skips docs/adr/, tools/vendor/ and ignored directories, and
// judges a regular file that is not ignored, except the generated CI run
// record: docs:check is a foundation target, so reading it would put the
// record into the digest each recorded run must match (ci:foundation).
func (c *docChecker) scanEntry(p string, d fs.DirEntry, withdrawn map[string]bool, ignored []ignoreRule) error {
	rel, err := filepath.Rel(c.root, p)
	if err != nil || rel == "." {
		return err
	}
	rel = filepath.ToSlash(rel)
	if d.IsDir() && (rel == "docs/adr" || rel == "tools/vendor" || ignoredBy(ignored, rel, true)) {
		return filepath.SkipDir
	}
	if d.IsDir() || !d.Type().IsRegular() || rel == checks.FoundationRunsPath || ignoredBy(ignored, rel, false) {
		return nil
	}
	return c.citations(p, rel, withdrawn)
}

// supersededADRs are the numbers of the ADRs whose status is Superseded.
func supersededADRs(root string) (map[string]bool, error) {
	dir := filepath.Join(root, "docs", "adr")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	withdrawn := map[string]bool{}
	for _, e := range entries {
		m := adrFile.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(adrStatusOf(string(data)), "Superseded") {
			withdrawn[m[1]] = true
		}
	}
	return withdrawn, nil
}

// adrStatusOf is the value of an ADR's first status line, or "".
func adrStatusOf(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if m := adrStatus.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// ignoreRules reads the root .gitignore's plain and wildcard name patterns;
// .git is always skipped. A missing .gitignore (the entry's snapshot) skips
// nothing else.
func ignoreRules(root string) ([]ignoreRule, error) {
	rules := []ignoreRule{{pattern: ".git"}}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if errors.Is(err, fs.ErrNotExist) {
		return rules, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if r, ok := ignoreRuleOf(strings.TrimSpace(line)); ok {
			rules = append(rules, r)
		}
	}
	return rules, nil
}

// ignoreRuleOf reads one .gitignore line, false for a comment, a negation or
// a pattern with an inner slash.
func ignoreRuleOf(line string) (ignoreRule, bool) {
	if line == "" || line[0] == '#' || line[0] == '!' {
		return ignoreRule{}, false
	}
	r := ignoreRule{}
	line, r.dirOnly = strings.CutSuffix(line, "/")
	if trimmed, wild := strings.CutSuffix(line, "/*"); wild {
		line, r.dirOnly = trimmed, true
	}
	line, r.anchored = strings.CutPrefix(line, "/")
	r.pattern = line
	return r, line != "" && !strings.Contains(line, "/")
}

// ignoredBy reports whether a rule skips rel (a directory when dir).
func ignoredBy(rules []ignoreRule, rel string, dir bool) bool {
	name := path.Base(rel)
	for _, r := range rules {
		if r.dirOnly && !dir || r.anchored && strings.Contains(rel, "/") {
			continue
		}
		if ok, _ := path.Match(r.pattern, name); ok {
			return true
		}
	}
	return false
}

// citations adds a finding for each Superseded ADR the file names or links.
func (c *docChecker) citations(file, rel string, withdrawn map[string]bool) error {
	if len(withdrawn) == 0 {
		return nil
	}
	data, err := os.ReadFile(file)
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		return err
	}
	seen := map[string]bool{}
	for _, m := range adrCite.FindAllSubmatchIndex(data, -1) {
		n := string(data[m[2]:m[3]])
		if withdrawn[n] && !seen[n] {
			seen[n] = true
			line := bytes.Count(data[:m[2]], []byte("\n")) + 1
			c.add("SUPERSEDED_ADR", rel, "line %d cites ADR-%s, whose status is Superseded; cite the ADR that replaced it", line, n)
		}
	}
	return nil
}

// 006 FR-019, the repository map. Map paths are the code spans of each table
// row's first cell in docs/reference/repository-map.md (a row may be indented
// up to three spaces; descriptions, prose and fenced blocks are not read). A
// map path that is not a clean relative path is MAP_PATH_GONE. The offline
// entry sees only the top-level names its snapshot admits
// (checks.EntryAdmitted), and under them only regular files and directories,
// none through a link or a name it skips (checks.EntrySkipped): a map path
// under an admitted name must be such an entry (MAP_PATH_GONE); a path under
// any other name is counted in MAP_UNJUDGED, present or not. Every admitted top-level directory present in
// the checkout needs a row of its own; a subpath row does not map it, and an
// absent map omits them all (MAP_MISSING_DIR).

const mapPage = "docs/reference/repository-map.md"

var mapSpan = regexp.MustCompile("`([^`]+)`")

// repositoryMap judges the map and returns how many map paths it skipped.
func (c *docChecker) repositoryMap() int {
	text, err := regularText(filepath.Join(c.root, mapPage))
	if err != nil {
		c.add("INPUT_UNREADABLE", mapPage, "%v", err)
		return 0
	}
	admitted := map[string]bool{}
	for _, name := range checks.EntryAdmitted() {
		admitted[name] = true
	}
	unjudged, mapped := 0, map[string]bool{}
	for _, p := range mapPaths(text) {
		top, _, _ := strings.Cut(p, "/")
		switch {
		case !cleanMapPath(p):
			c.add("MAP_PATH_GONE", mapPage, "%s is not a clean relative path; name the path itself", p)
		case !admitted[top]:
			unjudged++
		case !entryVisible(c.root, p):
			c.add("MAP_PATH_GONE", mapPage, "%s is not inside the entry (gone, behind a link, or skipped by the snapshot); remove or correct its row", p)
		case p == top:
			mapped[top] = true
		}
	}
	c.unmappedDirs(admitted, mapped)
	return unjudged
}

// unmappedDirs reports every admitted top-level directory without its own row.
func (c *docChecker) unmappedDirs(admitted, mapped map[string]bool) {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		c.add("INPUT_UNREADABLE", ".", "%v", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() && admitted[e.Name()] && !mapped[e.Name()] {
			c.add("MAP_MISSING_DIR", mapPage, "%s/ is an admitted top-level directory without a row of its own", e.Name())
		}
	}
}

// regularText is the content of a regular file, "" when it is absent or not a
// regular file (the pages walk skips a linked page the same way).
func regularText(file string) (string, error) {
	info, err := os.Lstat(file)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(file)
	return string(data), err
}

// mapPaths are the code spans of the first cell of each table body row (the
// rows after the delimiter row), a trailing / removed; a row may be indented up
// to three spaces and needs no leading pipe, and rows inside fenced blocks are
// examples, not read.
func mapPaths(text string) []string {
	var paths []string
	var code fenced
	table := false
	for _, line := range strings.Split(text, "\n") {
		row := strings.TrimPrefix(line, strings.Repeat(" ", min(3, len(line)-len(strings.TrimLeft(line, " ")))))
		piped := strings.Contains(row, "|") && !strings.HasPrefix(row, " ")
		switch {
		case code.step(line) || !piped:
			table = false
		case tableDelimiter.MatchString(row):
			table = true
		case table:
			paths = append(paths, cellPaths(row)...)
		}
	}
	return paths
}

// tableDelimiter is a table's delimiter row (| --- | :-: |); the rows after it,
// up to a line without a pipe, are the table's body (review round 2: a body
// row needs no leading pipe).
var tableDelimiter = regexp.MustCompile(`^\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)

// cellPaths are the code spans of a body row's first cell, a trailing /
// removed.
func cellPaths(row string) []string {
	first, _, _ := strings.Cut(strings.TrimPrefix(row, "|"), "|")
	var paths []string
	for _, m := range mapSpan.FindAllStringSubmatch(first, -1) {
		paths = append(paths, strings.TrimSuffix(m[1], "/"))
	}
	return paths
}

// cleanMapPath says whether p is a clean path relative to the repository.
func cleanMapPath(p string) bool {
	return !path.IsAbs(p) && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}

// entryVisible says whether every component of the clean path p is a
// directory, or a regular file at its end, none a symbolic link and none a
// name the snapshot skips: what the entry's snapshot copies.
func entryVisible(root, p string) bool {
	cur := root
	for _, part := range strings.Split(p, "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil || checks.EntrySkipped(part) || !info.IsDir() && !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}
