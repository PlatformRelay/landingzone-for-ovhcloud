package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The entry's limits: twice the largest peak measured across the CI targets, rounded up to a
// power of two (research R5, premise P4; the peaks are in specs/006-harden-first-slice/evidence/T050.md,
// measured in the entry's own child scope; TestEntryLimitsFollowPeaks pins both).
const (
	entryMemoryMax int64 = 4 << 30 // largest peak 1903194112 (test:live-lane) × 2, rounded up
	entryTasksMax        = 1024    // largest peak 333 (test:live-lane) × 2, rounded up
)

// Host programs the limits rely on. Their digests are not pinned: they are host software, like
// bwrap's dependencies; the limits themselves are verified in the started scope (research R5).
const (
	systemdRunPath = "/usr/bin/systemd-run"
	systemctlPath  = "/usr/bin/systemctl"
)

// limitsAction is the next action a refusal names (spec US3).
const limitsAction = "enable user cgroup delegation (a systemd user manager delegating the memory and pids controllers)"

// limits bounds the transient cgroup the entry runs one child in (006 FR-013, research R5).
type limits struct {
	memoryMax int64 // bytes
	tasksMax  int
}

// scopeArgv returns the command that runs child, the bwrap argv, unchanged inside a transient
// `systemd-run --user --scope` unit named unit with l's MemoryMax and TasksMax. Swap is off in the
// scope, so a child past MemoryMax is killed rather than swapped out.
func scopeArgv(systemdRun, unit string, l limits, child []string) []string {
	argv := []string{systemdRun, "--user", "--scope", "--quiet", "--collect", "--unit=" + unit,
		"-p", "MemoryMax=" + strconv.FormatInt(l.memoryMax, 10), "-p", "MemorySwapMax=0",
		"-p", "TasksMax=" + strconv.Itoa(l.tasksMax), "--"}
	return append(argv, child...)
}

// limitsUnavailable names why the limits cannot be applied (no-systemd-run, no-user-manager,
// no-memory-controller, no-pids-controller), or returns "" when they can. controllers is the user
// manager's cgroup.controllers; a controller counts only as a whole word of it.
func limitsUnavailable(lookPath func(string) (string, error), userManager func() error, controllers string) string {
	if _, err := lookPath(systemdRunPath); err != nil {
		return "no-systemd-run"
	}
	if err := userManager(); err != nil {
		return "no-user-manager"
	}
	available := strings.Fields(controllers)
	if !slices.Contains(available, "memory") {
		return "no-memory-controller"
	}
	if !slices.Contains(available, "pids") {
		return "no-pids-controller"
	}
	return ""
}

// checkScope refuses a started scope whose memory.max or pids.max is not the requested limit
// (limit-not-applied).
func checkScope(cgroupDir string, l limits) error {
	if err := scopeFileIs(cgroupDir, "memory.max", strconv.FormatInt(l.memoryMax, 10)); err != nil {
		return err
	}
	return scopeFileIs(cgroupDir, "pids.max", strconv.Itoa(l.tasksMax))
}

// scopeFileIs refuses unless the scope's file holds exactly want, with or without a newline.
func scopeFileIs(cgroupDir, name, want string) error {
	data, err := os.ReadFile(filepath.Join(cgroupDir, name))
	if err != nil {
		return fmt.Errorf("limit-not-applied: %s unreadable: %w", name, err)
	}
	if got := strings.TrimSuffix(string(data), "\n"); got != want {
		return fmt.Errorf("limit-not-applied: %s is %q, want %s", name, got, want)
	}
	return nil
}

// limitTripped reads the contents of the scope's memory.events and pids.events, as read while the
// scope still existed (a user scope is removed when its last process ends), and names the limit
// that stopped the child: memory, tasks, or "". memory.events' own "max" counts reclaim at the
// limit, not a kill, so only oom_kill counts.
func limitTripped(memoryEvents, pidsEvents string) string {
	if eventCount(memoryEvents, "oom_kill") > 0 {
		return "memory"
	}
	if eventCount(pidsEvents, "max") > 0 {
		return "tasks"
	}
	return ""
}

// eventCount returns the counter named key in a cgroup events file, or 0.
func eventCount(events, key string) int64 {
	for _, line := range strings.Split(events, "\n") {
		name, value, ok := strings.Cut(line, " ")
		if !ok || name != key {
			continue
		}
		if count, err := strconv.ParseInt(value, 10, 64); err == nil {
			return count
		}
	}
	return 0
}

