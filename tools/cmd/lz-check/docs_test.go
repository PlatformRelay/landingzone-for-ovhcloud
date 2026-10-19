package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// 006 T039 (FR-018): the fixture's tool pins, in the shapes and keys of the
// repository's own mise.toml, mise.live.toml and provider lock files. The
// versions differ from the repository's on purpose, so a gate that compares
// with constants instead of reading the pins fails. Each lock (one stage, one
// module) names another provider first, so a gate must read the ovh/ovh
// block; an [env] key named like a tool is not a pin.
const (
	fixtureMise = "# Toolchain identities.\n[tools]\ngo = \"1.27.1\"\nopentofu = \"1.15.2\"\n" +
		"\"aqua:terramate-io/terramate\" = \"0.19.1\"\ntask = \"3.56.0\"\ntflint = \"0.66.0\"\n\n" +
		"[env]\nopentofu = \"1.2.0\"\n"
	fixtureMiseLive = "# Host-only live tools.\n[tools]\n\"github:ovh/ovhcloud-cli\" = \"0.18.0\"\n"
	fixtureOvhBlock = "provider \"registry.opentofu.org/ovh/ovh\" {\n  version     = \"2.24.0\"\n  constraints = \"~> 2.24\"\n  hashes = [\n    \"h1:z26YaKCDJJThNY9i+hCIq7YSiWiTc8vsC0RhX0hRwMc=\",\n  ]\n}\n"
	fixtureLock     = "# Exact Linux amd64 package hashes from the official provider registry.\n" +
		"provider \"registry.opentofu.org/hashicorp/random\" {\n  version     = \"3.7.2\"\n  constraints = \"~> 3.7\"\n  hashes = [\n    \"h1:z26YaKCDJJThNY9i+hCIq7YSiWiTc8vsC0RhX0hRwMc=\",\n  ]\n}\n\n" + fixtureOvhBlock
)

// fixtureStamp is a verified_against line in data-model's form (minor precision).
func fixtureStamp(versions string) string { return "verified_against: {" + versions + "}\n" }

// pageA is the passing how-to: it names `task` commands, so it carries a
// verified_against for task at its pin.
var pageA = header(helloRef+fixtureStamp(`task: "3.56"`)) + guide

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
		"mise.toml":                              fixtureMise,
		"mise.live.toml":                         fixtureMiseLive,
		"stages/demo/.terraform.lock.hcl":        fixtureLock,
		"modules/x/.terraform.lock.hcl":          fixtureLock,
		"tools/hello.txt":                        "hello\n",
		"docs/how-to/a.md":                       pageA,
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

// docsFindingLine reports whether out holds a finding line of rule on page
// whose detail contains want (the detail's wording is the check's own).
func docsFindingLine(out, rule, page, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, rule+" "+page+":") && strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// docsCommandPage is a headed page with the given extra header lines and body.
func docsCommandPage(extra, body string) string {
	return header("references: []\n"+extra) + "# Steps\n\n" + body + "\n"
}

// docsStampedGuide is the passing how-to with the given verified_against.
func docsStampedGuide(versions string) string {
	return header(helloRef+fixtureStamp(versions)) + guide
}

// 006 T039, FR-018 (ADR-0013 "more than two minor versions behind"): a page
// at its pins, or two minors behind them, passes; pages outside how-to and
// reference need no stamp, and known-issue records are excepted.
func TestDocsVerifiedAgainstAccepted(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"every tool at its pin": {"docs/how-to/a.md": docsStampedGuide(
			`task: "3.56", tofu: "1.15", ovh: "2.24", terramate: "0.19", go: "1.27", tflint: "0.66", ovhcloud: "0.18"`)},
		"every tool two minors behind": {"docs/how-to/a.md": docsStampedGuide(
			`task: "3.54", tofu: "1.13", ovh: "2.22", terramate: "0.17", go: "1.25", tflint: "0.64", ovhcloud: "0.16"`)},
		"reference page stamped":          {"docs/reference/c.md": docsCommandPage(fixtureStamp(`tofu: "1.13"`), "Run `tofu plan`.")},
		"explanation page needs no stamp": {"docs/explanation/c.md": docsCommandPage("", "Run `tofu plan`, then `ovhcloud cloud project list`.")},
		"how-to without a command":        {"docs/how-to/c.md": docsCommandPage("", "OpenTofu and Terramate are pinned in mise.toml.")},
		"known issue excepted": {"docs/known-issues/KI-009-x.md": docsCommandPage("id: KI-009\n"+fixtureStamp(`tofu: "1.2", packer: "1.0"`),
			"Seen with `tofu plan`.")},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(docsRepo(t, files), "docs")
			if code != 0 || !strings.Contains(out, "DOCS_OK") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 0 with DOCS_OK (FR-018: two minors behind passes):\n%s", code, out)
			}
		})
	}
}

