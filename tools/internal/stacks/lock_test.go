package stacks

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// Run-lock controls of 005 T040, guard G14 (FR-009, FR-013; research R21 *Locks*;
// contracts/checks.md G14: "skip the account lock; admit a second tenant-lock holder").
//
// The first holder is a separate process (this test binary re-run as TestLockHelperProcess): it
// takes the run's locks through DirLocks, "applies" by writing one file per instance into an apply
// directory, prints "held" and keeps the locks until its stdin closes or it is killed. Only after
// that line does the test start the second run. Nothing here calls OVHcloud or reads a credential:
// lock directories and apply directories are temporary.
//
// Readings this file pins (evidence/T040.md; T041 decision 1): account and account-tenant stacks
// take the `account` lock, account-tenant, environment and region stacks their tenant's
// `tenant-<t>` lock; locks are taken
// account first, then tenants sorted; a refusal is immediate; a refused run holds nothing
// afterwards; an id that is not a row of the manifest takes no lock at all; a lock whose holder
// died is free.

const lockHelperEnv = "LZ_T040_LOCK_HELPER"

// lockManifest is the sandbox manifest with a second tenant `ops` (tenant-state and project).
func lockManifest(t *testing.T) ([]byte, *Manifest) {
	t.Helper()
	data := selManifestData(t, "ops", true)
	return data, decoded(t, data)
}

// TestLockHelperProcess is the first holder when run with LZ_T040_LOCK_HELPER set; otherwise it
// does nothing.
func TestLockHelperProcess(t *testing.T) {
	if os.Getenv(lockHelperEnv) == "" {
		return
	}
	data, err := os.ReadFile(os.Getenv("LZ_T040_MANIFEST"))
	if err == nil {
		var m *Manifest
		if m, err = DecodeManifest(data); err == nil {
			ids := strings.Split(os.Getenv("LZ_T040_IDS"), ",")
			var release func() error
			if release, err = HoldRun(DirLocks(os.Getenv("LZ_T040_LOCKS")), m, ids); err == nil {
				for _, id := range ids {
					if err = os.WriteFile(filepath.Join(os.Getenv("LZ_T040_APPLY"), id), []byte("applied\n"), 0o600); err != nil {
						break
					}
				}
				if err == nil {
					fmt.Println("held")
					_, _ = io.Copy(io.Discard, os.Stdin)
					err = release()
				}
			}
		}
	}
	if err != nil {
		fmt.Printf("refused: %v\n", err)
		os.Exit(3)
	}
	os.Exit(0)
}

// holder is a first (or second) run in another process.
type holder struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	line  string // its first line: "held" or "refused: …"
}

// startRun starts a run over ids in another process and waits for its first line.
func startRun(t *testing.T, manifest []byte, locks, apply string, ids ...string) *holder {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deployments.yaml")
	writeFile(t, path, manifest)
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), lockHelperEnv+"=1", "LZ_T040_MANIFEST="+path, "LZ_T040_LOCKS="+locks,
		"LZ_T040_APPLY="+apply, "LZ_T040_IDS="+strings.Join(ids, ","))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h := &holder{cmd: cmd, stdin: stdin}
	t.Cleanup(func() { h.kill() })
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		lines <- strings.TrimSpace(line)
		_, _ = io.Copy(io.Discard, stdout)
	}()
	select {
	case h.line = <-lines:
	case <-time.After(60 * time.Second):
		t.Fatalf("run %q printed nothing within 60s", ids)
	}
	return h
}

// release ends the holder normally (stdin closed) and waits for it, at most 30s: a release that
// hangs fails the control instead of the whole test binary's timeout.
func (h *holder) release(t *testing.T) {
	t.Helper()
	_ = h.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- h.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("holder: %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = h.cmd.Process.Kill()
		t.Fatalf("holder did not release within 30s")
	}
}

// kill ends the holder with SIGKILL: it releases nothing itself.
func (h *holder) kill() {
	if h.cmd.ProcessState == nil {
		_ = h.cmd.Process.Kill()
		_ = h.cmd.Wait()
	}
}

func mustHold(t *testing.T, h *holder) {
	t.Helper()
	if h.line != "held" {
		t.Fatalf("first run did not take its locks: %q", h.line)
	}
}

