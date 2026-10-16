package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// scopeOptions reads systemd-run's options up to the first "--" and returns the flags, the
// properties by name, the unit and the argv after the separator. A property set twice, an option
// it does not know or a missing separator fails the test, so a conflicting or hidden setting
// cannot pass.
func scopeOptions(t *testing.T, argv []string) (map[string]bool, map[string]string, string, []string) {
	t.Helper()
	flags, properties, unit := map[string]bool{}, map[string]string{}, ""
	property := func(value string) {
		name, setting, ok := strings.Cut(value, "=")
		if !ok {
			t.Fatalf("property %q has no value", value)
		}
		if _, seen := properties[name]; seen {
			t.Fatalf("property %s set twice in %q", name, argv)
		}
		properties[name] = setting
	}
	for index := 1; index < len(argv); index++ {
		option := argv[index]
		next := func() string {
			index++
			if index >= len(argv) {
				t.Fatalf("option %s has no value in %q", option, argv)
			}
			return argv[index]
		}
		switch {
		case option == "--":
			return flags, properties, unit, argv[index+1:]
		case option == "-p" || option == "--property":
			property(next())
		case strings.HasPrefix(option, "--property="):
			property(strings.TrimPrefix(option, "--property="))
		case option == "--unit" || option == "-u":
			unit = next()
		case strings.HasPrefix(option, "--unit="):
			unit = strings.TrimPrefix(option, "--unit=")
		case option == "--user" || option == "--scope" || option == "--quiet" || option == "-q" || option == "--collect" || option == "-G":
			flags[option] = true
		// Options that set no limit and take their value inline are admitted.
		case option == "--same-dir" || strings.HasPrefix(option, "--description=") || strings.HasPrefix(option, "--slice=") ||
			strings.HasPrefix(option, "--expand-environment="):
		default:
			t.Fatalf("unexpected systemd-run option %q in %q", option, argv)
		}
	}
	t.Fatalf("no -- separator before the child in %q", argv)
	return nil, nil, "", nil
}

// systemdBytes parses a systemd byte size: a decimal, or one with a K, M, G or T suffix (base 1024).
func systemdBytes(t *testing.T, value string) int64 {
	t.Helper()
	multiplier := int64(1)
	if suffix := strings.IndexAny(value, "KMGT"); suffix == len(value)-1 && suffix > 0 {
		multiplier = int64(1) << (10 * (strings.IndexByte("KMGT", value[suffix]) + 1))
		value = value[:suffix]
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Fatalf("MemoryMax=%q is not a byte size: %v", value, err)
	}
	return number * multiplier
}

