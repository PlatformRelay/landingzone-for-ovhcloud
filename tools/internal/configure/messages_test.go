package configure

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Controls of 004 T018 (FR-004, SC-002, V003, guard G32): every refusal prints one line
// `configure: <CODE>: <detail>. <plain sentence> Next: <action>` from the catalogue entry of its
// (source, code). Cases are written from spec.md FR-004, data-model.md *Refusal message* and
// contracts/checks.md *Message catalogue*.

const messagesManifestGo = "../stacks/manifest.go"

// The generated codes the derivation can reach (research R5); every other one is a tool defect.
var messagesReachable = []string{stacks.CodeInvalidValue, stacks.CodeDuplicateName, stacks.CodeDuplicateVLAN, stacks.CodeSharedStateProject}

const messagesToolDefect = "This is a defect of the tool, not of your answers."

// messagesParseCodes is the value of every exported Code* constant of one Go source, from a
// `const (…)` block or a single declaration. A Code* constant whose value is not a string literal
// is an error, so a change of form cannot drop a code silently.
func messagesParseCodes(src string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		return nil, err
	}
	var codes []string
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			found, err := messagesSpecCodes(spec.(*ast.ValueSpec))
			if err != nil {
				return nil, err
			}
			codes = append(codes, found...)
		}
	}
	return codes, nil
}

func messagesSpecCodes(spec *ast.ValueSpec) ([]string, error) {
	var codes []string
	for i, name := range spec.Names {
		if !strings.HasPrefix(name.Name, "Code") {
			continue
		}
		var lit *ast.BasicLit
		if i < len(spec.Values) {
			lit, _ = spec.Values[i].(*ast.BasicLit)
		}
		if lit == nil || lit.Kind != token.STRING {
			return nil, fmt.Errorf("%s: not a string literal", name.Name)
		}
		code, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// messagesWantKeys is every (source, code) pair the catalogue must hold: the parsed codes of
// manifest.go under generated, of the package's sources under configure. Zero codes for either
// source is an error.
func messagesWantKeys(generatedSrc string, configureSrcs []string) (map[catalogueKey]bool, error) {
	want := map[catalogueKey]bool{}
	for _, part := range []struct {
		source string
		srcs   []string
	}{{SourceGenerated, []string{generatedSrc}}, {SourceConfigure, configureSrcs}} {
		n := 0
		for _, src := range part.srcs {
			codes, err := messagesParseCodes(src)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", part.source, err)
			}
			for _, code := range codes {
				want[catalogueKey{part.source, code}] = true
			}
			n += len(codes)
		}
		if n == 0 {
			return nil, fmt.Errorf("%s: parsed zero Code constants", part.source)
		}
	}
	return want, nil
}

// messagesCheck is the completeness check: every wanted pair has an entry with a sentence and an
// action, and no entry is for a code no parse found.
func messagesCheck(cat map[catalogueKey]catalogueEntry, generatedSrc string, configureSrcs []string) error {
	want, err := messagesWantKeys(generatedSrc, configureSrcs)
	if err != nil {
		return err
	}
	var problems []string
	for key := range want {
		entry, ok := cat[key]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("missing entry (%s, %s)", key.source, key.code))
		case entry.sentence == "" || entry.next == "":
			problems = append(problems, fmt.Sprintf("empty sentence or action (%s, %s)", key.source, key.code))
		}
	}
	for key := range cat {
		if !want[key] {
			problems = append(problems, fmt.Sprintf("stale entry (%s, %s)", key.source, key.code))
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// messagesSources reads manifest.go and the package's non-test Go files.
func messagesSources(t *testing.T) (string, []string) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var configure []string
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			configure = append(configure, string(answersReadFile(t, f)))
		}
	}
	return string(answersReadFile(t, messagesManifestGo)), configure
}

func messagesSortedKeys(want map[catalogueKey]bool) []catalogueKey {
	keys := slices.Collect(maps.Keys(want))
	slices.SortFunc(keys, func(a, b catalogueKey) int { return strings.Compare(a.source+" "+a.code, b.source+" "+b.code) })
	return keys
}

// messagesLine is data-model's refusal line for an entry.
func messagesLine(code, detail string, entry catalogueEntry) string {
	return "configure: " + code + ": " + detail + ". " + entry.sentence + " Next: " + entry.next
}

// TestMessagesCatalogueComplete: every parsed Code* of manifest.go (block and single declarations)
// and of the package has an entry with a sentence and an action (G32: a missing entry).
func TestMessagesCatalogueComplete(t *testing.T) {
	generated, configure := messagesSources(t)
	if err := messagesCheck(catalogue, generated, configure); err != nil {
		t.Error(err)
	}
	want, err := messagesWantKeys(generated, configure)
	if err != nil {
		t.Fatal(err)
	}
	// both declaration forms of manifest.go, and the command codes of this package
	for _, key := range []catalogueKey{{SourceGenerated, "MANIFEST_SYNTAX"}, {SourceGenerated, "DUPLICATE_NAME"},
		{SourceGenerated, "DUPLICATE_VLAN"}, {SourceConfigure, "ANSWERS_SYNTAX"}, {SourceConfigure, "STAGING_NOT_DURABLE"}} {
		if !want[key] {
			t.Errorf("the parse misses (%s, %s)", key.source, key.code)
		}
	}
}