// 006 T039, FR-018: three minors behind a pin is stale, a tool with no pin is
// unknown, and a how-to or reference page naming a tofu, terramate, task or
// ovhcloud command without verified_against is unstamped. Each row breaks one
// clause of the passing tree.
func TestDocsVerifiedAgainstRejected(t *testing.T) {
	const a, c = "docs/how-to/a.md", "docs/how-to/c.md"
	stale, unknown, missing := "VERIFIED_AGAINST_STALE", "VERIFIED_AGAINST_UNKNOWN_TOOL", "VERIFIED_AGAINST_MISSING"
	noTflint := strings.Replace(fixtureMise, "tflint = \"0.66.0\"\n", "", 1)
	envOnlyTofu := strings.Replace(fixtureMise, "opentofu = \"1.15.2\"\n", "", 1)
	raisedTofu := strings.Replace(fixtureMise, "\"1.15.2\"", "\"1.16.0\"", 1)
	raisedOvh := strings.ReplaceAll(fixtureOvhBlock, "2.24", "2.25")
	for name, r := range map[string]struct {
		files              map[string]string
		rule, page, detail string
	}{
		"tofu three behind":            {map[string]string{a: docsStampedGuide(`task: "3.56", tofu: "1.12"`)}, stale, a, "tofu"},
		"task three behind":            {map[string]string{a: docsStampedGuide(`task: "3.53"`)}, stale, a, "task"},
		"ovh three behind its lock":    {map[string]string{a: docsStampedGuide(`task: "3.56", ovh: "2.21"`)}, stale, a, "ovh"},
		"terramate three behind":       {map[string]string{a: docsStampedGuide(`task: "3.56", terramate: "0.16"`)}, stale, a, "terramate"},
		"go three behind":              {map[string]string{a: docsStampedGuide(`task: "3.56", go: "1.24"`)}, stale, a, "go"},
		"tflint three behind":          {map[string]string{a: docsStampedGuide(`task: "3.56", tflint: "0.63"`)}, stale, a, "tflint"},
		"ovhcloud three behind":        {map[string]string{a: docsStampedGuide(`task: "3.56", ovhcloud: "0.15"`)}, stale, a, "ovhcloud"},
		"minor compared as a number":   {map[string]string{a: docsStampedGuide(`task: "3.56", tofu: "1.9"`)}, stale, a, "tofu"},
		"tofu pin raised":              {map[string]string{"mise.toml": raisedTofu, a: docsStampedGuide(`task: "3.56", tofu: "1.13"`)}, stale, a, "tofu"},
		"ovh lock raised":              {map[string]string{"stages/demo/.terraform.lock.hcl": raisedOvh, "modules/x/.terraform.lock.hcl": raisedOvh, a: docsStampedGuide(`task: "3.56", ovh: "2.22"`)}, stale, a, "ovh"},
		"reference page stale":         {map[string]string{"docs/reference/c.md": docsCommandPage(fixtureStamp(`tofu: "1.12"`), "Run `tofu plan`.")}, stale, "docs/reference/c.md", "tofu"},
		"explanation page stale":       {map[string]string{"docs/explanation/c.md": docsCommandPage(fixtureStamp(`tofu: "1.12"`), "Run `tofu plan`.")}, stale, "docs/explanation/c.md", "tofu"},
		"tool never pinned":            {map[string]string{a: docsStampedGuide(`task: "3.56", packer: "1.10"`)}, unknown, a, "packer"},
		"pin removed from mise.toml":   {map[string]string{"mise.toml": noTflint, a: docsStampedGuide(`task: "3.56", tflint: "0.66"`)}, unknown, a, "tflint"},
		"env key is not a pin":         {map[string]string{"mise.toml": envOnlyTofu, a: docsStampedGuide(`task: "3.56", tofu: "1.15"`)}, unknown, a, "tofu"},
		"live pins absent":             {map[string]string{"mise.live.toml": "", a: docsStampedGuide(`task: "3.56", ovhcloud: "0.18"`)}, unknown, a, "ovhcloud"},
		"no ovh lock":                  {map[string]string{"stages/demo/.terraform.lock.hcl": "", "modules/x/.terraform.lock.hcl": "", a: docsStampedGuide(`task: "3.56", ovh: "2.24"`)}, unknown, a, "ovh"},
		"how-to names tofu":            {map[string]string{c: docsCommandPage("", "Run `tofu plan`.")}, missing, c, ""},
		"how-to names terramate":       {map[string]string{c: docsCommandPage("", "Run `terramate list`.")}, missing, c, ""},
		"how-to names task":            {map[string]string{c: docsCommandPage("", "Run `task docs:check`.")}, missing, c, ""},
		"how-to names ovhcloud":        {map[string]string{c: docsCommandPage("", "Run `ovhcloud cloud project list`.")}, missing, c, ""},
		"how-to names tofu in a block": {map[string]string{c: docsCommandPage("", "```sh\ntofu init\n```")}, missing, c, ""},
		"how-to names tofu in prose":   {map[string]string{c: docsCommandPage("", "Run tofu plan, then read the summary.")}, missing, c, ""},
		"reference page names tofu":    {map[string]string{"docs/reference/c.md": docsCommandPage("", "Run `tofu plan`.")}, missing, "docs/reference/c.md", ""},
		"guide unstamped":              {map[string]string{a: header(helloRef) + guide}, missing, a, ""},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(docsRepo(t, r.files), "docs")
			if code != 1 || !docsFindingLine(out, r.rule, r.page, r.detail) || !strings.Contains(out, "DOCS_FAIL") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 1 with %s %s naming %q and DOCS_FAIL:\n%s", code, r.rule, r.page, r.detail, out)
			}
		})
	}
}