// Case 1: the scope carries both limits and the unit, and the bwrap argv runs unchanged.
func TestScopeArgv(t *testing.T) {
	bwrap := []string{"/usr/bin/bwrap", "--die-with-parent", "--new-session", "--unshare-pid", "--unshare-net",
		"--setenv", "PATH", "/tcb:/usr/bin:/bin", "--chdir", "/candidate", "/tcb/lz-offline", "--inside", "test:static"}
	// The child's own argv may carry a separator and look like a property; neither may leak into
	// the scope's options or be dropped.
	tricky := []string{"/usr/bin/bwrap", "--die-with-parent", "--", "/tcb/lz-offline", "--inside", "probe", "-p", "MemoryMax=infinity", "--"}
	for _, row := range []struct {
		name  string
		unit  string
		l     limits
		child []string
	}{
		{"placeholders", "lz-offline-1234", limits{memoryMax: 8 << 30, tasksMax: 1024}, bwrap},
		{"small", "lz-offline-x", limits{memoryMax: 64 << 20, tasksMax: 32}, bwrap},
		{"odd-values", "lz-offline-odd", limits{memoryMax: 3<<30 + 4096, tasksMax: 777}, tricky},
	} {
		t.Run(row.name, func(t *testing.T) {
			given := slices.Clone(row.child)
			argv := scopeArgv("/usr/bin/systemd-run", row.unit, row.l, row.child)
			if !slices.Equal(row.child, given) {
				t.Fatalf("scopeArgv changed the caller's child argv: %q, was %q", row.child, given)
			}
			if len(argv) == 0 || argv[0] != "/usr/bin/systemd-run" {
				t.Fatalf("argv %q does not start with the given systemd-run", argv)
			}
			flags, properties, unit, tail := scopeOptions(t, argv)
			if !flags["--user"] || !flags["--scope"] {
				t.Errorf("argv %q is not a --user --scope unit", argv)
			}
			if unit != row.unit {
				t.Errorf("unit %q, want %q", unit, row.unit)
			}
			memory, ok := properties["MemoryMax"]
			if !ok {
				t.Errorf("argv %q carries no MemoryMax", argv)
			} else if got := systemdBytes(t, memory); got != row.l.memoryMax {
				t.Errorf("MemoryMax=%s is %d bytes, want %d", memory, got, row.l.memoryMax)
			}
			if tasks, ok := properties["TasksMax"]; !ok {
				t.Errorf("argv %q carries no TasksMax", argv)
			} else if tasks != strconv.Itoa(row.l.tasksMax) {
				t.Errorf("TasksMax=%s, want %d", tasks, row.l.tasksMax)
			}
			if !slices.Equal(tail, row.child) {
				t.Errorf("child argv after -- is %q, want it unchanged: %q", tail, row.child)
			}
		})
	}
}

// Case 2: a host that cannot apply the limits is named, with data-model's reason.
func TestLimitsUnavailable(t *testing.T) {
	// Only systemd-run is missing in the missing rows: a lookup of another command finds it, so a
	// check that looks up the wrong name cannot pass.
	found := func(name string) (string, error) { return "/usr/bin/" + filepath.Base(name), nil }
	missing := func(name string) (string, error) {
		if filepath.Base(name) == "systemd-run" {
			return "", errors.New("executable file not found in $PATH")
		}
		return found(name)
	}
	reachable := func() error { return nil }
	unreachable := func() error { return errors.New("Failed to connect to bus: No medium found") }
	for _, row := range []struct {
		name        string
		lookPath    func(string) (string, error)
		userManager func() error
		controllers string
		want        []string
	}{
		{"available", found, reachable, "cpu memory pids\n", []string{""}},
		{"available-other-order", found, reachable, "pids io memory cpu", []string{""}},
		{"no-systemd-run", missing, reachable, "cpu memory pids\n", []string{"no-systemd-run"}},
		{"no-user-manager", found, unreachable, "cpu memory pids\n", []string{"no-user-manager"}},
		{"no-memory-controller", found, reachable, "cpu pids\n", []string{"no-memory-controller"}},
		{"no-pids-controller", found, reachable, "cpu memory\n", []string{"no-pids-controller"}},
		// A controller is a whole word of cgroup.controllers, not a substring of another.
		{"memory-only-as-substring", found, reachable, "cpu memory_x pids\n", []string{"no-memory-controller"}},
		{"pids-only-as-substring", found, reachable, "cpu memory xpids\n", []string{"no-pids-controller"}},
		{"no-controllers", found, reachable, "", []string{"no-memory-controller", "no-pids-controller"}},
		{"nothing", missing, unreachable, "", []string{"no-systemd-run", "no-user-manager", "no-memory-controller", "no-pids-controller"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := limitsUnavailable(row.lookPath, row.userManager, row.controllers)
			if !slices.Contains(row.want, got) {
				t.Errorf("limitsUnavailable = %q, want one of %q", got, row.want)
			}
		})
	}
}

