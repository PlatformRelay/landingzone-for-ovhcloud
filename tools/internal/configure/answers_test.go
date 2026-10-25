package configure

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// Controls of 004 T016 (FR-001, FR-008, V001, guard G29): the answers file is decoded strictly
// and read only as a regular file. Cases are written from data-model.md *Answers file* and the
// task text. Fixtures under tests/fixtures/answers/: sandbox.yaml (11 values, research R2),
// sandbox-two-tenants.yaml (15), all-fields.yaml (every optional field set off its default) and
// invalid/, one file per refusal case with cases.json naming its base, code, detail, why and the
// edit that removes the defect (its positive control; without one, the base is the control).

const answersFixtureDir = "../../../tests/fixtures/answers"

func answersFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(answersFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func answersVlan(id int) *int { return &id }

// answersSandboxWant is sandbox.yaml as data-model.md reads it, no default applied.
func answersSandboxWant() Answers {
	return Answers{
		APIVersion: "lz.platformrelay.dev/v1alpha1", Kind: "DeploymentAnswers", Name: "sandbox", Org: "lz",
		Forge: "github.com/platformrelay/landingzone-for-ovhcloud", StateRegion: "gra", SharedStateProject: true,
		Tenants: []Tenant{{Name: "demo", Environments: []Environment{
			{Name: "dev", Regions: []Region{{Name: "GRA11", CIDR: "10.20.0.0/24"}}},
		}}},
	}
}

// TestAnswersDecodeFixtures: every field of the fixtures is read and no default is applied
// (acceptance; exposes a decoder that drops or invents fields).
func TestAnswersDecodeFixtures(t *testing.T) {
	two := answersSandboxWant()
	two.Tenants = append(two.Tenants, Tenant{Name: "lab", Environments: []Environment{
		{Name: "dev", Regions: []Region{{Name: "GRA11", CIDR: "10.40.0.0/24"}}},
	}})
	all := Answers{
		APIVersion: "lz.platformrelay.dev/v1alpha1", Kind: "DeploymentAnswers", Name: "acme-prod", Org: "acme",
		Forge: "git.example.com/acme/landing-zone", StateRegion: "de", StateEndpoint: "https://s3.gra.io.cloud.ovh.net",
		StateRef: "ACME_STATE", SharedStateProject: true,
		Tenants: []Tenant{
			{Name: "shop", Environments: []Environment{
				{Name: "prod", ProjectRef: "SHOP_PRODUCTION", ProjectMode: "adopt", BudgetAlert: true, QuotaGuard: true,
					Regions: []Region{{Name: "DE1", CIDR: "10.60.0.0/24", VlanID: answersVlan(4000)}, {Name: "GRA11"}}},
				{Name: "test", Regions: []Region{}},
			}},
			{Name: "empty", Environments: []Environment{}},
		},
	}
	for _, row := range []struct {
		file string
		want Answers
	}{{"sandbox.yaml", answersSandboxWant()}, {"sandbox-two-tenants.yaml", two}, {"all-fields.yaml", all}} {
		t.Run(row.file, func(t *testing.T) { answersDecodeRow(t, answersFixture(t, row.file), row.want) })
	}
}

func answersDecodeRow(t *testing.T, data []byte, want Answers) {
	t.Helper()
	got, err := DecodeAnswers(data)
	if err != nil {
		t.Fatalf("want decoded, got %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded %+v\nwant    %+v", got, want)
	}
}

// answersValueCount counts the leaf values (JSON scalars) of an answers file, comments removed.
func answersValueCount(t *testing.T, data []byte) int {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines = append(lines, line)
		}
	}
	var doc any
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &doc); err != nil {
		t.Fatal(err)
	}
	var count func(any) int
	count = func(v any) int {
		n := 0
		switch v := v.(type) {
		case map[string]any:
			for _, item := range v {
				n += count(item)
			}
		case []any:
			for _, item := range v {
				n += count(item)
			}
		default:
			n = 1
		}
		return n
	}
	return count(doc)
}

// TestAnswersFixtureValueCounts (pin): the fixtures hold 11 and 15 values (research R2);
// control: the sandbox answers with one value more counts 12.
func TestAnswersFixtureValueCounts(t *testing.T) {
	for file, want := range map[string]int{"sandbox.yaml": 11, "sandbox-two-tenants.yaml": 15} {
		if got := answersValueCount(t, answersFixture(t, file)); got != want {
			t.Errorf("%s holds %d values, want %d", file, got, want)
		}
	}
	more := strings.Replace(string(answersFixture(t, "sandbox.yaml")), `"org": "lz",`, `"org": "lz", "state_ref": "STATE",`, 1)
	if got := answersValueCount(t, []byte(more)); got != 12 {
		t.Errorf("control: one value more counts %d, want 12", got)
	}
}

