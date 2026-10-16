package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The entry's wiring around the limit helpers (006 FR-013, SC-003; guard G22): runLimited with a
// fake systemd-run, a scope holder and a fake scope directory. The holder is this test binary
// (TestScopeHolderHelper), which runs the real holdScope, gate included, against the fake scope;
// the fake child marks that it ran.

// scopeFixture is one fake host: its systemd-run, its holder, its scope directory and the files
// the fakes leave behind.
type scopeFixture struct {
	host    scopeHost
	scope   string // the fake scope's cgroup directory
	started string // written by the fake systemd-run when it runs
	ran     string // written by the child once its hold on fd 3 ends
}

var scopeTestLimits = limits{memoryMax: 64 << 20, tasksMax: 32}

// Environment of the holder helper: its mode, and the fake scope it reports on.
const (
	holderHelperMode  = "LZ_T032_HOLDER_MODE"
	holderHelperScope = "LZ_T032_HOLDER_SCOPE"
)

func newScopeFixture(t *testing.T) *scopeFixture {
	t.Helper()
	dir := t.TempDir()
	fixture := &scopeFixture{scope: filepath.Join(dir, "scope"), started: filepath.Join(dir, "started"), ran: filepath.Join(dir, "ran")}
	systemdRun := filepath.Join(dir, "systemd-run")
	script := "#!/bin/sh\necho started > " + fixture.started + "\nwhile [ \"$1\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(systemdRun, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fixture.scope, 0700); err != nil {
		t.Fatal(err)
	}
	fixture.write(t, map[string]string{"memory.max": "67108864\n", "pids.max": "32\n", "memory.swap.max": "0\n",
		"memory.events": memoryEventsQuiet, "pids.events": pidsEventsQuiet, "cgroup.kill": ""})
	fixture.host = scopeHost{
		systemdRun:  systemdRun,
		holder:      []string{os.Args[0], "-test.run=^TestScopeHolderHelper$", "--"},
		lookPath:    func(name string) (string, error) { return name, nil },
		userManager: func() error { return nil },
		controllers: func() string { return "cpu memory pids\n" },
		// Like systemd-run, the fake is in its scope only once it runs.
		scopeDir: func(int, string) string {
			if !exists(fixture.started) {
				return ""
			}
			return fixture.scope
		},
		scopeWait: 2 * time.Second,
		poll:      5 * time.Millisecond,
	}
	fixture.holderMode("report")
	return fixture
}

// holderMode sets what the holder helper does: report (as production), vanish (report, then
// remove the events files, as a removed scope's files stop reading) or silent (report nothing).
func (fixture *scopeFixture) holderMode(mode string) {
	fixture.host.env = []string{"PATH=/usr/bin:/bin", holderHelperMode + "=" + mode, holderHelperScope + "=" + fixture.scope}
}

// TestScopeHolderHelper is not a test: it is the scope holder the runLimited tests start through
// their fake systemd-run (the helper-process pattern), and does nothing in a normal run.
func TestScopeHolderHelper(t *testing.T) {
	if mode := os.Getenv(holderHelperMode); mode != "" {
		os.Exit(fakeHolder(mode, os.Getenv(holderHelperScope), flag.Args()))
	}
}

func fakeHolder(mode, scope string, argv []string) int {
	if len(argv) < 1 {
		return 97
	}
	syscall.CloseOnExec(3)
	syscall.CloseOnExec(4)
	gate, report := os.NewFile(3, "scope-gate"), os.NewFile(4, "scope-report")
	if mode == "silent" {
		if opened, _ := gate.Read(make([]byte, 1)); opened != 1 {
			return 125
		}
		_ = exec.Command(argv[0], argv[1:]...).Run()
		return 0
	}
	code := holdScope(argv[0], argv[1:], gate, report, func() (string, error) { return scope, nil })
	if mode == "vanish" {
		_, _ = os.Remove(filepath.Join(scope, "memory.events")), os.Remove(filepath.Join(scope, "pids.events"))
	}
	return code
}

