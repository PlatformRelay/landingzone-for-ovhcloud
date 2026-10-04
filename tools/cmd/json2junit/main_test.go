package main

import (
	"bytes"
	"encoding/xml"
	"os"
	"strings"
	"testing"
)

const streams = "../../../tests/fixtures/tofu/streams/"

func convert(t *testing.T, name string, exit int) (int, suites, string) {
	t.Helper()
	stream, err := os.ReadFile(streams + name + ".jsonl")
	if err != nil {
		t.Fatalf("FIXTURE_MISSING: %v", err)
	}
	var out bytes.Buffer
	code := run(bytes.NewReader(stream), &out, exit)
	var parsed suites
	if err := xml.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JUnit XML: %v\n%s", err, out.String())
	}
	return code, parsed, out.String()
}

func TestPassingStreamRendersRuns(t *testing.T) {
	code, parsed, out := convert(t, "pass", 0)
	if code != 0 || len(parsed.Suites) != 1 {
		t.Fatalf("passing stream not rendered: code=%d\n%s", code, out)
	}
	suite := parsed.Suites[0]
	if suite.Tests != 2 || suite.Failures != 0 || suite.Errors != 0 || suite.Skipped != 0 || strings.Contains(out, "<failure") {
		t.Errorf("passing stream rendered with faults: %s", out)
	}
	if !strings.Contains(out, `name="greets"`) || !strings.Contains(out, `value="1.13.0"`) {
		t.Errorf("run or tool version missing: %s", out)
	}
}

// The JUnit view carries the observation's verdict, including faults tofu
// itself reports as success, and keeps the upstream diagnostic text.
func TestFaultsRenderAsFailures(t *testing.T) {
	for name, want := range map[string][]string{
		"cleanup": {"CLEANUP_FAILED", "local-exec provisioner error"},
		"zero":    {"ZERO_TESTS"},
		"fail":    {"TESTS_FAILED", "Test assertion failed", "unexpected greeting"},
		"skip":    {"SKIPPED_RUN", "<skipped"},
	} {
		t.Run(name, func(t *testing.T) {
			exit := 0
			if name == "fail" || name == "skip" {
				exit = 1
			}
			code, parsed, out := convert(t, name, exit)
			if code == 0 || len(parsed.Suites) != 1 || parsed.Suites[0].Failures == 0 {
				t.Errorf("BEHAVIORAL_RED: %s rendered as success: code=%d\n%s", name, code, out)
			}
			for _, text := range want {
				if !strings.Contains(out, text) {
					t.Errorf("BEHAVIORAL_RED: %s missing %q:\n%s", name, text, out)
				}
			}
		})
	}
}

// Warnings on a passing run are part of the report, not only of failures.
func TestWarningsOnPassingRunRendered(t *testing.T) {
	pass, err := os.ReadFile(streams + "pass.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.SplitAfter(pass, []byte("\n"))
	warning := []byte(`{"type":"diagnostic","diagnostic":{"severity":"warning","summary":"Deprecated attribute","detail":"use x instead"}}` + "\n")
	stream := bytes.Join([][]byte{lines[0], lines[1], lines[2], lines[3], warning, lines[4]}, nil)
	var out bytes.Buffer
	code := run(bytes.NewReader(stream), &out, 0)
	if code != 0 || !strings.Contains(out.String(), "Deprecated attribute") || !strings.Contains(out.String(), "use x instead") {
		t.Errorf("BEHAVIORAL_RED: warning on a passing run not rendered: code=%d\n%s", code, out.String())
	}
}

// After XML decoding, the observation case still holds tofu's complete
// diagnostic objects and cleanup records with their context.
func TestRawDiagnosticsSurviveDecoding(t *testing.T) {
	observationOut := func(parsed suites) string {
		for _, c := range parsed.Suites[0].Cases {
			if c.Class == "report" && c.Name == "observation" {
				return c.SystemOut
			}
		}
		return ""
	}
	_, parsed, _ := convert(t, "fail", 1)
	out := observationOut(parsed)
	for _, field := range []string{`"range"`, `"snippet"`, `"difference"`, "main.tftest.hcl/greets"} {
		if !strings.Contains(out, field) {
			t.Errorf("BEHAVIORAL_RED: decoded JUnit lost %s:\n%s", field, out)
		}
	}
	_, parsed, _ = convert(t, "cleanup", 0)
	out = observationOut(parsed)
	for _, field := range []string{"terraform_data.x", "main.tftest.hcl/applies", `"address"`} {
		if !strings.Contains(out, field) {
			t.Errorf("BEHAVIORAL_RED: decoded JUnit lost cleanup %s:\n%s", field, out)
		}
	}
}

func TestMalformedInputFails(t *testing.T) {
	var out bytes.Buffer
	if code := run(strings.NewReader("not json\n"), &out, 0); code == 0 || !strings.Contains(out.String(), "MALFORMED_STREAM") {
		t.Errorf("BEHAVIORAL_RED: malformed input accepted: code=%d %s", code, out.String())
	}
}
