//go:build hostlimits

// The host limit test (006 FR-013, premise P2) needs a host with systemd, a user manager and
// bwrap. It runs on the workstation, outside the entry and without credentials, never by default:
//
//	go -C tools test -tags hostlimits -run TestHostLimits ./cmd/lz-offline
//
// It composes the entry's limit steps (limitsUnavailable, scopeArgv, checkScope, limitTripped)
// around a bwrap sandbox shaped like the entry's and runs this test binary inside it as the child.
// The limits are small test values, so a child that runs without them stays harmless.

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const hostChildMode = "LZ_HOSTLIMITS_CHILD"

var hostLimits = limits{memoryMax: 64 << 20, tasksMax: 32}

// unitCount makes each unit name new, also across -count iterations in one process.
var unitCount int

// TestMain turns this binary into the sandboxed child when the host test starts it.
func TestMain(m *testing.M) {
	if mode := os.Getenv(hostChildMode); mode != "" {
		os.Exit(hostChild(mode))
	}
	os.Exit(m.Run())
}

// hostChild is the candidate's behaviour inside the sandbox. It reports how far it got on stdout.
func hostChild(mode string) int {
	switch mode {
	case "valid":
		// A quarter of each limit: well inside the scope.
		touch(hostLimits.memoryMax / 4)
		sleepers := make([]*exec.Cmd, 0, hostLimits.tasksMax/4)
		for range hostLimits.tasksMax / 4 {
			sleeper := exec.Command("sleep", "1")
			if err := sleeper.Start(); err != nil {
				fmt.Println("FORK_REFUSED", err)
				return 3
			}
			sleepers = append(sleepers, sleeper)
		}
		for _, sleeper := range sleepers {
			_ = sleeper.Wait()
		}
		fmt.Println("VALID_DONE")
		return 0
	case "alloc":
		touch(2 * hostLimits.memoryMax)
		fmt.Println("ALLOCATED")
		return 0
	case "fork":
		for range 4 * hostLimits.tasksMax {
			if err := exec.Command("sleep", "5").Start(); err != nil {
				fmt.Println("FORK_REFUSED", err)
				return 3
			}
		}
		fmt.Println("FORKED")
		return 0
	case "tree":
		sleeper := exec.Command("sleep", "60")
		if err := sleeper.Start(); err != nil {
			fmt.Println("FORK_REFUSED", err)
			return 3
		}
		_ = sleeper.Wait()
		return 0
	}
	fmt.Println("UNKNOWN_MODE", mode)
	return 2
}

var touched [][]byte

// touch allocates size bytes in 1 MiB chunks and writes every page, so they are resident.
func touch(size int64) {
	for allocated := int64(0); allocated < size; allocated += 1 << 20 {
		chunk := make([]byte, 1<<20)
		for page := 0; page < len(chunk); page += 4096 {
			chunk[page] = 1
		}
		touched = append(touched, chunk)
	}
}

// hostRun is what the parent observed of one scoped child.
type hostRun struct {
	cmd         *exec.Cmd
	output      bytes.Buffer
	scope       string // the child's cgroup directory once it is in the unit, else ""
	controllers string
	scopeErr    error         // checkScope on the running scope
	mu          sync.Mutex    // guards memory and pids
	memory      string        // the scope's memory.events as last read while it existed, "" if never
	pids        string        // the scope's pids.events as last read while it existed, "" if never
	polled      chan struct{} // closed once the poll of the events files has ended
	started     time.Time
	exited      chan struct{} // closed once the child has been reaped
	waitErr     error
}

