package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func candidateTree(t *testing.T) (string, string) {
	t.Helper()
	candidate, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(candidate, "Taskfile.yml"), []byte("version: '3'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(candidate, "tools"), 0700); err != nil {
		t.Fatal(err)
	}
	return candidate, destination
}

func TestSnapshotCopiesAllowlistOnly(t *testing.T) {
	candidate, destination := candidateTree(t)
	for name, body := range map[string]string{"tools/a.go": "package a\n", "tools/.env": "SECRET=x\n", "secrets.txt": "x\n"} {
		if err := os.WriteFile(filepath.Join(candidate, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := snapshot(context.Background(), candidate, destination); err != nil {
		t.Fatalf("valid candidate rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "tools/a.go")); err != nil {
		t.Errorf("allowlisted file missing: %v", err)
	}
	for _, name := range []string{"tools/.env", "secrets.txt"} {
		if _, err := os.Stat(filepath.Join(destination, name)); err == nil {
			t.Errorf("BEHAVIORAL_RED: %s copied", name)
		}
	}
}

func TestSnapshotRejects(t *testing.T) {
	cases := map[string]func(t *testing.T, candidate string){
		"symlink": func(t *testing.T, candidate string) {
			if err := os.Symlink("/etc/passwd", filepath.Join(candidate, "tools/link")); err != nil {
				t.Fatal(err)
			}
		},
		"fifo": func(t *testing.T, candidate string) {
			if err := syscall.Mkfifo(filepath.Join(candidate, "tools/pipe"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"hardlink": func(t *testing.T, candidate string) {
			path := filepath.Join(candidate, "tools/a.go")
			if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(path, filepath.Join(candidate, "tools/b.go")); err != nil {
				t.Fatal(err)
			}
		},
		// Excluded names still cost an entry; otherwise a candidate can make
		// the host enumerate without bound.
		"excluded-entry-budget": func(t *testing.T, candidate string) {
			for i := 0; i <= maxEntries; i++ {
				if err := os.WriteFile(filepath.Join(candidate, "tools/.env."+strconv.Itoa(i)), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
		},
		"oversized-file": func(t *testing.T, candidate string) {
			if err := os.WriteFile(filepath.Join(candidate, "tools/big"), make([]byte, maxFileBytes+1), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"depth": func(t *testing.T, candidate string) {
			path := filepath.Join(candidate, "tools", strings.Repeat("d/", maxDepth+1))
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			candidate, destination := candidateTree(t)
			setup(t, candidate)
			done := make(chan error, 1)
			go func() { done <- snapshot(context.Background(), candidate, destination) }()
			select {
			case err := <-done:
				if err == nil {
					t.Errorf("BEHAVIORAL_RED: %s admitted", name)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("BEHAVIORAL_RED: %s stalled the snapshot", name)
			}
		})
	}
}

// A stalled filesystem call cannot observe the context; the supervisor must
// return at the deadline regardless.
func TestSuperviseStopsStalledWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	stalled := make(chan struct{})
	defer close(stalled)
	started := time.Now()
	err := supervise(ctx, func() error { <-stalled; return nil })
	if err == nil || time.Since(started) > 5*time.Second {
		t.Errorf("BEHAVIORAL_RED: stalled work not stopped at the deadline: err=%v after %v", err, time.Since(started))
	}
	if err := supervise(context.Background(), func() error { return nil }); err != nil {
		t.Errorf("completed work rejected: %v", err)
	}
}

func TestSnapshotDeadlineDuringWork(t *testing.T) {
	candidate, destination := candidateTree(t)
	for i := 0; i < 2000; i++ {
		if err := os.WriteFile(filepath.Join(candidate, "tools", "f"+strconv.Itoa(i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := supervise(ctx, func() error { return snapshot(ctx, candidate, destination) }); err == nil {
		t.Error("BEHAVIORAL_RED: deadline expiring during the snapshot admitted")
	}
}

// After cleanup removes the destination, a late snapshot must not recreate it.
func TestSnapshotDoesNotRecreateRemovedDestination(t *testing.T) {
	candidate, _ := candidateTree(t)
	if err := os.WriteFile(filepath.Join(candidate, "tools/a.go"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	destination := filepath.Join(scratch, "candidate")
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatal(err)
	}
	if err := snapshot(context.Background(), candidate, destination); err == nil {
		t.Error("BEHAVIORAL_RED: snapshot into a removed destination succeeded")
	}
	if _, err := os.Stat(scratch); err == nil {
		t.Error("BEHAVIORAL_RED: snapshot recreated removed scratch")
	}
}

func TestTargetAdmission(t *testing.T) {
	for _, name := range []string{"verify:toolchain", "test:offline-boundary", "probe", "capture:tofu-pass", "test:reports"} {
		if err := admitTarget(name); err != nil {
			t.Errorf("target %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", "-x", "--taskfile", "a b", "../x", "x/y", "Upper", "x;y", strings.Repeat("a", 65)} {
		if err := admitTarget(name); err == nil {
			t.Errorf("BEHAVIORAL_RED: target %q admitted", name)
		}
	}
}

// Stdout and stderr are relayed separately but share one budget, so a captured
// stream stays clean and neither channel can exceed the cap.
func TestChildOutputSharedBudget(t *testing.T) {
	cancelled := false
	stdout, stderr := newChildOutput(func() { cancelled = true })
	if _, err := stdout.Write([]byte("{\"type\":\"version\"}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := stderr.Write([]byte("task: [x] tofu test\n")); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "{\"type\":\"version\"}\n" || stderr.String() != "task: [x] tofu test\n" {
		t.Fatalf("channels mixed: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	half := make([]byte, maxChildOutput/2)
	if _, err := stdout.Write(half); err != nil {
		t.Fatal(err)
	}
	if _, err := stderr.Write(half); err == nil || !cancelled || !stdout.overflowed() {
		t.Errorf("BEHAVIORAL_RED: shared budget exceeded without overflow: err=%v cancelled=%v", err, cancelled)
	}
}

func TestSnapshotHonoursDeadline(t *testing.T) {
	candidate, destination := candidateTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := snapshot(ctx, candidate, destination); err == nil {
		t.Error("BEHAVIORAL_RED: expired deadline admitted")
	}
}
