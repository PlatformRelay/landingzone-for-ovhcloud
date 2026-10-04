package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Streams captured through the offline entry from tests/fixtures/tofu/modules
// with pinned tofu 1.13.0; each sidecar records the entry's exit status.
const streams = "../../../tests/fixtures/tofu/streams"

type meta struct {
	EntryExit   int    `json:"entry_exit"`
	ToolVersion string `json:"tool_version"`
}

func fixture(t *testing.T, name string) ([]byte, meta) {
	t.Helper()
	stream, err := os.ReadFile(filepath.Join(streams, name+".jsonl"))
	if err != nil {
		t.Fatalf("FIXTURE_MISSING: %v", err)
	}
	var m meta
	if data, err := os.ReadFile(filepath.Join(streams, name+".meta.json")); err == nil {
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("FIXTURE_META: %v", err)
		}
	}
	return stream, m
}

func evaluateFixture(t *testing.T, name string) Observation {
	t.Helper()
	stream, m := fixture(t, name)
	return Evaluate(stream, m.EntryExit)
}

// Synthetic events in tofu 1.13.0's shapes, for controls that isolate one
// guard. The captured fixtures remain the evidence of genuine tool output.
const (
	version  = `{"type":"version","tofu":"1.13.0"}`
	abstract = `{"type":"test_abstract","test_abstract":{"a.tftest.hcl":["r1"]}}`
	fileA    = `{"type":"test_file","test_file":{"path":"a.tftest.hcl","status":"pass"}}`
	runPass  = `{"type":"test_run","test_run":{"path":"a.tftest.hcl","run":"r1","status":"pass"}}`
	sumPass  = `{"type":"test_summary","test_summary":{"status":"pass","passed":1,"failed":0,"errored":0,"skipped":0}}`
)

func stream(lines ...string) []byte { return []byte(strings.Join(lines, "\n") + "\n") }

func TestReportPassingStream(t *testing.T) {
	o := evaluateFixture(t, "pass")
	if o.Status != Pass || o.ToolVersion != "1.13.0" || o.Discovered != 1 || o.Passed != 1 || o.Failed+o.Errored+o.Skipped != 0 {
		t.Fatalf("valid passing stream rejected or miscounted: %s", o)
	}
	if o := Evaluate(stream(version, abstract, fileA, runPass, sumPass), 0); o.Status != Pass {
		t.Fatalf("synthetic baseline rejected: %s", o)
	}
}

// Valid but unusual streams must still pass.
func TestReportUnusualValidStreams(t *testing.T) {
	pass, _ := fixture(t, "pass")
	warning := `{"type":"diagnostic","diagnostic":{"severity":"warning","summary":"long warning","detail":"` + strings.Repeat("x", 1<<20) + `"}}`
	twoFiles := stream(version,
		`{"type":"test_abstract","test_abstract":{"a.tftest.hcl":["r1"],"b.tftest.hcl":["r1"]}}`,
		fileA, runPass,
		`{"type":"test_file","test_file":{"path":"b.tftest.hcl","status":"pass"}}`,
		`{"type":"test_run","test_run":{"path":"b.tftest.hcl","run":"r1","status":"pass"}}`,
		`{"type":"test_summary","test_summary":{"status":"pass","passed":2,"failed":0,"errored":0,"skipped":0}}`)
	for name, s := range map[string][]byte{
		"unknown-event":     append([]byte(`{"type":"future_event","@message":"new"}`+"\n"), pass...),
		"typeless-log-line": stream(version, abstract, fileA, runPass, `{"@message":"Writing state to file"}`, sumPass),
		"crlf":              bytes.ReplaceAll(pass, []byte("\n"), []byte("\r\n")),
		"no-final-newline":  bytes.TrimSuffix(pass, []byte("\n")),
		"multiple-files":    twoFiles,
		"long-warning":      stream(version, abstract, fileA, runPass, warning, sumPass),
	} {
		t.Run(name, func(t *testing.T) {
			if o := Evaluate(s, 0); o.Status != Pass {
				t.Errorf("valid %s stream rejected: %s", name, o)
			}
		})
	}
}