// startScoped starts the child in mode under scopeArgv and waits until it is in its unit's cgroup
// (at most 2 s). The scope's events files are polled while it exists: a scope is removed once its
// last process ends, and a removed cgroup's files no longer read (ENODEV, observed 2026-10-16).
func startScoped(t *testing.T, mode string) *hostRun {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unitCount++
	unit := fmt.Sprintf("lz-hostlimits-%s-%d-%d", mode, os.Getpid(), unitCount)
	bwrap := []string{"/usr/bin/bwrap", "--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid", "--unshare-ipc",
		"--unshare-uts", "--unshare-net", "--cap-drop", "ALL", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev",
		"--", executable}
	argv := scopeArgv("/usr/bin/systemd-run", unit, hostLimits, bwrap)
	if len(argv) == 0 {
		t.Fatal("scopeArgv returned no command")
	}
	run := &hostRun{exited: make(chan struct{}), polled: make(chan struct{})}
	run.cmd = exec.Command(argv[0], argv[1:]...)
	run.cmd.Env = append(os.Environ(), hostChildMode+"="+mode)
	run.cmd.Stdout, run.cmd.Stderr = &run.output, &run.output
	// Wait returns at most 5 s after the sandbox's process ends, even if a descendant still holds
	// the output pipe.
	run.cmd.WaitDelay = 5 * time.Second
	run.started = time.Now()
	if err := run.cmd.Start(); err != nil {
		t.Fatalf("start %q: %v", argv, err)
	}
	go func() { run.waitErr = run.cmd.Wait(); close(run.exited) }()
	t.Cleanup(func() {
		_ = run.cmd.Process.Kill()
		select {
		case <-run.exited:
		case <-time.After(10 * time.Second):
		}
	})
	membership := fmt.Sprintf("/proc/%d/cgroup", run.cmd.Process.Pid)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		data, err := os.ReadFile(membership)
		if err != nil {
			break // the child already ended
		}
		path := strings.TrimPrefix(strings.TrimSpace(string(data)), "0::")
		if strings.HasSuffix(path, "/"+unit+".scope") {
			run.scope = filepath.Join("/sys/fs/cgroup", path)
			break
		}
	}
	if run.scope == "" {
		close(run.polled)
		return run
	}
	go run.poll()
	if data, err := os.ReadFile(filepath.Join(run.scope, "cgroup.controllers")); err == nil {
		run.controllers = string(data)
	}
	run.scopeErr = checkScope(run.scope, hostLimits)
	return run
}

// wait returns the child's exit error and how long it ran; a child alive after limit is killed.
func (run *hostRun) wait(t *testing.T, limit time.Duration) (error, time.Duration, bool) {
	t.Helper()
	select {
	case <-run.exited:
		return run.waitErr, time.Since(run.started), true
	case <-time.After(limit - time.Since(run.started)):
		_ = run.cmd.Process.Kill()
		<-run.exited
		return run.waitErr, time.Since(run.started), false
	}
}