// scopeHost is what the entry needs of the host to run a child under limits. Production uses
// systemScopeHost; tests replace the parts that need a systemd user manager.
type scopeHost struct {
	systemdRun  string
	env         []string // systemd-run's environment, which bwrap inherits and clears for the child
	holder      []string // the scope's first process, which runs bwrap (scopeHoldMode)
	lookPath    func(string) (string, error)
	userManager func() error
	controllers func() string
	scopeDir    func(pid int, unit string) string // the cgroup directory once pid is in unit's scope, else ""
	scopeWait   time.Duration                     // how long the child may take to enter its scope
	poll        time.Duration                     // how often the scope's events files are read
}

// systemScopeHost reaches the invoking user's systemd manager at its fixed runtime directory; the
// entry's own executable holds the scope.
func systemScopeHost(executable string) scopeHost {
	uid := os.Getuid()
	env := []string{fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid)}
	return scopeHost{
		systemdRun:  systemdRunPath,
		env:         env,
		holder:      []string{executable, scopeHoldMode, strconv.Itoa(os.Getpid())},
		lookPath:    exec.LookPath,
		userManager: func() error { return userManagerReachable(env) },
		controllers: func() string { return delegatedControllers(uid) },
		scopeDir:    processScope,
		scopeWait:   10 * time.Second,
		poll:        5 * time.Millisecond,
	}
}

// userManagerReachable asks the user manager for its environment, bounded to 5 s.
func userManagerReachable(env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, systemctlPath, "--user", "show-environment")
	probe.Env, probe.Dir, probe.WaitDelay = env, "/", time.Second
	return probe.Run()
}

// delegatedControllers returns the controllers the user manager may hand to its units, or "".
func delegatedControllers(uid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service/cgroup.controllers", uid, uid))
	if err != nil {
		return ""
	}
	return string(data)
}

// processScope returns pid's cgroup v2 directory once it is unit's scope, else "".
func processScope(pid int, unit string) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return ""
	}
	return scopeFromCgroup(string(data), unit)
}

// scopeFromCgroup reads a /proc/<pid>/cgroup file: the cgroup v2 directory when it is the single
// unified line and names unit's scope, else "" (a hybrid or v1 host has several lines).
func scopeFromCgroup(data, unit string) string {
	path, ok := strings.CutPrefix(strings.TrimSuffix(data, "\n"), "0::")
	if !ok || strings.Contains(path, "\n") || !strings.HasSuffix(path, "/"+unit+".scope") {
		return ""
	}
	return filepath.Join("/sys/fs/cgroup", path)
}

// scopeUnit returns a unit name no other run of the entry uses.
func scopeUnit() string {
	return fmt.Sprintf("lz-offline-%d-%s", os.Getpid(), rand.Text())
}

// limitedRun is what became of a child that started under limits.
type limitedRun struct {
	tripped string // the limit that stopped it: memory, tasks or ""
	err     error  // its own exit error
}

// heldChild is the scope's holder, started with its gate at fd 3 and the final events due on fd 4.
type heldChild struct {
	command *exec.Cmd
	release *os.File // write end of the holder's gate
	report  *os.File // read end of the holder's final events
}

// runLimited runs bwrap with args in a transient scope with limits l. It refuses before the child
// starts when the host cannot apply the limits, and the holder starts bwrap only once the entry
// has checked that the started scope carries the limits and its events read, so a scope without
// them never runs the child. While the child runs, a tripped limit ends the whole scope at once. The holder reports
// the scope's events from inside it after bwrap ends, so a limit hit just before the end counts.
func runLimited(ctx context.Context, host scopeHost, l limits, unit, bwrap string, args []string, stdout, stderr io.Writer) (limitedRun, error) {
	if reason := limitsUnavailable(host.lookPath, host.userManager, host.controllers()); reason != "" {
		return limitedRun{}, fmt.Errorf("LIMITS_UNAVAILABLE %s: %s", reason, limitsAction)
	}
	child := append(append(slices.Clone(host.holder), bwrap), args...)
	held, err := startHeld(ctx, host, scopeArgv(host.systemdRun, unit, l, child), stdout, stderr)
	if err != nil {
		return limitedRun{}, fmt.Errorf("LIMITS_UNAVAILABLE limit-not-applied: %w", err)
	}
	defer held.report.Close()
	dir, err := host.appliedScope(held.command.Process.Pid, unit, l)
	if err != nil {
		// The holder starts bwrap only on a byte, never at end of file; it is ended all the same.
		_ = held.command.Process.Kill()
		_ = held.command.Wait()
		_ = held.release.Close()
		return limitedRun{}, fmt.Errorf("LIMITS_UNAVAILABLE %w; %s", err, limitsAction)
	}
	_, writeErr := held.release.Write([]byte{1})
	_ = held.release.Close()
	watch := watchScope(dir, host.poll, func() { endScope(dir, held.command.Process) })
	waitErr := held.command.Wait()
	tripped := watch.stop()
	final, reported := heldReport(held.report)
	if tripped == "" {
		tripped = final
	}
	switch {
	case waitErr != nil:
	case writeErr != nil:
		waitErr = writeErr
	case !reported:
		waitErr = errors.New("the scope's final events were not reported")
	}
	return limitedRun{tripped: tripped, err: waitErr}, nil
}