// C003.1: tofu's complete diagnostic, its run context and cleanup details
// are kept, including warnings on a passing run.
func TestReportPreservesDiagnostics(t *testing.T) {
	o := evaluateFixture(t, "fail")
	if o.Status != Fail || len(o.Diagnostics) == 0 {
		t.Fatalf("BEHAVIORAL_RED: failing stream lost its verdict or diagnostics: %s", o)
	}
	d := o.Diagnostics[0]
	if d.Severity != "error" || d.Summary != "Test assertion failed" || d.Detail != "unexpected greeting" || d.File != "main.tftest.hcl" || d.Run != "greets" {
		t.Errorf("BEHAVIORAL_RED: diagnostic fields or context lost: %+v", d)
	}
	for _, field := range []string{`"range"`, `"snippet"`, `"difference"`} {
		if !bytes.Contains(d.Raw, []byte(field)) {
			t.Errorf("BEHAVIORAL_RED: raw diagnostic lost %s", field)
		}
	}
	cleanup := evaluateFixture(t, "cleanup")
	if len(cleanup.Cleanup) != 1 || !bytes.Contains(cleanup.Cleanup[0], []byte("terraform_data.x")) {
		t.Errorf("BEHAVIORAL_RED: cleanup failure details lost: %v", cleanup.Cleanup)
	}
	warning := `{"type":"diagnostic","diagnostic":{"severity":"warning","summary":"deprecated","address":"x"}}`
	if o := Evaluate(stream(version, abstract, fileA, runPass, warning, sumPass), 0); o.Status != Pass || len(o.Diagnostics) != 1 || !bytes.Contains(o.Diagnostics[0].Raw, []byte(`"address"`)) {
		t.Errorf("BEHAVIORAL_RED: warning on a passing run lost: %s %+v", o, o.Diagnostics)
	}
}

// Captured fault streams fail with at least their own reason.
func TestReportCapturedFaults(t *testing.T) {
	for name, reason := range map[string]string{
		"fail": ReasonFailed, "skip": ReasonSkipped, "zero": ReasonZeroTests,
		"cleanup": ReasonCleanupFailed, "truncated": ReasonTruncated,
	} {
		t.Run(name, func(t *testing.T) {
			o := evaluateFixture(t, name)
			found := false
			for _, r := range o.Reasons {
				found = found || r == reason
			}
			if o.Status != Fail || !found {
				t.Errorf("BEHAVIORAL_RED: %s not rejected for %s: %s", name, reason, o)
			}
		})
	}
}