// poll reads the scope's events files every 2 ms until the child has been reaped, then once more,
// keeping each file's last successful read.
func (run *hostRun) poll() {
	defer close(run.polled)
	read := func() {
		memory, memoryErr := os.ReadFile(filepath.Join(run.scope, "memory.events"))
		pids, pidsErr := os.ReadFile(filepath.Join(run.scope, "pids.events"))
		run.mu.Lock()
		defer run.mu.Unlock()
		if memoryErr == nil {
			run.memory = string(memory)
		}
		if pidsErr == nil {
			run.pids = string(pids)
		}
	}
	for {
		read()
		select {
		case <-run.exited:
			read()
			return
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// tripped asks limitTripped about the events the poll last read while the scope existed.
func (run *hostRun) tripped(t *testing.T) string {
	t.Helper()
	<-run.polled
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.memory == "" || run.pids == "" {
		t.Errorf("the scope's memory.events or pids.events was never readable while the child ran")
		return ""
	}
	return limitTripped(run.memory, run.pids)
}

// requireScope fails a run whose child never entered its unit with the requested limits.
func (run *hostRun) requireScope(t *testing.T) {
	t.Helper()
	if run.scope == "" {
		t.Errorf("the child never ran in a transient scope unit: started without limits")
		return
	}
	if run.scopeErr != nil {
		t.Errorf("checkScope refused the running scope: %v", run.scopeErr)
	}
}

// userManager asks the user manager for its environment, bounded to 5 s.
func userManager() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, "systemctl", "--user", "show-environment")
	probe.WaitDelay = time.Second
	return probe.Run()
}

func TestHostLimits(t *testing.T) {
	t.Run("valid-child-passes", func(t *testing.T) {
		run := startScoped(t, "valid")
		err, elapsed, ended := run.wait(t, 30*time.Second)
		run.requireScope(t)
		if reason := limitsUnavailable(exec.LookPath, userManager, run.controllers); run.scope != "" && reason != "" {
			t.Errorf("limitsUnavailable on this host = %q (controllers %q), want \"\"", reason, run.controllers)
		}
		if !ended || err != nil || !strings.Contains(run.output.String(), "VALID_DONE") {
			t.Errorf("a valid child failed under the limits after %v: %v (output %q)", elapsed, err, run.output.String())
		}
		if run.scope != "" {
			if got := run.tripped(t); got != "" {
				t.Errorf("a valid child reported RESOURCE_LIMIT %s", got)
			}
		}
	})
	t.Run("allocation-past-memory-limit-stopped", func(t *testing.T) {
		run := startScoped(t, "alloc")
		err, elapsed, ended := run.wait(t, 30*time.Second)
		if strings.Contains(run.output.String(), "ALLOCATED") || err == nil {
			t.Fatalf("a child allocating %d bytes under memoryMax %d was not stopped: exit %v after %v (output %q)",
				2*hostLimits.memoryMax, hostLimits.memoryMax, err, elapsed, run.output.String())
		}
		if !ended || elapsed > 10*time.Second {
			t.Errorf("the allocating child was stopped after %v, want within 10 s", elapsed)
		}
		run.requireScope(t)
		if got := run.tripped(t); got != "memory" {
			t.Errorf("got RESOURCE_LIMIT %q, want RESOURCE_LIMIT memory (exit %v)", got, err)
		}
	})
	t.Run("fork-storm-past-task-limit-stopped", func(t *testing.T) {
		run := startScoped(t, "fork")
		err, elapsed, ended := run.wait(t, 30*time.Second)
		if strings.Contains(run.output.String(), "FORKED") || err == nil {
			t.Fatalf("a child starting %d processes under tasksMax %d was not stopped: exit %v after %v (output %q)",
				4*hostLimits.tasksMax, hostLimits.tasksMax, err, elapsed, run.output.String())
		}
		if !ended || elapsed > 10*time.Second {
			t.Errorf("the forking child was stopped after %v, want within 10 s", elapsed)
		}
		run.requireScope(t)
		if got := run.tripped(t); got != "tasks" {
			t.Errorf("got RESOURCE_LIMIT %q, want RESOURCE_LIMIT tasks (exit %v)", got, err)
		}
	})
	t.Run("die-with-parent-ends-the-tree", func(t *testing.T) {
		run := startScoped(t, "tree")
		run.requireScope(t)
		if run.scope == "" {
			return
		}
		sleeper := 0
		for deadline := time.Now().Add(5 * time.Second); sleeper == 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			sleeper = scopeProcess(run.scope, "sleep")
		}
		if sleeper == 0 {
			t.Fatalf("the child's sleeper never appeared in the scope")
		}
		t.Cleanup(func() { _ = syscall.Kill(sleeper, syscall.SIGKILL) })
		if err := run.cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		gone := false
		for deadline := time.Now().Add(5 * time.Second); !gone && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			gone = !alive(sleeper, "sleep")
		}
		if !gone {
			t.Errorf("killing the sandbox's parent left its child's sleeper (pid %d) running after 5 s", sleeper)
		}
	})
}

// scopeProcess returns the host pid of a process named comm in the scope, or 0.
func scopeProcess(scope, comm string) int {
	data, err := os.ReadFile(filepath.Join(scope, "cgroup.procs"))
	if err != nil {
		return 0
	}
	for _, field := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(field)
		if err == nil && alive(pid, comm) {
			return pid
		}
	}
	return 0
}

// alive reports whether pid still runs as comm (a reused pid with another name is not it).
func alive(pid int, comm string) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(data)) != comm {
		return false
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// A zombie has ended; only its parent has not reaped it yet.
	fields := strings.Fields(string(status))
	return len(fields) > 2 && fields[2] != "Z"
}
