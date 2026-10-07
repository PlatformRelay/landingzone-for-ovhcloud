package stacks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Generation controls of 005 T037 (FR-002, FR-007, FR-008; ADR-0004, ADR-0007, ADR-0009;
// research R5, R15, R16, R22, R23; data-model *Derived instance fields*, *Envelope-to-input
// adapter*, *Resolved-reference input*).
//
// Generation is the repository's Terramate configuration (the root *.tm.hcl files and
// stacks/_lz/, T038) run by the pinned `terramate generate` through Generate. Every control
// copies that configuration into a scratch Terramate project root with a manifest fixture as
// stacks/deployments.yaml (fixture manifests never generate under the repository, research R16),
// reconciles the stacks with Reconcile (T036) and generates them with Generate.
//
// Expectations are hand-written in tests/fixtures/manifests/<fixture>/expected/<path minus
// "stacks/">/<file>.golden and compared as HCL (canonicalHCL): attribute order, layout and
// comments do not count, nor do a variable's or output's `description` and a validation's
// `error_message` (documentation); a module argument set to a literal null counts as absent (the
// stage variable's default applies). Everything else does: every block, label, argument and
// expression; an expression the comparison cannot render fails it (TestGenerateComparator).
//
// Readings this file pins (evidence/T037.md): the root variable names of the resolved inputs
// (`state_project_id`, `project_id`, `tenants` with the generated `lz_tenants`) and of the bound
// account directory of the local backend (`lz_account_dir`); the offline test's run `plan` with a
// fixed, non-secret passphrase; the module name of the stage call (the stage with `-` → `_`); the
// tenant-only fixture's stage source is the same relative path to stages/<stage> as in this
// repository: the tenant repository holds no stages of its own, and its stages/ is supplied from
// outside it (here a link to this repository's stages/, decision 1).

const (
	repositoryRoot = "../../.."
	// generatedHeaderLine is the first line of every file Terramate's generate_hcl writes
	// (the dependency checker classifies a directory holding it as a generated instance).
	generatedHeaderLine = "// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT"
)

// generationConfig copies the repository's generation configuration into root: the regular
// *.tm and *.tm.hcl files at the repository root and directly under stacks/, and everything under
// stacks/_lz/. It reports whether the repository has a root Terramate file.
func generationConfig(t *testing.T, root string) bool {
	t.Helper()
	copyMatching := func(dir, dstDir string) int {
		entries, err := os.ReadDir(filepath.Join(repositoryRoot, dir))
		if errors.Is(err, fs.ErrNotExist) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range entries {
			if name := e.Name(); e.Type().IsRegular() && (strings.HasSuffix(name, ".tm.hcl") || strings.HasSuffix(name, ".tm")) {
				writeFile(t, filepath.Join(root, dstDir, name), readFile(t, filepath.Join(repositoryRoot, dir, name)))
				n++
			}
		}
		return n
	}
	rootFiles := copyMatching(".", ".")
	copyMatching("stacks", "stacks")
	lz := filepath.Join(repositoryRoot, "stacks", "_lz")
	err := filepath.WalkDir(lz, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(repositoryRoot, path)
		writeFile(t, filepath.Join(root, rel), readFile(t, path))
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return rootFiles > 0
}

// generationRoot makes a scratch Terramate project root with the repository's generation
// configuration and manifest as stacks/deployments.yaml. Without a root Terramate file in the
// repository the control fails, and the root gets the P17 seed so the remaining findings show.
func generationRoot(t *testing.T, manifest []byte) string {
	t.Helper()
	root := t.TempDir()
	if !generationConfig(t, root) {
		t.Errorf("the repository has no root Terramate configuration (terramate.tm.hcl, T038)")
		writeFile(t, filepath.Join(root, "terramate.tm.hcl"), readFile(t, filepath.Join(terramateFixtureDir, "p17/root.tm.hcl.seed")))
	}
	writeFile(t, filepath.Join(root, ManifestPath), manifest)
	return root
}

// generateIn reconciles and generates root; a reconcile refusal fails the control.
func generateIn(t *testing.T, root string) error {
	t.Helper()
	if _, err := Reconcile(ReconcileOptions{Root: root, Terramate: pinnedTerramate(t)}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return Generate(GenerateOptions{Root: root, Terramate: pinnedTerramate(t)})
}

// generatedFiles returns every file under root/stacks that belongs to a stack and is not its
// stack.tm.hcl, keyed by slash-separated path relative to root/stacks: the manifest, the copied
// configuration (stacks/_lz/, files directly in stacks/) and OpenTofu working files are not.
func generatedFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	base := filepath.Join(root, "stacks")
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(base, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "_lz" || d.Name() == ".terraform" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.Contains(rel, "/") || d.Name() == "stack.tm.hcl" || d.Name() == ".terraform.lock.hcl" {
			return nil
		}
		data, err := os.ReadFile(path)
		files[rel] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// expectedFiles reads a fixture's hand-written expectations, keyed like generatedFiles.
func expectedFiles(t *testing.T, fixture string) map[string][]byte {
	t.Helper()
	base := filepath.Join(manifestFixtureDir, fixture, "expected")
	files := map[string][]byte{}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(base, path)
		if !strings.HasSuffix(rel, ".golden") {
			return fmt.Errorf("%s: an expectation file ends in .golden", rel)
		}
		files[strings.TrimSuffix(filepath.ToSlash(rel), ".golden")] = readFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("%s: no expectations", fixture)
	}
	return files
}

// compareGenerated reports every expected file that is missing or differs, and every generated
// file that is not expected.
func compareGenerated(t *testing.T, label string, want, got map[string][]byte) {
	t.Helper()
	names := make([]string, 0, len(want)+len(got))
	for n := range want {
		names = append(names, n)
	}
	for n := range got {
		if _, ok := want[n]; !ok {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	for _, n := range names {
		w, inWant := want[n]
		g, inGot := got[n]
		switch {
		case !inGot:
			t.Errorf("%s: %s was not generated", label, n)
		case !inWant:
			t.Errorf("%s: %s was generated but is not expected", label, n)
		case strings.HasSuffix(n, ".json"):
			var wv, gv any
			if err := json.Unmarshal(w, &wv); err != nil {
				t.Fatalf("%s: expectation %s: %v", label, n, err)
			}
			if err := json.Unmarshal(g, &gv); err != nil || !reflect.DeepEqual(wv, gv) {
				t.Errorf("%s: %s = %s, want %s (%v)", label, n, g, w, err)
			}
		default:
			if !bytes.HasPrefix(g, []byte(generatedHeaderLine)) {
				t.Errorf("%s: %s does not start with the Terramate header %q", label, n, generatedHeaderLine)
			}
			wc, err := canonicalHCL(w, n+".golden")
			if err != nil {
				t.Fatalf("%s: expectation %s: %v", label, n, err)
			}
			gc, err := canonicalHCL(g, n)
			if err != nil {
				t.Errorf("%s: %s: %v", label, n, err)
				continue
			}
			if wc != gc {
				t.Errorf("%s: %s differs from its expectation:\ngot:  %s\nwant: %s", label, n, gc, wc)
			}
		}
	}
}

// canonicalHCL renders a configuration file as a canonical string (see the file comment).
func canonicalHCL(src []byte, name string) (string, error) {
	f, diags := hclsyntax.ParseConfig(src, name, hcl.InitialPos)
	if diags.HasErrors() {
		return "", diags
	}
	var b strings.Builder
	canonicalBody(&b, f.Body.(*hclsyntax.Body), "")
	if s := b.String(); strings.Contains(s, "<unsupported ") {
		return "", fmt.Errorf("an expression the comparison cannot render: %s", s)
	}
	return b.String(), nil
}

// documentation names the attributes that only document, by the block type that holds them; only
// there are they left out.
var documentation = map[string]string{"variable": "description", "output": "description", "validation": "error_message"}

// canonicalBody renders a body inside a block of type parent ("" at the top level). An argument
// of a module call set to a literal null is the same as an absent one (the stage variable's
// default applies); anywhere else a literal null counts (`default = null` is not no default).
func canonicalBody(b *strings.Builder, body *hclsyntax.Body, parent string) {
	names := make([]string, 0, len(body.Attributes))
	for n, a := range body.Attributes {
		if lit, ok := a.Expr.(*hclsyntax.LiteralValueExpr); documentation[parent] == n || (parent == "module" && ok && lit.Val.IsNull()) {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(b, "%s=%s;", n, canonicalExpr(body.Attributes[n].Expr))
	}
	for _, blk := range body.Blocks {
		b.WriteString(blk.Type)
		for _, l := range blk.Labels {
			b.WriteString(" " + strconv.Quote(l))
		}
		b.WriteString("{")
		canonicalBody(b, blk.Body, blk.Type)
		b.WriteString("}")
	}
}

// operators names hclsyntax's operator singletons.
var operators = map[*hclsyntax.Operation]string{
	hclsyntax.OpLogicalOr: "||", hclsyntax.OpLogicalAnd: "&&", hclsyntax.OpLogicalNot: "!",
	hclsyntax.OpEqual: "==", hclsyntax.OpNotEqual: "!=", hclsyntax.OpGreaterThan: ">",
	hclsyntax.OpGreaterThanOrEqual: ">=", hclsyntax.OpLessThan: "<", hclsyntax.OpLessThanOrEqual: "<=",
	hclsyntax.OpAdd: "+", hclsyntax.OpSubtract: "-", hclsyntax.OpMultiply: "*", hclsyntax.OpDivide: "/",
	hclsyntax.OpModulo: "%", hclsyntax.OpNegate: "neg",
}

// canonicalExpr renders an expression independent of layout: object keys sorted, a lone
// interpolation "${x}" as x.
func canonicalExpr(e hclsyntax.Expression) string {
	switch e := e.(type) {
	case *hclsyntax.LiteralValueExpr:
		return canonicalValue(e.Val)
	case *hclsyntax.TemplateWrapExpr:
		return canonicalExpr(e.Wrapped)
	case *hclsyntax.TemplateExpr:
		if e.IsStringLiteral() {
			v, _ := e.Value(nil)
			return canonicalValue(v)
		}
		var parts []string
		for _, p := range e.Parts {
			if lit, ok := p.(*hclsyntax.LiteralValueExpr); ok && lit.Val.Type() == cty.String {
				parts = append(parts, strconv.Quote(lit.Val.AsString()))
			} else {
				parts = append(parts, "${"+canonicalExpr(p)+"}")
			}
		}
		return "tpl(" + strings.Join(parts, "+") + ")"
	case *hclsyntax.ScopeTraversalExpr:
		return canonicalTraversal(e.Traversal)
	case *hclsyntax.RelativeTraversalExpr:
		return canonicalExpr(e.Source) + canonicalTraversal(e.Traversal)
	case *hclsyntax.IndexExpr:
		return canonicalExpr(e.Collection) + "[" + canonicalExpr(e.Key) + "]"
	case *hclsyntax.ObjectConsExpr:
		items := make([]string, 0, len(e.Items))
		for _, it := range e.Items {
			items = append(items, canonicalKey(it.KeyExpr)+"="+canonicalExpr(it.ValueExpr))
		}
		sort.Strings(items)
		return "{" + strings.Join(items, ",") + "}"
	case *hclsyntax.TupleConsExpr:
		items := make([]string, 0, len(e.Exprs))
		for _, x := range e.Exprs {
			items = append(items, canonicalExpr(x))
		}
		return "[" + strings.Join(items, ",") + "]"
	case *hclsyntax.FunctionCallExpr:
		args := make([]string, 0, len(e.Args))
		for _, x := range e.Args {
			args = append(args, canonicalExpr(x))
		}
		if e.ExpandFinal {
			args[len(args)-1] += "..."
		}
		return e.Name + "(" + strings.Join(args, ",") + ")"
	case *hclsyntax.ForExpr:
		s := "for(" + e.KeyVar + "," + e.ValVar + " in " + canonicalExpr(e.CollExpr) + ":"
		if e.KeyExpr != nil {
			s += canonicalExpr(e.KeyExpr) + "=>"
		}
		s += canonicalExpr(e.ValExpr)
		if e.CondExpr != nil {
			s += " if " + canonicalExpr(e.CondExpr)
		}
		return s + fmt.Sprintf(",group=%v)", e.Group)
	case *hclsyntax.ConditionalExpr:
		return "(" + canonicalExpr(e.Condition) + "?" + canonicalExpr(e.TrueResult) + ":" + canonicalExpr(e.FalseResult) + ")"
	case *hclsyntax.BinaryOpExpr:
		return "(" + canonicalExpr(e.LHS) + " " + operators[e.Op] + " " + canonicalExpr(e.RHS) + ")"
	case *hclsyntax.UnaryOpExpr:
		return "(" + operators[e.Op] + " " + canonicalExpr(e.Val) + ")"
	case *hclsyntax.ParenthesesExpr:
		return canonicalExpr(e.Expression)
	}
	// Splats, template directives and the rest: refused, so no expectation compares them blindly.
	return fmt.Sprintf("<unsupported %T>", e)
}

func canonicalKey(k hclsyntax.Expression) string {
	if w, ok := k.(*hclsyntax.ObjectConsKeyExpr); ok {
		if !w.ForceNonLiteral {
			if tr, ok := w.Wrapped.(*hclsyntax.ScopeTraversalExpr); ok && len(tr.Traversal) == 1 {
				return strconv.Quote(tr.Traversal.RootName())
			}
		}
		return canonicalExpr(w.Wrapped)
	}
	return canonicalExpr(k)
}

func canonicalTraversal(tr hcl.Traversal) string {
	var b strings.Builder
	for _, step := range tr {
		switch s := step.(type) {
		case hcl.TraverseRoot:
			b.WriteString(s.Name)
		case hcl.TraverseAttr:
			b.WriteString("." + s.Name)
		case hcl.TraverseIndex:
			b.WriteString("[" + canonicalValue(s.Key) + "]")
		case hcl.TraverseSplat:
			b.WriteString("[*]")
		}
	}
	return b.String()
}

func canonicalValue(v cty.Value) string {
	switch {
	case v.IsNull():
		return "null"
	case v.Type() == cty.String:
		return strconv.Quote(v.AsString())
	case v.Type() == cty.Bool:
		return strconv.FormatBool(v.True())
	case v.Type() == cty.Number:
		return v.AsBigFloat().Text('g', -1)
	}
	return fmt.Sprintf("<%s>", v.Type().FriendlyName())
}

// The comparison's own controls: layout, attribute order, documentation and a null module
// argument do not count; anything else does, and an expression it cannot render is an error.
func TestGenerateComparator(t *testing.T) {
	canon := func(src string) string {
		t.Helper()
		c, err := canonicalHCL([]byte(src), "control.tf")
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		return c
	}
	same := [][2]string{
		{"module \"m\" {\n  a = 1\n  b = [\"x\", \"y\"]\n}\n", "module \"m\" {\n  b = [\n    \"x\",\n    \"y\",\n  ]\n  a = 1\n}\n"},
		{"module \"m\" {\n  slot = null\n}\n", "module \"m\" {}\n"},
		{"variable \"v\" {\n  description = \"one\"\n  type = string\n}\n", "variable \"v\" {\n  description = \"two\"\n  type = string\n}\n"},
		{"variable \"v\" {\n  validation {\n    condition = true\n    error_message = \"one\"\n  }\n}\n", "variable \"v\" {\n  validation {\n    condition = true\n    error_message = \"two\"\n  }\n}\n"},
		{"module \"m\" {\n  e = { s3 = \"u\", k = 1 }\n}\n", "module \"m\" {\n  e = {\n    k = 1\n    s3 = \"u\"\n  }\n}\n"},
		{"module \"m\" {\n  p = \"${var.x}\"\n}\n", "module \"m\" {\n  p = var.x\n}\n"},
	}
	for _, pair := range same {
		if canon(pair[0]) != canon(pair[1]) {
			t.Errorf("compared different, want equal:\n%s\n%s", pair[0], pair[1])
		}
	}
	differ := [][2]string{
		{"module \"m\" {\n  description = \"x\"\n}\n", "module \"m\" {}\n"},
		{"variable \"v\" {\n  default = null\n}\n", "variable \"v\" {}\n"},
		{"module \"m\" {\n  a = f(x...)\n}\n", "module \"m\" {\n  a = f(x)\n}\n"},
		{"module \"m\" {\n  a = x == y\n}\n", "module \"m\" {\n  a = x != y\n}\n"},
		{"module \"m\" {\n  a = \"x\"\n}\n", "module \"n\" {\n  a = \"x\"\n}\n"},
		{"terraform {\n  backend \"s3\" {\n    use_lockfile = true\n  }\n}\n", "terraform {\n  backend \"s3\" {}\n}\n"},
	}
	for _, pair := range differ {
		if canon(pair[0]) == canon(pair[1]) {
			t.Errorf("compared equal, want different:\n%s\n%s", pair[0], pair[1])
		}
	}
	for _, src := range []string{"module \"m\" {\n  a = var.x[*].id\n}\n", "module \"m\" {\n  a = \"%{for s in var.x}${s}%{endfor}\"\n}\n"} {
		if c, err := canonicalHCL([]byte(src), "control.tf"); err == nil {
			t.Errorf("%q rendered as %q, want an error (the comparison cannot render it)", src, c)
		}
	}
}

// A missing generated file, a changed expression or an extra file in the sandbox fixture fails;
// the sandbox's project is referenced (D94), so it gets no import block.
func TestGenerateSandboxMatchesExpectations(t *testing.T) {
	root := generationRoot(t, manifestOf(t, "sandbox"))
	if err := generateIn(t, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	compareGenerated(t, "sandbox", expectedFiles(t, "sandbox"), generatedFiles(t, root))
	configUnchanged(t, root)
}

// configUnchanged fails when generation wrote into the configuration areas generatedFiles leaves
// out (stacks/_lz/ and the files directly in stacks/): they must hold exactly the copied
// configuration and the manifest.
func configUnchanged(t *testing.T, root string) {
	t.Helper()
	want := t.TempDir()
	generationConfig(t, want)
	read := func(base string) map[string]string {
		files := map[string]string{}
		entries, err := os.ReadDir(filepath.Join(base, "stacks"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() && e.Name() != "deployments.yaml" {
				files[e.Name()] = string(readFile(t, filepath.Join(base, "stacks", e.Name())))
			}
		}
		filepath.WalkDir(filepath.Join(base, "stacks", "_lz"), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(base, path)
				files[filepath.ToSlash(rel)] = string(readFile(t, path))
			}
			return nil
		})
		return files
	}
	if got, w := read(root), read(want); !reflect.DeepEqual(got, w) {
		t.Errorf("generation changed the configuration areas: %d files there, want the %d copied", len(got), len(w))
	}
}

// The tenant-only fixture generates in a scratch repository of its own, which holds no stages,
// components or modules and gets no account stack. Its stage sources are relative paths to a
// stages/ that is a link to this repository's stages/, so every source resolves to the right
// stage outside the tenant repository (decision 1); its adopted project gets the import block.
func TestGenerateTenantOnlyInSeparateRepository(t *testing.T) {
	data := manifestOf(t, "tenant-only")
	m, err := DecodeManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	root := generationRoot(t, data)
	stages, err := filepath.Abs(filepath.Join(repositoryRoot, "stages"))
	if err != nil {
		t.Fatal(err)
	}
	if stages, err = filepath.EvalSymlinks(stages); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stages, filepath.Join(root, "stages")); err != nil {
		t.Fatal(err)
	}
	if err := generateIn(t, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, dir := range []string{"components", "modules", "stacks/account"} {
		if _, err := os.Stat(filepath.Join(root, dir)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the tenant scratch repository holds %s (%v)", dir, err)
		}
	}
	all := generatedFiles(t, root)
	compareGenerated(t, "tenant-only", expectedFiles(t, "tenant-only"), all)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range m.Instances {
		modules, _ := parseStack(t, all, in.Path).blocks("module")
		if len(modules) != 1 {
			t.Errorf("%s: %d module calls, want one", in.Path, len(modules))
			continue
		}
		source, err := strconv.Unquote(attr(modules[0].Body, "source"))
		if err != nil {
			t.Errorf("%s: module source %s is not a literal string", in.Path, attr(modules[0].Body, "source"))
			continue
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(in.Path), filepath.FromSlash(source)))
		if err != nil || resolved != filepath.Join(stages, in.Stage) || strings.HasPrefix(resolved, realRoot+string(filepath.Separator)) {
			t.Errorf("%s: source %q resolves to %q (%v), want this repository's stages/%s, outside the tenant repository", in.Path, source, resolved, err, in.Stage)
		}
	}
}

// rawSpec is the part of a manifest fixture's spec the invariants read beyond the decoded Manifest.
type rawSpec struct {
	Spec struct {
		Forge       string `json:"forge"`
		StageSource struct {
			Kind string `json:"kind"`
		} `json:"stage_source"`
		State struct {
			Region   string `json:"region"`
			Endpoint string `json:"endpoint"`
		} `json:"state"`
	} `json:"spec"`
}

// resolvedInputs are the root variables the live lane fills from the bound account per stage
// (data-model *Resolved-reference input*; `lz_account_dir` locates the local state, research R5;
// `lz_tenants` is the generated tenant list).
var resolvedInputs = map[string][]string{
	"bootstrap":          {"lz_account_dir", "state_project_id"},
	"tenant-state":       {"state_project_id"},
	"account-governance": {"lz_tenants", "tenants"},
	"project":            {"project_id"},
}

// credentialArguments are provider and backend arguments that would carry a credential.
var credentialArguments = []string{"client_id", "client_secret", "application_key", "application_secret", "consumer_key",
	"access_token", "access_key", "secret_key", "token", "password"}

// parsedStack is one generated stack's configuration files, parsed.
type parsedStack struct {
	files  map[string][]byte
	bodies map[string]*hclsyntax.Body
}

func parseStack(t *testing.T, all map[string][]byte, path string) parsedStack {
	t.Helper()
	ps := parsedStack{files: map[string][]byte{}, bodies: map[string]*hclsyntax.Body{}}
	prefix := strings.TrimPrefix(path, "stacks/") + "/"
	for rel, data := range all {
		name, ok := strings.CutPrefix(rel, prefix)
		if !ok || strings.Contains(strings.TrimPrefix(name, "tests/"), "/") {
			continue
		}
		ps.files[name] = data
		if strings.HasSuffix(name, ".tf") || strings.HasSuffix(name, ".tftest.hcl") {
			f, diags := hclsyntax.ParseConfig(data, rel, hcl.InitialPos)
			if diags.HasErrors() {
				t.Errorf("%s: %v", rel, diags)
				continue
			}
			ps.bodies[name] = f.Body.(*hclsyntax.Body)
		}
	}
	return ps
}

// blocks returns the stack's blocks of a type, in the root .tf files, with the file each is in.
func (ps parsedStack) blocks(typ string) (found []*hclsyntax.Block, files []string) {
	names := make([]string, 0, len(ps.bodies))
	for n := range ps.bodies {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		if !strings.HasSuffix(n, ".tf") {
			continue
		}
		for _, b := range ps.bodies[n].Blocks {
			if b.Type == typ {
				found, files = append(found, b), append(files, n)
			}
		}
	}
	return found, files
}

func attr(b *hclsyntax.Body, name string) string {
	a, ok := b.Attributes[name]
	if !ok {
		return ""
	}
	if lit, ok := a.Expr.(*hclsyntax.LiteralValueExpr); ok && lit.Val.IsNull() {
		return ""
	}
	return canonicalExpr(a.Expr)
}

func childBlocks(b *hclsyntax.Body, typ string) []*hclsyntax.Block {
	var out []*hclsyntax.Block
	for _, c := range b.Blocks {
		if c.Type == typ {
			out = append(out, c)
		}
	}
	return out
}

// stageVariableType is the canonical type of a variable a stage declares (stages/<stage>).
func stageVariableType(t *testing.T, stage, name string) string {
	t.Helper()
	f, diags := hclsyntax.ParseConfig(readFile(t, filepath.Join(repositoryRoot, "stages", stage, "variables.tf")), stage, hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatal(diags)
	}
	for _, b := range f.Body.(*hclsyntax.Body).Blocks {
		if b.Type == "variable" && len(b.Labels) == 1 && b.Labels[0] == name {
			return attr(b.Body, "type")
		}
	}
	t.Fatalf("stages/%s declares no variable %s", stage, name)
	return ""
}

// stageOutputs are the outputs stages/<stage>/outputs.tf declares, true where sensitive.
func stageOutputs(t *testing.T, stage string) map[string]bool {
	t.Helper()
	f, diags := hclsyntax.ParseConfig(readFile(t, filepath.Join(repositoryRoot, "stages", stage, "outputs.tf")), stage, hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatal(diags)
	}
	out := map[string]bool{}
	for _, b := range f.Body.(*hclsyntax.Body).Blocks {
		if b.Type == "output" && len(b.Labels) == 1 {
			out[b.Labels[0]] = attr(b.Body, "sensitive") == "true"
		}
	}
	if len(out) == 0 {
		t.Fatalf("stages/%s declares no output", stage)
	}
	return out
}

// checkStackInvariants holds every generated stack of a manifest to the rules that do not need a
// hand-written expectation, with the decoder's derived fields as the oracle.
func checkStackInvariants(t *testing.T, fixture string, m *Manifest, raw rawSpec, all map[string][]byte) {
	t.Helper()
	stageOf := map[string]string{}
	for _, in := range m.Instances {
		stageOf[in.ID] = in.Stage
	}
	for _, e := range m.External {
		stageOf[e.ID] = e.Stage
	}
	modeOf := map[[2]string]string{}
	for _, tn := range m.Tenants {
		for _, e := range tn.Environments {
			modeOf[[2]string{tn.Name, e.Name}] = e.ProjectMode
		}
	}
	for rel, data := range all {
		if bytes.Contains(data, []byte("terraform_remote_state")) {
			t.Errorf("%s: %s reads terraform_remote_state (FR-003)", fixture, rel)
		}
	}
	for _, in := range m.Instances {
		label := fixture + ": " + in.Path
		ps := parseStack(t, all, in.Path)
		adopt := in.Stage == "project" && modeOf[[2]string{in.Tenant, in.Environment}] == "adopt"

		// File set.
		want := []string{"_lz_backend.tf", "_lz_main.tf", "_lz_outputs.tf", "_lz_providers.tf", "_lz_variables.tf", "tests/_lz_offline.tftest.hcl"}
		if adopt {
			want = append(want, "_lz_import.tf")
		}
		if in.Stage == "account-governance" {
			want = append(want, "_lz_tenants.auto.tfvars.json")
		}
		slices.Sort(want)
		got := make([]string, 0, len(ps.files))
		for n := range ps.files {
			got = append(got, n)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s: generated files %q, want %q", label, got, want)
			continue
		}
		if in.Stage == "account-governance" {
			var list struct {
				Tenants []string `json:"lz_tenants"`
			}
			var names []string
			for _, tn := range m.Tenants {
				names = append(names, tn.Name)
			}
			if err := json.Unmarshal(ps.files["_lz_tenants.auto.tfvars.json"], &list); err != nil || !slices.Equal(list.Tenants, names) {
				t.Errorf("%s: _lz_tenants.auto.tfvars.json lists %q (%v), want every manifest tenant %q", label, list.Tenants, err, names)
			}
		}

		// Backend and encryption.
		terraform, _ := ps.blocks("terraform")
		var backends, encryptions []*hclsyntax.Block
		for _, tb := range terraform {
			backends = append(backends, childBlocks(tb.Body, "backend")...)
			encryptions = append(encryptions, childBlocks(tb.Body, "encryption")...)
		}
		if len(backends) != 1 || len(backends[0].Labels) != 1 {
			t.Errorf("%s: %d backend blocks, want one", label, len(backends))
		} else {
			be := backends[0]
			switch kind := be.Labels[0]; {
			case in.Stage == "bootstrap":
				if kind != "local" || attr(be.Body, "path") != `tpl(${var.lz_account_dir}+"/state/`+in.ID+`.tfstate")` {
					t.Errorf("%s: backend %s path %s, want local at ${var.lz_account_dir}/state/%s.tfstate", label, kind, attr(be.Body, "path"), in.ID)
				}
			case kind != "s3":
				t.Errorf("%s: backend %s, want s3 (only bootstrap keeps local state)", label, kind)
			default:
				for name, v := range map[string]string{
					"bucket":       strconv.Quote(in.StateBucket),
					"key":          strconv.Quote(in.StateKey),
					"region":       strconv.Quote(raw.Spec.State.Region),
					"endpoints":    `{"s3"=` + strconv.Quote(raw.Spec.State.Endpoint) + `}`,
					"use_lockfile": "true",
				} {
					if got := attr(be.Body, name); got != v {
						t.Errorf("%s: backend %s = %s, want %s", label, name, got, v)
					}
				}
			}
			for _, c := range credentialArguments {
				if _, ok := be.Body.Attributes[c]; ok {
					t.Errorf("%s: backend sets credential argument %s", label, c)
				}
			}
		}
		if len(encryptions) != 1 {
			t.Errorf("%s: %d encryption blocks, want one", label, len(encryptions))
		} else {
			for _, target := range []string{"state", "plan"} {
				blocks := childBlocks(encryptions[0].Body, target)
				if len(blocks) != 1 || attr(blocks[0].Body, "enforced") != "true" || attr(blocks[0].Body, "method") == "" {
					t.Errorf("%s: encryption %s is not one enforced block with a method", label, target)
				}
			}
			if kp := childBlocks(encryptions[0].Body, "key_provider"); len(kp) != 1 || attr(kp[0].Body, "passphrase") != "var.state_passphrase" {
				t.Errorf("%s: encryption key provider is not one passphrase from var.state_passphrase", label)
			}
		}

		// Provider without credentials.
		providers, _ := ps.blocks("provider")
		if len(providers) != 1 || len(providers[0].Labels) != 1 || providers[0].Labels[0] != "ovh" {
			t.Errorf("%s: %d provider blocks, want one provider \"ovh\"", label, len(providers))
		} else {
			for _, c := range credentialArguments {
				if _, ok := providers[0].Body.Attributes[c]; ok {
					t.Errorf("%s: provider sets credential argument %s", label, c)
				}
			}
		}

		// The one stage call.
		modules, files := ps.blocks("module")
		if len(modules) != 1 || files[0] != "_lz_main.tf" {
			t.Errorf("%s: module calls %d in %q, want one in _lz_main.tf", label, len(modules), files)
			continue
		}
		mod := modules[0]
		if len(mod.Labels) != 1 || mod.Labels[0] != strings.ReplaceAll(in.Stage, "-", "_") {
			t.Errorf("%s: module %q, want %q", label, mod.Labels, strings.ReplaceAll(in.Stage, "-", "_"))
		}
		rel, _ := filepath.Rel(filepath.FromSlash(in.Path), filepath.Join("stages", in.Stage))
		args := map[string]string{
			"source":     strconv.Quote(filepath.ToSlash(rel)),
			"org":        strconv.Quote(m.Org),
			"instance":   strconv.Quote(in.ID),
			"managed_in": strconv.Quote(raw.Spec.Forge + "//" + in.Path),
			"slot":       "",
		}
		switch in.Stage {
		case "tenant-state":
			args["tenant"] = strconv.Quote(in.Tenant)
		case "account-governance":
			args["tenants"] = "for(,t in var.lz_tenants:t=>var.tenants[t],group=false)"
		case "project":
			args["tenant"], args["environment"], args["project_id"] = strconv.Quote(in.Tenant), strconv.Quote(in.Environment), "var.project_id"
			args["project_mode"] = strconv.Quote(modeOf[[2]string{in.Tenant, in.Environment}])
		case "project-network", "runtime":
			args["region"], args["project"] = strconv.Quote(in.Region), "var.project"
		}
		if in.Slot != "" {
			args["slot"] = strconv.Quote(in.Slot)
		}
		for name, v := range args {
			if got := attr(mod.Body, name); got != v {
				t.Errorf("%s: module argument %s = %s, want %s", label, name, got, v)
			}
		}

		// Root variables: the passphrase, the resolved inputs, one typed object per data producer
		// stage, nothing for an authority edge.
		wantVars := append([]string{"state_passphrase"}, resolvedInputs[in.Stage]...)
		producerVars := map[string]string{}
		for _, e := range in.Edges {
			if e.Kind == EdgeData {
				v := strings.ReplaceAll(stageOf[e.Producer], "-", "_")
				producerVars[v] = stageOf[e.Producer]
				wantVars = append(wantVars, v)
			}
		}
		slices.Sort(wantVars)
		variables, vfiles := ps.blocks("variable")
		var gotVars []string
		for i, v := range variables {
			if vfiles[i] != "_lz_variables.tf" || len(v.Labels) != 1 {
				t.Errorf("%s: variable %q in %s, want variables in _lz_variables.tf", label, v.Labels, vfiles[i])
				continue
			}
			gotVars = append(gotVars, v.Labels[0])
			if _, ok := producerVars[v.Labels[0]]; ok {
				if typ := attr(v.Body, "type"); !strings.HasPrefix(typ, "object(") || typ != stageVariableType(t, in.Stage, v.Labels[0]) {
					t.Errorf("%s: producer variable %s has type %s, want the stage's object type", label, v.Labels[0], typ)
				}
			}
			if v.Labels[0] == "state_passphrase" && attr(v.Body, "sensitive") != "true" {
				t.Errorf("%s: state_passphrase is not sensitive", label)
			}
		}
		slices.Sort(gotVars)
		if !slices.Equal(gotVars, wantVars) {
			t.Errorf("%s: root variables %q, want %q", label, gotVars, wantVars)
		}

		// Root outputs (005 T038, research R4, T037 decision 3): exactly the stage's outputs, in
		// _lz_outputs.tf, each re-exporting the stage output of its name with the stage's
		// sensitivity, so `tofu output -json` of the root is the envelope builder's input.
		wantOutputs := stageOutputs(t, in.Stage)
		outputs, ofiles := ps.blocks("output")
		gotOutputs := map[string]bool{}
		for i, o := range outputs {
			if ofiles[i] != "_lz_outputs.tf" || len(o.Labels) != 1 {
				t.Errorf("%s: output %q in %s, want outputs in _lz_outputs.tf", label, o.Labels, ofiles[i])
				continue
			}
			name := o.Labels[0]
			gotOutputs[name] = true
			sensitive, declared := wantOutputs[name]
			if !declared {
				t.Errorf("%s: output %s is not an output of stages/%s", label, name, in.Stage)
				continue
			}
			if v := attr(o.Body, "value"); v != "module."+mod.Labels[0]+"."+name {
				t.Errorf("%s: output %s = %s, want module.%s.%s", label, name, v, mod.Labels[0], name)
			}
			if s := attr(o.Body, "sensitive"); s != strconv.FormatBool(sensitive) {
				t.Errorf("%s: output %s sensitive = %q, want %v as the stage declares it", label, name, s, sensitive)
			}
		}
		for name := range wantOutputs {
			if !gotOutputs[name] {
				t.Errorf("%s: stage output %s is not re-exported", label, name)
			}
		}

		// Import only for an adopted project, at the stage's address of the project.
		imports, _ := ps.blocks("import")
		switch {
		case adopt && (len(imports) != 1 ||
			attr(imports[0].Body, "to") != "module.project.module.project_factory.module.project.ovh_cloud_project.this[0]" ||
			attr(imports[0].Body, "id") != "var.project_id"):
			t.Errorf("%s: adopted project: %d import blocks, want one importing var.project_id to the project resource", label, len(imports))
		case !adopt && len(imports) != 0:
			t.Errorf("%s: %d import blocks on a stack that adopts nothing", label, len(imports))
		}

		// The offline test runs a plan under the mocked provider.
		test := ps.bodies["tests/_lz_offline.tftest.hcl"]
		if test == nil {
			continue
		}
		mocks, runs := childBlocks(test, "mock_provider"), childBlocks(test, "run")
		if len(mocks) != 1 || len(mocks[0].Labels) != 1 || mocks[0].Labels[0] != "ovh" || len(runs) == 0 {
			t.Errorf("%s: the offline test has %d mock_provider and %d run blocks, want mock_provider \"ovh\" and a run", label, len(mocks), len(runs))
		}
		for _, r := range runs {
			if attr(r.Body, "command") != "plan" {
				t.Errorf("%s: offline run %q is not command = plan", label, r.Labels)
			}
		}
	}
	// No stack is generated without a row.
	rows := map[string]bool{}
	for _, in := range m.Instances {
		rows[strings.TrimPrefix(in.Path, "stacks/")] = true
	}
	for rel := range all {
		dir := strings.TrimSuffix(strings.TrimSuffix(rel, "/"+filepath.Base(rel)), "/tests")
		if !rows[dir] {
			t.Errorf("%s: %s is generated outside every row's stack", fixture, rel)
		}
	}
}

// Every stack of the sandbox, tenant-only and growth fixtures (25 stacks, two runtime slots) has
// its backend, encryption, provider, stage call, variables, import and offline test.
func TestGenerateStackInvariants(t *testing.T) {
	for _, fixture := range []string{"sandbox", "tenant-only", "growth"} {
		t.Run(fixture, func(t *testing.T) {
			data := manifestOf(t, fixture)
			m, err := DecodeManifest(data)
			if err != nil {
				t.Fatal(err)
			}
			var raw rawSpec
			if err := json.Unmarshal(stripComments(data), &raw); err != nil {
				t.Fatal(err)
			}
			root := generationRoot(t, data)
			if err := generateIn(t, root); err != nil {
				t.Fatalf("generate: %v", err)
			}
			checkStackInvariants(t, fixture, m, raw, generatedFiles(t, root))
		})
	}
}

// Generate renders stacks/ only (005 T039): a file outside stacks/ carrying Terramate's generated
// header (the dependency-check fixtures under tests/ do) is neither deleted nor changed. Whole
// project `terramate generate` removes such files as orphans (observed 2026-10-07, 0.17.3).
func TestGenerateLeavesFilesOutsideStacks(t *testing.T) {
	root := generationRoot(t, manifestOf(t, "sandbox"))
	outside := map[string]string{
		"tests/check/fixtures/purity/stacks/account/bootstrap/main.tf": "// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT\n\nmodule \"x\" {\n  source = \"../x\"\n}\n",
		"docs/examples/versions.tf":                                    "// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT\n\nterraform {\n}\n",
	}
	for rel, content := range outside {
		writeFile(t, filepath.Join(root, rel), []byte(content))
	}
	if err := generateIn(t, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(generatedFiles(t, root)) == 0 {
		t.Fatal("nothing generated under stacks/")
	}
	for rel, content := range outside {
		got, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("%s outside stacks/ was removed: %v", rel, err)
		} else if string(got) != content {
			t.Errorf("%s outside stacks/ was changed", rel)
		}
	}
}

// Every stack of the repository's own manifest carries its stage's dependency lock file byte for
// byte (005 T039): `task lint` and the live runner initialise the stack root with
// -lockfile=readonly, and nothing generates the lock, so a new row without it, or a stage lock
// changed without its stacks, fails here.
func TestRepositoryStackLocksMatchStages(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repositoryRoot, ManifestPath))
	if err != nil {
		t.Fatalf("the repository manifest (T039): %v", err)
	}
	m, err := DecodeManifest(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, in := range m.Instances {
		want := readFile(t, filepath.Join(repositoryRoot, "stages", in.Stage, ".terraform.lock.hcl"))
		got, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(in.Path), ".terraform.lock.hcl"))
		if err != nil {
			t.Errorf("%s: no dependency lock file; copy stages/%s/.terraform.lock.hcl: %v", in.Path, in.Stage, err)
		} else if !bytes.Equal(got, want) {
			t.Errorf("%s/.terraform.lock.hcl differs from stages/%s/.terraform.lock.hcl", in.Path, in.Stage)
		}
	}
}

// A `git` stage source is the schema's seam: Generate refuses it before writing anything, and the
// stacks:check comparison refuses it too instead of rendering it silently.
func TestGenerateRefusesGitStageSource(t *testing.T) {
	manifest := edit(t, manifestOf(t, "sandbox"), `"stage_source": {"kind": "local"}`, `"stage_source": {"kind": "git"}`, 1)
	t.Run("generate", func(t *testing.T) {
		root := generationRoot(t, manifest)
		err := generateIn(t, root)
		var ge *GenerateError
		if !errors.As(err, &ge) || ge.Code != CodeStageSourceNotImplemented || ge.Detail == "" {
			t.Fatalf("want refusal %s with a detail, got %v", CodeStageSourceNotImplemented, err)
		}
		if files := generatedFiles(t, root); len(files) != 0 {
			t.Errorf("a refused generation wrote %d files", len(files))
		}
	})
	t.Run("check", func(t *testing.T) {
		root := generationRoot(t, manifest)
		if _, err := Reconcile(ReconcileOptions{Root: root, Terramate: pinnedTerramate(t)}); err != nil {
			t.Fatal(err)
		}
		findings, err := CheckStacks(root, pinnedTerramate(t))
		if err == nil || !strings.Contains(err.Error(), CodeStageSourceNotImplemented) {
			t.Fatalf("stacks:check of a git stage source: findings %q, error %v; want %s", findings, err, CodeStageSourceNotImplemented)
		}
	})
}

var (
	tofuOnce sync.Once
	tofuPath string
	tofuErr  error
)

// pinnedTofu returns the OpenTofu binary the planned-name controls run: LZ_TOFU, else the
// entry's /tcb/tofu, else tofu on PATH; it must report the version mise.toml pins.
func pinnedTofu(t *testing.T) string {
	t.Helper()
	tofuOnce.Do(func() {
		pin := regexp.MustCompile(`(?m)^opentofu\s*=\s*"([^"]+)"`).FindSubmatch(readFile(t, filepath.Join(repositoryRoot, "mise.toml")))
		if pin == nil {
			tofuErr = errors.New("mise.toml pins no opentofu version")
			return
		}
		path := os.Getenv("LZ_TOFU")
		if path == "" {
			if _, err := os.Stat("/tcb/tofu"); err == nil {
				path = "/tcb/tofu"
			} else if path, err = exec.LookPath("tofu"); err != nil {
				tofuErr = fmt.Errorf("no tofu binary (set LZ_TOFU): %v", err)
				return
			}
		}
		out, err := exec.Command(path, "version", "-json").Output()
		var v struct {
			Version string `json:"terraform_version"`
		}
		if err != nil || json.Unmarshal(out, &v) != nil || v.Version != string(pin[1]) {
			tofuErr = fmt.Errorf("%s reports version %q (%v), want the pinned %s; set LZ_TOFU", path, v.Version, err, pin[1])
			return
		}
		tofuPath = path
	})
	if tofuErr != nil {
		t.Fatal(tofuErr)
	}
	return tofuPath
}

// copyTree copies the regular files under src (OpenTofu working directories left out) to dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".terraform" {
			return fs.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		writeFile(t, filepath.Join(dst, rel), readFile(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// planStack plans one generated stack with its generated offline test and the pinned OpenTofu,
// with vars as a var file, and returns the test_plan of its run `plan`.
func planStack(tofu, root, stack, cache string, vars map[string]any) ([]byte, error) {
	dir := filepath.Join(root, filepath.FromSlash(stack))
	varFile := filepath.Join(cache, "..", strings.ReplaceAll(stack, "/", "_")+".tfvars.json")
	data, _ := json.Marshal(vars)
	if err := os.WriteFile(varFile, data, 0o600); err != nil {
		return nil, err
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(tofu, append([]string{"-chdir=" + dir}, args...)...)
		cmd.Env = append(os.Environ(), "TF_PLUGIN_CACHE_DIR="+cache, "TF_IN_AUTOMATION=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return out, fmt.Errorf("tofu %s: %v: %s %s", args[0], err, stderr.String(), lastLines(out, 5))
		}
		return out, nil
	}
	if _, err := run("init", "-backend=false", "-input=false", "-no-color"); err != nil {
		return nil, err
	}
	out, err := run("test", "-json", "-verbose", "-no-color", "-var-file="+varFile)
	if err != nil {
		return nil, err
	}
	var plan []byte
	passed := false
	for _, line := range bytes.Split(out, []byte("\n")) {
		var msg struct {
			Type    string          `json:"type"`
			Run     string          `json:"@testrun"`
			Plan    json.RawMessage `json:"test_plan"`
			Summary struct {
				Status string `json:"status"`
			} `json:"test_summary"`
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		switch {
		case msg.Type == "test_plan" && msg.Run == "plan":
			plan = msg.Plan
		case msg.Type == "test_summary":
			passed = msg.Summary.Status == "pass"
		}
	}
	if plan == nil || !passed {
		return nil, fmt.Errorf("%s: no passing plan of run \"plan\": %s", stack, lastLines(out, 5))
	}
	return plan, nil
}

func lastLines(out []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// fixtureInputs are the inputs the live lane would give one stack: its resolved references and
// its producers' published values, synthesised from the manifest with fixed ids.
func fixtureInputs(m *Manifest, in Instance) map[string]any {
	const projectID = "0123456789abcdef0123456789abcdef"
	vars := map[string]any{}
	switch in.Stage {
	case "bootstrap":
		vars["state_project_id"], vars["lz_account_dir"] = projectID, "/home/offline/.config/ovh-lz/accounts/fixture"
	case "tenant-state":
		vars["state_project_id"] = projectID
	case "project-network", "runtime":
		var regions []string
		for _, tn := range m.Tenants {
			for _, e := range tn.Environments {
				if tn.Name == in.Tenant && e.Name == in.Environment {
					regions = e.Regions
				}
			}
		}
		vars["project"] = map[string]any{"tenant": in.Tenant, "environment": in.Environment, "project_id": projectID,
			"project_urn": "urn:v1:eu:resource:publicCloudProject:" + projectID, "regions": regions, "unlabelled": []string{}}
	}
	return vars
}

// The bucket names the generated growth stacks plan (account state bucket, one state bucket per
// tenant, one runtime bucket per runtime with the slot in its name) are unique across stacks, and
// a plan set in which two runtime slots plan one bucket name is refused with NAME_COLLISION.
func TestPlannedNames(t *testing.T) {
	data := manifestOf(t, "growth")
	m, err := DecodeManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := map[string]string{}
	for _, in := range m.Instances {
		switch in.Stage {
		case "bootstrap":
			wantNames[in.Path] = m.Org + "-bkt-state"
		case "tenant-state":
			wantNames[in.Path] = m.Org + "-" + in.Tenant + "-bkt-state"
		case "runtime":
			name := strings.Join([]string{m.Org, in.Tenant, in.Environment, strings.ToLower(in.Region), "bkt", "runtime"}, "-")
			if in.Slot != "" {
				name += "-" + in.Slot
			}
			wantNames[in.Path] = name
		}
	}
	if len(wantNames) != 12 {
		t.Fatalf("growth: %d bucket-planning stacks, want 12 (bootstrap, 2 tenant-state, 9 runtime)", len(wantNames))
	}

	collision := func(t *testing.T, plans []StackPlan, stack string) {
		t.Helper()
		err := CheckPlannedNames(plans)
		var ge *GenerateError
		if !errors.As(err, &ge) || ge.Code != CodeNameCollision || ge.Path != stack || !strings.Contains(ge.Detail, "lz-demo-dev-gra11-bkt-runtime-blue") {
			t.Fatalf("want %s at %s naming the bucket, got %v", CodeNameCollision, stack, err)
		}
	}
	// The checker on its own, from the pinned OpenTofu's captured plan of the runtime stage with
	// slot blue (tests/fixtures/outputs/captures/runtime-slot-plan.json, T032).
	var msg struct {
		Plan json.RawMessage `json:"test_plan"`
	}
	if err := json.Unmarshal(stagePlanFile(t, "runtime-slot"), &msg); err != nil {
		t.Fatal(err)
	}
	captured := []byte(msg.Plan)
	t.Run("captured plan names its bucket", func(t *testing.T) {
		if names, err := PlannedBucketNames(captured); err != nil || !slices.Equal(names, []string{"lz-demo-dev-gra11-bkt-runtime-blue"}) {
			t.Fatalf("captured slot plan: buckets %q (%v), want [lz-demo-dev-gra11-bkt-runtime-blue]", names, err)
		}
	})
	t.Run("captured slot plan twice", func(t *testing.T) {
		collision(t, []StackPlan{
			{Stack: "stacks/tenants/demo/dev/gra11/runtime-blue", Plan: captured},
			{Stack: "stacks/tenants/demo/dev/gra11/runtime-green", Plan: captured},
		}, "stacks/tenants/demo/dev/gra11/runtime-green")
	})
	t.Run("unknown bucket name refused", func(t *testing.T) {
		var doc map[string]any
		if err := json.Unmarshal(captured, &doc); err != nil {
			t.Fatal(err)
		}
		changes, _ := doc["resource_changes"].([]any)
		n := 0
		for _, c := range changes {
			rc := c.(map[string]any)
			if rc["type"] == "ovh_cloud_project_storage" {
				change := rc["change"].(map[string]any)
				delete(change["after"].(map[string]any), "name")
				change["after_unknown"] = map[string]any{"name": true}
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%d buckets in the captured slot plan, want 1", n)
		}
		plan, _ := json.Marshal(doc)
		if names, err := PlannedBucketNames(plan); err == nil {
			t.Fatalf("a bucket whose name is unknown at plan time gave %q and no error; uniqueness cannot be judged", names)
		}
		if err := CheckPlannedNames([]StackPlan{{Stack: "stacks/tenants/demo/dev/gra11/runtime-blue", Plan: plan}}); err == nil {
			t.Fatal("CheckPlannedNames accepted a plan whose bucket name is unknown")
		}
	})

	// The generated growth stacks, planned under their generated offline tests.
	root := generationRoot(t, data)
	if err := generateIn(t, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, in := range m.Instances {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(in.Path), "tests", "_lz_offline.tftest.hcl")); err != nil {
			t.Fatalf("growth: %s has no generated offline test: %v", in.Path, err)
		}
	}
	for _, dir := range []string{"stages", "components", "modules"} {
		copyTree(t, filepath.Join(repositoryRoot, dir), filepath.Join(root, dir))
	}
	tofu := pinnedTofu(t)
	cache := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	plans := make([]StackPlan, len(m.Instances))
	errs := make([]error, len(m.Instances))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	// One init first fills the plugin cache; the rest run four at a time.
	order := []int{}
	for i, in := range m.Instances {
		if _, ok := wantNames[in.Path]; ok {
			order = append(order, i)
		}
	}
	first := order[0]
	plans[first].Stack = m.Instances[first].Path
	plans[first].Plan, errs[first] = planStack(tofu, root, m.Instances[first].Path, cache, fixtureInputs(m, m.Instances[first]))
	for _, i := range order[1:] {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			in := m.Instances[i]
			plans[i].Stack = in.Path
			plans[i].Plan, errs[i] = planStack(tofu, root, in.Path, cache, fixtureInputs(m, in))
		}(i)
	}
	wg.Wait()
	var planned []StackPlan
	byPath := map[string][]byte{}
	for _, i := range order {
		if errs[i] != nil {
			t.Errorf("plan %s: %v", m.Instances[i].Path, errs[i])
			continue
		}
		planned = append(planned, plans[i])
		byPath[plans[i].Stack] = plans[i].Plan
	}
	if t.Failed() {
		t.FailNow()
	}

	t.Run("unique across the growth stacks", func(t *testing.T) {
		seen := map[string]string{}
		for _, p := range planned {
			names, err := PlannedBucketNames(p.Plan)
			if err != nil || !slices.Equal(names, []string{wantNames[p.Stack]}) {
				t.Errorf("%s plans buckets %q (%v), want [%s]", p.Stack, names, err, wantNames[p.Stack])
			}
			for _, n := range names {
				if other, dup := seen[n]; dup {
					t.Errorf("%s and %s both plan bucket %s", other, p.Stack, n)
				}
				seen[n] = p.Stack
			}
		}
		if err := CheckPlannedNames(planned); err != nil {
			t.Errorf("unique names refused: %v", err)
		}
		blue, green := "stacks/tenants/demo/dev/gra11/runtime-blue", "stacks/tenants/demo/dev/gra11/runtime-green"
		if wantNames[blue] != "lz-demo-dev-gra11-bkt-runtime-blue" || wantNames[green] != "lz-demo-dev-gra11-bkt-runtime-green" {
			t.Errorf("growth slots: %q, %q", wantNames[blue], wantNames[green])
		}
	})

	t.Run("two generated slots planning one name", func(t *testing.T) {
		blue := byPath["stacks/tenants/demo/dev/gra11/runtime-blue"]
		plans := slices.Clone(planned)
		for i := range plans {
			if plans[i].Stack == "stacks/tenants/demo/dev/gra11/runtime-green" {
				plans[i].Plan = blue
			}
		}
		collision(t, plans, "stacks/tenants/demo/dev/gra11/runtime-green")
	})
}

// git runs git hermetically (no global or system configuration) in dir.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=lz-test", "-c", "user.email=lz-test@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// The authoring flow (research R16, contracts/checks.md *stacks:reconcile / stacks:generate*):
// in a linked worktree of a scratch git repository, with a dirty tree and no ~/.config/ovh-lz/,
// a manifest edit adding a row, then reconcile, then generate, then the stacks:check comparison
// is green, the generated files are the sandbox's, and ~/.config stays absent. A generator
// that refuses a linked worktree or a dirty tree (the host guard's refusals) fails here.
func TestAuthoringFlow(t *testing.T) {
	home := authoringEnvironment(t)
	authoringFlow(t)
	// Terramate keeps its own checkpoint and analytics signatures in ~/.terramate.d (observed with
	// 0.17.3, evidence/T037.md); nothing may appear under ~/.config, where the credentials live.
	if _, err := os.Lstat(filepath.Join(home, ".config")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the flow created ~/.config (%v); the credential directory ~/.config/ovh-lz must stay absent", err)
	}
}

// authoringEnvironment gives the test an empty HOME (so no ~/.config/ovh-lz/) and no OVH_ or
// AWS_ variables, and skips without git (the offline entry's image has none: the flow runs on the
// host, evidence/T037.md).
func authoringEnvironment(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git binary (the offline entry's image has none): the authoring flow runs on the host Verify line (evidence/T037.md)")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "OVH_") || strings.HasPrefix(k, "AWS_") {
			t.Setenv(k, "")
			os.Unsetenv(k)
		}
	}
	return home
}

// authoringFlow runs edit → reconcile → generate → stacks:check in a linked worktree with a
// dirty tree and checks the outcome.
func authoringFlow(t *testing.T) {
	t.Helper()
	full := manifestOf(t, "sandbox")
	base := edit(t, full, `,
      {"id": "demo-dev-gra11-runtime", "stage": "runtime", "tenant": "demo", "environment": "dev", "region": "GRA11"}`, "", 1)
	main := generationRoot(t, base)
	if err := generateIn(t, main); err != nil {
		t.Fatalf("generate the committed base: %v", err)
	}
	git(t, main, "init", "-q")
	git(t, main, "add", "-A")
	git(t, main, "commit", "-q", "-m", "base")
	wt := filepath.Join(t.TempDir(), "authoring")
	git(t, main, "worktree", "add", "-q", "-b", "authoring", wt)

	if fi, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("%s is not a linked worktree (.git: %v)", wt, err)
	}
	if git(t, wt, "rev-parse", "--absolute-git-dir") == git(t, wt, "rev-parse", "--path-format=absolute", "--git-common-dir") {
		t.Fatal("the worktree shares its git dir with the main checkout: not a linked worktree")
	}
	writeFile(t, filepath.Join(wt, ManifestPath), full)
	if git(t, wt, "status", "--porcelain") == "" {
		t.Fatal("the manifest edit left the tree clean")
	}

	report, err := Reconcile(ReconcileOptions{Root: wt, Terramate: pinnedTerramate(t)})
	if err != nil || !slices.Equal(report.Created, []string{"stacks/tenants/demo/dev/gra11/runtime"}) {
		t.Fatalf("reconcile in the authoring worktree: created %q, error %v; want the runtime stack", report.Created, err)
	}
	if err := Generate(GenerateOptions{Root: wt, Terramate: pinnedTerramate(t)}); err != nil {
		t.Fatalf("generate in the authoring worktree: %v", err)
	}
	findings, err := CheckStacks(wt, pinnedTerramate(t))
	if err != nil || len(findings) != 0 {
		t.Errorf("stacks:check after the authoring flow: findings %q, error %v; want none", findings, err)
	}
	compareGenerated(t, "authoring worktree", expectedFiles(t, "sandbox"), generatedFiles(t, wt))
	if git(t, wt, "status", "--porcelain") == "" {
		t.Error("the flow committed or reverted the edit; the tree must stay dirty")
	}
}

// The authoring flow's code reaches no credential and no host guard: no package lz-stacks
// imports, directly or not, is the live lane (internal/live: the host guard, credential files,
// the run core), and none of their non-test files names a credential variable, the credential directory or
// a config directory, or looks up the user's home.
func TestAuthoringFlowReachesNoCredentialCode(t *testing.T) {
	const module = "github.com/PlatformRelay/landingzone-for-ovhcloud/tools/"
	forbidden := []string{"internal/live", "internal/probes", "cmd/lz-live"}
	markers := []string{"OVH_", "AWS_", "ovh-lz", "sandbox.env", "/.config"}
	seen := map[string]bool{}
	var reached []string
	var visit func(pkg string)
	visit = func(pkg string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		reached = append(reached, pkg)
		dir := filepath.Join(repositoryRoot, "tools", filepath.FromSlash(pkg))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			src := readFile(t, filepath.Join(dir, name))
			f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("%s/%s: %v", pkg, name, err)
			}
			for _, lit := range stringLiterals(f) {
				for _, m := range markers {
					if strings.Contains(lit, m) {
						t.Errorf("%s/%s: string %s names %s", pkg, name, lit, m)
					}
				}
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if path == "os/user" {
					t.Errorf("%s/%s imports os/user", pkg, name)
				}
				if p, ok := strings.CutPrefix(path, module); ok {
					visit(p)
				}
			}
			if bytes.Contains(src, []byte("os.UserHomeDir")) {
				t.Errorf("%s/%s calls os.UserHomeDir", pkg, name)
			}
		}
	}
	visit("cmd/lz-stacks")
	// The Terramate step runs the repository's generation configuration: it names no credential
	// either.
	config := t.TempDir()
	generationConfig(t, config)
	err := filepath.WalkDir(config, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(config, path)
		for _, m := range markers {
			if bytes.Contains(readFile(t, path), []byte(m)) {
				t.Errorf("generation configuration %s names %s", filepath.ToSlash(rel), m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range reached {
		for _, f := range forbidden {
			if pkg == f || strings.HasPrefix(pkg, f+"/") {
				t.Errorf("lz-stacks reaches %s", pkg)
			}
		}
	}
	if !slices.Contains(reached, "internal/stacks") {
		t.Fatalf("lz-stacks reaches %q, not internal/stacks: the walk is broken", reached)
	}
}

// stringLiterals lists a Go file's string literals as written.
func stringLiterals(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && b.Kind == token.STRING {
			out = append(out, b.Value)
		}
		return true
	})
	return out
}
