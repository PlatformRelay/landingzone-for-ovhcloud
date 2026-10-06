package live

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestGuardGitEnvironment: the guard's own git calls answer for the checkout it was given, not
// for whatever the caller's environment or git configuration points them at (T052 gap). A caller
// exporting GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE or GIT_CONFIG_* (or a global/system gitconfig
// with status.showUntrackedFiles, core.fsmonitor or replace refs) could otherwise steer the answers
// of rev-parse, status and merge-base.
func TestGuardGitEnvironment(t *testing.T) {
	w := newGuardWorld(t)
	for k, v := range map[string]string{
		"GIT_DIR":                filepath.Join(w.clone, ".git") + "-ambient",
		"GIT_WORK_TREE":          w.clone + "-ambient",
		"GIT_INDEX_FILE":         "/nonexistent/ambient-index",
		"GIT_COMMON_DIR":         "/nonexistent/ambient-common",
		"GIT_OBJECT_DIRECTORY":   "/nonexistent/ambient-objects",
		"GIT_CONFIG":             "/nonexistent/ambient-config",
		"GIT_CONFIG_GLOBAL":      "/nonexistent/ambient-gitconfig",
		"GIT_CONFIG_SYSTEM":      "/nonexistent/ambient-system",
		"GIT_CONFIG_COUNT":       "1",
		"GIT_CONFIG_KEY_0":       "status.showUntrackedFiles",
		"GIT_CONFIG_VALUE_0":     "no-ambient",
		"GIT_CONFIG_PARAMETERS":  "'core.fsmonitor'='ambient'",
		"GIT_NO_REPLACE_OBJECTS": "0-ambient",
	} {
		t.Setenv(k, v)
	}
	h := w.host(t, guardCase{})
	if err := h.Check(); err != nil {
		t.Fatalf("Check() = %v, want admitted", err)
	}
	calls := gitCalls(t, h.Git)
	if len(calls) == 0 {
		t.Fatal("no git calls logged")
	}
	for _, line := range calls {
		var c struct {
			Args []string `json:"args"`
			Argv []string `json:"argv"`
			Env  []string `json:"env"`
		}
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		for _, kv := range c.Env {
			if strings.Contains(kv, "ambient") || strings.HasPrefix(kv, "GIT_CONFIG_COUNT=") {
				t.Errorf("git %v ran with the caller's %s", c.Args, kv)
			}
		}
		for _, want := range []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_REPLACE_OBJECTS=1"} {
			if !slices.Contains(c.Env, want) {
				t.Errorf("git %v ran without %s (env %v)", c.Args, want, c.Env)
			}
		}
		// core.fsmonitor from the repository's own config (shared by every linked worktree) runs
		// a program that can answer status for git.
		sub := len(c.Argv) - len(c.Args)
		if i := slices.Index(c.Argv, "core.fsmonitor=false"); i < 1 || i >= sub || c.Argv[i-1] != "-c" {
			t.Errorf("git %v ran without -c core.fsmonitor=false before the subcommand (argv %v)", c.Args, c.Argv)
		}
		// Stat shortcuts in the repository config let a same-size edit with a restored mtime
		// read as clean (host git 2.53.0, T053 review round 2).
		for _, want := range []string{"core.checkStat=default", "core.trustctime=true"} {
			if i := slices.Index(c.Argv, want); i < 1 || i >= sub || c.Argv[i-1] != "-c" {
				t.Errorf("git %v ran without -c %s (argv %v)", c.Args, want, c.Argv)
			}
		}
		// submodule.<name>.ignore=all in the repository config hides a dirty submodule.
		if len(c.Args) > 0 && c.Args[0] == "status" && !slices.Contains(c.Args, "--ignore-submodules=none") {
			t.Errorf("status without --ignore-submodules=none: %v", c.Args)
		}
		// A local branch or tag named origin/main shadows the remote-tracking ref.
		if len(c.Args) > 0 && c.Args[0] == "merge-base" && c.Args[len(c.Args)-1] != "refs/remotes/origin/main" {
			t.Errorf("merge-base against %q, want refs/remotes/origin/main", c.Args[len(c.Args)-1])
		}
	}
}
