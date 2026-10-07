package stacks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pinned Terramate resolution of lz-stacks (005 T036): the same order as the reconciler controls
// (LZ_TERRAMATE, else the entry's /tcb/terramate, else terramate on PATH), and the binary must
// report the version mise.toml at the project root pins.

// fakeTerramate writes an executable that prints version for `terramate version`.
func fakeTerramate(t *testing.T, dir, version string) string {
	t.Helper()
	path := filepath.Join(dir, "terramate")
	writeFile(t, path, []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo "+version+"; exit 0; fi\nexit 3\n"))
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func pinRoot(t *testing.T, mise string) string {
	t.Helper()
	root := t.TempDir()
	if mise != "" {
		writeFile(t, filepath.Join(root, "mise.toml"), []byte(mise))
	}
	return root
}

const pinMise = "[tools]\ngo = \"1.27.1\"\n\"aqua:terramate-io/terramate\" = \"0.17.3\"\n"

func TestPinnedTerramateResolution(t *testing.T) {
	right := fakeTerramate(t, t.TempDir(), "0.17.3")
	wrong := fakeTerramate(t, t.TempDir(), "0.17.1")
	onPath := t.TempDir()
	fakeTerramate(t, onPath, "0.17.3")
	empty := t.TempDir()
	cases := []struct {
		name, mise, override, tcb, path string
		want                            string // resolved binary, or "" for a refusal
		refusal                         string // substring of the refusal
	}{
		{"override wins over tcb", pinMise, right, wrong, empty, right, ""},
		{"override with another version is refused, not skipped", pinMise, wrong, right, onPath, "", `"0.17.1"`},
		{"tcb when no override", pinMise, "", right, empty, right, ""},
		{"tcb with another version is refused, not skipped", pinMise, "", wrong, onPath, "", `"0.17.1"`},
		{"PATH when no override and no tcb", pinMise, "", filepath.Join(empty, "absent"), onPath, filepath.Join(onPath, "terramate"), ""},
		{"no binary at all", pinMise, "", filepath.Join(empty, "absent"), empty, "", "LZ_TERRAMATE"},
		{"no mise.toml", "", right, right, onPath, "", "mise.toml"},
		{"mise.toml without a terramate pin", "[tools]\ngo = \"1.27.1\"\n", right, right, onPath, "", "pins no terramate"},
		{"pin compared exactly, not as a prefix", strings.Replace(pinMise, "0.17.3", "0.17", 1), right, right, onPath, "", `"0.17.3"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PATH", c.path)
			got, err := resolveTerramate(pinRoot(t, c.mise), c.override, c.tcb)
			if c.want != "" {
				if err != nil || got != c.want {
					t.Fatalf("resolved %q, %v; want %q", got, err, c.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.refusal) {
				t.Fatalf("resolved %q, %v; want a refusal mentioning %s", got, err, c.refusal)
			}
		})
	}
}

// The exported entry point reads LZ_TERRAMATE.
func TestPinnedTerramateReadsOverride(t *testing.T) {
	wrong := fakeTerramate(t, t.TempDir(), "0.17.1")
	t.Setenv("LZ_TERRAMATE", wrong)
	if _, err := PinnedTerramate(pinRoot(t, pinMise)); err == nil || !strings.Contains(err.Error(), wrong) {
		t.Fatalf("PinnedTerramate with LZ_TERRAMATE=%s: %v; want its version refused", wrong, err)
	}
}
