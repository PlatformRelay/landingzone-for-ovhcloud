package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scoreSource exercises each rule of gocognit v1.2.1. The expected scores
// below are what golangci-lint v2.13.1 (gocognit v1.2.1) reports for this
// source as a module of its own (evidence/T057.md), not hand-derived.
const scoreSource = `package x

func call() {}

func straight() { call() }

func ifOnly(a bool) {
	if a {
		call()
	}
}

func ifElse(a bool) {
	if a {
		call()
	} else {
		call()
	}
}

func elseIfChain(a, b bool) {
	if a {
		call()
	} else if b {
		call()
	} else {
		call()
	}
}

func nestedIf(a, b bool) {
	if a {
		if b {
			call()
		}
	}
}

func nestedElseIf(a, b, c bool) {
	if a {
		if b {
			call()
		} else if c {
			call()
		}
	}
}

func elseBlockNotNested(a int) {
	if a > 0 {
		call()
	} else if b := a; b < 0 {
		call()
	} else {
		if a == 0 {
			call()
		}
	}
}

func loops(xs []int) {
	for i := 0; i < 3; i++ {
		for range xs {
			if i > 1 {
				call()
			}
		}
	}
}

func headers(a, b bool) {
	for a && b {
		call()
	}
	switch c := a || b; c {
	case true:
		call()
	}
}

func switches(v any, n int) {
	switch n {
	case 1:
		if n > 0 {
			call()
		}
	}
	switch v.(type) {
	case int:
		call()
	}
}

func selects(c chan int) {
	for {
		select {
		case <-c:
			return
		}
	}
}

func literal() {
	f := func(a bool) {
		if a {
			call()
		}
	}
	f(true)
}

func labelled(n int) {
outer:
	for {
		for {
			if n > 0 {
				continue outer
			}
			break outer
		}
	}
	goto end
end:
	call()
}

func boolSeq(a, b, c, d bool) bool { return a && b && c || d }

func boolMixed(a, b, c bool) bool { return a && b || c && a }

func boolParen(a, b, c bool) bool { return a && (b || c) }

func boolNot(a, b, c bool) bool { return !(a && b) && c }

func boolInIf(a, b bool) {
	if a && b {
		call()
	}
}

func boolCompare(a, b, c bool) bool { return (a && b) == c }

func recursive(n int) int {
	if n == 0 {
		return 0
	}
	return recursive(n - 1)
}

func shadowed() {
	shadowed := func() {}
	shadowed()
}

func generic[V any](v V) {
	generic[V](v)
	generic(v)
}

type T struct{}

func (T) method(n int) {
	if n > 0 {
		T{}.method(n - 1)
	}
}

func (t *T) pointer(a bool) {
	if a {
		call()
	}
}

type G[V any] struct{}

func (g G[V]) typed(a bool) {
	if a {
		call()
	}
}

type M[K comparable, V any] struct{}

func (m *M[K, V]) listed(a bool) {
	if a {
		call()
	}
}

func pick(a bool, xs []int) []int { return xs }

func next(a bool) int { return 1 }

func moreHeaders(a, b bool, xs []int) {
	for _, x := range pick(a && b, xs) {
		_ = x
	}
	for i := 0; i < 1; i = next(a || b) {
		call()
	}
}
`

var wantScores = map[string]int{
	"call": 0, "straight": 0, "ifOnly": 1, "ifElse": 2, "elseIfChain": 3, "nestedIf": 3,
	"nestedElseIf": 4, "elseBlockNotNested": 4, "loops": 6, "headers": 4, "switches": 4, "selects": 3,
	"literal": 2, "labelled": 9, "boolSeq": 2, "boolMixed": 3, "boolParen": 2, "boolNot": 2,
	"boolInIf": 2, "boolCompare": 1, "recursive": 2, "shadowed": 0, "generic": 1,
	"(T).method": 1, "(*T).pointer": 1, "(G).typed": 1, "(*M).listed": 1,
	"pick": 0, "next": 0, "moreHeaders": 4,
}

