package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// The documentation rules of ADR-0013 that hold without a site build: every
// page under docs/ except the ADRs (history, written before the rule) carries
// front matter naming the repository files it describes with their git blob
// ids and the commit it was last checked against; a page is stale once a
// referenced file's blob differs or the file is gone. On every page, ADRs
// included, relative links and anchors resolve and no 32-hex id (an OVHcloud
// project or account id) appears; outside the ADRs, which name targets that
// were designed but not built, every `task <target>` named exists in
// Taskfile.yml.
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
	c := &docChecker{root: root, targets: taskfileTargets(string(taskfile)), anchors: map[string]map[string]bool{}}
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
		c.body(page, text, !adr)
	}
	if len(pages) == 0 {
		c.add("NO_PAGES", "docs", "no Markdown page under docs/")
	}
	for _, f := range c.findings {
		fmt.Fprintf(out, "%s %s: %s\n", f.rule, f.page, f.detail)
	}
	if len(c.findings) > 0 {
		fmt.Fprintf(out, "DOCS_FAIL findings=%d\n", len(c.findings))
		return 1
	}
	fmt.Fprintf(out, "DOCS_OK pages=%d headed=%d references=%d\n", len(pages), headed, references)
	return 0
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
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
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

// body judges a page's links, anchors, ids and, when targets is set, its
// `task` invocations. Code blocks and code spans hold no links; a target in
// code is judged whatever its form, one in prose only when namespaced
// ("this task covers" is English, "task live:plan" is a command).
func (c *docChecker) body(page, text string, targets bool) {
	var code fenced
	for n, line := range strings.Split(text, "\n") {
		if hexID.MatchString(line) {
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
			c.link(page, m[1])
		}
		if m := refLink.FindStringSubmatch(prose); m != nil {
			c.link(page, m[1])
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
func (c *docChecker) link(page, target string) {
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