func (fixture *scopeFixture) write(t *testing.T, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(fixture.scope, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// child's arguments mark that it ran, then run body.
func (fixture *scopeFixture) child(body string) []string {
	return []string{"-c", "echo ran > " + fixture.ran + "; " + body}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (fixture *scopeFixture) run(t *testing.T, body string) (limitedRun, error, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	start := time.Now()
	outcome, err := runLimited(ctx, fixture.host, scopeTestLimits, "lz-offline-test", "/bin/sh", fixture.child(body), &stdout, &stderr)
	return outcome, err, time.Since(start)
}

// A host that cannot apply the limits is refused before systemd-run, let alone the child, starts.
func TestRunLimitedRefusesUnavailableHost(t *testing.T) {
	for _, row := range []struct {
		name   string
		change func(*scopeHost)
		reason string
	}{
		{"no-systemd-run", func(h *scopeHost) {
			h.lookPath = func(string) (string, error) { return "", errors.New("not found") }
		}, "no-systemd-run"},
		{"no-user-manager", func(h *scopeHost) { h.userManager = func() error { return errors.New("no bus") } }, "no-user-manager"},
		{"no-memory-controller", func(h *scopeHost) { h.controllers = func() string { return "cpu pids\n" } }, "no-memory-controller"},
		{"no-pids-controller", func(h *scopeHost) { h.controllers = func() string { return "cpu memory\n" } }, "no-pids-controller"},
	} {
		t.Run(row.name, func(t *testing.T) {
			fixture := newScopeFixture(t)
			row.change(&fixture.host)
			_, err, _ := fixture.run(t, "exit 0")
			if err == nil || !strings.HasPrefix(err.Error(), "LIMITS_UNAVAILABLE "+row.reason) {
				t.Fatalf("runLimited = %v, want LIMITS_UNAVAILABLE %s", err, row.reason)
			}
			if !strings.Contains(err.Error(), "enable user cgroup delegation") {
				t.Errorf("refusal %q names no next action", err)
			}
			if exists(fixture.started) || exists(fixture.ran) {
				t.Errorf("systemd-run or the child started on a host refused for %s", row.reason)
			}
		})
	}
	t.Run("available-control", func(t *testing.T) {
		fixture := newScopeFixture(t)
		outcome, err, _ := fixture.run(t, "exit 0")
		if err != nil || outcome.err != nil || outcome.tripped != "" || !exists(fixture.ran) {
			t.Fatalf("runLimited on an able host = %+v, %v; child ran %v", outcome, err, exists(fixture.ran))
		}
	})
}

// A started scope that does not carry the limits is ended while the child is still held: the
// child never runs, and the refusal is LIMITS_UNAVAILABLE limit-not-applied.
func TestRunLimitedRefusesUnappliedScope(t *testing.T) {
	files := func(files map[string]string) func(*testing.T, *scopeFixture) {
		return func(t *testing.T, fixture *scopeFixture) { fixture.write(t, files) }
	}
	for _, row := range []struct {
		name   string
		change func(*testing.T, *scopeFixture)
	}{
		{"memory-max-differs", files(map[string]string{"memory.max": "134217728\n"})},
		{"memory-unlimited", files(map[string]string{"memory.max": "max\n"})},
		{"pids-max-differs", files(map[string]string{"pids.max": "64\n"})},
		{"swap-allowed", files(map[string]string{"memory.swap.max": "max\n"})},
		{"never-in-scope", func(_ *testing.T, fixture *scopeFixture) {
			fixture.host.scopeDir = func(int, string) string { return "" }
			fixture.host.scopeWait = 200 * time.Millisecond
		}},
		{"events-unreadable", func(t *testing.T, fixture *scopeFixture) {
			if err := os.Remove(filepath.Join(fixture.scope, "memory.events")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			fixture := newScopeFixture(t)
			row.change(t, fixture)
			_, err, _ := fixture.run(t, "exit 0")
			if err == nil || !strings.HasPrefix(err.Error(), "LIMITS_UNAVAILABLE limit-not-applied") {
				t.Fatalf("runLimited = %v, want LIMITS_UNAVAILABLE limit-not-applied", err)
			}
			// The child is gone once runLimited returns; give a late start time to show.
			time.Sleep(200 * time.Millisecond)
			if exists(fixture.ran) {
				t.Errorf("the child ran in a scope without the limits")
			}
			if !exists(fixture.started) {
				t.Errorf("systemd-run never started: the row tested nothing")
			}
		})
	}
}

// writeAfter writes files into the fake scope after delay, while the child runs.
func (fixture *scopeFixture) writeAfter(delay time.Duration, files map[string]string) {
	go func() {
		time.Sleep(delay)
		for name, body := range files {
			_ = os.WriteFile(filepath.Join(fixture.scope, name), []byte(body), 0600)
		}
	}()
}

// killWritten returns what the entry wrote to the fake scope's cgroup.kill.
func (fixture *scopeFixture) killWritten() string {
	data, _ := os.ReadFile(filepath.Join(fixture.scope, "cgroup.kill"))
	return string(data)
}

// A tripped limit ends the scope while the child still runs, and names the limit; a fork storm
// that keeps retrying past pids.max is stopped, not left to the deadline.
func TestRunLimitedStopsAtLimit(t *testing.T) {
	for _, row := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"memory", map[string]string{"memory.events": memoryEventsKilled}, "memory"},
		{"tasks", map[string]string{"pids.events": pidsEventsHit}, "tasks"},
	} {
		t.Run(row.name, func(t *testing.T) {
			fixture := newScopeFixture(t)
			// The limit trips after the child started, and the child goes on as a retrying storm would.
			fixture.writeAfter(100*time.Millisecond, row.files)
			outcome, err, elapsed := fixture.run(t, "exec sleep 30")
			if err != nil {
				t.Fatalf("runLimited refused: %v", err)
			}
			if outcome.tripped != row.want {
				t.Errorf("tripped = %q, want %q", outcome.tripped, row.want)
			}
			if elapsed > 10*time.Second {
				t.Errorf("the child ran %v after the limit tripped, want it ended within 10 s", elapsed)
			}
			if got := fixture.killWritten(); got != "1" {
				t.Errorf("cgroup.kill = %q, want the whole scope killed", got)
			}
		})
	}
}

// Control for TestRunLimitedStopsAtLimit: an ordinary failure under quiet events names no limit
// and ends nothing.
func TestRunLimitedOrdinaryFailure(t *testing.T) {
	fixture := newScopeFixture(t)
	outcome, err, _ := fixture.run(t, "exit 3")
	if err != nil || outcome.tripped != "" || outcome.err == nil {
		t.Fatalf("an ordinary failure = %+v, %v; want no limit and the child's exit error", outcome, err)
	}
	if got := fixture.killWritten(); got != "" {
		t.Errorf("cgroup.kill = %q for a child no limit stopped", got)
	}
}

// The entry's verdict: RESOURCE_LIMIT with the limit and the target, ahead of the exit error.
func TestChildResult(t *testing.T) {
	failed := errors.New("exit status 1")
	for _, row := range []struct {
		name    string
		outcome limitedRun
		want    string
	}{
		{"memory", limitedRun{tripped: "memory", err: failed}, "RESOURCE_LIMIT memory target=test:static"},
		{"tasks", limitedRun{tripped: "tasks", err: failed}, "RESOURCE_LIMIT tasks target=test:static"},
		{"tasks-exit-zero", limitedRun{tripped: "tasks"}, "RESOURCE_LIMIT tasks target=test:static"},
		{"failed", limitedRun{err: failed}, "CHILD_FAILED: exit status 1"},
		{"passed", limitedRun{}, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			err := childResult("test:static", row.outcome)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != row.want {
				t.Errorf("childResult = %q, want %q", got, row.want)
			}
		})
	}
}

// Swap is off in the scope: on a host with free swap, a child past MemoryMax would otherwise be
// swapped out instead of stopped (evidence/T029.md, findings for T032).
func TestScopeArgvSwapOff(t *testing.T) {
	argv := scopeArgv("/usr/bin/systemd-run", "lz-offline-swap", scopeTestLimits, []string{"/usr/bin/bwrap"})
	_, properties, _, _ := scopeOptions(t, argv)
	if swap, ok := properties["MemorySwapMax"]; !ok || swap != "0" {
		t.Errorf("MemorySwapMax = %q (set %v) in %q, want 0", swap, ok, argv)
	}
}

// A limit hit just before the child ends counts although the scope's files no longer read once it
// is empty: the holder reports them from inside the scope. Without a report the run is not a pass.
func TestRunLimitedFinalEvents(t *testing.T) {
	t.Run("reported-after-the-scope-emptied", func(t *testing.T) {
		fixture := newScopeFixture(t)
		fixture.holderMode("vanish")
		fixture.host.poll = time.Hour // the watch reads once at the start and once at the end
		outcome, err, _ := fixture.run(t, "sleep 0.2; printf 'max 1\\n' > "+filepath.Join(fixture.scope, "pids.events")+"; exit 0")
		if err != nil || outcome.tripped != "tasks" {
			t.Fatalf("runLimited = %+v, %v; want RESOURCE_LIMIT tasks from the holder's report", outcome, err)
		}
		if exists(filepath.Join(fixture.scope, "pids.events")) {
			t.Fatalf("the events file is still there: the row tested nothing")
		}
	})
	t.Run("no-report-is-no-pass", func(t *testing.T) {
		fixture := newScopeFixture(t)
		fixture.holderMode("silent")
		outcome, err, _ := fixture.run(t, "exit 0")
		if err != nil || outcome.err == nil {
			t.Fatalf("runLimited = %+v, %v; want the run failed for want of final events", outcome, err)
		}
	})
}

// The holder passes the program's exit on, signals as 128 plus the signal, and reports the scope's
// events; a scope it cannot read is exit 125 with no report.
func TestHoldScope(t *testing.T) {
	scope := t.TempDir()
	for name, body := range map[string]string{"memory.events": memoryEventsKilled, "pids.events": pidsEventsQuiet} {
		if err := os.WriteFile(filepath.Join(scope, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		name    string
		script  string
		scopeOf func() (string, error)
		code    int
		report  string
	}{
		{"exit-code", "exit 3", func() (string, error) { return scope, nil }, 3, memoryEventsKilled + "\x00" + pidsEventsQuiet},
		{"signal", "kill -9 $$", func() (string, error) { return scope, nil }, 128 + 9, memoryEventsKilled + "\x00" + pidsEventsQuiet},
		{"scope-unreadable", "exit 0", func() (string, error) { return "", errors.New("not a cgroup v2 process") }, 125, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			hold, release, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			report, reportEnd, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer report.Close()
			_, _ = release.Write([]byte{1})
			_ = release.Close()
			code := holdScope("/bin/sh", []string{"-c", row.script}, hold, reportEnd, row.scopeOf)
			got, _ := io.ReadAll(report)
			if code != row.code || string(got) != row.report {
				t.Errorf("holdScope = %d with report %q, want %d with %q", code, got, row.code, row.report)
			}
		})
	}
}

// Only a single unified line naming the unit's scope is that scope.
func TestScopeFromCgroup(t *testing.T) {
	const path = "/user.slice/user-1000.slice/user@1000.service/app.slice/lz-offline-1-X.scope"
	for _, row := range []struct {
		name, data, want string
	}{
		{"unified", "0::" + path + "\n", "/sys/fs/cgroup" + path},
		{"no-newline", "0::" + path, "/sys/fs/cgroup" + path},
		{"other-unit", "0::/user.slice/user-1000.slice/user@1000.service/app.slice/lz-offline-1-Y.scope\n", ""},
		{"unit-as-prefix", "0::" + path + "-more.scope\n", ""},
		{"hybrid", "12:pids:/user.slice\n0::" + path + "\n", ""},
		{"hybrid-scope-only-in-v1", "0::/user.slice\n1:name=systemd:" + path + "\n", ""},
		{"not-yet-moved", "0::/user.slice/user-1000.slice/session-2.scope\n", ""},
		{"empty", "", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := scopeFromCgroup(row.data, "lz-offline-1-X"); got != row.want {
				t.Errorf("scopeFromCgroup(%q) = %q, want %q", row.data, got, row.want)
			}
		})
	}
}

// The holder starts the program only after a byte at its gate: a gate closed without one (the
// entry refused the scope) runs nothing and reports nothing.
func TestHoldScopeGateClosed(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	gate, release, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	report, reportEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer report.Close()
	_ = release.Close()
	code := holdScope("/bin/sh", []string{"-c", "echo ran > " + ran}, gate, reportEnd, func() (string, error) { return t.TempDir(), nil })
	got, _ := io.ReadAll(report)
	if code != 125 || len(got) != 0 || exists(ran) {
		t.Errorf("holdScope at a closed gate = %d with report %q, program ran %v; want 125, nothing, not run", code, got, exists(ran))
	}
}