// docsSupersededADRs are an ADR whose status is Superseded and an accepted one
// whose body says "superseded" (as ADR-0004's does): the status line decides.
var docsSupersededADRs = map[string]string{
	"docs/adr/0007-old-choice.md": "# ADR-0007: Old choice\n- Status: Superseded by ADR-0013\n- Date: 2026-09-01\n\n## Context\nWithdrawn.\n",
	"docs/adr/0004-kept.md":       "# ADR-0004: Kept\n- Status: Accepted\n- Date: 2026-10-06\n\n## Context\nAn apply-time check aborts on a superseded input.\n",
}

func docsSupersededRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	all := map[string]string{}
	for name, content := range docsSupersededADRs {
		all[name] = content
	}
	for name, content := range files {
		all[name] = content
	}
	return docsRepo(t, all)
}

// 006 T039, FR-018: a file outside docs/adr/ and tools/vendor/ that names or
// links a Superseded ADR fails; the ADRs themselves, vendored code and
// citations of an accepted ADR pass.
func TestDocsSupersededADR(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"nothing cites it": nil,
		"adrs cite it": {
			"docs/adr/README.md":    "# ADRs\n\n- [ADR-0007](0007-old-choice.md): superseded by ADR-0013\n",
			"docs/adr/0013-docs.md": "# ADR-0013\n\nSupersedes ADR-0007.\n",
		},
		"vendored code cites it": {"tools/vendor/example.com/lib/lib.go": "// Package lib, see ADR-0007.\npackage lib\n"},
		"accepted adr cited":     {"docs/how-to/a.md": pageA + "See ADR-0004 and [it](../adr/0004-kept.md).\n"},
	} {
		t.Run("accepted/"+name, func(t *testing.T) {
			code, out := lzCheck(docsSupersededRepo(t, files), "docs")
			if code != 0 || !strings.Contains(out, "DOCS_OK") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 0 with DOCS_OK:\n%s", code, out)
			}
		})
	}
	ki := "docs/known-issues/KI-009-x.md"
	for name, r := range map[string]struct {
		files map[string]string
		page  string
	}{
		"page names it":        {map[string]string{"docs/how-to/a.md": pageA + "See ADR-0007.\n"}, "docs/how-to/a.md"},
		"page links it":        {map[string]string{"docs/how-to/a.md": pageA + "See [the old choice](../adr/0007-old-choice.md).\n"}, "docs/how-to/a.md"},
		"known issue names it": {map[string]string{ki: docsCommandPage("id: KI-009\n", "See ADR-0007.")}, ki},
		"readme names it":      {map[string]string{"modules/x/README.md": moduleReadme + "Decided in ADR-0007.\n"}, "modules/x/README.md"},
		"source names it":      {map[string]string{"tools/x/x.go": "// Package x follows ADR-0007.\npackage x\n"}, "tools/x/x.go"},
		"spec names it":        {map[string]string{"specs/001-x/notes.md": "As ADR-0007 decided.\n"}, "specs/001-x/notes.md"},
		"taskfile names it":    {map[string]string{"Taskfile.yml": docsTaskfile + "# ADR-0007\n"}, "Taskfile.yml"},
	} {
		t.Run("refused/"+name, func(t *testing.T) {
			code, out := lzCheck(docsSupersededRepo(t, r.files), "docs")
			if code != 1 || !docsFindingLine(out, "SUPERSEDED_ADR", r.page, "0007") || !strings.Contains(out, "DOCS_FAIL") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 1 with SUPERSEDED_ADR %s naming 0007 and DOCS_FAIL:\n%s", code, r.page, out)
			}
		})
	}
}

