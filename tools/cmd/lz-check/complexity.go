package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The cognitive complexity of every function in the tools module, judged
// against tools/harness/complexity-baseline.txt, a baseline that may only
// shrink (006 T057, FR-012). A function over complexityLimit that the
// baseline does not list, or a listed function whose score rose, is a
// finding; a lower score or a deleted function passes and asks for the
// baseline to be rewritten downwards.
//
// The score is gocognit v1.2.1's (github.com/uudashr/gocognit, gocognit.go;
// its README states the rules, after G. Ann Campbell, "Cognitive Complexity",
// SonarSource, 2017), ported because the offline runtime carries no gocognit:
//   - +1 plus the nesting level for if, switch, type switch, select, for and
//     range;
//   - +1 for else if and for else, without the nesting level; an else block's
//     statements stay at the if statement's level;
//   - the nesting level rises inside those bodies and inside function literals;
//   - +1 for goto, and for break or continue with a label;
//   - +1 for each run of like operators in a chain of && and ||, a parenthesis
//     ending the chain;
//   - +1 for a direct recursive call by plain name (the same ast.Object).
//
// Every .go file under tools/ counts, tests and build-tagged files included,
// except under tools/vendor and file names starting with "." or "_", which
// the go tool never compiles. Directories named testdata or starting with
// "." or "_" are scored: ./... skips them, but an import can still compile a
// package there. A link (to a file or a directory) is an input
// error. Unlike gocognit, //gocognit:ignore is not honoured: the baseline is
// the only exception list. The command lets a stale line pass; the repository's
// own test (TestComplexityRealTree) wants none, so the change that lowers a
// score rewrites its line too.

const (
	complexityLimit    = 14
	complexityBaseline = "tools/harness/complexity-baseline.txt"
)

// baselineFunc is a function name as funcName prints it: Name, (T).Name or
// (*T).Name.
var baselineFunc = regexp.MustCompile(`^(\(\*?[\p{L}_][\p{L}\p{N}_]*\)\.)?[\p{L}_][\p{L}\p{N}_]*$`)

func complexity(out io.Writer, root string) int {
	listed, err := readBaseline(root)
	if err != nil {
		fmt.Fprintln(out, err)
		return 2
	}
	scores, err := treeScores(root)
	if err != nil {
		fmt.Fprintln(out, err)
		return 2
	}
	t := judgeComplexity(out, listed, scores)
	if t.lower+t.gone > 0 {
		fmt.Fprintf(out, "rewrite %s downwards: set each COMPLEXITY_LOWER line to its score (remove it at %d or under) and remove each COMPLEXITY_GONE line\n", complexityBaseline, complexityLimit)
	}
	if t.findings > 0 {
		fmt.Fprintf(out, "COMPLEXITY_FAIL findings=%d\n", t.findings)
		return 1
	}
	fmt.Fprintf(out, "COMPLEXITY_OK over=%d listed=%d lower=%d gone=%d\n", t.over, len(listed), t.lower, t.gone)
	return 0
}

type complexityTally struct{ over, lower, gone, findings int }

// judgeComplexity prints one line per function whose score differs from its
// baseline line, keyed "<file> <function>".
func judgeComplexity(out io.Writer, listed, scores map[string]int) complexityTally {
	var t complexityTally
	for _, key := range slices.Sorted(maps.Keys(scores)) {
		score, base := scores[key], listed[key]
		switch {
		case score > complexityLimit && base == 0:
			fmt.Fprintf(out, "COMPLEXITY_NEW %d %s\n", score, key)
			t.findings++
		case base > 0 && score > base:
			fmt.Fprintf(out, "COMPLEXITY_ROSE %d %s baseline=%d\n", score, key, base)
			t.findings++
		case score < base:
			fmt.Fprintf(out, "COMPLEXITY_LOWER %d %s baseline=%d\n", score, key, base)
			t.lower++
		}
		if score > complexityLimit {
			t.over++
		}
	}
	for _, key := range slices.Sorted(maps.Keys(listed)) {
		if _, ok := scores[key]; !ok {
			fmt.Fprintf(out, "COMPLEXITY_GONE %s baseline=%d\n", key, listed[key])
			t.gone++
		}
	}
	return t
}

// readBaseline reads "<score> <file> <function>" lines, sorted by file and
// function as one string, each over complexityLimit; blank lines and lines
// starting with # are skipped.
func readBaseline(root string) (map[string]int, error) {
	data, err := os.ReadFile(filepath.Join(root, complexityBaseline))
	if err != nil {
		return nil, err
	}
	listed := map[string]int{}
	last := ""
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, score, err := baselineEntry(line)
		if err == nil && key <= last {
			err = errors.New("not after the line before it (sorted by file, then function, no repeats)")
		}
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", complexityBaseline, i+1, err)
		}
		listed[key], last = score, key
	}
	return listed, nil
}

func baselineEntry(line string) (string, int, error) {
	f := strings.Split(line, " ")
	if len(f) != 3 {
		return "", 0, errors.New("want <score> <file> <function>, one space apart")
	}
	score, err := strconv.Atoi(f[0])
	if err != nil || score <= complexityLimit {
		return "", 0, fmt.Errorf("score %q is not a number over %d", f[0], complexityLimit)
	}
	if path.Clean(f[1]) != f[1] || !strings.HasPrefix(f[1], "tools/") || !strings.HasSuffix(f[1], ".go") {
		return "", 0, fmt.Errorf("file %q is not a clean .go path under tools/", f[1])
	}
	if !baselineFunc.MatchString(f[2]) {
		return "", 0, fmt.Errorf("function %q is not a name as gocognit prints it", f[2])
	}
	return f[1] + " " + f[2], score, nil
}