// hold runs HoldRun in this process with a deadline, so a lock that waits instead of refusing
// fails the control.
func hold(t *testing.T, m *Manifest, locks string, ids ...string) (func() error, error) {
	t.Helper()
	type result struct {
		release func() error
		err     error
	}
	done := make(chan result, 1)
	go func() {
		r, err := HoldRun(DirLocks(locks), m, ids)
		done <- result{r, err}
	}()
	select {
	case r := <-done:
		return r.release, r.err
	case <-time.After(10 * time.Second):
		t.Fatalf("run %q waited for a lock instead of being refused", ids)
		return nil, nil
	}
}

func wantRefused(t *testing.T, m *Manifest, locks string, ids ...string) {
	t.Helper()
	release, err := hold(t, m, locks, ids...)
	if err == nil {
		_ = release()
		t.Fatalf("run %q admitted while the first run holds its lock", ids)
	}
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("run %q refused with %v, want ErrLocked", ids, err)
	}
}

func wantAdmitted(t *testing.T, m *Manifest, locks string, ids ...string) {
	t.Helper()
	release, err := hold(t, m, locks, ids...)
	if err != nil {
		t.Fatalf("run %q refused: %v", ids, err)
	}
	if err := release(); err != nil {
		t.Fatalf("release of run %q: %v", ids, err)
	}
}

// applied lists the instances the runs applied (the apply directory).
func applied(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids
}

// The locks a run takes, in the order it takes them.
func TestLockRunLocks(t *testing.T) {
	_, m := lockManifest(t)
	for _, c := range []struct {
		ids  []string
		want []string
	}{
		{[]string{"demo-dev-gra11-runtime"}, []string{"tenant-demo"}},
		{[]string{"demo-dev-project", "demo-dev-gra11-network"}, []string{"tenant-demo"}},
		{[]string{"account-governance"}, []string{"account"}},
		{[]string{"account-bootstrap"}, []string{"account"}},
		{[]string{"ops-dev-project", "demo-dev-project", "account-governance"}, []string{"account", "tenant-demo", "tenant-ops"}},
		{[]string{"demo-dev-gra11-runtime", "ops-dev-project"}, []string{"tenant-demo", "tenant-ops"}},
		{[]string{"ops-dev-project", "demo-dev-gra11-runtime"}, []string{"tenant-demo", "tenant-ops"}},
	} {
		got, err := RunLocks(m, c.ids)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("RunLocks(%q) = %q, %v; want %q", c.ids, got, err, c.want)
		}
	}
	// An account-tenant stack touches account state and writes the bucket its tenant's runs use: it
	// takes the account lock, first, and its tenant's lock (coordinator decision 1, 2026-10-07).
	for _, c := range []struct {
		ids  []string
		want []string
	}{
		{[]string{"demo-state"}, []string{"account", "tenant-demo"}},
		{[]string{"demo-state", "ops-dev-project"}, []string{"account", "tenant-demo", "tenant-ops"}},
		{[]string{"demo-state", "demo-dev-project"}, []string{"account", "tenant-demo"}},
	} {
		got, err := RunLocks(m, c.ids)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("RunLocks(%q) = %q, %v; want %q", c.ids, got, err, c.want)
		}
	}
	for _, ids := range [][]string{{"unknown"}, {"demo-dev-project", ""}} {
		if got, err := RunLocks(m, ids); err == nil {
			t.Errorf("RunLocks(%q) = %q, want a refusal", ids, got)
		}
	}
}

// fakeLocks records every TryLock and release, and refuses the names in held.
type fakeLocks struct {
	held  map[string]bool
	calls []string // "lock <name>", "release <name>"
}

func (f *fakeLocks) TryLock(name string) (func() error, error) {
	f.calls = append(f.calls, "lock "+name)
	if f.held[name] {
		return nil, fmt.Errorf("%s: %w", name, ErrLocked)
	}
	f.held[name] = true
	return func() error {
		f.calls = append(f.calls, "release "+name)
		delete(f.held, name)
		return nil
	}, nil
}

