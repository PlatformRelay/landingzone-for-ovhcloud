package live

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Spec 007 T029 (stage B follow-ups, D131): probe start and `--cleanup` take one exclusive lock per
// run, a lock file in the run's directory; a lock left by a dead process (its pid gone, or reused
// by a process with another start time) is taken over, never one by age alone; a run directory
// that holds nothing but its claim is retired by a plain `--cleanup`.

// rlLock writes a run lock in dir as a holder with pid and start would.
func rlLock(t *testing.T, dir string, pid int, start string) string {
	t.Helper()
	p := filepath.Join(dir, runLockName)
	if err := os.WriteFile(p, fmt.Appendf(nil, "%s=%d\n%s=%s\n", lockPIDKey, pid, lockStartKey, start), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// rlSelfStart is this process's start time, field 22 of /proc/self/stat (read here, not through
// the code under test).
func rlSelfStart(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Skipf("no /proc: %v", err)
	}
	s := string(raw)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return fields[19]
}

// rlDeadPID is the pid of a process that ran and was reaped.
func rlDeadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// rlHolder is a lock's holder for a row: "live" (this process), "old-but-live" (this process, a
// file 25 years old), "dead" (a reaped pid), "reused-pid" (this pid, another start time).
func rlHolder(t *testing.T, dir, holder string) string {
	t.Helper()
	switch holder {
	case "live":
		return rlLock(t, dir, os.Getpid(), rlSelfStart(t))
	case "old-but-live":
		p := rlLock(t, dir, os.Getpid(), rlSelfStart(t))
		old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	case "dead":
		return rlLock(t, dir, rlDeadPID(t), "1")
	case "reused-pid":
		return rlLock(t, dir, os.Getpid(), "1")
	}
	t.Fatalf("unknown holder %q", holder)
	return ""
}

// rlFailedRun is a probe run whose destroy failed (unreconciled, its files kept); its next destroy
// passes.
func rlFailedRun(t *testing.T) (*runWorld, Probe, string) {
	t.Helper()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{DestroyExit: 1})
	if err := p.Start(context.Background()); err == nil {
		t.Fatal("premise: a probe whose destroy failed passed")
	}
	w.scenario.Stacks["p"] = tofuStack{ApplyStream: w.scenario.Stacks["p"].ApplyStream}
	w.save(t)
	return w, p, root
}

// rlRefused fails unless err is a run-locked refusal naming runID.
func rlRefused(t *testing.T, what string, err error, runID string) {
	t.Helper()
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Condition != CondRunLocked {
		t.Errorf("%s: %v; want refused (%s)", what, err, CondRunLocked)
	} else if !strings.Contains(ref.Detail, runID) {
		t.Errorf("%s: refusal %q does not name run %s", what, ref.Detail, runID)
	}
}

// TestRunLockCleanup: given an unreconciled run whose directory holds a run lock / when
// `--cleanup` runs / then a lock held by a live process (however old its file) refuses the cleanup
// naming the run, before any tofu call, the run's files untouched; a lock of a dead process, or of
// a pid now running with another start time, is taken over and the cleanup retires the run →
// exposes a cleanup that runs beside a live start, or a stale lock that blocks forever or is
// judged by age.
func TestRunLockCleanup(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		holder string
		held   bool
	}{{"live", true}, {"old-but-live", true}, {"dead", false}, {"reused-pid", false}} {
		t.Run(c.holder, func(t *testing.T) {
			t.Parallel()
			rlCleanupRow(t, c.holder, c.held)
		})
	}
}

func rlCleanupRow(t *testing.T, holder string, held bool) {
	w, p, root := rlFailedRun(t)
	dir, _, _ := probeFiles(t, root)
	rlHolder(t, dir, holder)
	before, n := treeSnapshot(t, dir), len(w.calls(t, "tofu.log"))
	err := p.Cleanup(context.Background())
	if held {
		rlRefused(t, "a cleanup beside a live holder's lock", err, p.Run.ID)
		if m := len(w.calls(t, "tofu.log")); m != n {
			t.Errorf("the refused cleanup made %d tofu calls", m-n)
		}
		sameTree(t, "the locked run's directory", dir, before)
		return
	}
	if err != nil {
		t.Errorf("a cleanup beside a %s lock: %v; want the lock taken over and the run retired", holder, err)
	}
	if exists(dir) {
		t.Errorf("the cleanup beside a %s lock left %s", holder, dir)
	}
}

// TestRunLockStart: given a cleanup of the same run id that took the run's lock in the window
// right after the start claimed its directory / when the start goes on / then a live holder
// refuses the start naming the run, before any file or tofu call, and the holder's lock stays; a
// dead holder's lock is taken over and the start runs → exposes a start that writes beside a
// cleanup retiring its claim.
func TestRunLockStart(t *testing.T) {
	t.Parallel()
	for _, holder := range []string{"live", "dead"} {
		t.Run(holder, func(t *testing.T) {
			t.Parallel()
			rlStartRow(t, holder)
		})
	}
}

