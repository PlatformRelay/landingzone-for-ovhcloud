package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Admission of the Terramate stacks spec 005 generates (001 T024): `stacks/` and the root
// `terramate.tm.hcl` are data for the stack checks, admitted under the same limits as every
// other snapshotted path. The fixture holds the generated shapes as path → content.

// stacksCandidate writes the generated-stack fixture into a fresh candidate tree.
func stacksCandidate(t *testing.T) (string, string, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", "stacks-candidate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var files map[string]string
	if err := json.Unmarshal(data, &files); err != nil {
		t.Fatal(err)
	}
	candidate, destination := candidateTree(t)
	for name, body := range files {
		writeCandidate(t, candidate, name, body, 0600)
	}
	return candidate, destination, files
}

func writeCandidate(t *testing.T, candidate, name, body string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(candidate, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// snapshotFiles describes every entry of a snapshot without following links, keyed by
// slash-separated path: its type and permissions, and for a regular file its content.
func snapshotFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		description := info.Mode().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			description = fileEntry(info.Mode(), string(data))
		}
		files[filepath.ToSlash(relative)] = description
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// fileEntry is snapshotFiles' description of a regular file.
func fileEntry(mode os.FileMode, body string) string { return mode.String() + " " + body }

func TestSnapshotAdmitsGeneratedStacks(t *testing.T) {
	candidate, destination, files := stacksCandidate(t)
	if err := snapshot(context.Background(), candidate, destination); err != nil {
		t.Fatalf("generated-stack candidate rejected: %v", err)
	}
	got := snapshotFiles(t, destination)
	for name, body := range files {
		data, ok := got[name]
		switch {
		case !ok:
			t.Errorf("BEHAVIORAL_RED: %s missing from the snapshot", name)
		case data != fileEntry(0600, body):
			t.Errorf("BEHAVIORAL_RED: %s changed in the snapshot: %q", name, data)
		}
	}
}

// A hostile file under stacks/ is copied with its bytes and mode below stacks/ and nowhere
// else: every entry of the snapshot outside stacks/ (the Taskfile candidate targets run, the
// root configuration, the names the boundary suite plants) is identical with and without it,
// and excluded names stay out. That the entry's trusted targets never read stacks/ is not
// visible here: they never snapshot (run), which the boundary suite holds.
func TestSnapshotStacksStayData(t *testing.T) {
	hostile := map[string]string{
		"stacks/Taskfile.yml":              "version: '3'\ntasks:\n  verify:toolchain:\n    cmds:\n      - echo LZ_OBSERVATION stacks-taskfile-ran\n",
		"stacks/terramate.tm.hcl":          "terramate {\n  required_version = \"0.0.1\"\n}\n",
		"stacks/included.yml":              "version: '3'\ntasks:\n  attack:\n    cmds:\n      - ./probe.sh\n",
		"stacks/account/bootstrap/main.tf": "resource \"terraform_data\" \"x\" {\n  provisioner \"local-exec\" {\n    command = \"echo LZ_OBSERVATION\"\n  }\n}\n",
	}
	executables := map[string]string{
		"stacks/probe.sh":   "#!/bin/sh\necho LZ_OBSERVATION stacks-probe-ran\n",
		"stacks/bin/task":   "#!/bin/sh\nexec ./probe.sh\n",
		"stacks/lz-offline": "#!/bin/sh\nexec ./probe.sh\n",
	}
	excluded := []string{"stacks/.env", "stacks/.env.local", "stacks/.envrc", "stacks/.git/config",
		"stacks/account/bootstrap/.terraform/terraform.tfstate", "stacks/.cache/x", "stacks/.local/x"}

	base, baseDestination, _ := stacksCandidate(t)
	if err := snapshot(context.Background(), base, baseDestination); err != nil {
		t.Fatalf("base candidate rejected: %v", err)
	}
	candidate, destination, _ := stacksCandidate(t)
	for name, body := range hostile {
		writeCandidate(t, candidate, name, body, 0600)
	}
	for name, body := range executables {
		writeCandidate(t, candidate, name, body, 0700)
	}
	for _, name := range excluded {
		writeCandidate(t, candidate, name, "SECRET=x\n", 0600)
	}
	if err := snapshot(context.Background(), candidate, destination); err != nil {
		t.Fatalf("candidate with hostile stacks/ files rejected: %v", err)
	}
	got, want := snapshotFiles(t, destination), snapshotFiles(t, baseDestination)
	for mode, set := range map[os.FileMode]map[string]string{0600: hostile, 0700: executables} {
		for name, body := range set {
			if data, ok := got[name]; !ok || data != fileEntry(mode, body) {
				t.Errorf("BEHAVIORAL_RED: hostile %s not kept verbatim below stacks/ (present=%v)", name, ok)
			}
		}
	}
	for _, name := range excluded {
		if _, ok := got[name]; ok {
			t.Errorf("BEHAVIORAL_RED: excluded %s copied from below stacks/", name)
		}
	}
	for name, body := range got {
		if strings.HasPrefix(name, "stacks/") {
			continue
		}
		if data, ok := want[name]; !ok || data != body {
			t.Errorf("BEHAVIORAL_RED: hostile stacks/ files changed %s outside stacks/", name)
		}
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("BEHAVIORAL_RED: hostile stacks/ files removed %s", name)
		}
	}
	if _, ok := want["terramate.tm.hcl"]; !ok {
		t.Error("BEHAVIORAL_RED: root terramate.tm.hcl missing, so the comparison proves nothing")
	}
}

