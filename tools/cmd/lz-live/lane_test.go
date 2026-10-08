package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// T059: `lz-live plan|apply --reviewed-sha <sha> <instance|all>` hands live.Apply (internal/live,
// T058 tests) the reviewed checkout's manifest, the config root, the account the sandbox admin's
// credential names (GET /auth/details), a fresh run id and its run directory under .local/live/,
// the records directory, the reviewed commit as revision, tofu from PATH, the checkout's
// schemas/outputs, the account's lock directory, the S3 store of spec.state, the ovh-eu API and
// the redacted stdout. A missing sandbox.env is blocked (exit 2) naming bootstrap:account
// (decision 2, 2026-10-08). No test here reaches the real API, S3 or tofu.

const laneEntryAccount = "xx000059-ovh"

// laneEntryAPI answers the OAuth2 token request and GET /auth/details with laneEntryAccount.
func laneEntryAPI(t *testing.T, w *bootstrapWorld) {
	t.Helper()
	w.api = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.calls.Add(1)
		rw.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/oauth2/token":
			json.NewEncoder(rw).Encode(map[string]string{"access_token": "fake-entry-token"})
		case "/v1/auth/details":
			json.NewEncoder(rw).Encode(map[string]string{"account": laneEntryAccount})
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(w.api.Close)
}

func laneEntryWorld(t *testing.T, sandbox bool) (*bootstrapWorld, string) {
	t.Helper()
	w := newBootstrapWorld(t)
	laneEntryAPI(t, w)
	cfg := filepath.Join(w.home, ".config", "ovh-lz")
	if sandbox {
		if err := live.WriteCredentialFile(cfg, "sandbox.env", map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": "EU.entry-admin", "OVH_CLIENT_SECRET": "lz-seed-t059-entry-secret"}); err != nil {
			t.Fatal(err)
		}
	}
	// The children's scratch goes under TMPDIR, which must lie outside the checkout.
	t.Setenv("TMPDIR", t.TempDir())
	return w, cfg
}

func TestLaneEntryUsage(t *testing.T) {
	w, _ := laneEntryWorld(t, true)
	for _, args := range [][]string{
		{"plan", "--reviewed-sha", fakeHead},
		{"apply", "--reviewed-sha", fakeHead},
		{"apply", "--reviewed-sha", fakeHead, "all", "demo-dev-project"},
		{"plan", "--reviewed-sha", fakeHead, "--deadline", "5m", "all"},
		// T047: destroy and chain take one target; only chain takes --deadline, a positive one.
		{"destroy", "--reviewed-sha", fakeHead},
		{"destroy", "--reviewed-sha", fakeHead, "demo-dev-gra11-runtime", "demo-dev-gra11-network"},
		{"destroy", "--reviewed-sha", fakeHead, "--deadline", "5m", "demo-dev-gra11-runtime"},
		{"chain", "--reviewed-sha", fakeHead},
		{"chain", "--reviewed-sha", fakeHead, "all", "demo-dev-project"},
		{"chain", "--reviewed-sha", fakeHead, "all", "--deadline", "soon"},
		{"chain", "--reviewed-sha", fakeHead, "all", "--deadline", "0s"},
		{"chain", "--reviewed-sha", fakeHead, "all", "--plan-only"},
	} {
		code, stderr := w.run(args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Errorf("lz-live %q: exit %d, stderr %q; want 2 with usage", args, code, stderr)
		}
	}
	if n := w.gitCalls(t); n != 0 || w.calls.Load() != 0 {
		t.Errorf("a usage error ran the guard (%d git calls) or reached the API (%d)", n, w.calls.Load())
	}
}