func rlStartRow(t *testing.T, holder string) {
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	var lock string
	var content []byte
	p.claimed = func(dir string) {
		lock = rlHolder(t, dir, holder)
		content, _ = os.ReadFile(lock)
	}
	err := p.Start(context.Background())
	if lock == "" {
		t.Fatal("the start never reached its claim window")
	}
	dir, _, _ := probeFiles(t, root)
	if holder == "dead" {
		if err != nil || exists(dir) {
			t.Errorf("a start beside a dead holder's lock: %v, directory left %v; want it taken over and the run to pass", err, exists(dir))
		}
		return
	}
	rlRefused(t, "a start beside a live holder's lock", err, p.Run.ID)
	if calls := w.calls(t, "tofu.log"); len(calls) > 0 {
		t.Errorf("the refused start made %d tofu calls", len(calls))
	}
	if exists(filepath.Join(dir, "probe.env")) {
		t.Error("the refused start wrote probe.env")
	}
	if got, _ := os.ReadFile(lock); string(got) != string(content) {
		t.Errorf("the refused start changed the holder's lock: %q, want %q", got, content)
	}
}

// TestRunLockHeldDuringRun: the lock is in the run's directory while the probe applies, and gone
// on every way out: a start whose destroy failed keeps its directory without the lock, a cleanup
// that fails likewise → exposes a lock never taken, or one left behind that blocks the cleanup.
func TestRunLockHeldDuringRun(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	dir, _, _ := probeFiles(t, root)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{DestroyExit: 1, ScanRoots: []string{dir}, ScanFor: []string{lockPIDKey + "="}})
	if err := p.Start(context.Background()); err == nil {
		t.Fatal("premise: a probe whose destroy failed passed")
	}
	lock := filepath.Join(dir, runLockName)
	if apply := pbApply(w.calls(t, "tofu.log")); apply == nil {
		t.Error("the probe root was never applied")
	} else if _, ok := pbHolders(apply)[lock]; !ok {
		t.Errorf("when the apply ran, %s was not there (holders %v)", lock, apply.Holders)
	}
	if !exists(filepath.Join(dir, "probe.env")) || exists(lock) {
		t.Errorf("after the failed start: probe.env kept %v, lock left %v; want the run kept and the lock released",
			exists(filepath.Join(dir, "probe.env")), exists(lock))
	}
	if err := p.Cleanup(context.Background()); err == nil {
		t.Fatal("premise: a cleanup whose destroy fails passed")
	}
	if !exists(filepath.Join(dir, "probe.env")) || exists(lock) {
		t.Errorf("after the failed cleanup: probe.env kept %v, lock left %v; want the run kept and the lock released",
			exists(filepath.Join(dir, "probe.env")), exists(lock))
	}
}

// ecRow is a run directory holding only its claim plus extra; retired says whether a plain
// `--cleanup` retires it.
type ecRow struct {
	name    string
	extra   func(t *testing.T, w *runWorld, root, dir string)
	retired bool
	locked  bool
	accept  bool // --accept-lost-baseline
}