// Each rule of gocognit v1.2.1 scores as the pinned linter does, and names
// carry the receiver as gocognit prints it.
func TestCognitiveScores(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", scoreSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			got[funcName(fn)] = cognitive(fn)
		}
	}
	for name, want := range wantScores {
		if score, ok := got[name]; !ok || score != want {
			t.Errorf("BEHAVIORAL_RED: %s scored %d (found %v), want %d", name, score, ok, want)
		}
	}
	if len(got) != len(wantScores) {
		t.Errorf("BEHAVIORAL_RED: scored %d functions, want %d: %v", len(got), len(wantScores), got)
	}
}

// scored is a function of cognitive complexity n: n ifs in sequence.
func scored(name string, n int) string {
	return "func " + name + "(a bool) {\n" + strings.Repeat("\tif a {\n\t\tprintln()\n\t}\n", n) + "}\n"
}

func goFile(funcs ...string) string {
	return "package a\n\n" + strings.Join(funcs, "\n")
}

const (
	baselinePath = "tools/harness/complexity-baseline.txt"
	rewriteHint  = "rewrite " + baselinePath + " downwards"
)

func baselineOf(lines ...string) string {
	return "# header\n\n" + strings.Join(lines, "\n") + "\n"
}

// Unchanged, lower, dropped and deleted functions pass; a lower score says to
// rewrite the line downwards, a deleted one that its line may go.
func TestComplexityAccepted(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		want  []string
	}{
		"unchanged": {map[string]string{"tools/a/a.go": goFile(scored("f", 16), scored("g", 3)), baselinePath: baselineOf("16 tools/a/a.go f")},
			[]string{"COMPLEXITY_OK over=1 listed=1 lower=0 gone=0"}},
		"two-listed": {map[string]string{"tools/a/a.go": goFile(scored("f", 16), scored("g", 20)), "tools/b/b_test.go": goFile(scored("h", 15)), baselinePath: baselineOf("16 tools/a/a.go f", "20 tools/a/a.go g", "15 tools/b/b_test.go h")},
			[]string{"COMPLEXITY_OK over=3 listed=3 lower=0 gone=0"}},
		"at-the-limit": {map[string]string{"tools/a/a.go": goFile(scored("f", 14)), baselinePath: baselineOf()},
			[]string{"COMPLEXITY_OK over=0 listed=0 lower=0 gone=0"}},
		"lower": {map[string]string{"tools/a/a.go": goFile(scored("f", 15)), baselinePath: baselineOf("17 tools/a/a.go f")},
			[]string{rewriteHint, "COMPLEXITY_LOWER 15 tools/a/a.go f baseline=17", "COMPLEXITY_OK over=1 listed=1 lower=1 gone=0"}},
		"dropped-under": {map[string]string{"tools/a/a.go": goFile(scored("f", 3)), baselinePath: baselineOf("17 tools/a/a.go f")},
			[]string{"COMPLEXITY_LOWER 3 tools/a/a.go f baseline=17", "COMPLEXITY_OK over=0 listed=1 lower=1 gone=0"}},
		"deleted": {map[string]string{"tools/a/a.go": goFile(scored("g", 2)), baselinePath: baselineOf("17 tools/a/a.go f", "16 tools/c/c.go (*T).m")},
			[]string{rewriteHint, "COMPLEXITY_GONE tools/a/a.go f baseline=17", "COMPLEXITY_GONE tools/c/c.go (*T).m baseline=16", "COMPLEXITY_OK over=0 listed=2 lower=0 gone=2"}},
		"skipped-dirs": {map[string]string{"tools/a/a.go": goFile(scored("f", 2)), "tools/vendor/v/v.go": goFile(scored("v", 20)),
			"tools/a/_x.go": goFile(scored("x", 20)), "tools/a/.y.go": goFile(scored("y", 20)), "modules/x/x.go": goFile(scored("x", 20)), "tools/a/notes.txt": scored("n", 20), baselinePath: baselineOf()},
			[]string{"COMPLEXITY_OK over=0 listed=0 lower=0 gone=0"}},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(repo(t, c.files), "complexity")
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("BEHAVIORAL_RED: %q missing:\n%s", w, out)
				}
			}
			if code != 0 || strings.Contains(out, "COMPLEXITY_NEW") || strings.Contains(out, "COMPLEXITY_ROSE") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 0 without findings:\n%s", code, out)
			}
		})
	}
}

