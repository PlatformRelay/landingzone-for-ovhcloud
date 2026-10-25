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
		Forge: "git.example.com/acme/landing-zone", StateRegion: "de", StateEndpoint: "https://s3.de.io.cloud.ovh.net",
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

// TestAnswersReaderRefusesNonRegular: a symlink to the sandbox answers, a directory and a missing
// path are ANSWERS_NOT_REGULAR; the regular file (the control) is read (FR-008; G29: a followed
// symlink).
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
	for _, path := range []string{link, dir, filepath.Join(dir, "missing.yaml")} {
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
// network, a process or the environment (FR-008).
var (
	answersForbiddenImports = []string{"net", "net/http", "os/exec"}
	answersForbiddenCalls   = []string{"Getenv", "LookupEnv", "Environ", "UserHomeDir"}
)

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
	osName := ""
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if slices.Contains(answersForbiddenImports, p) {
			uses = append(uses, path+": imports "+p)
		}
		if p == "os" {
			osName = "os"
			if imp.Name != nil {
				osName = imp.Name.Name
			}
		}
	}
	if osName == "." {
		uses = append(uses, path+": dot-imports os, so its functions cannot be told apart")
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && osName != "" && id.Name == osName && slices.Contains(answersForbiddenCalls, sel.Sel.Name) {
			uses = append(uses, path+": uses os."+sel.Sel.Name)
		}
		return true
	})
	return uses
}

// TestAnswersSourceHasNoAuthority (pin): the package imports no net, net/http or os/exec and
// uses no os.Getenv, os.LookupEnv, os.Environ or os.UserHomeDir (FR-008); controls: a planted
// os.Getenv call under another import name and a planted net import in a scratch copy are found.
func TestAnswersSourceHasNoAuthority(t *testing.T) {
	files, uses, err := answersForbiddenUses(".")
	if err != nil || files == 0 || len(uses) > 0 {
		t.Errorf("package source: %d files read, %v, uses %v; want files read and no use", files, err, uses)
	}
	scratch := t.TempDir()
	for name, src := range map[string]string{
		"env.go": "package configure\n\nimport sys \"os\"\n\nfunc home() string { return sys.Getenv(\"HOME\") }\n",
		"net.go": "package configure\n\nimport _ \"net\"\n",
	} {
		if err := os.WriteFile(filepath.Join(scratch, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, uses, err := answersForbiddenUses(scratch); err != nil || len(uses) != 2 {
		t.Errorf("control: planted os.Getenv and net import: %v, uses %v; want both found", err, uses)
	}
}