// TestMessagesCheckRefusesEmptyParse (pin): a source that yields no Code* constant fails the
// check, for either source. Control: TestMessagesCatalogueComplete parses the real files.
func TestMessagesCheckRefusesEmptyParse(t *testing.T) {
	generated, configure := messagesSources(t)
	for name, c := range map[string]struct {
		generated string
		configure []string
	}{
		"empty generated source":     {"", configure},
		"generated without a code":   {"package stacks\n\nconst ManifestKind = \"Deployments\"\n", configure},
		"generated code not literal": {"package stacks\n\nconst CodeX = prefix + \"X\"\n", configure},
		"no configure source":        {generated, nil},
		"configure without a code":   {generated, []string{"package configure\n"}},
	} {
		if err := messagesCheck(catalogue, c.generated, c.configure); err == nil {
			t.Errorf("%s: the check passed", name)
		}
	}
}

// TestMessagesCheckNamesMissingEntry: a copy of the catalogue without one entry fails naming that
// source and code, for every entry; an empty catalogue fails. Control: the whole catalogue passes.
func TestMessagesCheckNamesMissingEntry(t *testing.T) {
	generated, configure := messagesSources(t)
	if err := messagesCheck(catalogue, generated, configure); err != nil {
		t.Fatalf("control: the whole catalogue: %v", err)
	}
	if err := messagesCheck(map[catalogueKey]catalogueEntry{}, generated, configure); err == nil {
		t.Error("an empty catalogue passed")
	}
	want, err := messagesWantKeys(generated, configure)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range messagesSortedKeys(want) {
		cut := maps.Clone(catalogue)
		delete(cut, key)
		named := fmt.Sprintf("(%s, %s)", key.source, key.code)
		if err := messagesCheck(cut, generated, configure); err == nil || !strings.Contains(err.Error(), named) {
			t.Errorf("without %s: %v", named, err)
		}
	}
}

// messagesWantExplained checks that err is explained as exactly the line of code, detail and the
// entry of source.
func messagesWantExplained(t *testing.T, err error, source, code, detail string) {
	t.Helper()
	line, xerr := ExplainError(err)
	entry := catalogue[catalogueKey{source, code}]
	if want := messagesLine(code, detail, entry); xerr != nil || line != want || entry.sentence == "" {
		t.Errorf("explain %v:\n got %q, %v\nwant %q", err, line, xerr, want)
	}
	if strings.Contains(line, "\n") || strings.Count(line, " Next: ") != 1 {
		t.Errorf("not one line with one Next: %q", line)
	}
}