// Case 3: a started scope whose limits differ from the request is refused.
func TestCheckScope(t *testing.T) {
	request := limits{memoryMax: 64 << 20, tasksMax: 32}
	for _, row := range []struct {
		name          string
		memory, tasks *string // nil: the file is absent
		refuse        bool
	}{
		{"applied", ptr("67108864\n"), ptr("32\n"), false},
		{"applied-no-newline", ptr("67108864"), ptr("32"), false},
		{"memory-differs", ptr("134217728\n"), ptr("32\n"), true},
		{"memory-prefix", ptr("671088640\n"), ptr("32\n"), true},
		{"memory-unlimited", ptr("max\n"), ptr("32\n"), true},
		{"tasks-differ", ptr("67108864\n"), ptr("64\n"), true},
		{"tasks-prefix", ptr("67108864\n"), ptr("320\n"), true},
		{"tasks-unlimited", ptr("67108864\n"), ptr("max\n"), true},
		{"memory-file-absent", nil, ptr("32\n"), true},
		{"tasks-file-absent", ptr("67108864\n"), nil, true},
		{"memory-empty", ptr(""), ptr("32\n"), true},
		{"tasks-empty", ptr("67108864\n"), ptr(""), true},
		{"memory-lower", ptr("33554432\n"), ptr("32\n"), true},
		{"tasks-lower", ptr("67108864\n"), ptr("16\n"), true},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, value := range map[string]*string{"memory.max": row.memory, "pids.max": row.tasks} {
				if value == nil {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(*value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := checkScope(dir, request)
			switch {
			case row.refuse && err == nil:
				t.Errorf("checkScope admitted memory.max=%s pids.max=%s for %+v", show(row.memory), show(row.tasks), request)
			case row.refuse && !strings.Contains(err.Error(), "limit-not-applied"):
				t.Errorf("refusal %q does not name limit-not-applied", err)
			case !row.refuse && err != nil:
				t.Errorf("checkScope refused the requested limits: %v", err)
			}
		})
	}
	t.Run("no-cgroup-directory", func(t *testing.T) {
		if err := checkScope(filepath.Join(t.TempDir(), "gone"), request); err == nil {
			t.Error("checkScope admitted a cgroup directory that does not exist")
		}
	})
}

func ptr(value string) *string { return &value }

func show(value *string) string {
	if value == nil {
		return "<absent>"
	}
	return strconv.Quote(*value)
}

// Kernel event files as a user scope left them on the maintainer host (Linux 7.0, cgroup v2,
// 2026-10-16; command in evidence/T029.md): a 64 MiB scope that a 300 MB allocation hit, and a
// 32-task scope that a shell forking 100 sleepers hit.
const (
	memoryEventsKilled = "low 0\nhigh 0\nmax 154\noom 1\noom_kill 1\noom_group_kill 0\nsock_throttled 0\n"
	memoryEventsQuiet  = "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\noom_group_kill 0\nsock_throttled 0\n"
	pidsEventsHit      = "max 1\n"
	pidsEventsQuiet    = "max 0\n"
)

// Case 4: a limit kill is named, an ordinary failure is not.
func TestLimitTripped(t *testing.T) {
	for _, row := range []struct {
		name         string
		memory, pids string
		want         []string
	}{
		{"memory", memoryEventsKilled, pidsEventsQuiet, []string{"memory"}},
		{"memory-many-kills", strings.Replace(memoryEventsKilled, "oom_kill 1", "oom_kill 12", 1), pidsEventsQuiet, []string{"memory"}},
		{"tasks", memoryEventsQuiet, pidsEventsHit, []string{"tasks"}},
		{"tasks-many", memoryEventsQuiet, "max 37\n", []string{"tasks"}},
		{"both", memoryEventsKilled, pidsEventsHit, []string{"memory", "tasks"}},
		{"none", memoryEventsQuiet, pidsEventsQuiet, []string{""}},
		// memory.events' own "max" counts reclaim at the limit, not a kill.
		{"memory-reclaimed-not-killed", strings.Replace(memoryEventsQuiet, "max 0", "max 154", 1), pidsEventsQuiet, []string{""}},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := limitTripped(row.memory, row.pids); !slices.Contains(row.want, got) {
				t.Errorf("limitTripped = %q, want one of %q", got, row.want)
			}
		})
	}
}