// TestLaneEntryOptions: what the entry hands live.Apply, for both verbs and both target forms; the
// error live.Apply returns decides the exit code.
func TestLaneEntryOptions(t *testing.T) {
	for _, c := range []struct{ verb, target string }{{"plan", "all"}, {"apply", "all"}, {"apply", "demo-dev-project"}, {"destroy", "demo-dev-gra11-runtime"}} {
		t.Run(c.verb+"/"+c.target, func(t *testing.T) {
			w, cfg := laneEntryWorld(t, true)
			s3 := newEntryS3(t)
			w.s3 = s3.client()
			var got live.ApplyOptions
			n := 0
			w.apply = func(_ context.Context, o live.ApplyOptions) error {
				got = o
				n++
				return &live.Blocked{Phase: "inputs", Detail: "captured"}
			}
			code, stderr := w.run(c.verb, "--reviewed-sha", fakeHead, c.target)
			if code != live.BlockedExit || n != 1 {
				t.Fatalf("exit %d, stderr %q, Apply called %d times; want %d (Apply's error) and once", code, stderr, n, live.BlockedExit)
			}
			if got.Verb != c.verb || got.Target != c.target {
				t.Errorf("verb %q target %q, want %q %q", got.Verb, got.Target, c.verb, c.target)
			}
			if got.Checkout != w.checkout || got.ConfigRoot != cfg || got.Account != laneEntryAccount {
				t.Errorf("checkout %q config %q account %q", got.Checkout, got.ConfigRoot, got.Account)
			}
			if got.Manifest == nil || got.Manifest.Org != "lz" || len(got.Manifest.Instances) == 0 {
				t.Errorf("manifest %+v: not the checkout's stacks/deployments.yaml", got.Manifest)
			}
			if !live.RunIDPattern.MatchString(got.RunID) || got.RunDir != filepath.Join(w.checkout, ".local", "live", got.RunID) {
				t.Errorf("run id %q, run dir %q", got.RunID, got.RunDir)
			}
			if got.Records != filepath.Join(w.checkout, ".local", "live", "records") {
				t.Errorf("records %q", got.Records)
			}
			if got.Revision != fakeHead || got.Tofu != w.tofu || got.Schemas == nil {
				t.Errorf("revision %q tofu %q schemas %v", got.Revision, got.Tofu, got.Schemas)
			}
			if got.Terminal != io.Writer(&w.stdout) {
				t.Errorf("terminal is not the entry's stdout")
			}
			if got.API.BaseURL != w.api.URL+"/v1" {
				t.Errorf("API %q, want the ovh-eu API", got.API.BaseURL)
			}
			// The locks are the account's lock directory.
			if got.Locks == nil {
				t.Fatal("no lock store")
			}
			release, err := got.Locks.TryLock("account")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(cfg, "accounts", laneEntryAccount, "locks", "account.lock")); err != nil {
				t.Errorf("the lock store is not accounts/<account>/locks: %v", err)
			}
			_ = release()
			// The store is the S3 store of spec.state (region gra).
			if got.Store == nil {
				t.Fatal("no store")
			}
			st, err := got.Store(map[string]string{"AWS_ACCESS_KEY_ID": "AK-entry", "AWS_SECRET_ACCESS_KEY": "lz-seed-t059-entry-s3"})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = st.Get("lz-demo-bkt-state", stacks.ArtifactKey("demo-dev-project"))
			if len(s3.seen) != 1 || !strings.Contains(s3.seen[0], "s3.gra.io.cloud.ovh.net") {
				t.Errorf("store requests %q, want one to the gra endpoint", s3.seen)
			}
			if out := w.stdout.String(); !strings.Contains(out, "LZ-LIVE run "+got.RunID+" start "+c.verb+" "+c.target) {
				t.Errorf("stdout %q does not announce the run", out)
			}
		})
	}
}

// TestLaneEntryBlockedWithoutSandbox: without sandbox.env the run is blocked (exit 2) naming
// bootstrap:account, before any API request or Apply.
func TestLaneEntryBlockedWithoutSandbox(t *testing.T) {
	w, _ := laneEntryWorld(t, false)
	n := 0
	w.apply = func(context.Context, live.ApplyOptions) error { n++; return nil }
	code, stderr := w.run("apply", "--reviewed-sha", fakeHead, "all")
	if code != live.BlockedExit || !strings.Contains(stderr, "bootstrap:account") {
		t.Errorf("exit %d, stderr %q; want %d naming bootstrap:account", code, stderr, live.BlockedExit)
	}
	if n != 0 || w.calls.Load() != 0 {
		t.Errorf("Apply called %d times, %d API requests without sandbox.env", n, w.calls.Load())
	}
}

