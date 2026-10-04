package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestReportPassingStream(t *testing.T) {
	if o := evaluateFixture(t, "pass"); o.Status != Pass {
		t.Fatalf("valid passing stream rejected: %+v", o)
	}
}

// Unusual but valid: an event type the adapter does not know is ignored.
func TestReportUnusualValidStream(t *testing.T) {
	stream, m := fixture(t, "pass")
	extra := []byte(`{"@level":"info","@message":"future event","type":"future_event"}` + "\n")
	if o := Evaluate(append(append([]byte{}, extra...), stream...), m.EntryExit); o.Status != Pass {
		t.Fatalf("unknown event type rejected a valid stream: %+v", o)
	}
}

func TestReportCounts(t *testing.T) {
	o := evaluateFixture(t, "pass")
	if o.ToolVersion != "1.13.0" || o.Discovered != 1 || o.Passed != 1 || o.Failed+o.Errored+o.Skipped != 0 {
		t.Errorf("BEHAVIORAL_RED: passing stream counts not reported: %+v", o)
	}
}

// C003.1: a failure keeps tofu's own diagnostic.
func TestReportPreservesDiagnostics(t *testing.T) {
	o := evaluateFixture(t, "fail")
	found := false
	for _, d := range o.Diagnostics {
		if d.Severity == "error" && d.Summary == "Test assertion failed" && strings.Contains(d.Detail, "unexpected greeting") {
			found = true
		}
	}
	if o.Status != Fail || !found {
		t.Errorf("BEHAVIORAL_RED: failing stream lost its verdict or diagnostic: %+v", o)
	}
}

// Each case must fail for its own clause, so removing any one rule turns its
// case red even when the stream also trips another rule.
func TestReportRejects(t *testing.T) {
	pass, passMeta := fixture(t, "pass")
	lines := bytes.SplitAfter(pass, []byte("\n"))
	join := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	type rejection struct {
		stream []byte
		exit   int
		reason string
	}
	cases := map[string]rejection{}
	for name, reason := range map[string]string{
		"fail": ReasonFailed, "skip": ReasonSkipped, "zero": ReasonZeroTests,
		"cleanup": ReasonCleanupFailed, "truncated": ReasonTruncated,
	} {
		stream, m := fixture(t, name)
		cases[name] = rejection{stream, m.EntryExit, reason}
	}
	// Derived from the captured passing stream: lines 0 version, 1 abstract,
	// 2 file, 3 run, 4 summary.
	errorDiagnostic := []byte(`{"@level":"error","@message":"Error: synthetic","diagnostic":{"severity":"error","summary":"synthetic","detail":"synthetic"},"type":"diagnostic"}` + "\n")
	twoRuns := bytes.Replace(lines[1], []byte(`["greets"]`), []byte(`["greets","other"]`), 1)
	cases["crash-exit"] = rejection{pass, 11, ReasonExitStatus}
	cases["missing-summary"] = rejection{join(lines[0], lines[1], lines[2], lines[3]), passMeta.EntryExit, ReasonMissingSummary}
	cases["missing-version"] = rejection{join(lines[1:]...), passMeta.EntryExit, ReasonMissingVersion}
	cases["error-diagnostic"] = rejection{join(lines[0], lines[1], lines[2], lines[3], errorDiagnostic, lines[4]), passMeta.EntryExit, ReasonErrorDiagnostic}
	cases["count-mismatch"] = rejection{join(lines[0], twoRuns, lines[2], lines[3], lines[4]), passMeta.EntryExit, ReasonCountMismatch}
	cases["empty"] = rejection{nil, 0, ReasonEmpty}
	cases["malformed"] = rejection{[]byte("panic: runtime error\n"), 0, ReasonMalformed}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			o := Evaluate(c.stream, c.exit)
			found := false
			for _, reason := range o.Reasons {
				found = found || reason == c.reason
			}
			if o.Status != Fail || !found {
				t.Errorf("BEHAVIORAL_RED: %s not rejected for %s: %s", name, c.reason, o)
			}
		})
	}
}