// 006 T040, FR-019: the repository map in the shape of the real
// docs/reference/repository-map.md: front matter, then tables whose first
// column names paths in code spans (two in one cell where the real map
// groups them), descriptions holding code spans that are not paths, and a
// closing prose line naming `.local/`. Map paths are the first column.
const docsMapPage = "docs/reference/repository-map.md"

// The map's three tables. The fixture checkout (docsRepo plus .tflint.hcl)
// has the admitted top-level directories .specify, docs, modules, specs,
// stages, tests and tools, each mapped, and .local, which is not admitted;
// .gitignore and .sonarcloud.properties lie outside the entry's admitted
// names, sit in different tables, and are absent from the fixture as they are
// inside the entry.
const (
	docsMapDeployed = "| `modules/` | Modules, layer 1. |\n| `modules/x/` | One module and its lock. |\n| `stages/` | Stages, layer 3 (`bootstrap`). |\n"
	docsMapVerified = "| `tools/` | One Go module (`go.mod`). |\n| `tools/hello.txt` | A referenced file. |\n| `tests/` | Fixtures for the checks. |\n" +
		"| `Taskfile.yml` | Every check and operation. |\n| `mise.toml`, `mise.live.toml` | Pinned tool versions. |\n" +
		"| `.tflint.hcl`, `.sonarcloud.properties` | Linter and code-analysis configuration. |\n"
	docsMapRecord = "| `docs/` | Design and user documentation. |\n| `docs/reference/` | Look-up pages. |\n| `specs/` | The delivery record. |\n| `.specify/` | Spec Kit templates. |\n" +
		"| `README.md` | The front page. |\n| `.gitignore` | Keeps local files out of version control. |\n"
)

// docsMap is a map page holding the given tables.
func docsMap(tables ...string) string {
	page := header("references: []\n") + "# Repository map\n\nEvery top-level entry and its main subdirectories, one line each.\n"
	for i, rows := range tables {
		page += "\n## Part " + string(rune('A'+i)) + "\n\n| Path | What it holds |\n| --- | --- |\n" + rows
	}
	return page + "\nNot in the repository: `.local/` (built binaries and live run records).\n"
}

// docsMapRepo is docsRepo with a map of the given tables, .tflint.hcl, and
// files replaced or added ("" removes one).
func docsMapRepo(t *testing.T, files map[string]string, tables ...string) string {
	t.Helper()
	all := map[string]string{docsMapPage: docsMap(tables...), ".tflint.hcl": "config {}\n"}
	for name, content := range files {
		all[name] = content
	}
	return docsRepo(t, all)
}

// docsUnjudged reports whether out carries the count MAP_UNJUDGED=<n> as a
// word of its own.
func docsUnjudged(out string, n int) bool {
	return regexp.MustCompile(`(?m)(^|\s)MAP_UNJUDGED=` + strconv.Itoa(n) + `(\s|$)`).MatchString(out)
}