// TestMessagesExplainAnswersRefusals: each invalid answers fixture, and the reader's two file
// refusals, print the line of their code, their detail and the configure entry (V003: a raw
// decoder error printed as is).
func TestMessagesExplainAnswersRefusals(t *testing.T) {
	var cases map[string]answersCase
	if err := json.Unmarshal(answersFixture(t, "invalid/cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no invalid cases")
	}
	for name, c := range cases {
		_, err := DecodeAnswers(answersFixture(t, filepath.Join("invalid", name)))
		messagesWantExplained(t, err, SourceConfigure, c.Code, c.Detail)
	}
	dir := t.TempDir()
	missing, over := filepath.Join(dir, "missing.yaml"), filepath.Join(dir, "over.yaml")
	if err := os.WriteFile(over, answersPadded(t, MaxAnswersBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, code := range map[string]string{missing: CodeAnswersNotRegular, over: CodeAnswersTooLarge} {
		_, err := ReadAnswers(path)
		messagesWantExplained(t, err, SourceConfigure, code, path)
	}
}

// TestMessagesExplainDerivationRefusals: each refused derivation of T017 prints the decoder's
// code and detail with the generated entry.
func TestMessagesExplainDerivationRefusals(t *testing.T) {
	for _, c := range exportGateCases {
		_, err := Export(exportGateAnswers(t, c.defect))
		refusal, ok := err.(*stacks.ManifestError)
		if !ok || refusal.Code != c.code {
			t.Fatalf("%s: want the decoder's %s, got %v", c.name, c.code, err)
		}
		messagesWantExplained(t, err, SourceGenerated, c.code, refusal.Detail)
	}
}

// TestMessagesSourcePicksEntry: UNKNOWN_FIELD from the answers decoder gets the spelling entry,
// from the generated manifest the tool-defect entry (G32: a lookup by code alone).
func TestMessagesSourcePicksEntry(t *testing.T) {
	_, answersErr := DecodeAnswers(answersFixture(t, "invalid/case-variant.yaml"))
	line, err := ExplainError(answersErr)
	if want := "configure: UNKNOWN_FIELD: Tenants. This key is not part of the answers file, or its case differs. " +
		"Next: Check the spelling against the how-to; keys are lower case with `_`."; err != nil || line != want {
		t.Errorf("answers UNKNOWN_FIELD:\n got %q, %v\nwant %q", line, err, want)
	}

	generated, err := Export(exportAnswers(t, "sandbox.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(generated), `"kind": "Deployments",`, `"kind": "Deployments", "colour": "blue",`, 1)
	_, manifestErr := stacks.DecodeManifest([]byte(edited))
	refusal, ok := manifestErr.(*stacks.ManifestError)
	if !ok || refusal.Code != stacks.CodeUnknownField {
		t.Fatalf("the edited manifest: want UNKNOWN_FIELD, got %v", manifestErr)
	}
	line, err = ExplainError(manifestErr)
	if want := "configure: UNKNOWN_FIELD: " + refusal.Detail + ". Unreachable: the tool built a manifest the decoder refuses. " +
		"This is a defect of the tool, not of your answers. Next: Report it with the answers file; " +
		"meanwhile edit `stacks/deployments.yaml` by hand."; err != nil || line != want {
		t.Errorf("generated UNKNOWN_FIELD:\n got %q, %v\nwant %q", line, err, want)
	}
}

// TestMessagesUnreachableBlameTheTool: every generated code outside the four the derivation
// reaches says it is a defect of the tool; the four and every configure code do not.
func TestMessagesUnreachableBlameTheTool(t *testing.T) {
	generated, configure := messagesSources(t)
	want, err := messagesWantKeys(generated, configure)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range messagesSortedKeys(want) {
		line, err := Explain(key.source, key.code, "detail")
		blames := key.source == SourceGenerated && !slices.Contains(messagesReachable, key.code)
		if err != nil || strings.Contains(line, messagesToolDefect) != blames {
			t.Errorf("(%s, %s): tool defect %v wanted; got %q, %v", key.source, key.code, blames, line, err)
		}
	}
}

// TestMessagesNoFallback: a wrapped or foreign error, an unknown code and an unknown source get an
// error and no line, never the raw text (G32: fall back to the raw decoder error). Controls: the
// same refusals unwrapped, and a known pair, explain.
func TestMessagesNoFallback(t *testing.T) {
	refusal := &AnswersError{Code: CodeMissingField, Detail: "org"}
	if line, err := ExplainError(refusal); err != nil || !strings.HasPrefix(line, "configure: MISSING_FIELD: org. ") {
		t.Errorf("control: %q, %v", line, err)
	}
	if line, err := Explain(SourceGenerated, stacks.CodeUnknownField, "x"); err != nil || line == "" {
		t.Errorf("control: %q, %v", line, err)
	}
	for name, err := range map[string]error{
		"wrapped answers error": fmt.Errorf("read answers file: %w", refusal),
		"wrapped manifest":      fmt.Errorf("current: %w", &stacks.ManifestError{Code: stacks.CodeUnknownField, Detail: "x"}),
		"foreign error":         errors.New("UNKNOWN_FIELD: x"),
		"nil":                   nil,
		"unknown answers code":  &AnswersError{Code: "NO_SUCH_CODE", Detail: "x"},
		"unknown manifest code": &stacks.ManifestError{Code: "ANSWERS_SYNTAX", Detail: "x"},
	} {
		if line, xerr := ExplainError(err); xerr == nil || line != "" {
			t.Errorf("%s: want an error and no line, got %q, %v", name, line, xerr)
		}
	}
	if line, err := Explain("manifest", CodeUnknownField, "x"); err == nil || line != "" {
		t.Errorf("unknown source: want an error and no line, got %q, %v", line, err)
	}
}

// TestMessagesOneLine: a detail with a newline (an answers path) is printed quoted, so the
// refusal stays one line. Control: a plain path is printed as is.
func TestMessagesOneLine(t *testing.T) {
	dir := t.TempDir()
	for path, shown := range map[string]string{
		filepath.Join(dir, "a\nb.yaml"): strconv.Quote(filepath.Join(dir, "a\nb.yaml")),
		filepath.Join(dir, "ab.yaml"):   filepath.Join(dir, "ab.yaml"),
	} {
		_, err := ReadAnswers(path)
		line, xerr := ExplainError(err)
		want := "configure: ANSWERS_NOT_REGULAR: " + shown + ". "
		if xerr != nil || !strings.HasPrefix(line, want) || strings.ContainsAny(line, "\n\r") {
			t.Errorf("%q: got %q, %v; want one line starting %q", path, line, xerr, want)
		}
	}
}