// startHeld starts argv with fd 3 the read end of the gate and fd 4 the write end of the report,
// and keeps the other ends.
func startHeld(ctx context.Context, host scopeHost, argv []string, stdout, stderr io.Writer) (*heldChild, error) {
	held, release, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	report, reportEnd, err := os.Pipe()
	if err != nil {
		_, _ = held.Close(), release.Close()
		return nil, err
	}
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Env, command.Dir, command.WaitDelay = host.env, "/", 5*time.Second
	command.Stdout, command.Stderr = stdout, stderr
	command.ExtraFiles = []*os.File{held, reportEnd}
	err = command.Start()
	_, _ = held.Close(), reportEnd.Close()
	if err != nil {
		_, _ = release.Close(), report.Close()
		return nil, err
	}
	return &heldChild{command: command, release: release, report: report}, nil
}

// heldReport reads the holder's final events and names the limit they show; reported is false
// when the holder wrote none (it died, or could not read them).
func heldReport(report io.Reader) (string, bool) {
	data, err := io.ReadAll(io.LimitReader(report, 1<<16))
	memory, pids, ok := strings.Cut(string(data), "\x00")
	if err != nil || !ok {
		return "", false
	}
	return limitTripped(memory, pids), true
}

// appliedScope waits for pid to enter unit's scope and checks the scope carries l with swap off
// and that its events files read, so the run can be watched.
func (host scopeHost) appliedScope(pid int, unit string, l limits) (string, error) {
	for deadline := time.Now().Add(host.scopeWait); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		dir := host.scopeDir(pid, unit)
		if dir == "" {
			continue
		}
		if err := checkScope(dir, l); err != nil {
			return "", err
		}
		if err := scopeFileIs(dir, "memory.swap.max", "0"); err != nil {
			return "", err
		}
		_, err := scopeEvents(dir)
		return dir, err
	}
	return "", fmt.Errorf("limit-not-applied: the child did not enter unit %s within %v", unit, host.scopeWait)
}

// scopeEvents reads the scope's memory.events and pids.events, joined by a NUL byte.
func scopeEvents(dir string) ([]byte, error) {
	memory, err := os.ReadFile(filepath.Join(dir, "memory.events"))
	if err != nil {
		return nil, fmt.Errorf("limit-not-applied: memory.events unreadable: %w", err)
	}
	pids, err := os.ReadFile(filepath.Join(dir, "pids.events"))
	if err != nil {
		return nil, fmt.Errorf("limit-not-applied: pids.events unreadable: %w", err)
	}
	return append(append(memory, 0), pids...), nil
}

// scopeHoldMode makes the entry its child's scope holder:
// `lz-offline --scope-hold <entry pid> <bwrap> <args…>`, with fd 3 the gate and fd 4 the report.
const scopeHoldMode = "--scope-hold"

// scopeHolder runs as the scope's first process. It dies with the entry, waits at its gate, runs
// bwrap (which dies with it), and once bwrap has ended reports the scope's events from inside the
// scope, which therefore still exists. Its exit code is bwrap's.
func scopeHolder(args []string) int {
	_, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_PDEATHSIG, uintptr(syscall.SIGKILL), 0)
	if len(args) < 2 || errno != 0 || args[0] != strconv.Itoa(os.Getppid()) {
		fmt.Fprintln(os.Stderr, "SCOPE_HOLD: usage or parent mismatch")
		return 125
	}
	syscall.CloseOnExec(3) // neither the gate nor the report reaches the sandbox
	syscall.CloseOnExec(4)
	report := os.NewFile(4, "scope-report")
	return holdScope(args[1], args[2:], os.NewFile(3, "scope-hold"), report, ownScope)
}