// HoldRun takes its locks account first, then tenants sorted, whatever the order of ids, and
// releases every one of them.
func TestLockFixedOrder(t *testing.T) {
	_, m := lockManifest(t)
	for _, ids := range [][]string{
		{"ops-dev-project", "demo-dev-gra11-runtime", "account-governance"},
		{"account-governance", "demo-dev-gra11-runtime", "ops-dev-project"},
		{"demo-dev-gra11-runtime", "ops-dev-project", "account-governance"},
	} {
		f := &fakeLocks{held: map[string]bool{}}
		release, err := HoldRun(f, m, ids)
		if err != nil {
			t.Fatalf("HoldRun(%q): %v", ids, err)
		}
		want := []string{"lock account", "lock tenant-demo", "lock tenant-ops"}
		if !slices.Equal(f.calls, want) {
			t.Errorf("HoldRun(%q) took %q, want %q", ids, f.calls, want)
		}
		if err := release(); err != nil {
			t.Fatal(err)
		}
		if len(f.held) != 0 || len(f.calls) != 6 {
			t.Errorf("after release of %q: held %v, calls %q", ids, f.held, f.calls)
		}
	}
}

// A run that is refused holds nothing: an id that is not a row takes no lock at all, and a run
// refused on a later lock releases the ones it took.
func TestLockRefusedSelectionTakesNoLock(t *testing.T) {
	_, m := lockManifest(t)
	tenantOnly := decoded(t, manifestOf(t, "tenant-only"))
	for _, c := range []struct {
		name string
		m    *Manifest
		ids  []string
	}{
		{"unknown", m, []string{"account-governance", "unknown"}},
		{"empty-id", m, []string{"demo-dev-project", ""}},
		{"external", tenantOnly, []string{tenantOnly.External[0].ID, "demo-dev-project"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeLocks{held: map[string]bool{}}
			if _, err := HoldRun(f, c.m, c.ids); err == nil {
				t.Fatalf("HoldRun(%q) admitted", c.ids)
			}
			if len(f.calls) != 0 {
				t.Errorf("refused selection %q took locks: %q", c.ids, f.calls)
			}
		})
	}
	t.Run("later-lock-held", func(t *testing.T) {
		f := &fakeLocks{held: map[string]bool{"tenant-ops": true}}
		_, err := HoldRun(f, m, []string{"ops-dev-project", "account-governance", "demo-dev-project"})
		if !errors.Is(err, ErrLocked) {
			t.Fatalf("HoldRun with tenant-ops held: %v, want ErrLocked", err)
		}
		if len(f.held) != 1 || !f.held["tenant-ops"] {
			t.Errorf("refused run left %v held, want only the other run's tenant-ops", f.held)
		}
	})
}

// G14: with a run of tenant demo holding its lock, a second run for tenant demo is refused, in
// this process and in another one, and applies nothing; another tenant's run is admitted.
func TestLockSecondRunSameTenant(t *testing.T) {
	data, m := lockManifest(t)
	locks, apply := t.TempDir(), t.TempDir()
	first := startRun(t, data, locks, apply, "demo-dev-project", "demo-dev-gra11-network")
	mustHold(t, first)
	wantRefused(t, m, locks, "demo-dev-gra11-runtime")
	wantRefused(t, m, locks, "demo-dev-project")
	if second := startRun(t, data, locks, apply, "demo-dev-gra11-runtime"); !strings.HasPrefix(second.line, "refused") {
		t.Errorf("second process for tenant demo: %q, want refused", second.line)
	}
	wantAdmitted(t, m, locks, "ops-dev-project")
	if got := applied(t, apply); !slices.Equal(got, []string{"demo-dev-gra11-network", "demo-dev-project"}) {
		t.Errorf("applied %q, want only the first run's stacks", got)
	}
	first.release(t)
	wantAdmitted(t, m, locks, "demo-dev-gra11-runtime")
}

// G14: with a run touching account stacks holding the account lock, every second run touching an
// account or account-tenant stack is refused.
func TestLockSecondRunTouchingAccount(t *testing.T) {
	data, m := lockManifest(t)
	locks, apply := t.TempDir(), t.TempDir()
	first := startRun(t, data, locks, apply, "account-governance")
	mustHold(t, first)
	wantRefused(t, m, locks, "account-governance")
	wantRefused(t, m, locks, "account-bootstrap")
	wantRefused(t, m, locks, "demo-state")
	wantRefused(t, m, locks, "ops-state", "ops-dev-project")
	if second := startRun(t, data, locks, apply, "account-bootstrap"); !strings.HasPrefix(second.line, "refused") {
		t.Errorf("second process touching account stacks: %q, want refused", second.line)
	}
	first.release(t)
	wantAdmitted(t, m, locks, "account-bootstrap")
}