// treeScores scores every function of the tools module's Go files, keyed
// "<file> <function>".
func treeScores(root string) (map[string]int, error) {
	scores := map[string]int{}
	err := filepath.WalkDir(filepath.Join(root, "tools"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir() && skippedDir(rel):
			return filepath.SkipDir
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			// A link to a directory would hide a package the build can
			// still import; refuse every link rather than follow or skip it.
			return fmt.Errorf("%s: not a regular file or directory", rel)
		case !strings.HasSuffix(d.Name(), ".go") || ignoredName(d.Name()):
			return nil
		}
		return fileScores(p, rel, scores)
	})
	return scores, err
}

func ignoredName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func skippedDir(rel string) bool { return rel == "tools/vendor" }

// fileScores adds one file's functions to scores. Two functions may share a
// key only as init or _, and then neither may be over the limit.
func fileScores(p, rel string, scores map[string]int) error {
	file, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		key, score := rel+" "+funcName(fn), cognitive(fn)
		if prev, dup := scores[key]; dup && max(prev, score) > complexityLimit {
			return fmt.Errorf("%s: two functions of this name, one over %d", key, complexityLimit)
		}
		scores[key] = max(scores[key], score)
	}
	return nil
}

// funcName is gocognit's name: "(T).Name" or "(*T).Name" for a method, type
// parameters dropped, else "Name".
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || fn.Recv.NumFields() == 0 {
		return fn.Name.Name
	}
	return "(" + receiverName(fn.Recv.List[0].Type) + ")." + fn.Name.Name
}

func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	}
	return "BADRECV"
}

// cognitive is gocognit v1.2.1's score of one function declaration.
func cognitive(fn *ast.FuncDecl) int {
	v := &cognitiveVisitor{name: fn.Name, elseIfs: map[ast.Node]bool{}, counted: map[ast.Expr]bool{}}
	ast.Walk(v, fn)
	return v.score
}

type cognitiveVisitor struct {
	name           *ast.Ident
	score, nesting int
	elseIfs        map[ast.Node]bool // if statements that are an else if
	counted        map[ast.Expr]bool // operands of a && / || chain already scored
}

func (v *cognitiveVisitor) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.IfStmt:
		v.ifStmt(n)
	case *ast.SwitchStmt:
		v.structure(n.Body, n.Init, n.Tag)
	case *ast.TypeSwitchStmt:
		v.structure(n.Body, n.Init, n.Assign)
	case *ast.SelectStmt:
		v.structure(n.Body)
	case *ast.ForStmt:
		v.structure(n.Body, n.Init, n.Cond, n.Post)
	case *ast.RangeStmt:
		v.structure(n.Body, n.Key, n.Value, n.X)
	case *ast.FuncLit:
		ast.Walk(v, n.Type)
		v.nested(n.Body)
	default:
		v.increment(n)
		return v
	}
	return nil
}

// structure scores a nesting structure and walks its header, then its body
// one level deeper.
func (v *cognitiveVisitor) structure(body *ast.BlockStmt, header ...ast.Node) {
	v.score += v.nesting + 1
	for _, h := range header {
		if h != nil {
			ast.Walk(v, h)
		}
	}
	v.nested(body)
}

func (v *cognitiveVisitor) nested(body *ast.BlockStmt) {
	v.nesting++
	ast.Walk(v, body)
	v.nesting--
}

func (v *cognitiveVisitor) ifStmt(n *ast.IfStmt) {
	if v.elseIfs[n] {
		v.score++
	} else {
		v.score += v.nesting + 1
	}
	if n.Init != nil {
		ast.Walk(v, n.Init)
	}
	ast.Walk(v, n.Cond)
	v.nested(n.Body)
	switch e := n.Else.(type) {
	case *ast.BlockStmt:
		v.score++
		ast.Walk(v, e)
	case *ast.IfStmt:
		v.elseIfs[e] = true
		ast.Walk(v, e)
	}
}

// increment scores the nodes that add without nesting: labelled jumps,
// logical operator chains and direct recursion.
func (v *cognitiveVisitor) increment(n ast.Node) {
	switch n := n.(type) {
	case *ast.BranchStmt:
		if n.Label != nil {
			v.score++
		}
	case *ast.BinaryExpr:
		if logicalOp(n.Op) && !v.counted[n] {
			v.chain(n)
		}
	case *ast.CallExpr:
		if id, ok := n.Fun.(*ast.Ident); ok && id.Obj == v.name.Obj && id.Name == v.name.Name {
			v.score++
		}
	}
}

// chain scores one run of like operators at a time in a && / || chain.
func (v *cognitiveVisitor) chain(n *ast.BinaryExpr) {
	last := token.ILLEGAL
	for _, op := range v.logicalOps(n, nil) {
		if op != last {
			v.score++
			last = op
		}
	}
}

// logicalOps lists a chain's && and || operators left to right, through
// binary expressions only, and marks each operand as counted.
func (v *cognitiveVisitor) logicalOps(e ast.Expr, ops []token.Token) []token.Token {
	v.counted[e] = true
	b, ok := e.(*ast.BinaryExpr)
	if !ok {
		return ops
	}
	ops = v.logicalOps(b.X, ops)
	if logicalOp(b.Op) {
		ops = append(ops, b.Op)
	}
	return v.logicalOps(b.Y, ops)
}

func logicalOp(op token.Token) bool { return op == token.LAND || op == token.LOR }