// holdScope waits for one byte on gate and then runs program, or returns 125 without running it
// when the gate closes first; then it writes scopeOf's events to report.
func holdScope(program string, args []string, gate, report *os.File, scopeOf func() (string, error)) int {
	runtime.LockOSThread() // Pdeathsig follows the thread that started the program
	defer runtime.UnlockOSThread()
	opened, gateErr := gate.Read(make([]byte, 1))
	_ = gate.Close()
	if opened != 1 {
		_ = report.Close()
		fmt.Fprintln(os.Stderr, "SCOPE_HOLD: gate closed:", gateErr)
		return 125
	}
	command := exec.Command(program, args...)
	command.Stdin, command.Stdout, command.Stderr = nil, os.Stdout, os.Stderr
	command.Env = os.Environ()
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	runErr := command.Run()
	dir, err := scopeOf()
	var events []byte
	if err == nil {
		events, err = scopeEvents(dir)
	}
	if err == nil {
		_, err = report.Write(events)
	}
	_ = report.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "SCOPE_HOLD:", err)
		return 125
	}
	return exitCode(command, runErr)
}

// ownScope is this process's cgroup v2 directory.
func ownScope() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	path, ok := strings.CutPrefix(strings.TrimSuffix(string(data), "\n"), "0::")
	if !ok || strings.Contains(path, "\n") {
		return "", errors.New("not a cgroup v2 process")
	}
	return filepath.Join("/sys/fs/cgroup", path), nil
}

// exitCode is the code a finished command's caller passes on: its own, or 128 plus the signal.
func exitCode(command *exec.Cmd, runErr error) int {
	state := command.ProcessState
	if state == nil {
		fmt.Fprintln(os.Stderr, "SCOPE_HOLD:", runErr)
		return 125
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return state.ExitCode()
}

// endScope stops every process in the scope at once (cgroup.kill) and the sandbox's outer process,
// whose death also ends the sandbox through --die-with-parent.
func endScope(dir string, process *os.Process) {
	if file, err := os.OpenFile(filepath.Join(dir, "cgroup.kill"), os.O_WRONLY, 0); err == nil {
		_, _ = file.WriteString("1")
		_ = file.Close()
	}
	_ = process.Kill()
}

// scopeWatch reads a scope's events files while it exists: a user scope is removed when its last
// process ends, and its files then no longer read.
type scopeWatch struct {
	dir          string
	memory, pids string // the last successful reads
	tripped      string
	end          func()
	done         chan struct{}
	result       chan string
}

func watchScope(dir string, poll time.Duration, end func()) *scopeWatch {
	watch := &scopeWatch{dir: dir, end: end, done: make(chan struct{}), result: make(chan string, 1)}
	go func() {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		for {
			watch.read()
			select {
			case <-watch.done:
				watch.read()
				watch.result <- watch.tripped
				return
			case <-ticker.C:
			}
		}
	}()
	return watch
}

// read keeps each file's last successful read and ends the scope the first time a limit trips:
// pids.max only refuses forks, so a child that retries would otherwise run until the deadline.
func (watch *scopeWatch) read() {
	if data, err := os.ReadFile(filepath.Join(watch.dir, "memory.events")); err == nil {
		watch.memory = string(data)
	}
	if data, err := os.ReadFile(filepath.Join(watch.dir, "pids.events")); err == nil {
		watch.pids = string(data)
	}
	if watch.tripped == "" {
		if watch.tripped = limitTripped(watch.memory, watch.pids); watch.tripped != "" {
			watch.end()
		}
	}
}

// stop ends the watch after the child has been reaped and returns the limit that tripped, or "".
func (watch *scopeWatch) stop() string {
	close(watch.done)
	return <-watch.result
}

// childResult is the entry's verdict on a finished child: a tripped limit wins over the exit error.
func childResult(target string, outcome limitedRun) error {
	if outcome.tripped != "" {
		return fmt.Errorf("RESOURCE_LIMIT %s target=%s", outcome.tripped, target)
	}
	if outcome.err != nil {
		return fmt.Errorf("CHILD_FAILED: %w", outcome.err)
	}
	return nil
}