// Only terramate.tm.hcl is admitted at the root: Terramate reads every root *.tm and *.tm.hcl
// file, so a second root file would be configuration the allowlist never named.
func TestSnapshotRootAllowlistWithStacks(t *testing.T) {
	candidate, destination, _ := stacksCandidate(t)
	outside := []string{"other.tm.hcl", "terramate.tm", "stack.tm.hcl", "Terramate.tm.hcl", "terramate.tm.hcl.bak",
		"terramate.tm.hcl~", ".terramate.tm.hcl", "deployments.yaml", "stacks.yaml", "config.tm.json", "lz.tm.hcl"}
	for _, name := range outside {
		writeCandidate(t, candidate, name, "terramate {}\n", 0600)
	}
	if err := snapshot(context.Background(), candidate, destination); err != nil {
		t.Fatalf("candidate rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "terramate.tm.hcl")); err != nil {
		t.Errorf("BEHAVIORAL_RED: root terramate.tm.hcl missing: %v", err)
	}
	for _, name := range outside {
		if _, err := os.Lstat(filepath.Join(destination, name)); err == nil {
			t.Errorf("BEHAVIORAL_RED: root file %s outside the allowlist copied", name)
		}
	}
}

// The limits of TestSnapshotRejects apply below stacks/ and to the new root file. Each case
// starts from the admissible generated-stack candidate, so only the planted entry differs.
func TestSnapshotRejectsUnderStacks(t *testing.T) {
	stack := "stacks/tenants/demo/dev/gra11/runtime-blue"
	cases := map[string]func(t *testing.T, candidate string){
		// Each link target is admissible, so following the link would succeed.
		"symlink": func(t *testing.T, candidate string) {
			outside := filepath.Join(t.TempDir(), "_lz_extra.tf")
			if err := os.WriteFile(outside, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(candidate, stack, "_lz_link.tf")); err != nil {
				t.Fatal(err)
			}
		},
		"linked-directory": func(t *testing.T, candidate string) {
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "_lz_extra.tf"), []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(candidate, stack, "tests", "linked")); err != nil {
				t.Fatal(err)
			}
		},
		"linked-stacks": func(t *testing.T, candidate string) {
			moved := filepath.Join(filepath.Dir(candidate), filepath.Base(candidate)+"-stacks")
			if err := os.Rename(filepath.Join(candidate, "stacks"), moved); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(moved) })
			if err := os.Symlink(moved, filepath.Join(candidate, "stacks")); err != nil {
				t.Fatal(err)
			}
		},
		"linked-root-config": func(t *testing.T, candidate string) {
			path := filepath.Join(candidate, "terramate.tm.hcl")
			if err := os.Rename(path, filepath.Join(candidate, "tools", "terramate.tm.hcl")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("tools/terramate.tm.hcl", path); err != nil {
				t.Fatal(err)
			}
		},
		"fifo": func(t *testing.T, candidate string) {
			if err := syscall.Mkfifo(filepath.Join(candidate, stack, "_lz_pipe.tf"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"fifo-root-config": func(t *testing.T, candidate string) {
			path := filepath.Join(candidate, "terramate.tm.hcl")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"hardlink": func(t *testing.T, candidate string) {
			if err := os.Link(filepath.Join(candidate, stack, "_lz_main.tf"), filepath.Join(candidate, stack, "_lz_copy.tf")); err != nil {
				t.Fatal(err)
			}
		},
		"excluded-entry-budget": func(t *testing.T, candidate string) {
			for i := 0; i <= maxEntries; i++ {
				if err := os.WriteFile(filepath.Join(candidate, "stacks", ".env."+strconv.Itoa(i)), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
		},
		"oversized-file": func(t *testing.T, candidate string) {
			if err := os.WriteFile(filepath.Join(candidate, stack, "_lz_big.tf"), make([]byte, maxFileBytes+1), 0600); err != nil {
				t.Fatal(err)
			}
		},
		// Each file fits the per-file limit; together with the fixture they exceed the total.
		// Sparse, so only the snapshot's copy costs space.
		"total-size": func(t *testing.T, candidate string) {
			for i := 0; i < maxTotalBytes/maxFileBytes; i++ {
				path := filepath.Join(candidate, stack, "_lz_part"+strconv.Itoa(i)+".tf")
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Truncate(path, maxFileBytes); err != nil {
					t.Fatal(err)
				}
			}
		},
		"oversized-root-config": func(t *testing.T, candidate string) {
			if err := os.WriteFile(filepath.Join(candidate, "terramate.tm.hcl"), make([]byte, maxFileBytes+1), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"depth": func(t *testing.T, candidate string) {
			if err := os.MkdirAll(filepath.Join(candidate, "stacks", strings.Repeat("d/", maxDepth+1)), 0700); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			candidate, destination, _ := stacksCandidate(t)
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

// The depth limit counts from the admitted top-level name, as for tools/: the deepest
// path that fits is admitted below stacks/ too, so the depth case above is not refused
// for another reason.
func TestSnapshotStacksDepthBoundary(t *testing.T) {
	candidate, destination, _ := stacksCandidate(t)
	path := filepath.Join(candidate, "stacks", strings.Repeat("d/", maxDepth-1), "f")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot(context.Background(), candidate, destination); err != nil {
		t.Fatalf("path at the depth limit rejected: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "stacks", strings.Repeat("d/", maxDepth-1), "f"))
	if err != nil || !bytes.Equal(data, []byte("x")) {
		t.Errorf("BEHAVIORAL_RED: file at the depth limit below stacks/ not copied: %v", err)
	}
}
