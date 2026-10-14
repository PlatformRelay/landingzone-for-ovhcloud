package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Git's own blob ids (git hash-object) of "hello\n" and of the empty file;
// the checker must agree with git, not with itself.
const (
	helloBlob = "ce013625030ba8dba906f756967f9e9ca394464a"
	emptyBlob = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	commit    = "3a1f06584f6a987bfd13d6a4de1168cf59b5f4fe"
)

const docsTaskfile = "version: '3'\n\ntasks:\n  docs:check:\n    cmds:\n      - 'true'\n\n  lint:\n    desc: x\n    cmds:\n      - 'true'\n"

// header is ADR-0013 front matter with the given references block.
func header(references string) string {
	return "---\n" + references + "last_verified: " + commit + "\n---\n"
}

const helloRef = "references:\n  - path: tools/hello.txt\n    blob: " + helloBlob + "\n"

// guide exercises every rule's accepted form: a reference with git's blob id,
// links to another page's anchor, a duplicate heading's anchor, a directory
// and external schemes, targets in code and in prose, and links and targets
// inside code that are not links.
const guide = "# Guide A\n\n## Steps\n\n## Steps\n\n" +
	"Run `task docs:check`, then `task lint -- modules/x`, then task docs:check in prose.\n" +
	"This task has no target and `task capture:<stage>-plan` and `task live:*` are placeholders.\n" +
	"See [B](../reference/b.md#the-b-page-1st), [same](#steps-1), [top](#guide-a), [dir](../reference/),\n" +
	"[web](https://example.com/x#y), [mail](mailto:a@b.c) and [ref].\n\n" +
	"```sh\ntask lint\n[not a link](nowhere.md)\n```\n\n" +
	"Inline `[not a link](nowhere2.md)` too.\n\n" +
	"[ref]: ../reference/b.md#the-b-page-1st\n" +
	"A [titled](../reference/b.md \"B page\") link, an [encoded](#%73teps) anchor, a [collided](../reference/b.md#x-1-1) anchor and a footnote.[^1]\n\n" +
	"[^1]: See the plan, not a path.\n"

const pageB = "# The `B` page: 1st!\n\nNo links.\n\n## X\n\n## X\n\n## X-1\n"

// T107: the root README and the directory READMEs are held to the link rules
// only: no front matter, and a 32-hex id or an unknown `task` target is not
// judged on them. READMEs under fixtures, testdata and vendor trees and in
// hidden directories are not read (their links are a fixture's or a third
// party's).
const (
	rootReadme   = "# Landing zone\n\nSee [the guide](docs/how-to/a.md#steps-1), [modules](modules/x/README.md#inputs) and [tools](tools/).\n"
	moduleReadme = "# x\n\n## Inputs\n\nBack to [the guide](../../docs/how-to/a.md#guide-a) and [B](/docs/reference/b.md#x-1).\n" +
		"Project 0123456789abcdef0123456789abcdef; run `task nope-not-judged`.\n"
	ignoredReadme = "# ignored\n\n[gone](nowhere.md) and [anchor](#nope)\n"
)