type answersCase struct {
	Base    string `json:"base"`
	Code    string `json:"code"`
	Detail  string `json:"detail"`
	Why     string `json:"why"`
	Control *struct {
		Old string `json:"old"`
		New string `json:"new"`
	} `json:"control"`
}

// TestAnswersRefusesInvalid: each invalid/ case is refused with exactly its code and detail,
// and its positive control (the same file with the defect removed) is accepted (G29).
func TestAnswersRefusesInvalid(t *testing.T) {
	var cases map[string]answersCase
	if err := json.Unmarshal(answersFixture(t, "invalid/cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(answersFixtureDir, "invalid", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 || len(files) != len(cases) {
		t.Errorf("%d invalid fixtures, cases.json lists %d", len(files), len(cases))
	}
	codes := map[string]bool{}
	for _, f := range files {
		name := filepath.Base(f)
		c, listed := cases[name]
		if !listed {
			t.Errorf("%s: not in cases.json", name)
			continue
		}
		codes[c.Code] = true
		t.Run(strings.TrimSuffix(name, ".yaml"), func(t *testing.T) { answersRefusalRow(t, name, c) })
	}
	for _, code := range []string{CodeAnswersSyntax, CodeDuplicateKey, CodeUnknownField, CodeMissingField,
		CodeUnsupportedVersion, CodeInvalidValue} {
		if !codes[code] {
			t.Errorf("no invalid case expects %s", code)
		}
	}
}

func answersRefusalRow(t *testing.T, name string, c answersCase) {
	t.Helper()
	data := answersFixture(t, filepath.Join("invalid", name))
	answersWantRefusal(t, data, c.Code, c.Detail, c.Why)
	control := answersFixture(t, c.Base)
	if c.Control != nil {
		if n := strings.Count(string(data), c.Control.Old); n != 1 {
			t.Fatalf("control edit: %q occurs %d times, want once", c.Control.Old, n)
		}
		control = []byte(strings.Replace(string(data), c.Control.Old, c.Control.New, 1))
	}
	if _, err := DecodeAnswers(control); err != nil {
		t.Errorf("positive control (the defect removed): want decoded, got %v", err)
	}
}

func answersWantRefusal(t *testing.T, data []byte, code, detail, why string) {
	t.Helper()
	_, err := DecodeAnswers(data)
	var ae *AnswersError
	if !errors.As(err, &ae) {
		t.Fatalf("want refusal %s at %s (%s), got %T: %v", code, detail, why, err, err)
	}
	if ae.Code != code || ae.Detail != detail {
		t.Errorf("refused %s at %q, want %s at %q (%s)", ae.Code, ae.Detail, code, detail, why)
	}
}

// answersPadded is the sandbox answers padded with a whole-line comment to exactly size bytes.
func answersPadded(t *testing.T, size int) []byte {
	t.Helper()
	base := answersFixture(t, "sandbox.yaml")
	pad := size - len(base) - 2 // "#" and "\n"
	if pad < 0 {
		t.Fatalf("sandbox answers exceed %d bytes", size)
	}
	return append(base, []byte("#"+strings.Repeat("x", pad)+"\n")...)
}

// TestAnswersSizeBound: exactly 64 KiB of valid answers is decoded and read; one byte more is
// ANSWERS_TOO_LARGE, through the decoder and through the file reader (G29: the size bound).
func TestAnswersSizeBound(t *testing.T) {
	fits, over := answersPadded(t, 64<<10), answersPadded(t, 64<<10+1)
	if len(fits) != 65536 || len(over) != 65537 {
		t.Fatalf("padding: %d and %d bytes", len(fits), len(over))
	}
	answersDecodeRow(t, fits, answersSandboxWant())
	answersWantRefusal(t, over, CodeAnswersTooLarge, "65537 bytes", "64 KiB plus one byte")

	dir := t.TempDir()
	fitsPath, overPath := filepath.Join(dir, "fits.yaml"), filepath.Join(dir, "over.yaml")
	for path, data := range map[string][]byte{fitsPath: fits, overPath: over} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := ReadAnswers(fitsPath); err != nil || string(got) != string(fits) {
		t.Errorf("read 64 KiB: %d bytes, %v; want the file", len(got), err)
	}
	answersWantReadRefusal(t, overPath, CodeAnswersTooLarge)
}

func answersWantReadRefusal(t *testing.T, path, code string) {
	t.Helper()
	data, err := ReadAnswers(path)
	var ae *AnswersError
	if !errors.As(err, &ae) || ae.Code != code || ae.Detail != path || data != nil {
		t.Errorf("read %s: %d bytes, %v; want %s naming the path and no bytes", path, len(data), err, code)
	}
}

// TestAnswersReaderRefusesNonRegular: a symlink to the sandbox answers, a directory, a FIFO, a
// missing path and a path under a regular file are ANSWERS_NOT_REGULAR; the regular file (the
// control) is read (FR-008; G29: a followed symlink).
func TestAnswersReaderRefusesNonRegular(t *testing.T) {
	regular, err := filepath.Abs(filepath.Join(answersFixtureDir, "sandbox.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "answers.yaml")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "answers.fifo") // without O_NONBLOCK the open would wait for a writer
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, dir, fifo, filepath.Join(dir, "missing.yaml"), filepath.Join(regular, "x")} {
		answersWantReadRefusal(t, path, CodeAnswersNotRegular)
	}
	if got, err := ReadAnswers(regular); err != nil || string(got) != string(answersFixture(t, "sandbox.yaml")) {
		t.Errorf("control: read the regular file: %d bytes, %v", len(got), err)
	}
}

// answersSchemaNode walks schemas/deployments.schema.json, read as JSON data, to the node of a
// manifest path ("." between keys, "[]" for an array element).
func answersSchemaNode(schema map[string]any, path string) (map[string]any, bool) {
	node := schema
	for _, key := range strings.Split(path, ".") {
		name, array := strings.CutSuffix(key, "[]")
		props, _ := node["properties"].(map[string]any)
		next, ok := props[name].(map[string]any)
		if array && ok {
			next, ok = next["items"].(map[string]any)
		}
		if !ok {
			return nil, false
		}
		node = next
	}
	return node, true
}

// answersSchemaRule reads a schema node's pattern, enum and range (minimum, maximum).
func answersSchemaRule(node map[string]any) (pattern string, enum []string, rng []int) {
	pattern, _ = node["pattern"].(string)
	if list, ok := node["enum"].([]any); ok {
		for _, e := range list {
			enum = append(enum, fmt.Sprint(e))
		}
	}
	if lo, ok := node["minimum"].(float64); ok {
		hi, _ := node["maximum"].(float64)
		rng = []int{int(lo), int(hi)}
	}
	return pattern, enum, rng
}

// answersSchemaAgree compares each rule with the schema node of its manifest field: pattern,
// enum and range. It fails when it compared no field, so an empty rule list is not a pass.
func answersSchemaAgree(rules []fieldRule, schema map[string]any) error {
	var diffs []string
	for _, r := range rules {
		node, ok := answersSchemaNode(schema, r.Manifest)
		if !ok {
			diffs = append(diffs, r.Answers+": no schema node "+r.Manifest)
			continue
		}
		pattern, enum, rng := answersSchemaRule(node)
		if pattern != r.Pattern || !slices.Equal(enum, r.Enum) || !slices.Equal(rng, r.Range) {
			diffs = append(diffs, fmt.Sprintf("%s: schema %s has pattern %q enum %v range %v, code %q %v %v",
				r.Answers, r.Manifest, pattern, enum, rng, r.Pattern, r.Enum, r.Range))
		}
	}
	if len(rules) == 0 {
		diffs = append(diffs, "compared zero fields")
	}
	if len(diffs) > 0 {
		return errors.New(strings.Join(diffs, "\n"))
	}
	return nil
}

// TestAnswersPatternsMatchManifestSchema (pin): every shared field's rule equals the one
// schemas/deployments.schema.json gives its manifest field; controls: a changed pattern and an
// empty rule list each fail the comparison.
func TestAnswersPatternsMatchManifestSchema(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(answersReadFile(t, "../../../schemas/deployments.schema.json"), &schema); err != nil {
		t.Fatal(err)
	}
	if len(answerRules) < 13 {
		t.Errorf("%d rules compared, want at least the 13 shared fields of data-model.md", len(answerRules))
	}
	if err := answersSchemaAgree(answerRules, schema); err != nil {
		t.Errorf("answers rules differ from the manifest schema:\n%v", err)
	}
	changed := slices.Clone(answerRules)
	changed[1].Pattern = `^[a-z][a-z0-9]{0,16}$`
	if answersSchemaAgree(changed, schema) == nil {
		t.Error("control: a changed pattern passed")
	}
	if answersSchemaAgree(nil, schema) == nil {
		t.Error("control: an empty rule list passed")
	}
}

func answersReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// answersForbidden are the imports and os functions that would give the package a path to the
// network, a process or the environment (FR-008): net and every package under it, os/exec,
// os/user (the home directory by another way), the
// os functions that read or change the environment or start a process, and every syscall name
// but the open flags and errno values ReadAnswers needs.
var (
	answersForbiddenImports = []string{"net", "os/exec", "os/user", "golang.org/x/sys/unix"}
	answersForbiddenCalls   = []string{"Getenv", "LookupEnv", "Environ", "UserHomeDir", "ExpandEnv",
		"Setenv", "Unsetenv", "Clearenv", "StartProcess"}
	answersAllowedSyscall = []string{"O_NOFOLLOW", "O_NONBLOCK", "ELOOP", "ENOTDIR"}
)

// answersForbiddenImport reports whether an import path is forbidden: one of the list or under it.
func answersForbiddenImport(p string) bool {
	return slices.ContainsFunc(answersForbiddenImports, func(f string) bool { return p == f || strings.HasPrefix(p, f+"/") })
}

// answersForbiddenUses parses the non-test Go files of dir and reports each forbidden import
// and each use of a forbidden os function, under any import name. files counts the files read.
func answersForbiddenUses(dir string) (files int, uses []string, err error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return 0, nil, err
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return files, uses, err
		}
		files++
		uses = append(uses, answersFileUses(path, f)...)
	}
	return files, uses, nil
}

