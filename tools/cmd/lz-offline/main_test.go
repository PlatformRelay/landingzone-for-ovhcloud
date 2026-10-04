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

func TestSnapshotHonoursDeadline(t *testing.T) {
	candidate, destination := candidateTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := snapshot(ctx, candidate, destination); err == nil {
		t.Error("BEHAVIORAL_RED: expired deadline admitted")
	}
}
