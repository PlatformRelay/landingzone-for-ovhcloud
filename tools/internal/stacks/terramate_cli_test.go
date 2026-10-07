package stacks

import (
	"os"
	"path/filepath"
	"testing"
)

// Every Terramate run lz-stacks makes (version check, create, generate, the stacks:check
// generate) leaves HOME untouched (apart from the empty ~/.terraform.d below) and its own temporary
// directory removed (005 T038): the pinned
// Terramate 0.17.3 otherwise writes ~/.terramate.d (checkpoint cache, checkpoint and analytics
// signatures) and opens an HTTPS connection on every run; it reads no TM_DISABLE_CHECKPOINT, only
// its CLI configuration (TM_CLI_CONFIG_FILE: disable_checkpoint, disable_checkpoint_signature,
// disable_telemetry, user_terramate_dir; ui/tui/cliconfig of v0.17.3). That no connection is
// opened is shown by strace in evidence/T038.md, not here.
func TestTerramateLeavesHomeEmpty(t *testing.T) {
	bin := pinnedTerramate(t)
	root := generationRoot(t, manifestOf(t, "sandbox"))
	home, tmp := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", tmp)
	t.Setenv("LZ_TERRAMATE", bin)
	if _, err := PinnedTerramate(repositoryRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(ReconcileOptions{Root: root, Terramate: bin}); err != nil {
		t.Fatal(err)
	}
	if err := Generate(GenerateOptions{Root: root, Terramate: bin}); err != nil {
		t.Fatal(err)
	}
	if findings, err := CheckStacks(root, bin); err != nil || len(findings) != 0 {
		t.Fatalf("stacks:check of the generated root: %q, %v", findings, err)
	}
	// Terramate runs `tofu -version` from PATH (strace, evidence/T038.md), and that tofu creates an
	// empty ~/.terraform.d: the one entry admitted, and only empty.
	tofuDir := filepath.Join(home, ".terraform.d")
	for _, dir := range []string{home, tmp} {
		var left []string
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if path != dir && !(path == tofuDir && d.IsDir()) {
				left = append(left, path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(left) != 0 {
			t.Errorf("Terramate runs left %q", left)
		}
	}
}