// TestLaneEntryScratchInsideCheckout: a TMPDIR inside the checkout is refused (exit 3) before any
// credential is read.
func TestLaneEntryScratchInsideCheckout(t *testing.T) {
	w, _ := laneEntryWorld(t, true)
	tmp := filepath.Join(w.checkout, ".local", "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	n := 0
	w.apply = func(context.Context, live.ApplyOptions) error { n++; return nil }
	code, stderr := w.run("plan", "--reviewed-sha", fakeHead, "all")
	if code != live.RefusalExit || !strings.Contains(stderr, "("+live.CondScratch+")") {
		t.Errorf("exit %d, stderr %q; want 3 naming %s", code, stderr, live.CondScratch)
	}
	if n != 0 || w.calls.Load() != 0 {
		t.Errorf("Apply called %d times, %d API requests", n, w.calls.Load())
	}
}

// TestChainEntryOptions (T047): `lz-live chain --reviewed-sha <sha> <instance|all> [--deadline
// <dur>]` hands live.Chain the lane's options (as plan/apply get them), the deadline of the flag
// (0 without it: live.DefaultDeadline), no Signals (live.Chain handles the process's SIGINT,
// SIGTERM and SIGHUP) and no Lister (the leftover check lists through the API with the sandbox
// admin credential, coordinator decision 1); it never runs live.Apply. Chain's error decides the
// exit code.
func TestChainEntryOptions(t *testing.T) {
	for _, c := range []struct {
		args     []string
		target   string
		deadline time.Duration
	}{
		{[]string{"all"}, "all", 0},
		{[]string{"--deadline", "7m", "demo-dev-gra11-runtime"}, "demo-dev-gra11-runtime", 7 * time.Minute},
	} {
		t.Run(c.target, func(t *testing.T) {
			w, cfg := laneEntryWorld(t, true)
			s3 := newEntryS3(t)
			w.s3 = s3.client()
			w.apply = func(context.Context, live.ApplyOptions) error {
				t.Error("chain ran live.Apply")
				return nil
			}
			var got live.ChainOptions
			n := 0
			w.chain = func(_ context.Context, o live.ChainOptions) error {
				got = o
				n++
				return &live.Blocked{Phase: "inputs", Detail: "captured"}
			}
			code, stderr := w.run(append([]string{"chain", "--reviewed-sha", fakeHead}, c.args...)...)
			if code != live.BlockedExit || n != 1 {
				t.Fatalf("exit %d, stderr %q, Chain called %d times; want %d (Chain's error) and once", code, stderr, n, live.BlockedExit)
			}
			if got.Target != c.target || got.Deadline != c.deadline || got.Lister != nil || got.Signals != nil {
				t.Errorf("target %q deadline %v lister %v signals %v; want %q %v, no lister, no signals", got.Target, got.Deadline, got.Lister, got.Signals, c.target, c.deadline)
			}
			if got.Checkout != w.checkout || got.ConfigRoot != cfg || got.Account != laneEntryAccount || got.Manifest == nil {
				t.Errorf("checkout %q config %q account %q manifest %v", got.Checkout, got.ConfigRoot, got.Account, got.Manifest)
			}
			if !live.RunIDPattern.MatchString(got.RunID) || got.RunDir != filepath.Join(w.checkout, ".local", "live", got.RunID) ||
				got.Records != filepath.Join(w.checkout, ".local", "live", "records") {
				t.Errorf("run id %q, run dir %q, records %q", got.RunID, got.RunDir, got.Records)
			}
			if got.Revision != fakeHead || got.Tofu != w.tofu || got.Schemas == nil || got.Locks == nil || got.Store == nil {
				t.Errorf("revision %q tofu %q schemas %v locks %v store set %t", got.Revision, got.Tofu, got.Schemas, got.Locks, got.Store != nil)
			}
			if got.Terminal != io.Writer(&w.stdout) || got.API.BaseURL != w.api.URL+"/v1" {
				t.Errorf("terminal or API (%q) are not the entry's", got.API.BaseURL)
			}
			if out := w.stdout.String(); !strings.Contains(out, "LZ-LIVE run "+got.RunID+" start chain "+c.target) {
				t.Errorf("stdout %q does not announce the run", out)
			}
		})
	}
}

// TestChainEntryDefault (T047): without the seams the entry runs live.Chain and live.Apply
// themselves: a chain of account-bootstrap is live.Chain's own refusal (bootstrap-owned, exit 3),
// still ending with the summary line; a destroy of a retained instance is live.Apply's (exit 3).
func TestChainEntryDefault(t *testing.T) {
	w, _ := laneEntryWorld(t, true)
	code, stderr := w.run("chain", "--reviewed-sha", fakeHead, "account-bootstrap")
	if code != live.RefusalExit || !strings.Contains(stderr, "("+live.CondBootstrapOwned+")") {
		t.Errorf("chain: exit %d, stderr %q; want 3 naming %s (live.Chain)", code, stderr, live.CondBootstrapOwned)
	}
	if out := w.stdout.String(); !strings.Contains(out, "LZ-LIVE summary ") || !strings.Contains(out, " fail known-deviations=") {
		t.Errorf("chain refused without its summary line:\n%s", out)
	}
	code, stderr = w.run("destroy", "--reviewed-sha", fakeHead, "demo-state")
	if code != live.RefusalExit || !strings.Contains(stderr, "("+live.CondRetained+")") {
		t.Errorf("destroy: exit %d, stderr %q; want 3 naming %s (live.Apply)", code, stderr, live.CondRetained)
	}
}

// TestLaneEntryDefaultApply: without the seam the entry runs live.Apply itself (a refused destroy
// would never reach it; a bootstrap-owned target is live.Apply's own refusal).
func TestLaneEntryDefaultApply(t *testing.T) {
	w, _ := laneEntryWorld(t, true)
	code, stderr := w.run("apply", "--reviewed-sha", fakeHead, "account-bootstrap")
	if code != live.RefusalExit || !strings.Contains(stderr, "("+live.CondBootstrapOwned+")") {
		t.Errorf("exit %d, stderr %q; want 3 naming %s (live.Apply)", code, stderr, live.CondBootstrapOwned)
	}
}