// G14: two tenants' runs both touching account stacks: the second is refused, applies nothing,
// and keeps no lock (a tenant-only run of its tenant is admitted while the first still runs); the
// first run applied only its own tenant's stacks.
func TestLockTwoTenantsTouchingAccount(t *testing.T) {
	data, m := lockManifest(t)
	locks, apply := t.TempDir(), t.TempDir()
	first := startRun(t, data, locks, apply, "demo-state", "demo-dev-project")
	mustHold(t, first)
	wantRefused(t, m, locks, "ops-state", "ops-dev-project")
	if second := startRun(t, data, locks, apply, "ops-state", "ops-dev-project"); !strings.HasPrefix(second.line, "refused") {
		t.Errorf("second tenant's process touching account stacks: %q, want refused", second.line)
	}
	if got := applied(t, apply); !slices.Equal(got, []string{"demo-dev-project", "demo-state"}) {
		t.Errorf("applied %q, want only tenant demo's stacks", got)
	}
	wantAdmitted(t, m, locks, "ops-dev-project")
	first.release(t)
	wantAdmitted(t, m, locks, "ops-state", "ops-dev-project")
}

// A run refused on its tenant lock does not keep the account lock it took first.
func TestLockPartialRefusalReleases(t *testing.T) {
	data, m := lockManifest(t)
	locks, apply := t.TempDir(), t.TempDir()
	first := startRun(t, data, locks, apply, "demo-dev-project")
	mustHold(t, first)
	wantRefused(t, m, locks, "account-governance", "demo-dev-gra11-runtime")
	wantAdmitted(t, m, locks, "account-governance")
	first.release(t)
}

// A lock whose holder died is free: a killed holder blocks no later run.
func TestLockStaleHolder(t *testing.T) {
	data, m := lockManifest(t)
	locks, apply := t.TempDir(), t.TempDir()
	first := startRun(t, data, locks, apply, "account-governance", "demo-dev-project")
	mustHold(t, first)
	wantRefused(t, m, locks, "demo-dev-project")
	first.kill()
	wantAdmitted(t, m, locks, "account-governance", "demo-dev-project")
}

// A released run frees its locks for the next one in the same process.
func TestLockRelease(t *testing.T) {
	_, m := lockManifest(t)
	locks := t.TempDir()
	for i := 0; i < 3; i++ {
		wantAdmitted(t, m, locks, "account-governance", "demo-dev-project", "ops-dev-project")
	}
}

// A second holder in the same process is refused too (a per-process lock such as a POSIX record
// lock would admit it).
func TestLockSecondHolderSameProcess(t *testing.T) {
	_, m := lockManifest(t)
	locks := t.TempDir()
	release, err := hold(t, m, locks, "demo-dev-project")
	if err != nil {
		t.Fatalf("first run refused: %v", err)
	}
	wantRefused(t, m, locks, "demo-dev-gra11-runtime")
	if err := release(); err != nil {
		t.Fatal(err)
	}
	wantAdmitted(t, m, locks, "demo-dev-gra11-runtime")
}

// A lock name is `account` or `tenant-<t>`, never a path: DirLocks refuses any other name before
// it creates anything, so no lock file lands outside the lock directory; a release is idempotent
// (005 T041).
func TestLockNameStaysInDir(t *testing.T) {
	parent := t.TempDir()
	store := DirLocks(filepath.Join(parent, "locks"))
	for _, name := range []string{"../escape", "a/b", "", ".hidden", "Account", "tenant-demo/../../x"} {
		if release, err := store.TryLock(name); err == nil {
			_ = release()
			t.Errorf("TryLock(%q) admitted", name)
		}
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("refused lock names created %d entries under the parent", len(entries))
	}
	release, err := store.TryLock("tenant-demo")
	if err != nil {
		t.Fatalf("TryLock(tenant-demo): %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Errorf("second release: %v, want the first result again", err)
	}
}