// Each guard has controls that trip it and nothing else, so removing any one
// guard turns its controls red.
func TestReportIsolatedGuards(t *testing.T) {
	pass, _ := fixture(t, "pass")
	runFail := `{"type":"test_run","test_run":{"path":"a.tftest.hcl","run":"r1","status":"fail"}}`
	fileFail := `{"type":"test_file","test_file":{"path":"a.tftest.hcl","status":"fail"}}`
	sumFail := `{"type":"test_summary","test_summary":{"status":"fail","passed":0,"failed":1,"errored":0,"skipped":0}}`
	twoRuns := `{"type":"test_abstract","test_abstract":{"a.tftest.hcl":["r1","r2"]}}`
	runSkip := `{"type":"test_run","test_run":{"path":"a.tftest.hcl","run":"r2","status":"skip"}}`
	sumSkip := `{"type":"test_summary","test_summary":{"status":"pass","passed":1,"failed":0,"errored":0,"skipped":1}}`
	errorDiag := `{"type":"diagnostic","diagnostic":{"severity":"error","summary":"synthetic"}}`
	cleanup := `{"type":"test_cleanup","test_cleanup":{"failed_resources":[{"instance":"terraform_data.x"}]}}`
	zeroAbstract := `{"type":"test_abstract","test_abstract":{}}`
	zeroSummary := `{"type":"test_summary","test_summary":{"status":"pass","passed":0,"failed":0,"errored":0,"skipped":0}}`
	complete := stream(version, abstract, fileA, runPass, sumPass)
	sumMiscount := `{"type":"test_summary","test_summary":{"status":"pass","passed":0,"failed":0,"errored":0,"skipped":0}}`
	// Each case lists every reason it must produce. Each guard has at least one
	// case where it is the only reason; some faults inherently imply others
	// (a payload-rejected summary is also a missing summary).
	cases := map[string]struct {
		stream  []byte
		exit    int
		reasons []string
	}{
		"empty":                {nil, 0, []string{ReasonEmpty}},
		"truncated":            {append(append([]byte{}, complete...), `{"@message":"Writ`...), 0, []string{ReasonTruncated}},
		"malformed-line":       {stream(version, abstract, fileA, runPass, "panic: runtime error", sumPass), 0, []string{ReasonMalformed}},
		"empty-cleanup":        {stream(version, abstract, fileA, runPass, `{"type":"test_cleanup"}`, sumPass), 0, []string{ReasonPayload}},
		"negative-count":       {stream(version, abstract, fileA, runPass, `{"type":"test_summary","test_summary":{"status":"pass","passed":2,"failed":-1,"errored":0,"skipped":0}}`), 0, []string{ReasonPayload, ReasonMissingSummary}},
		"unknown-run-status":   {stream(version, abstract, fileA, `{"type":"test_run","test_run":{"path":"a.tftest.hcl","run":"r1","status":"unknown"}}`, sumPass), 0, []string{ReasonPayload, ReasonRunMismatch}},
		"summary-before-run":   {stream(version, abstract, fileA, sumPass, runPass), 0, []string{ReasonLifecycle}},
		"duplicate-summary":    {stream(version, abstract, fileA, runPass, sumPass, sumPass), 0, []string{ReasonLifecycle}},
		"version-after-start":  {stream(version, abstract, version, fileA, runPass, sumPass), 0, []string{ReasonLifecycle}},
		"missing-version":      {stream(abstract, fileA, runPass, sumPass), 0, []string{ReasonMissingVersion}},
		"missing-summary":      {stream(version, abstract, fileA, runPass), 0, []string{ReasonMissingSummary}},
		"missing-run":          {stream(version, abstract, fileA, sumMiscount), 0, []string{ReasonRunMismatch}},
		"duplicate-run":        {stream(version, abstract, fileA, runPass, runPass, sumPass), 0, []string{ReasonRunMismatch}},
		"summary-miscount":     {stream(version, abstract, fileA, runPass, sumMiscount), 0, []string{ReasonRunMismatch}},
		"summary-hides-fail":   {stream(version, abstract, fileFail, runFail, sumPass), 0, []string{ReasonFailed, ReasonRunMismatch}},
		"file-status-disagree": {stream(version, abstract, fileFail, runPass, sumPass), 0, []string{ReasonRunMismatch}},
		"crash-exit":           {pass, 11, []string{ReasonExitStatus}},
		"zero-tests":           {stream(version, zeroAbstract, zeroSummary), 0, []string{ReasonZeroTests}},
		"failed-run":           {stream(version, abstract, fileFail, runFail, sumFail), 0, []string{ReasonFailed}},
		"skipped-run":          {stream(version, twoRuns, fileA, runPass, runSkip, sumSkip), 0, []string{ReasonSkipped}},
		"error-diagnostic":     {stream(version, abstract, fileA, runPass, errorDiag, sumPass), 0, []string{ReasonErrorDiagnostic}},
		"cleanup-failure":      {stream(version, abstract, fileA, runPass, cleanup, sumPass), 0, []string{ReasonCleanupFailed}},
		// One control per remaining structural guard site.
		"oversized-line":          {append(append([]byte{}, complete...), stream(`{"@message":"`+strings.Repeat("x", maxLine)+`"}`)...), 0, []string{ReasonMalformed}},
		"version-without-tofu":    {stream(`{"type":"version"}`, abstract, fileA, runPass, sumPass), 0, []string{ReasonPayload}},
		"duplicate-abstract":      {stream(version, abstract, abstract, fileA, runPass, sumPass), 0, []string{ReasonLifecycle}},
		"abstract-without-body":   {stream(version, `{"type":"test_abstract"}`, fileA, runPass, sumPass), 0, []string{ReasonPayload, ReasonRunMismatch, ReasonZeroTests}},
		"duplicate-run-name":      {stream(version, `{"type":"test_abstract","test_abstract":{"a.tftest.hcl":["r1","r1"]}}`, fileA, runPass, sumPass), 0, []string{ReasonPayload}},
		"file-before-abstract":    {stream(version, fileA, abstract, runPass, sumPass), 0, []string{ReasonLifecycle}},
		"invalid-file-status":     {stream(version, abstract, `{"type":"test_file","test_file":{"path":"a.tftest.hcl","status":"weird"}}`, fileA, runPass, sumPass), 0, []string{ReasonPayload}},
		"duplicate-file":          {stream(version, abstract, fileA, fileA, runPass, sumPass), 0, []string{ReasonLifecycle}},
		"run-without-file":        {stream(version, abstract, runPass, sumPass), 0, []string{ReasonLifecycle}},
		"unexpected-run":          {stream(version, abstract, fileA, runPass, `{"type":"test_run","test_run":{"path":"a.tftest.hcl","run":"r9","status":"pass"}}`, `{"type":"test_summary","test_summary":{"status":"pass","passed":2,"failed":0,"errored":0,"skipped":0}}`), 0, []string{ReasonRunMismatch}},
		"diagnostic-no-summary":   {stream(version, abstract, fileA, runPass, `{"type":"diagnostic","diagnostic":{"severity":"error"}}`, sumPass), 0, []string{ReasonPayload}},
		"summary-before-abstract": {stream(version, sumMiscount), 0, []string{ReasonLifecycle, ReasonZeroTests}},
	}
	sole := map[string]bool{}
	for name, c := range cases {
		want := append([]string{}, c.reasons...)
		sort.Strings(want)
		if len(want) == 1 {
			sole[want[0]] = true
		}
		t.Run(name, func(t *testing.T) {
			o := Evaluate(c.stream, c.exit)
			if o.Status != Fail || !reflect.DeepEqual(o.Reasons, want) {
				t.Errorf("BEHAVIORAL_RED: %s must fail for exactly %v: %s", name, want, o)
			}
		})
	}
	for _, reason := range Reasons {
		if !sole[reason] {
			t.Errorf("guard %s has no control where it is the only reason", reason)
		}
	}
}
