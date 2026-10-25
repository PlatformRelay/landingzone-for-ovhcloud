package configure

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Ratchet of 004 T024 (c), FR-008: `stacks` and `checks` hold functions with authority (a process
// started, the environment read), and the source pins above see only direct imports. Every
// `stacks.` and `checks.` selector in the package's non-test files is held to an allow-list: the
// decoders, the error and row types the package reads, and the `Code*` and `Manifest*` constants
// stacks declares. A new selector is a reviewed change to this list, never a silent call.

const (
	selStacksPath = "github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
	selChecksPath = "github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
	selStacksDir  = "../stacks"
)

// selAllowed are the names the package may select by name, per import path.
var selAllowed = map[string][]string{
	selStacksPath: {"DecodeManifest", "Manifest", "ManifestError", "Instance"},
	selChecksPath: {"DecodeStrict"},
}

// selStacksConstants are the `Code*` and `Manifest*` constants declared in stacks' non-test files.
func selStacksConstants(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, f := range selParse(t, selStacksDir) {
		for _, decl := range f.Decls {
			if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.CONST {
				names = append(names, selConstNames(g)...)
			}
		}
	}
	return names
}

// selConstNames are the `Code*` and `Manifest*` names of one const declaration.
func selConstNames(g *ast.GenDecl) []string {
	var names []string
	for _, spec := range g.Specs {
		for _, n := range spec.(*ast.ValueSpec).Names {
			if strings.HasPrefix(n.Name, "Code") || strings.HasPrefix(n.Name, "Manifest") {
				names = append(names, n.Name)
			}
		}
	}
	return names
}

// selParse parses the non-test Go files of dir.
func selParse(t *testing.T, dir string) map[string]*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = f
	}
	return files
}

// selImports maps each file's import name of stacks or checks to its path; a dot import is
// reported, because its names cannot be told apart from the package's own.
func selImports(path string, f *ast.File) (map[string]string, []string) {
	names, uses := map[string]string{}, []string(nil)
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != selStacksPath && p != selChecksPath {
			continue
		}
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			uses = append(uses, path+": dot-imports "+p)
		}
		names[name] = p
	}
	return names, uses
}

// selUses returns every stacks or checks selector of dir's non-test files (sorted, as
// "<path>.<Name>") and each one off the allow-list or under a dot import.
func selUses(t *testing.T, dir string, constants []string) (seen, refused []string) {
	t.Helper()
	for path, f := range selParse(t, dir) {
		names, uses := selImports(path, f)
		refused = append(refused, uses...)
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				s, off := selCheck(names, sel, constants)
				seen = append(seen, s...)
				for _, o := range off {
					refused = append(refused, path+": uses "+o)
				}
			}
			return true
		})
	}
	slices.Sort(seen)
	return slices.Compact(seen), refused
}

// selCheck is one selector as "<package>.<Name>" when it selects from stacks or checks, and again
// in off when the name is not allowed.
func selCheck(names map[string]string, sel *ast.SelectorExpr, constants []string) (seen, off []string) {
	id, ok := sel.X.(*ast.Ident)
	if !ok || names[id.Name] == "" {
		return nil, nil
	}
	p, name := names[id.Name], sel.Sel.Name
	shown := p[strings.LastIndex(p, "/")+1:] + "." + name
	if slices.Contains(selAllowed[p], name) || (p == selStacksPath && slices.Contains(constants, name)) {
		return []string{shown}, nil
	}
	return []string{shown}, []string{shown}
}

// TestSelectorsHeldToAllowList (ratchet, FR-008): the package selects only allowed names of
// stacks and checks; the parse finds DecodeManifest, DecodeStrict and a Code constant (no vacuous
// pass on a parse that finds nothing). Controls in a scratch copy: stacks.ResolveTofu under another
// import name, a checks function and a dot import of stacks are each refused; an allowed selector
// and a constant are not.
func TestSelectorsHeldToAllowList(t *testing.T) {
	constants := selStacksConstants(t)
	if !slices.Contains(constants, "CodeInvalidValue") || !slices.Contains(constants, "ManifestAPIVersion") {
		t.Fatalf("stacks constants parsed: %v; want the Code and Manifest constants", constants)
	}
	seen, refused := selUses(t, ".", constants)
	for _, want := range []string{"stacks.DecodeManifest", "checks.DecodeStrict", "stacks.CodeInvalidValue"} {
		if !slices.Contains(seen, want) {
			t.Errorf("parse found %v; want %s among them", seen, want)
		}
	}
	if len(refused) > 0 {
		t.Errorf("selectors off the allow-list: %v", refused)
	}
	scratch := t.TempDir()
	for name, src := range map[string]string{
		"tofu.go":    "package configure\n\nimport st \"" + selStacksPath + "\"\n\nfunc tofu() { _, _ = st.ResolveTofu(\"\") }\n",
		"checks.go":  "package configure\n\nimport \"" + selChecksPath + "\"\n\nvar _ = checks.RunUnit\n",
		"dot.go":     "package configure\n\nimport . \"" + selStacksPath + "\"\n\nvar _ = DecodeManifest\n",
		"allowed.go": "package configure\n\nimport \"" + selStacksPath + "\"\n\nvar _, _ = stacks.DecodeManifest, stacks.CodeDuplicateVLAN\n",
	} {
		if err := os.WriteFile(filepath.Join(scratch, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, refused = selUses(t, scratch, constants)
	slices.Sort(refused)
	want := []string{
		filepath.Join(scratch, "checks.go") + ": uses checks.RunUnit",
		filepath.Join(scratch, "dot.go") + ": dot-imports " + selStacksPath,
		filepath.Join(scratch, "tofu.go") + ": uses stacks.ResolveTofu",
	}
	if !slices.Equal(refused, want) {
		t.Errorf("controls: refused %q, want %q", refused, want)
	}
}
