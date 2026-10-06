package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The entry point runs the host guard (tools/internal/live) for every verb before anything else
// and maps its refusal to exit 3 (FR-011). Every host fact is injected: HOME is a temporary
// directory, so no test reads the owner's ~/.config/ovh-lz/.

const fakeHead = "1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b"

// TestMain doubles the test binary as a fake git for a clean main checkout whose HEAD is
// fakeHead on origin/main; it logs each call next to itself.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "git" {
		os.Exit(fakeGit(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeGit(args []string) int {
	dir := filepath.Dir(os.Args[0])
	if f, err := os.OpenFile(filepath.Join(dir, "git.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
	}
	for len(args) > 0 {
		if len(args) > 1 && (args[0] == "-c" || args[0] == "-C") {
			args = args[2:]
		} else if strings.HasPrefix(args[0], "--no-") {
			args = args[1:]
		} else {
			break
		}
	}
	cwd, _ := os.Getwd()
	switch {
	case len(args) > 0 && args[0] == "status":
		return 0
	case len(args) > 0 && args[0] == "merge-base":
		return 0
	case len(args) > 0 && args[0] == "ls-files":
		fmt.Print("H README.md\x00")
		return 0
	case len(args) > 0 && args[0] == "rev-parse":
		for _, a := range args[1:] {
			switch a {
			case "--git-dir", "--git-common-dir", "--absolute-git-dir":
				fmt.Println(filepath.Join(cwd, ".git"))
			case "--show-toplevel":
				fmt.Println(cwd)
			case "HEAD", "HEAD^{commit}":
				fmt.Println(fakeHead)
			}
		}
		return 0
	}
	return 128
}

type world struct {
	home, checkout, git, marker string
	env                         map[string]string
}

func newWorld(t *testing.T, withLiveEnv bool) world {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := world{
		home:     filepath.Join(base, "home"),
		checkout: filepath.Join(base, "checkout"),
		git:      filepath.Join(base, "bin", "git"),
		marker:   filepath.Join(base, "tcb"),
		env:      map[string]string{},
	}
	for _, d := range []string{w.home, w.checkout + "/.git", filepath.Dir(w.git)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, w.git); err != nil {
		t.Fatal(err)
	}
	if withLiveEnv {
		cfg := filepath.Join(w.home, ".config", "ovh-lz")
		if err := os.MkdirAll(cfg, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg, "live.env"), []byte("LZ_OWNER_CHECKOUT="+w.checkout+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (w world) run(args ...string) (int, string) {
	var stderr bytes.Buffer
	code := run(args, deps{
		Getwd:         func() (string, error) { return w.checkout, nil },
		Home:          func() (string, error) { return w.home, nil },
		Getenv:        func(k string) string { return w.env[k] },
		Git:           w.git,
		OfflineMarker: w.marker,
		Stderr:        &stderr,
	})
	return code, stderr.String()
}

func (w world) gitCalls(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(w.git), "git.log"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), "\n")
}

var verbs = []string{"bootstrap", "probe", "plan", "apply", "destroy", "chain"}

func TestUsage(t *testing.T) {
	w := newWorld(t, true)
	for _, args := range [][]string{
		nil, {"help"}, {"reconcile", "--reviewed-sha", fakeHead}, {"Plan", "--reviewed-sha", fakeHead},
		{"plan"}, {"plan", "--reviewed-sha"}, {"plan", "--unknown", "x"},
	} {
		code, stderr := w.run(args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Errorf("lz-live %q: exit %d, stderr %q; want 2 with usage", args, code, stderr)
		}
	}
	if n := w.gitCalls(t); n != 0 {
		t.Errorf("a usage error ran the guard (%d git calls)", n)
	}
}

// TestEntryGuardsEveryVerb: on a refused host every verb exits 3 naming the condition.
func TestEntryGuardsEveryVerb(t *testing.T) {
	for _, verb := range verbs {
		for name, setup := range map[string]func(*world){
			"offline-marker": func(w *world) { os.Mkdir(w.marker, 0o700) },
			"offline-env":    func(w *world) { w.env["LZ_OFFLINE"] = "1" },
			"head-mismatch":  func(w *world) {},
		} {
			t.Run(verb+"/"+name, func(t *testing.T) {
				w := newWorld(t, true)
				setup(&w)
				sha := fakeHead
				if name == "head-mismatch" {
					sha = strings.Repeat("0a", 20)
				}
				code, stderr := w.run(verb, "--reviewed-sha", sha)
				want := map[string]string{"offline-marker": "offline", "offline-env": "offline", "head-mismatch": "head"}[name]
				if code != 3 || !strings.Contains(stderr, "("+want+")") {
					t.Fatalf("exit %d, stderr %q; want 3 naming %s", code, stderr, want)
				}
			})
		}
	}
}

// TestEntryReadsLiveEnvFromHome: the guard reads ~/.config/ovh-lz/live.env of the injected home;
// without it the run is refused, with it the guard admits and the verb body runs.
func TestEntryReadsLiveEnvFromHome(t *testing.T) {
	w := newWorld(t, false)
	code, stderr := w.run("plan", "--reviewed-sha", fakeHead)
	if code != 3 || !strings.Contains(stderr, "(live-env)") {
		t.Fatalf("no live.env: exit %d, stderr %q; want 3 naming live-env", code, stderr)
	}
	w = newWorld(t, true)
	code, stderr = w.run("plan", "--reviewed-sha", fakeHead, "stage-x")
	if w.gitCalls(t) == 0 {
		t.Fatal("admitted without asking git")
	}
	// The verb bodies arrive with the run core (T055) and plan/apply (T059): an admitted run
	// stops there with exit 1, never 0.
	if code != 1 || !strings.Contains(stderr, "not implemented") {
		t.Fatalf("admitted: exit %d, stderr %q; want 1, not implemented", code, stderr)
	}
}