// A new function over 14, wherever it lives, and a listed function whose
// score rose fail with the function named.
func TestComplexityRejected(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		want  []string
	}{
		"new": {map[string]string{"tools/a/a.go": goFile(scored("f", 15)), baselinePath: baselineOf()},
			[]string{"COMPLEXITY_NEW 15 tools/a/a.go f", "COMPLEXITY_FAIL findings=1"}},
		"new-in-test": {map[string]string{"tools/a/a_test.go": goFile(scored("TestF", 21)), baselinePath: baselineOf()},
			[]string{"COMPLEXITY_NEW 21 tools/a/a_test.go TestF", "COMPLEXITY_FAIL findings=1"}},
		"new-behind-build-tag": {map[string]string{"tools/a/a_test.go": "//go:build offlinetools\n\n" + goFile(scored("f", 15)), baselinePath: baselineOf()},
			[]string{"COMPLEXITY_NEW 15 tools/a/a_test.go f", "COMPLEXITY_FAIL findings=1"}},
		"rose": {map[string]string{"tools/a/a.go": goFile(scored("f", 18)), baselinePath: baselineOf("17 tools/a/a.go f")},
			[]string{"COMPLEXITY_ROSE 18 tools/a/a.go f baseline=17", "COMPLEXITY_FAIL findings=1"}},
		"moved-file": {map[string]string{"tools/b/b.go": goFile(scored("f", 17)), baselinePath: baselineOf("17 tools/a/a.go f")},
			[]string{"COMPLEXITY_NEW 17 tools/b/b.go f", "COMPLEXITY_GONE tools/a/a.go f baseline=17", "COMPLEXITY_FAIL findings=1"}},
		"new-method-beside-listed": {map[string]string{"tools/a/a.go": goFile(scored("f", 17), "type T struct{}\n", strings.Replace(scored("f", 16), "func ", "func (t *T) ", 1)), baselinePath: baselineOf("17 tools/a/a.go f")},
			[]string{"COMPLEXITY_NEW 16 tools/a/a.go (*T).f", "COMPLEXITY_FAIL findings=1"}},
		"importable-dirs": {map[string]string{"tools/a/testdata/t.go": goFile(scored("t", 20)), "tools/.hidden/h.go": goFile(scored("h", 21)), "tools/_old/o.go": goFile(scored("o", 22)), baselinePath: baselineOf()},
			[]string{"COMPLEXITY_NEW 20 tools/a/testdata/t.go t", "COMPLEXITY_NEW 21 tools/.hidden/h.go h", "COMPLEXITY_NEW 22 tools/_old/o.go o", "COMPLEXITY_FAIL findings=3"}},
		"two": {map[string]string{"tools/a/a.go": goFile(scored("f", 19), scored("g", 30)), baselinePath: baselineOf("17 tools/a/a.go f")},
			[]string{"COMPLEXITY_ROSE 19 tools/a/a.go f baseline=17", "COMPLEXITY_NEW 30 tools/a/a.go g", "COMPLEXITY_FAIL findings=2"}},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(repo(t, c.files), "complexity")
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("BEHAVIORAL_RED: %q missing:\n%s", w, out)
				}
			}
			if code != 1 || strings.Contains(out, "COMPLEXITY_OK") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 1:\n%s", code, out)
			}
		})
	}
}

