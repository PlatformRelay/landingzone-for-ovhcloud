package main

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrepareCommandLine: `lz-pack prepare` reads every fetched artefact by
// its pin-table name.
func TestPrepareCommandLine(t *testing.T) {
	o, err := prepareOptions([]string{"-root", "/r", "-toolchain", "/t", "-crane", "/c", "-archive", "task=/a/task.tar.gz", "-archive", "git-2.56.0-r0.apk=/a/git.apk"})
	want := map[string]string{"task": "/a/task.tar.gz", "git-2.56.0-r0.apk": "/a/git.apk"}
	if err != nil || o.Root != "/r" || o.Toolchain != "/t" || o.Crane != "/c" || !maps.Equal(o.Archives, want) {
		t.Errorf("BEHAVIORAL_RED: options %+v, %v", o, err)
	}
}

// TestPrepareCommandLineRefused: a malformed command line is refused with the
// usage (exit 2) before anything is prepared.
func TestPrepareCommandLineRefused(t *testing.T) {
	valid := []string{"-root", "/r", "-toolchain", "/t", "-crane", "/c"}
	for name, args := range map[string][]string{
		"no-root":              {"-toolchain", "/t", "-crane", "/c"},
		"no-toolchain":         {"-root", "/r", "-crane", "/c"},
		"no-crane":             {"-root", "/r", "-toolchain", "/t"},
		"relative-archive":     append(valid, "-archive", "task=task.tar.gz"),
		"archive-without-name": append(valid, "-archive", "=/a/task.tar.gz"),
		"archive-without-file": append(valid, "-archive", "task"),
		"archive-twice":        append(valid, "-archive", "task=/a/one", "-archive", "task=/a/two"),
		"extra-argument":       append(valid, "extra"),
		"unknown-flag":         append(valid, "-bundle", "/b"),
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := runPackMain(t, append([]string{"prepare"}, args...)...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, "PREPARE_ARGS") || !strings.Contains(stderr, prepareUsage) {
				t.Errorf("BEHAVIORAL_RED: exit %d, stdout %q, stderr %q; want exit 2 with the usage", code, stdout, stderr)
			}
		})
	}
}

// TestPrepareCommandRefusedPreparation: a refused preparation exits 1 with
// its reason and leaves nothing under the root.
func TestPrepareCommandRefusedPreparation(t *testing.T) {
	root := t.TempDir()
	stdout, stderr, code := runPackMain(t, "prepare", "-root", root, "-toolchain", t.TempDir(), "-crane", "/absent/crane")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "PIN_ARCHIVE") {
		t.Errorf("BEHAVIORAL_RED: exit %d, stdout %q, stderr %q; want exit 1 naming the missing archive", code, stdout, stderr)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Errorf("BEHAVIORAL_RED: root holds %v after a refusal, %v", entries, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "bundle")); !os.IsNotExist(err) {
		t.Errorf("BEHAVIORAL_RED: bundle written: %v", err)
	}
}
