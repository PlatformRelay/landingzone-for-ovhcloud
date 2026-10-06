//go:build offlinetools

// The unit runner drives the real pinned tofu, which exists only inside the
// offline entry (/tcb). Streams tofu cannot be made to emit (a run killed
// mid-write, a crash) come from a fake tofu that replays the report adapter's
// captured streams (tests/fixtures/tofu/streams, spec 001 T005).
package checks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const (
	unitFixtures = "../../../tests/check/fixtures/unit/"
	tofuStreams  = "../../../tests/fixtures/tofu/streams/"
)

func unitFixture(t *testing.T, name string) string {
	t.Helper()
	return copyFixture(t, unitFixtures, name)
}

// unitKept joins everything the observation keeps from upstream: init output and
// the adapter's diagnostics.
func unitKept(o UnitObservation) string {
	parts := append(append([]string{}, o.Init.Files...), o.Init.Messages...)
	if o.Report != nil {
		for _, d := range o.Report.Diagnostics {
			parts = append(parts, d.Summary, d.Detail)
		}
	}
	return strings.Join(parts, "\n")
}

func TestUnitPass(t *testing.T) {
	tools := pinnedTools(t)
	requireTools(t, tools)
	o := RunUnit(tools, unitFixture(t, "pass"))
	if !o.Pass || o.Status != StatusPass || o.Reason != "" || o.Init.Status != StatusPass {
		t.Fatalf("BEHAVIORAL_RED: passing tests rejected: %+v", o)
	}
	if r := o.Report; r == nil || r.Status != "pass" || r.ToolVersion != "1.13.0" || r.Discovered != 1 || r.Passed != 1 || len(r.Reasons) != 0 {
		t.Errorf("BEHAVIORAL_RED: pass not backed by the adapter's observation of one passing run: %+v", r)
	}
}

// Every way a directory's tests can fail to prove anything is a fail, with
// the adapter's reason and the upstream detail kept.
func TestUnitRejected(t *testing.T) {
	tools := pinnedTools(t)
	requireTools(t, tools)
	for name, c := range map[string]struct {
		reason string // adapter reason that must be reported
		detail string // upstream text that must be kept
	}{
		"fail": {"TESTS_FAILED", "unexpected greeting"},
		// tofu 1.13.0 exits 0 and reports pass for zero tests.
		"zero": {"ZERO_TESTS", ""},
		"skip": {"SKIPPED_RUN", "Reference to undeclared resource"},
	} {
		t.Run(name, func(t *testing.T) {
			o := RunUnit(tools, unitFixture(t, name))
			if o.Pass || o.Status != StatusFail || o.Reason != "TEST_REPORT_FAILED" || o.Init.Status != StatusPass {
				t.Fatalf("BEHAVIORAL_RED: %s accepted or misreported: %+v", name, o)
			}
			if o.Report == nil || o.Report.Status != "fail" || !slices.Contains(o.Report.Reasons, c.reason) {
				t.Errorf("BEHAVIORAL_RED: %s: adapter reason %s missing: %+v", name, c.reason, o.Report)
			}
			if !strings.Contains(unitKept(o), c.detail) {
				t.Errorf("BEHAVIORAL_RED: %s lost %q: %+v", name, c.detail, o)
			}
		})
	}
}

// A test file that does not parse stops at the mirror-only init; tofu test
// is not run and the parse error is kept.
func TestUnitMalformed(t *testing.T) {
	tools := pinnedTools(t)
	requireTools(t, tools)
	o := RunUnit(tools, unitFixture(t, "malformed"))
	if o.Pass || o.Status != StatusFail || o.Reason != "INIT_FAILED" || o.Init.Status != StatusFail || o.Report != nil {
		t.Fatalf("BEHAVIORAL_RED: malformed tests accepted or misreported: %+v", o)
	}
	if kept := unitKept(o); !strings.Contains(kept, "Invalid expression") || !strings.Contains(kept, "main.tftest.hcl") {
		t.Errorf("BEHAVIORAL_RED: parse error lost: %q", kept)
	}
}

// fakeTofu writes a tofu stand-in that logs its working directory and
// arguments, replays stream with exit code on test and succeeds silently on
// anything else (init, or a call T004 may add such as version).
func fakeTofu(t *testing.T, stream []byte, code int) (tofu, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	replay := filepath.Join(dir, "stream.jsonl")
	if err := os.WriteFile(replay, stream, 0o644); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s %%s\n' "$PWD" "$*" >> '%s'
for a in "$@"; do
  case $a in
    init) exit 0 ;;
    test) cat '%s'; exit %d ;;
  esac