// 006 T040, FR-019: a map whose paths under the entry's admitted top-level
// names exist and which lists every admitted top-level directory passes;
// map paths outside the admitted names are skipped, whether or not they exist
// on the host, and counted, never failed (case 3).
func TestDocsRepositoryMapAccepted(t *testing.T) {
	all := []string{docsMapDeployed, docsMapVerified, docsMapRecord}
	onlyAdmitted := []string{docsMapDeployed,
		strings.Replace(docsMapVerified, ", `.sonarcloud.properties`", "", 1),
		strings.Replace(docsMapRecord, "| `.gitignore` | Keeps local files out of version control. |\n", "", 1)}
	unadmitted := []string{docsMapDeployed, docsMapVerified, docsMapRecord + "| `agent-context/` | Local coordination. |\n| `.local/bin/` | Built binaries. |\n"}
	inbox := map[string]string{"agent-context/INBOX.md": "# Inbox\n"}
	for name, r := range map[string]struct {
		files    map[string]string
		tables   []string
		unjudged int
	}{
		"hidden files unjudged":         {nil, all, 2},
		"hidden files on the host":      {map[string]string{".gitignore": ".local/\n", ".sonarcloud.properties": "sonar.projectKey=x\n"}, all, 2},
		"only admitted paths":           {nil, onlyAdmitted, 0},
		"unadmitted directories named":  {inbox, unadmitted, 4},
		"unadmitted directory unmapped": {inbox, all, 2},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(docsMapRepo(t, r.files, r.tables...), "docs")
			if code != 0 || !strings.Contains(out, "DOCS_OK") || !docsUnjudged(out, r.unjudged) {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 0 with DOCS_OK and MAP_UNJUDGED=%d (FR-019: paths outside the admitted names are counted, never failed):\n%s", code, r.unjudged, out)
			}
		})
	}
}

// 006 T040, FR-019: a map naming a path under an admitted top-level name that
// does not exist fails with MAP_PATH_GONE (case 1), hidden admitted names
// (.github, .tflint.hcl) included; an admitted top-level directory present in
// the checkout and absent from the map fails with MAP_MISSING_DIR (case 2).
// The unjudged paths of the base map never appear in a finding.
func TestDocsRepositoryMapRejected(t *testing.T) {
	gone, missing := "MAP_PATH_GONE", "MAP_MISSING_DIR"
	withRow := func(i int, row string) []string {
		tables := []string{docsMapDeployed, docsMapVerified, docsMapRecord}
		tables[i] += row
		return tables
	}
	without := func(i int, rows string) []string {
		tables := []string{docsMapDeployed, docsMapVerified, docsMapRecord}
		tables[i] = strings.Replace(tables[i], rows, "", 1)
		return tables
	}
	firstRow := []string{"| `components/state-backend/` | A component since removed. |\n" + docsMapDeployed, docsMapVerified, docsMapRecord}
	for name, r := range map[string]struct {
		files        map[string]string
		tables       []string
		rule, detail string
	}{
		"deleted path under a directory":     {nil, withRow(1, "| `tools/cmd/lz-gone/` | A command since removed. |\n"), gone, "tools/cmd/lz-gone"},
		"deleted path in the first row":      {nil, firstRow, gone, "components/state-backend"},
		"deleted admitted directory":         {nil, withRow(2, "| `catalog/` | A catalogue never built. |\n"), gone, "catalog"},
		"deleted admitted file":              {nil, withRow(0, "| `terramate.tm.hcl` | The Terramate project root. |\n"), gone, "terramate.tm.hcl"},
		"deleted file under .github":         {nil, withRow(1, "| `.github/workflows/foundation.yml` | The one CI workflow. |\n"), gone, ".github/workflows/foundation.yml"},
		"deleted hidden admitted file":       {map[string]string{".tflint.hcl": ""}, withRow(0, ""), gone, ".tflint.hcl"},
		"deleted second path of a cell":      {map[string]string{"mise.live.toml": ""}, withRow(0, ""), gone, "mise.live.toml"},
		"directory unmapped":                 {nil, without(0, "| `modules/` | Modules, layer 1. |\n| `modules/x/` | One module and its lock. |\n"), missing, "modules"},
		"hidden admitted directory unmapped": {nil, without(2, "| `.specify/` | Spec Kit templates. |\n"), missing, ".specify"},
		"new admitted directory unmapped":    {map[string]string{"components/state-backend/main.tf": "# a component\n"}, withRow(0, ""), missing, "components"},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(docsMapRepo(t, r.files, r.tables...), "docs")
			if code != 1 || !docsFindingLine(out, r.rule, docsMapPage, r.detail) || !strings.Contains(out, "DOCS_FAIL") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 1 with %s %s naming %q and DOCS_FAIL:\n%s", code, r.rule, docsMapPage, r.detail, out)
			}
			for _, unjudged := range []string{".gitignore", ".sonarcloud.properties"} {
				if docsFindingLine(out, gone, docsMapPage, unjudged) || docsFindingLine(out, missing, docsMapPage, unjudged) {
					t.Errorf("BEHAVIORAL_RED: %s is outside the admitted names and must stay unjudged (FR-019):\n%s", unjudged, out)
				}
			}
		})
	}
}
