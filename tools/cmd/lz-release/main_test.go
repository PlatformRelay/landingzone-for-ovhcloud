package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/release"
)

// Tests of 006 T038 (FR-016) for the lz-release command: it prints exactly what release.Manifest
// renders, and refuses without printing a manifest.

// commandRepo builds a one-commit repository with git isolated from the host and returns it with
// the commit's id.
func commandRepo(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_AUTHOR_DATE=2026-09-01T00:00:00+00:00", "GIT_COMMITTER_DATE=2026-09-01T00:00:00+00:00"}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, "modules", "naming"), 0o700); err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		"mise.toml":              "[tools]\ngo = \"1.27.1\"\n",
		"modules/naming/main.tf": "# naming\n",
	} {
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "--object-format=sha1", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "--no-verify", "-m", ":tada: chore(repo): start")
	return repo, git("rev-parse", "HEAD")
}

func runCommand(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestReleaseCommand(t *testing.T) {
	repo, commit := commandRepo(t)
	want, err := release.Manifest(repo, commit, "2026.10.0")
	if err != nil {
		t.Fatalf("control: Manifest: %v", err)
	}
	t.Run("prints-the-manifest", func(t *testing.T) {
		code, stdout, stderr := runCommand("manifest", "--repo", repo, "--commit", commit, "--version", "2026.10.0")
		if code != 0 || stdout != string(want) || stderr != "" {
			t.Errorf("exit %d, stdout %q, stderr %q; want 0 and the manifest only", code, stdout, stderr)
		}
	})
	t.Run("repository-from-a-subdirectory", func(t *testing.T) {
		t.Chdir(filepath.Join(repo, "modules"))
		code, stdout, stderr := runCommand("manifest", "--commit", commit, "--version", "2026.10.0")
		if code != 0 || stdout != string(want) {
			t.Errorf("exit %d, stdout %q, stderr %q; want 0 and the manifest", code, stdout, stderr)
		}
	})
	for name, c := range map[string]struct {
		code int
		rule string
		args []string
	}{
		"malformed-version":  {1, "RELEASE_VERSION ", []string{"manifest", "--repo", repo, "--commit", commit, "--version", "2026.13.0"}},
		"abbreviated-commit": {1, "RELEASE_COMMIT ", []string{"manifest", "--repo", repo, "--commit", commit[:12], "--version", "2026.10.0"}},
		"no-arguments":       {2, "usage", nil},
		"other-subcommand":   {2, "usage", []string{"render", "--repo", repo, "--commit", commit, "--version", "2026.10.0"}},
		"no-commit":          {2, "usage", []string{"manifest", "--repo", repo, "--version", "2026.10.0"}},
		"no-version":         {2, "usage", []string{"manifest", "--repo", repo, "--commit", commit}},
		"extra-argument":     {2, "usage", []string{"manifest", "--repo", repo, "--commit", commit, "--version", "2026.10.0", "extra"}},
		"unknown-flag":       {2, "usage", []string{"manifest", "--repo", repo, "--commit", commit, "--version", "2026.10.0", "--write"}},
	} {
		t.Run("refused/"+name, func(t *testing.T) {
			code, stdout, stderr := runCommand(c.args...)
			if code != c.code || stdout != "" || !strings.Contains(stderr, c.rule) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit %d, nothing on stdout, %q on stderr", code, stdout, stderr, c.code, c.rule)
			}
		})
	}
}
