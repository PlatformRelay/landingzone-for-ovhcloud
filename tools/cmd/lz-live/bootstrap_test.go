package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// `lz-live bootstrap` (T043): the entry hands the bootstrap phases (internal/live, T042 tests) the
// manifest's org and project references, the ovh-eu API, the operator's terminal only under
// --fresh-account, and the redacted stdout. No test here reaches the real API or a real terminal.

// bootstrapWorld is an admitted host with the repository's manifest in the checkout.
type bootstrapWorld struct {
	world
	calls     atomic.Int64 // requests the fake API received
	terminals atomic.Int64 // terminals opened
	term      live.Terminal
	stdout    bytes.Buffer
	api       *httptest.Server
}

func newBootstrapWorld(t *testing.T) *bootstrapWorld {
	t.Helper()
	w := &bootstrapWorld{world: newWorld(t, true)}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "stacks", "deployments.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(w.checkout, "stacks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.checkout, "stacks", "deployments.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	w.api = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.calls.Add(1)
		rw.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(w.api.Close)
	return w
}

func (w *bootstrapWorld) run(args ...string) (int, string) {
	var stderr bytes.Buffer
	code := run(args, deps{
		Getwd:         func() (string, error) { return w.checkout, nil },
		Home:          func() (string, error) { return w.home, nil },
		Getenv:        func(k string) string { return w.env[k] },
		Git:           w.git,
		OfflineMarker: w.marker,
		Stderr:        &stderr,
		Stdout:        &w.stdout,
		API: func(endpoint string) (live.API, error) {
			if endpoint != "ovh-eu" {
				return live.API{}, errors.New("unexpected endpoint " + endpoint)
			}
			return live.API{TokenURL: w.api.URL + "/auth/oauth2/token", BaseURL: w.api.URL + "/v1", HTTP: w.api.Client()}, nil
		},
		Terminal: func(context.Context) (live.Terminal, error) {
			w.terminals.Add(1)
			if w.term == nil {
				return nil, errors.New("no terminal in this test")
			}
			return w.term, nil
		},
	})
	return code, stderr.String()
}

// refusingTerminal is an operator who aborts every prompt.
type refusingTerminal struct{ reads atomic.Int64 }

func (r *refusingTerminal) ReadSecret(string) (string, error) {
	r.reads.Add(1)
	return "", errors.New("aborted")
}

func (r *refusingTerminal) ReadLine(string) (string, error) {
	r.reads.Add(1)
	return "", errors.New("aborted")
}

// TestBootstrapEntryManifest: org and project references come from the checkout's
// stacks/deployments.yaml: the state project's reference first, then every environment's.
func TestBootstrapEntryManifest(t *testing.T) {
	w := newBootstrapWorld(t)
	org, refs, err := manifestBinding(w.checkout)
	if err != nil {
		t.Fatal(err)
	}
	if org != "lz" || !slices.Equal(refs, []string{"STATE", "DEMO_DEV"}) {
		t.Errorf("org %q refs %v, want lz [STATE DEMO_DEV]", org, refs)
	}
	if err := os.Remove(filepath.Join(w.checkout, "stacks", "deployments.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manifestBinding(w.checkout); err == nil {
		t.Error("no manifest: no error")
	}
}

// TestBootstrapEntryWithoutFresh: without --fresh-account and without sandbox.env the run is
// blocked (exit 2) naming the flag, with no terminal opened and no API call.
func TestBootstrapEntryWithoutFresh(t *testing.T) {
	w := newBootstrapWorld(t)
	code, stderr := w.run("bootstrap", "--reviewed-sha", fakeHead)
	if code != 2 || !strings.Contains(stderr, "--fresh-account") {
		t.Errorf("exit %d, stderr %q; want 2 naming --fresh-account", code, stderr)
	}
	if n := w.terminals.Load(); n != 0 {
		t.Errorf("%d terminals opened without --fresh-account", n)
	}
	if n := w.calls.Load(); n != 0 {
		t.Errorf("%d API calls without a credential", n)
	}
	if out := w.stdout.String(); !strings.Contains(out, "LZ-LIVE identify bootstrap blocked") {
		t.Errorf("stdout %q: no LZ-LIVE identify line", out)
	}
}

// TestBootstrapEntryFresh: --fresh-account reads the root keys from the operator's terminal
// before any API call; an aborted prompt fails the run (exit 1) with nothing sent.
func TestBootstrapEntryFresh(t *testing.T) {
	w := newBootstrapWorld(t)
	term := &refusingTerminal{}
	w.term = term
	code, stderr := w.run("bootstrap", "--fresh-account", "--reviewed-sha", fakeHead)
	if code != 1 {
		t.Errorf("exit %d, stderr %q; want 1", code, stderr)
	}
	if w.terminals.Load() != 1 || term.reads.Load() == 0 {
		t.Errorf("terminal opened %d times, read %d times; want the root-key prompt", w.terminals.Load(), term.reads.Load())
	}
	if n := w.calls.Load(); n != 0 {
		t.Errorf("%d API calls without root keys", n)
	}
	// No terminal at all: the run fails before any API call too.
	w = newBootstrapWorld(t)
	if code, stderr := w.run("bootstrap", "--reviewed-sha", fakeHead, "--fresh-account"); code != 1 || w.calls.Load() != 0 {
		t.Errorf("no terminal: exit %d, stderr %q, %d API calls; want 1 and none", code, stderr, w.calls.Load())
	}
}

// TestBootstrapEntryUsage: bootstrap takes --reviewed-sha and --fresh-account only.
func TestBootstrapEntryUsage(t *testing.T) {
	w := newBootstrapWorld(t)
	for _, args := range [][]string{
		{"bootstrap", "--reviewed-sha", fakeHead, "extra"},
		{"bootstrap", "--reviewed-sha", fakeHead, "--plan-only"},
		{"bootstrap", "--fresh-account"},
		{"probe", "--reviewed-sha", fakeHead, "--fresh-account", "tests/live/probes/a"},
	} {
		if code, stderr := w.run(args...); code != 2 || !strings.Contains(stderr, "usage") {
			t.Errorf("lz-live %q: exit %d, stderr %q; want 2 with usage", args, code, stderr)
		}
	}
	if n := w.gitCalls(t); n != 0 {
		t.Errorf("a usage error ran the guard (%d git calls)", n)
	}
}