// A baseline that is missing, malformed, unsorted or lists a function at or
// under the limit, and Go source that does not parse, are input errors.
func TestComplexityInputErrors(t *testing.T) {
	src := goFile(scored("f", 16), scored("g", 17))
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"no-baseline":    {map[string]string{"tools/a/a.go": src}, "complexity-baseline.txt"},
		"two-fields":     {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/a/a.go")}, "line 3"},
		"four-fields":    {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/a/a.go f x")}, "line 3"},
		"not-a-number":   {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("x16 tools/a/a.go f")}, "line 3"},
		"at-the-limit":   {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("14 tools/a/a.go f")}, "line 3"},
		"outside-tools":  {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 modules/a/a.go f")}, "line 3"},
		"not-go":         {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/a/a.txt f")}, "line 3"},
		"unclean":        {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/b/../a/a.go f")}, "line 3"},
		"unsorted":       {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("17 tools/a/a.go g", "16 tools/a/a.go f")}, "line 4"},
		"unsorted-files": {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/b/a.go f", "17 tools/a/a.go g")}, "line 4"},
		"duplicate":      {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/a/a.go f", "16 tools/a/a.go f")}, "line 4"},
		"unparsable":     {map[string]string{"tools/a/a.go": src, "tools/b/b.go": "package b\nfunc {", baselinePath: baselineOf()}, "tools/b/b.go"},
		"empty-function": {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/a/a.go ")}, "line 3"},
		"bad-function":   {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/a/a.go f-g")}, "line 3"},
		"bad-receiver":   {map[string]string{"tools/a/a.go": src, baselinePath: baselineOf("16 tools/a/a.go (T.f")}, "line 3"},
		"duplicate-func": {map[string]string{"tools/a/a.go": goFile(scored("init", 15), scored("init", 2)), baselinePath: baselineOf("15 tools/a/a.go init")}, "tools/a/a.go init"},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := lzCheck(repo(t, c.files), "complexity")
			if code != 2 || !strings.Contains(out, c.want) || strings.Contains(out, "COMPLEXITY_OK") {
				t.Errorf("BEHAVIORAL_RED: code=%d, want 2 naming %q:\n%s", code, c.want, out)
			}
		})
	}
}

// A Go file that is a link is refused rather than skipped or followed.
func TestComplexityRefusesLinkedSource(t *testing.T) {
	root := repo(t, map[string]string{"tools/a/a.go": goFile(scored("f", 2)), baselinePath: baselineOf()})
	if err := os.Symlink("a.go", filepath.Join(root, "tools/a/b.go")); err != nil {
		t.Fatal(err)
	}
	if code, out := lzCheck(root, "complexity"); code != 2 || !strings.Contains(out, "tools/a/b.go: not a regular file") {
		t.Errorf("BEHAVIORAL_RED: code=%d, want 2 refusing the link:\n%s", code, out)
	}
}

// A link to a directory is refused too: the build can import a package
// through it, so skipping it would leave its functions unscored.
func TestComplexityRefusesLinkedDirectory(t *testing.T) {
	root := repo(t, map[string]string{"tools/a/a.go": goFile(scored("f", 2)), "outside/b.go": goFile(scored("g", 30)), baselinePath: baselineOf()})
	if err := os.Symlink("../../outside", filepath.Join(root, "tools/a/b")); err != nil {
		t.Fatal(err)
	}
	if code, out := lzCheck(root, "complexity"); code != 2 || !strings.Contains(out, "tools/a/b: not a regular file or directory") {
		t.Errorf("BEHAVIORAL_RED: code=%d, want 2 refusing the directory link:\n%s", code, out)
	}
}

// The repository's own functions hold to the committed baseline, and the
// baseline holds no headroom: a lower score or a deleted function is
// rewritten in the change that made it, so the score cannot climb back.
func TestComplexityRealTree(t *testing.T) {
	if code, out := lzCheck("../../..", "complexity"); code != 0 || !strings.Contains(out, " lower=0 gone=0\n") {
		t.Errorf("BEHAVIORAL_RED: the real tree breaks its complexity baseline: code=%d\n%s", code, out)
	}
}
