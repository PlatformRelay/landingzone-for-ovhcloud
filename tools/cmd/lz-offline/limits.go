package main

// limits bounds the transient cgroup the entry runs one child in (006 FR-013, research R5).
type limits struct {
	memoryMax int64 // bytes
	tasksMax  int
}

// scopeArgv returns the command that runs child, the bwrap argv, unchanged inside a transient
// `systemd-run --user --scope` unit named unit with l's MemoryMax and TasksMax. Not implemented
// (T032): it returns child, so the child would start without limits.
func scopeArgv(systemdRun, unit string, l limits, child []string) []string {
	return child
}

// limitsUnavailable names why the limits cannot be applied (no-systemd-run, no-user-manager,
// no-memory-controller, no-pids-controller), or returns "" when they can. Not implemented (T032).
func limitsUnavailable(lookPath func(string) (string, error), userManager func() error, controllers string) string {
	return ""
}

// checkScope refuses a started scope whose memory.max or pids.max is not the requested limit
// (limit-not-applied). Not implemented (T032).
func checkScope(cgroupDir string, l limits) error {
	return nil
}

// limitTripped reads the contents of the scope's memory.events and pids.events, as read while the
// scope still existed (a user scope is removed when its last process ends), and names the limit
// that stopped the child: memory, tasks, or "". Not implemented (T032).
func limitTripped(memoryEvents, pidsEvents string) string {
	return ""
}
