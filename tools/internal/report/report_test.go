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

func TestReportRejects(t *testing.T) {
	pass, passMeta := fixture(t, "pass")
	withoutSummary := bytes.Join(bytes.SplitN(pass, []byte("\n"), -1)[:4], []byte("\n"))
	cases := map[string]struct {
		stream []byte
		exit   int
		reason string
	}{}
	for _, name := range []string{"fail", "skip", "zero", "cleanup", "truncated"} {
		stream, m := fixture(t, name)
		cases[name] = struct {
			stream []byte
			exit   int
			reason string
		}{stream, m.EntryExit, ""}
	}
	// Derived cases: the passing stream with a crash exit status, without its
	// summary, empty, and not JSON at all.
	cases["crash-exit"] = struct {
		stream []byte
		exit   int
		reason string
	}{pass, 11, ""}
	cases["missing-summary"] = struct {
		stream []byte
		exit   int
		reason string
	}{append(withoutSummary, '\n'), passMeta.EntryExit, ""}
	cases["empty"] = struct {
		stream []byte
		exit   int
		reason string
	}{nil, 0, ""}
	cases["malformed"] = struct {
		stream []byte
		exit   int
		reason string
	}{[]byte("panic: runtime error\n"), 0, ""}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			o := Evaluate(c.stream, c.exit)
			if o.Status != Fail || len(o.Reasons) == 0 {
				t.Errorf("BEHAVIORAL_RED: %s accepted: %+v", name, o)
			}
		})
	}
}