// docsRepo is a repository whose docs pass every rule, with files replaced or
// added ("" removes a file from the base).
func docsRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	base := map[string]string{
		"Taskfile.yml":                           docsTaskfile,
		"tools/hello.txt":                        "hello\n",
		"docs/how-to/a.md":                       header(helloRef) + guide,
		"docs/reference/b.md":                    header("id: KI-001\nverified:\n  at: 996b845\nreferences: []\n") + pageB,
		"docs/LICENSE":                           "not a page, no header\n",
		"docs/adr/0013-docs.md":                  "# ADR-0013\n\nHistory names `task new:module`, never built, and has no header.\n",
		"README.md":                              rootReadme,
		"modules/x/README.md":                    moduleReadme,
		"tests/check/fixtures/m/README.md":       ignoredReadme,
		"tools/internal/x/testdata/README.md":    ignoredReadme,
		"tools/vendor/example.com/lib/README.md": ignoredReadme,
		".specify/templates/README.md":           ignoredReadme,
		"modules/x/notes.md":                     ignoredReadme, // not a README: not read
	}
	var gone []string
	for name, content := range files {
		if content == "" {
			delete(base, name)
			gone = append(gone, name)
			continue
		}
		base[name] = content
	}
	root := repo(t, base)
	for _, name := range gone {
		if err := os.Remove(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return root
}

func TestDocsAccepted(t *testing.T) {
	code, out := lzCheck(docsRepo(t, nil), "docs")
	if code != 0 || !strings.Contains(out, "DOCS_OK pages=6 headed=2 references=1 readmes=2 root_readme=checked\n") {
		t.Errorf("BEHAVIORAL_RED: valid docs refused: code=%d\n%s", code, out)
	}
}

// T107: without a root README.md (the offline entry's snapshot holds none) the
// check passes and says the root README was absent, never silently.
// A link to the absent root README cannot be judged there: the line counts
// such links (unjudged_root_links), so they are never passed silently; with
// the root README present they are judged (TestDocsRejected).
func TestDocsRootReadmeAbsent(t *testing.T) {
	code, out := lzCheck(docsRepo(t, map[string]string{"README.md": ""}), "docs")
	if code != 0 || !strings.Contains(out, "DOCS_OK pages=6 headed=2 references=1 readmes=1 root_readme=absent unjudged_root_links=0\n") {
		t.Errorf("BEHAVIORAL_RED: code=%d, want 0 with readmes=1 root_readme=absent unjudged_root_links=0:\n%s", code, out)
	}
	linked := map[string]string{"README.md": "", "modules/x/README.md": moduleReadme + "[top](../../README.md#landing-zone) and [root](/README.md)\n"}
	code, out = lzCheck(docsRepo(t, linked), "docs")
	if code != 0 || !strings.Contains(out, "DOCS_OK pages=6 headed=2 references=1 readmes=1 root_readme=absent unjudged_root_links=2\n") {
		t.Errorf("BEHAVIORAL_RED: code=%d, want 0 with root_readme=absent unjudged_root_links=2:\n%s", code, out)
	}
}

// One control per rule: each breaks exactly one clause of a passing tree.
func TestDocsRejected(t *testing.T) {
	a := "docs/how-to/a.md"
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"header missing":           {map[string]string{a: guide}, "HEADER_MISSING docs/how-to/a.md"},
		"header unterminated":      {map[string]string{a: "---\nreferences: []\nlast_verified: " + commit + "\n" + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"no references":            {map[string]string{a: header("") + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"no last_verified":         {map[string]string{a: "---\nreferences: []\n---\n" + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"short last_verified":      {map[string]string{a: "---\nreferences: []\nlast_verified: 3a1f065\n---\n" + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"misindented entry":        {map[string]string{a: header("references:\n - path: tools/hello.txt\n   blob: "+helloBlob+"\n") + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"references not list":      {map[string]string{a: header("references: tools/hello.txt\n") + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"duplicate key":            {map[string]string{a: header("references: []\nreferences: []\n") + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"short blob":               {map[string]string{a: header("references:\n  - path: tools/hello.txt\n    blob: ce01362\n") + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"reference no blob":        {map[string]string{a: header("references:\n  - path: tools/hello.txt\n") + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"references empty":         {map[string]string{a: header("references:\n") + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"duplicate reference":      {map[string]string{a: header(helloRef+"  - path: tools/hello.txt\n    blob: "+helloBlob+"\n") + guide}, "REFERENCE_PATH docs/how-to/a.md"},
		"reference outside":        {map[string]string{a: header("references:\n  - path: ../hello.txt\n    blob: "+helloBlob+"\n") + guide}, "REFERENCE_PATH docs/how-to/a.md"},
		"reference absolute":       {map[string]string{a: header("references:\n  - path: /etc/hostname\n    blob: "+helloBlob+"\n") + guide}, "REFERENCE_PATH docs/how-to/a.md"},
		"reference directory":      {map[string]string{a: header("references:\n  - path: tools\n    blob: "+helloBlob+"\n") + guide}, "REFERENCE_GONE docs/how-to/a.md"},
		"reference gone":           {map[string]string{"tools/hello.txt": ""}, "REFERENCE_GONE docs/how-to/a.md: tools/hello.txt"},
		"reference stale":          {map[string]string{"tools/hello.txt": "hello, changed\n"}, "REFERENCE_STALE docs/how-to/a.md: tools/hello.txt"},
		"recorded id stale":        {map[string]string{a: header("references:\n  - path: tools/hello.txt\n    blob: "+emptyBlob+"\n") + guide}, "REFERENCE_STALE docs/how-to/a.md: tools/hello.txt"},
		"broken link":              {map[string]string{a: header(helloRef) + guide + "[gone](../reference/c.md)\n"}, "LINK_BROKEN docs/how-to/a.md: ../reference/c.md"},
		"link outside repo":        {map[string]string{a: header(helloRef) + guide + "[up](../../../x.md)\n"}, "LINK_BROKEN docs/how-to/a.md: ../../../x.md"},
		"anchor missing":           {map[string]string{a: header(helloRef) + guide + "[b](../reference/b.md#nope)\n"}, "ANCHOR_MISSING docs/how-to/a.md: ../reference/b.md#nope"},
		"own anchor missing":       {map[string]string{a: header(helloRef) + guide + "[s](#steps-2)\n"}, "ANCHOR_MISSING docs/how-to/a.md: #steps-2"},
		"ref anchor missing":       {map[string]string{a: header(helloRef) + guide + "[r2]: ../reference/b.md#nope2\n"}, "ANCHOR_MISSING docs/how-to/a.md: ../reference/b.md#nope2"},
		"adr link broken":          {map[string]string{"docs/adr/0013-docs.md": "# ADR\n\n[x](gone.md)\n"}, "LINK_BROKEN docs/adr/0013-docs.md: gone.md"},
		"target in prose":          {map[string]string{a: header(helloRef) + guide + "Then task live:nope.\n"}, "TASK_TARGET docs/how-to/a.md: live:nope"},
		"target in code span":      {map[string]string{a: header(helloRef) + guide + "Then `task nope -- x`.\n"}, "TASK_TARGET docs/how-to/a.md: nope"},
		"target in code block":     {map[string]string{a: header(helloRef) + guide + "```\ntask nope2\n```\n"}, "TASK_TARGET docs/how-to/a.md: nope2"},
		"32-hex id":                {map[string]string{"docs/reference/b.md": header("references: []\n") + pageB + "Project 0123456789abcdef0123456789abcdef.\n"}, "HEX_ID docs/reference/b.md"},
		"32-hex id in adr":         {map[string]string{"docs/adr/0013-docs.md": "# ADR\n\n0123456789abcdef0123456789abcdef\n"}, "HEX_ID docs/adr/0013-docs.md"},
		"titled link broken":       {map[string]string{a: header(helloRef) + guide + "[gone](../reference/c.md \"Title\")\n"}, "LINK_BROKEN docs/how-to/a.md: ../reference/c.md"},
		"link after fences":        {map[string]string{a: header(helloRef) + guide + "~~~\n```\n~~~\n[gone](c2.md)\n"}, "LINK_BROKEN docs/how-to/a.md: c2.md"},
		"target before colon":      {map[string]string{a: header(helloRef) + guide + "Run task live:nope: now.\n"}, "TASK_TARGET docs/how-to/a.md: live:nope"},
		"owned key spaced":         {map[string]string{a: header("references: []\nreferences : []\n") + guide}, "HEADER_MALFORMED docs/how-to/a.md"},
		"32-hex id prefixed":       {map[string]string{"docs/reference/b.md": header("references: []\n") + pageB + "project_0123456789abcdef0123456789abcdef\n"}, "HEX_ID docs/reference/b.md"},
		"32-hex id upper":          {map[string]string{"docs/reference/b.md": header("references: []\n") + pageB + "0123456789ABCDEF0123456789ABCDEF\n"}, "HEX_ID docs/reference/b.md"},
		"link after long fence":    {map[string]string{a: header(helloRef) + guide + "~~~~\n~~~\n```sh\n~~~~\n[gone](c3.md)\n"}, "LINK_BROKEN docs/how-to/a.md: c3.md"},
		"paren-titled link broken": {map[string]string{a: header(helloRef) + guide + "[gone](c4.md (Title))\n"}, "LINK_BROKEN docs/how-to/a.md: c4.md"},
		"no pages":                 {map[string]string{a: "", "docs/reference/b.md": "", "docs/adr/0013-docs.md": "", "docs/adr/0011-opentofu.md": "", "docs/adr/README.md": "", "docs/adr/template-not-an-adr.md": ""}, "NO_PAGES"},
		// T107: the link rules on the READMEs, one control per rule and README kind.
		"readme link broken":         {map[string]string{"modules/x/README.md": moduleReadme + "[gone](../y/README.md)\n"}, "LINK_BROKEN modules/x/README.md: ../y/README.md"},
		"readme link outside":        {map[string]string{"modules/x/README.md": moduleReadme + "[up](../../../x.md)\n"}, "LINK_BROKEN modules/x/README.md: ../../../x.md"},
		"readme anchor missing":      {map[string]string{"modules/x/README.md": moduleReadme + "[a](../../docs/how-to/a.md#nope)\n"}, "ANCHOR_MISSING modules/x/README.md: ../../docs/how-to/a.md#nope"},
		"readme own anchor missing":  {map[string]string{"modules/x/README.md": moduleReadme + "[o](#outputs)\n"}, "ANCHOR_MISSING modules/x/README.md: #outputs"},
		"root readme link broken":    {map[string]string{"README.md": rootReadme + "[gone](docs/gone.md)\n"}, "LINK_BROKEN README.md: docs/gone.md"},
		"root readme anchor":         {map[string]string{"README.md": rootReadme + "[m](modules/x/README.md#outputs)\n"}, "ANCHOR_MISSING README.md: modules/x/README.md#outputs"},
		"page link to absent root":   {map[string]string{"README.md": "", a: header(helloRef) + guide + "[r](../../README.md)\n"}, "LINK_BROKEN docs/how-to/a.md: ../../README.md does not exist"},
		"link to root readme judged": {map[string]string{"modules/x/README.md": moduleReadme + "[top](../../README.md#nope)\n"}, "ANCHOR_MISSING modules/x/README.md: ../../README.md#nope"},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(docsRepo(t, c.files), "docs")
			if code != 1 || !strings.Contains(out, c.want) || !strings.Contains(out, "DOCS_FAIL") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 1 with %q and DOCS_FAIL:\n%s", code, c.want, out)
			}
		})
	}
}

// T107: the committed registry's docs:check scope names every README the check
// reads in this repository, the root one included (the offline entry admits it
// since r4-e2), so editing a README selects docs:check and changes its digest.
func TestDocsRegistryScopeREADMEs(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	readmes, _, err := readmePages(root)
	if err != nil || len(readmes) == 0 {
		t.Fatalf("readmePages: %v %v", readmes, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "tools", "harness", "checks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		Checks []struct {
			ID    string   `json:"id"`
			Scope []string `json:"scope"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	var scope []string
	for _, c := range reg.Checks {
		if c.ID == "docs:check" {
			scope = c.Scope
		}
	}
	if !slices.Contains(scope, "docs") {
		t.Errorf("BEHAVIORAL_RED: docs:check scope %v lost docs", scope)
	}
	for _, p := range readmes {
		if !slices.Contains(scope, p) {
			t.Errorf("BEHAVIORAL_RED: docs:check reads %s, but its registry scope does not name it", p)
		}
	}
}

// A reference through a symbolically linked directory that leaves the
// repository is refused, though its path is lexically inside.
func TestDocsReferenceThroughSymlink(t *testing.T) {
	outside := t.TempDir()
	writeFiles(t, outside, map[string]string{"hello.txt": "hello\n"})
	root := docsRepo(t, map[string]string{"docs/how-to/a.md": header("references:\n  - path: alias/hello.txt\n    blob: "+helloBlob+"\n") + guide})
	if err := os.Symlink(outside, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	code, out := lzCheck(root, "docs")
	if code != 1 || !strings.Contains(out, "REFERENCE_PATH docs/how-to/a.md: alias/hello.txt") {
		t.Errorf("BEHAVIORAL_RED: code=%d, want 1 with REFERENCE_PATH alias/hello.txt:\n%s", code, out)
	}
}

// T107 review r2: a root README.md that is a symbolic link (here to a page
// outside the repository) is an input error, never read through.
func TestDocsRootReadmeSymlink(t *testing.T) {
	outside := t.TempDir()
	writeFiles(t, outside, map[string]string{"page.md": "# H\n\n[gone](nowhere.md)\n"})
	root := docsRepo(t, map[string]string{"README.md": ""})
	if err := os.Symlink(filepath.Join(outside, "page.md"), filepath.Join(root, "README.md")); err != nil {
		t.Fatal(err)
	}
	code, out := lzCheck(root, "docs")
	if code != 2 || !strings.Contains(out, "README_UNREADABLE: README.md is not a regular file") {
		t.Errorf("BEHAVIORAL_RED: code=%d, want 2 with README_UNREADABLE:\n%s", code, out)
	}
}

// A link through a symbolically linked directory that leaves the repository
// is broken, though the file and its anchor exist outside.
func TestDocsLinkThroughSymlink(t *testing.T) {
	outside := t.TempDir()
	writeFiles(t, outside, map[string]string{"page.md": "# H\n"})
	root := docsRepo(t, map[string]string{"docs/how-to/a.md": header(helloRef) + guide + "[out](../../alias/page.md#h)\n"})
	if err := os.Symlink(outside, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	code, out := lzCheck(root, "docs")
	if code != 1 || !strings.Contains(out, "LINK_BROKEN docs/how-to/a.md: ../../alias/page.md#h") {
		t.Errorf("BEHAVIORAL_RED: code=%d, want 1 with LINK_BROKEN ../../alias/page.md#h:\n%s", code, out)
	}
}

// A missing Taskfile or docs directory is an input error, not a finding.
func TestDocsInputErrors(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no Taskfile": {"Taskfile.yml": ""},
	} {
		t.Run(name, func(t *testing.T) {
			if code, out := lzCheck(docsRepo(t, files), "docs"); code != 2 || !strings.Contains(out, "TASKFILE_MISSING") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 2 with TASKFILE_MISSING:\n%s", code, out)
			}
		})
	}
	if code, out := lzCheck(t.TempDir(), "docs"); code != 2 || !strings.Contains(out, "TASKFILE_MISSING") {
		t.Errorf("BEHAVIORAL_RED: empty root: code=%d, want 2 with TASKFILE_MISSING:\n%s", code, out)
	}
	noDocs := t.TempDir()
	writeFiles(t, noDocs, map[string]string{"Taskfile.yml": docsTaskfile})
	if code, out := lzCheck(noDocs, "docs"); code != 2 || !strings.Contains(out, "DOCS_MISSING") {
		t.Errorf("BEHAVIORAL_RED: no docs/: code=%d, want 2 with DOCS_MISSING:\n%s", code, out)
	}
	if code, _ := lzCheck(docsRepo(t, nil), "docs", "x"); code != 2 {
		t.Errorf("BEHAVIORAL_RED: docs with an argument accepted: code=%d", code)
	}
}