done
`, log, replay, code)
	tofu = filepath.Join(dir, "tofu")
	if err := os.WriteFile(tofu, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return tofu, log
}

func capturedStream(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(tofuStreams + name + ".jsonl")
	if err != nil {
		t.Fatalf("FIXTURE_MISSING: %v", err)
	}
	return data
}

// The runner judges tofu's own stream and exit status: a captured passing
// stream passes; a stream with a non-JSON line, one cut mid-write or a crash
// without output fails.
func TestUnitStream(t *testing.T) {
	pass := capturedStream(t, "pass")
	first := bytes.IndexByte(pass, '\n') + 1
	garbled := append(append(append([]byte{}, pass[:first]...), "panic: not a JSON event\n"...), pass[first:]...)
	for name, c := range map[string]struct {
		stream  []byte
		code    int
		pass    bool
		reasons []string
	}{
		"captured pass": {pass, 0, true, nil},
		"malformed":     {garbled, 0, false, []string{"MALFORMED_STREAM"}},
		"truncated":     {capturedStream(t, "truncated"), 0, false, []string{"TRUNCATED_STREAM"}},
		"crash":         {nil, 2, false, []string{"EMPTY_STREAM", "NONZERO_EXIT"}},
	} {
		t.Run(name, func(t *testing.T) {
			tofu, _ := fakeTofu(t, c.stream, c.code)
			o := RunUnit(StaticTools{Tofu: tofu}, unitFixture(t, "pass"))
			status, reason := StatusFail, "TEST_REPORT_FAILED"
			if c.pass {
				status, reason = StatusPass, ""
			}
			if o.Pass != c.pass || o.Status != status || o.Reason != reason || o.Init.Status != StatusPass || o.Report == nil {
				t.Fatalf("BEHAVIORAL_RED: %s: pass=%v, want %v: %+v", name, o.Pass, c.pass, o)
			}
			for _, r := range c.reasons {
				if !slices.Contains(o.Report.Reasons, r) {
					t.Errorf("BEHAVIORAL_RED: %s: reason %s missing: %v", name, r, o.Report.Reasons)
				}
			}
		})
	}
}

// init is mirror-only and precedes tofu test -json, both in the directory.
func TestUnitInvocation(t *testing.T) {
	tofu, log := fakeTofu(t, capturedStream(t, "pass"), 0)
	dir := unitFixture(t, "pass")
	RunUnit(StaticTools{Tofu: tofu}, dir)
	data, _ := os.ReadFile(log)
	calls := lines(string(data))
	initCall := slices.IndexFunc(calls, func(c string) bool { return slices.Contains(strings.Fields(c), "init") })
	testCall := slices.IndexFunc(calls, func(c string) bool { return slices.Contains(strings.Fields(c), "test") })
	if initCall < 0 || testCall < 0 || initCall > testCall {
		t.Fatalf("BEHAVIORAL_RED: want init then test, got %q", calls)
	}
	for _, flag := range []string{"-backend=false", "-lockfile=readonly"} {
		if !slices.Contains(strings.Fields(calls[initCall]), flag) {
			t.Errorf("BEHAVIORAL_RED: init not mirror-only, %s missing: %q", flag, calls[initCall])
		}
	}
	if !slices.Contains(strings.Fields(calls[testCall]), "-json") {
		t.Errorf("BEHAVIORAL_RED: test not machine-readable: %q", calls[testCall])
	}
	for _, c := range []string{calls[initCall], calls[testCall]} {
		if !strings.Contains(c, dir) {
			t.Errorf("BEHAVIORAL_RED: call not in %s: %q", dir, c)
		}
	}
}

// A missing tofu blocks; it is never a pass or a red.
func TestUnitToolAbsent(t *testing.T) {
	o := RunUnit(StaticTools{Tofu: filepath.Join(t.TempDir(), "tofu")}, unitFixture(t, "pass"))
	if o.Pass || o.Status != StatusBlocked || o.Reason != "TOOL_ABSENT" || o.Report != nil {
		t.Errorf("BEHAVIORAL_RED: absent tofu not blocked: %+v", o)
	}
}

// The slice runs lint and unit on every listed directory without stopping at
// the first failure; an empty list never passes.
func TestUnitSlice(t *testing.T) {
	tools := pinnedTools(t)
	requireTools(t, tools)
	for name, dirs := range map[string][]string{"nil": nil, "empty": {}} {
		o := RunSlice(tools, dirs)
		if o.Pass || o.Status != StatusFail || o.Reason != "NO_DISCOVERY" || len(o.Entries) != 0 {
			t.Errorf("BEHAVIORAL_RED: %s directory list accepted: %+v", name, o)
		}
	}
	pass := unitFixture(t, "pass")
	o := RunSlice(tools, []string{pass})
	if !o.Pass || o.Status != StatusPass || len(o.Entries) != 1 || !o.Entries[0].Static.Pass || !o.Entries[0].Unit.Pass {
		t.Errorf("BEHAVIORAL_RED: passing slice rejected: %+v", o)
	}
	fail, zero := unitFixture(t, "fail"), unitFixture(t, "zero")
	o = RunSlice(tools, []string{pass, fail, zero})
	var dirs []string
	var units []bool
	for _, e := range o.Entries {
		dirs = append(dirs, e.Dir)
		units = append(units, e.Unit.Pass)
	}
	if o.Pass || o.Status != StatusFail || !reflect.DeepEqual(dirs, []string{pass, fail, zero}) || !reflect.DeepEqual(units, []bool{true, false, false}) {
		t.Errorf("BEHAVIORAL_RED: failing slice accepted or cut short: pass=%v dirs=%v units=%v", o.Pass, dirs, units)
	}
	// Passing tests do not excuse a lint failure: the pass module, unformatted.
	unformatted := unitFixture(t, "pass")
	source, err := os.ReadFile(filepath.Join(unformatted, "main.tf"))
	if err != nil {
		t.Fatal(err)
	}
	misaligned := strings.Replace(string(source), "type        = string", "type = string", 1)
	if misaligned == string(source) {
		t.Fatal("FIXTURE_INVALID: pass/main.tf no longer has the aligned type line")
	}
	if err := os.WriteFile(filepath.Join(unformatted, "main.tf"), []byte(misaligned), 0o644); err != nil {
		t.Fatal(err)
	}
	o = RunSlice(tools, []string{unformatted})
	if o.Pass || o.Status != StatusFail || len(o.Entries) != 1 || o.Entries[0].Static.Pass || !o.Entries[0].Unit.Pass {
		t.Errorf("BEHAVIORAL_RED: lint failure with passing tests accepted: %+v", o)
	}
}