func answersFileUses(path string, f *ast.File) []string {
	var uses []string
	names := map[string]string{} // import name -> "os" or "syscall"
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if answersForbiddenImport(p) {
			uses = append(uses, path+": imports "+p)
		}
		if p != "os" && p != "syscall" {
			continue
		}
		name := p
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			uses = append(uses, path+": dot-imports "+p+", so its names cannot be told apart")
		}
		names[name] = p
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			uses = append(uses, answersSelectorUse(path, names, sel)...)
		}
		return true
	})
	return uses
}

// answersSelectorUse reports a forbidden os function or a syscall name off the allow-list.
func answersSelectorUse(path string, names map[string]string, sel *ast.SelectorExpr) []string {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil
	}
	switch names[id.Name] {
	case "os":
		if slices.Contains(answersForbiddenCalls, sel.Sel.Name) {
			return []string{path + ": uses os." + sel.Sel.Name}
		}
	case "syscall":
		if !slices.Contains(answersAllowedSyscall, sel.Sel.Name) {
			return []string{path + ": uses syscall." + sel.Sel.Name}
		}
	}
	return nil
}

// TestAnswersSourceHasNoAuthority (pin): the package imports nothing under net, no os/exec, no
// os/user and no golang.org/x/sys/unix, uses no os function that reads or changes the environment or starts a
// process (os.Getenv, os.LookupEnv, os.Environ, os.UserHomeDir among them) and no syscall name
// but the open flags and errno values ReadAnswers needs (FR-008); controls: a planted os.Getenv
// under another import name, a planted net/http import and a planted syscall.Getenv in a scratch
// copy are each found.
func TestAnswersSourceHasNoAuthority(t *testing.T) {
	files, uses, err := answersForbiddenUses(".")
	if err != nil || files == 0 || len(uses) > 0 {
		t.Errorf("package source: %d files read, %v, uses %v; want files read and no use", files, err, uses)
	}
	scratch := t.TempDir()
	for name, src := range map[string]string{
		"env.go":     "package configure\n\nimport sys \"os\"\n\nfunc home() string { return sys.Getenv(\"HOME\") }\n",
		"net.go":     "package configure\n\nimport _ \"net/http\"\n",
		"syscall.go": "package configure\n\nimport \"syscall\"\n\nfunc user() (string, bool) { return syscall.Getenv(\"USER\") }\n",
	} {
		if err := os.WriteFile(filepath.Join(scratch, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, uses, err := answersForbiddenUses(scratch); err != nil || len(uses) != 3 {
		t.Errorf("control: planted os.Getenv, net/http import and syscall.Getenv: %v, uses %v; want all three found", err, uses)
	}
}