func ecFile(name string) func(*testing.T, *runWorld, string, string) {
	return func(t *testing.T, _ *runWorld, _, dir string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("TF_VAR_state_passphrase=0123456789abcdef0123\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestProbeCleanupEmptyClaim: given a probe run directory without probe.env / when a plain
// `--cleanup <run-id>` runs / then a claim holding nothing, a dead holder's lock or a `.write-*`
// temporary is retired with no tofu call; a claim holding any other file (unknown, state,
// passphrase), or whose baseline has a copy, is not, and nothing is removed; a live holder's lock
// refuses it (run-locked) → exposes an empty claim that blocks every start until removed by hand,
// or a retire that removes a run something may exist for.
func TestProbeCleanupEmptyClaim(t *testing.T) {
	t.Parallel()
	for _, c := range []ecRow{
		{name: "empty", retired: true},
		{name: "accept-lost-baseline", accept: true},
		{name: "stale-lock", extra: func(t *testing.T, _ *runWorld, _, dir string) { rlHolder(t, dir, "dead") }, retired: true},
		{name: "write-temp", extra: ecFile(".write-12345"), retired: true},
		{name: "live-lock", extra: func(t *testing.T, _ *runWorld, _, dir string) { rlHolder(t, dir, "live") }, locked: true},
		{name: "other-file", extra: ecFile("notes.txt")},
		{name: "state", extra: ecFile("p.tfstate")},
		{name: "passphrase", extra: ecFile("passphrase.env")},
		{name: "recovery-copy", extra: func(t *testing.T, _ *runWorld, root, _ string) {
			writePrivateFile(t, pbRecovery(root, "20261006T120000Z-a1b2"), `{"run_id":"x"}`)
		}},
		// Review round 2: a passing run keeps its record copy, and retire removes the recovery copy
		// before probe.env, so a crash in retire after probe.env leaves exactly this.
		{name: "record-copy-after-retire", extra: func(t *testing.T, w *runWorld, _, dir string) {
			writePrivateFile(t, filepath.Join(w.runDir, "baseline.json"), `{"run_id":"x"}`)
			rlHolder(t, dir, "dead")
		}, retired: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ecCleanupRow(t, c)
		})
	}
}

// writePrivateFile writes content to path, its missing directories 0700.
func writePrivateFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirPrivate(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ecCleanupRow(t *testing.T, c ecRow) {
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	p.AcceptLostBaseline = c.accept
	dir, _, _ := probeFiles(t, root)
	mkdirPrivate(t, dir)
	if c.extra != nil {
		c.extra(t, w, root, dir)
	}
	state := pbStateDir(root)
	before := treeSnapshot(t, state)
	err := p.Cleanup(context.Background())
	if calls := w.calls(t, "tofu.log"); len(calls) > 0 {
		t.Errorf("the cleanup of a claim without probe.env made %d tofu calls", len(calls))
	}
	switch {
	case c.retired:
		if err != nil || exists(dir) {
			t.Errorf("plain --cleanup of an empty claim (%s): %v, directory left %v; want it retired", c.name, err, exists(dir))
		}
	case c.locked:
		rlRefused(t, "a cleanup of a claim beside a live holder's lock", err, p.Run.ID)
		sameTree(t, "the claimed run's state directory", state, before)
	default:
		if err == nil {
			t.Errorf("plain --cleanup of a claim with %s passed; want it refused", c.name)
		}
		sameTree(t, "the claimed run's state directory", state, before)
	}
}

// TestRunLockTakeoverSerialised (review rounds 1 and 2): given a dead holder's lock while another
// process is taking the run lock (it holds the run directory's flock, and may be stopped there) /
// when lockRun runs / then it refuses at once (run-locked, naming the run) and leaves the stale
// lock to that process; once the flock is free it takes the stale lock over → exposes two
// processes that both judge one stale lock and both take it, or a take that waits forever.
func TestRunLockTakeoverSerialised(t *testing.T) {
	t.Parallel()
	const id = "20261006T120000Z-a1b2"
	dir := tempPrivate(t)
	lock := rlHolder(t, dir, "dead")
	stale, _ := os.ReadFile(lock)
	d, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := syscall.Flock(int(d.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	rlRefused(t, "lockRun while another process takes the run lock", rlLockRun(t, dir, id), id)
	if got, _ := os.ReadFile(lock); string(got) != string(stale) {
		t.Errorf("the refused take changed the stale lock: %q, want %q", got, stale)
	}
	if err := syscall.Flock(int(d.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := rlLockRun(t, dir, id); err != nil {
		t.Errorf("lockRun once the run lock's flock is free: %v; want the stale lock taken over", err)
	}
}

// rlLockRun takes and releases the run lock of dir, failing the test if lockRun does not return
// within 5 s.
func rlLockRun(t *testing.T, dir, id string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		l, err := lockRun(dir, id)
		if err == nil {
			err = l.release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("lockRun did not return within 5 s")
		return nil
	}
}

// TestRunLockNotRegular (review round 1): a run.lock that is a FIFO or a symlink is refused at
// once (run-locked, naming the run) and left in place → exposes an open that blocks forever on a
// FIFO, or a lock read through a symlink.
func TestRunLockNotRegular(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"fifo", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			rlNotRegularRow(t, kind)
		})
	}
}

func rlNotRegularRow(t *testing.T, kind string) {
	dir := tempPrivate(t)
	lock := filepath.Join(dir, runLockName)
	if kind == "fifo" {
		if err := syscall.Mkfifo(lock, 0o600); err != nil {
			t.Fatal(err)
		}
	} else {
		target := rlLock(t, tempPrivate(t), rlDeadPID(t), "1")
		if err := os.Symlink(target, lock); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := lockRun(dir, "20261006T120000Z-a1b2")
		done <- err
	}()
	select {
	case err := <-done:
		rlRefused(t, "lockRun beside a "+kind+" run.lock", err, "20261006T120000Z-a1b2")
	case <-time.After(5 * time.Second):
		t.Fatalf("lockRun beside a %s run.lock did not return", kind)
	}
	if fi, err := os.Lstat(lock); err != nil || fi.Mode().IsRegular() {
		t.Errorf("the %s run.lock was replaced or removed (%v)", kind, err)
	}
}

// TestProbeStartEmptyClaimRefusal: given another run's directory holding only its claim / when a
// probe start runs / then it is refused unreconciled-run naming that run and its --cleanup, not
// manual removal, and leaves the other directory → exposes a refusal that still sends the owner to
// remove it by hand.
func TestProbeStartEmptyClaimRefusal(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	other := mkdirPrivate(t, filepath.Join(pbStateDir(root), "probes", pbRun2))
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{})
	err := p.Start(context.Background())
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Condition != CondUnreconciledRun {
		t.Fatalf("probe start beside an empty claim: %v; want refused (%s)", err, CondUnreconciledRun)
	}
	if !strings.Contains(ref.Detail, "--cleanup "+pbRun2) || strings.Contains(ref.Detail, "by hand") {
		t.Errorf("refusal %q: want it to name --cleanup %s and not manual removal", ref.Detail, pbRun2)
	}
	if !exists(other) {
		t.Error("the refused start removed the other run's claim")
	}
}
