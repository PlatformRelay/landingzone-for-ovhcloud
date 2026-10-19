package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T096 through the entry: `lz-live probe` runs the state-backend probe's check (package
// probes/live/state) after the root's apply, with the S3 client lz-live gives it (deps.S3HTTP),
// writes observations.json to the run record and undoes before the destroy; no other probe root
// gets a check.

const (
	checkStateAK = "AKENTRYSTATE0001"
	checkStateSK = "entry-state-secret-5a4b3c2d1e0f"
	checkJanAK   = "AKENTRYJANITOR01"
	checkJanSK   = "entry-janitor-secret-0f1e2d3c4b5a"
)

// checkS3 is a fake S3 endpoint for the entry: a versioned bucket where the state user is refused
// what its Deny names; it records who sent which method and query.
type checkS3 struct {
	mu       sync.Mutex
	seen     []string
	versions int
}

func (f *checkS3) serve(rw http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	who := "unknown"
	switch {
	case strings.Contains(r.Header.Get("Authorization"), "Credential="+checkStateAK+"/"):
		who = "state"
	case strings.Contains(r.Header.Get("Authorization"), "Credential="+checkJanAK+"/"):
		who = "janitor"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, who+" "+r.Method+" "+r.URL.RawQuery)
	q := r.URL.Query()
	deny := func() {
		rw.WriteHeader(http.StatusForbidden)
		fmt.Fprint(rw, "<Error><Code>AccessDenied</Code></Error>")
	}
	switch {
	case who == "unknown":
		deny()
	case who == "state" && (q.Has("versionId") || q.Has("policy") || q.Has("versioning")):
		deny()
	// T099: every version is locked (governance); the janitor deletes one only with the bypass.
	case who == "janitor" && q.Has("versionId") && r.Header.Get("X-Amz-Bypass-Governance-Retention") != "true":
		deny()
	case r.Method == http.MethodPut && !q.Has("policy") && !q.Has("versioning"):
		f.versions++
		rw.Header().Set("X-Amz-Version-Id", fmt.Sprintf("v%d", f.versions))
	case r.Method == http.MethodDelete:
		rw.WriteHeader(http.StatusNoContent)
	case q.Has("versions"):
		fmt.Fprint(rw, `<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`)
	case q.Has("policy"):
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, "<Error><Code>NoSuchBucketPolicy</Code></Error>")
	case q.Has("versioning"):
		fmt.Fprint(rw, `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`)
	default:
		fmt.Fprint(rw, "object")
	}
}

func (w *probeWorld) withS3(t *testing.T) *checkS3 {
	t.Helper()
	f := &checkS3{}
	srv := httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	addr := srv.Listener.Addr().String()
	w.s3 = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}}
	return f
}

// stateBackendRoot adds the state-backend root to the fake checkout and the outputs it publishes.
func (w *probeWorld) stateBackendRoot(t *testing.T) {
	t.Helper()
	root := filepath.Join(w.checkout, "tests", "live", "probes", "state-backend")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte("# probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.publishCheck(t)
}

func (w *probeWorld) publishCheck(t *testing.T) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"deny_check": map[string]any{"sensitive": true, "type": []any{"map", "string"}, "value": map[string]string{
		"bucket": "lzprobe-state-20261006t120000z-ab12", "region": "GRA", "state_key_id": checkStateAK, "state_secret": checkStateSK,
		"janitor_key_id": checkJanAK, "janitor_secret": checkJanSK}}})
	writePrivate(t, filepath.Join(filepath.Dir(w.git), "outputs.json"), string(raw))
}

func TestProbeEntryCheck(t *testing.T) {
	t.Run("state-backend", func(t *testing.T) {
		w := newProbeWorld(t)
		f := w.withS3(t)
		w.stateBackendRoot(t)
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/state-backend")
		if code != 0 {
			t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		id := runIDOf(t, stdout)
		raw, err := os.ReadFile(filepath.Join(w.checkout, ".local", "live", id, "observations.json"))
		if err != nil {
			t.Fatalf("no observations.json: %v\n%s", err, stdout)
		}
		var rec struct {
			Check        string           `json:"check"`
			Observations []map[string]any `json:"observations"`
			Findings     []string         `json:"findings"`
		}
		if json.Unmarshal(raw, &rec) != nil || rec.Check != "state-backend-deny" || len(rec.Observations) != 10 || len(rec.Findings) != 0 {
			t.Errorf("observations.json %s: want the state-backend check's ten observations (T099), no finding", raw)
		}
		var who []string
		for _, s := range f.seen {
			who = append(who, strings.Fields(s)[0])
		}
		if !slices.Contains(who, "state") || !slices.Contains(who, "janitor") || slices.Contains(who, "unknown") {
			t.Errorf("the fake S3 saw %v: want the state user's and the janitor's signed requests only", f.seen)
		}
		// Undo before the destroy: the janitor's last listing comes before the root's destroy plan.
		calls := subcommands(w.childCalls(t, "tofu.log"))
		if want := []string{"init", "plan", "show", "show", "apply", "output", "output", "plan-destroy", "show-destroy", "destroy"}; !slices.Equal(calls, want) {
			t.Errorf("tofu calls %v, want %v", calls, want)
		}
		for _, s := range []string{checkStateSK, checkJanSK, checkStateAK, checkJanAK} {
			if strings.Contains(stdout+stderr+string(raw), s) {
				t.Errorf("a check credential reached the terminal or the record")
			}
			for _, c := range w.childCalls(t, "tofu.log") {
				if strings.Contains(strings.Join(append(c.Args, c.Env...), " "), s) {
					t.Errorf("tofu %v ran with a check credential", c.Args)
				}
			}
		}
		w.noSecret(t, stdout, stderr)
	})

	// A failed destroy keeps the probe; its cleanup undoes again (the janitor lists the bucket)
	// before it destroys.
	t.Run("cleanup", func(t *testing.T) {
		w := newProbeWorld(t)
		f := w.withS3(t)
		w.stateBackendRoot(t)
		marker := filepath.Join(filepath.Dir(w.git), "destroy-fails")
		writePrivate(t, marker, "")
		code, stdout, _ := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/state-backend")
		if code == 0 {
			t.Fatal("a probe whose destroy failed exited 0")
		}
		id := runIDOf(t, stdout)
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		f.seen = nil
		f.mu.Unlock()
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
		if code != 0 {
			t.Fatalf("cleanup exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		if len(f.seen) == 0 || !strings.HasPrefix(f.seen[len(f.seen)-1], "janitor GET versions") {
			t.Errorf("cleanup's S3 requests %v: want the janitor's undo, ending with its listing", f.seen)
		}
		w.noSecret(t, stdout, stderr)
	})

	t.Run("other-root-no-check", func(t *testing.T) {
		w := newProbeWorld(t)
		f := w.withS3(t)
		w.publishCheck(t)
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code != 0 {
			t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		if calls := subcommands(w.childCalls(t, "tofu.log")); slices.Contains(calls, "output") {
			t.Errorf("tofu calls %v: a root without a check reads no output", calls)
		}
		if len(f.seen) != 0 {
			t.Errorf("S3 requests %v for a root without a check", f.seen)
		}
		if _, err := os.Stat(filepath.Join(w.checkout, ".local", "live", runIDOf(t, stdout), "observations.json")); err == nil {
			t.Error("observations.json for a root without a check")
		}
	})
}

// TestProbeCheckSwiftReach (spec 003 T030): the swift-reach root gets the swift-reach check with a
// Sweep; a root of another name still gets none. (That the check is handed the entry's S3 client
// is read from probe.go, not asserted: the options are inside the check's closures.)
func TestProbeCheckSwiftReach(t *testing.T) {
	s3 := &http.Client{}
	c := probeCheck("swift-reach", deps{S3HTTP: s3}, live.API{}, live.Credential{}, nil, nil, "", runPaths{})
	if c == nil || c.Output != "swift_check" || c.Sweep == nil || c.Observe == nil || c.Undo == nil || c.Validate == nil {
		t.Fatalf("probeCheck(swift-reach) = %+v, want the swift-reach check with a Sweep", c)
	}
	if c := probeCheck("swift-reach-x", deps{S3HTTP: s3}, live.API{}, live.Credential{}, nil, nil, "", runPaths{}); c != nil {
		t.Errorf("probeCheck(swift-reach-x) = %+v, want none", c)
	}
}

// TestProbeCheckTokenResidual (spec 003 T032): the token-residual root gets the token-residual
// check with a Sweep; a root of another name still gets none.
func TestProbeCheckTokenResidual(t *testing.T) {
	c := probeCheck("token-residual", deps{}, live.API{}, live.Credential{}, nil, nil, "", runPaths{})
	if c == nil || c.Output != "residual_check" || c.Sweep == nil || c.Observe == nil || c.Undo == nil || c.Validate == nil {
		t.Fatalf("probeCheck(token-residual) = %+v, want the token-residual check with a Sweep", c)
	}
	if c := probeCheck("token-residual-x", deps{}, live.API{}, live.Credential{}, nil, nil, "", runPaths{}); c != nil {
		t.Errorf("probeCheck(token-residual-x) = %+v, want none", c)
	}
}

// TestProbeCheckBudgetAlert (spec 007 T012, FR-007): the budget-alert root gets the budget-alert
// check with an Observe and a Sweep, selecting the map output budget_alert_check (never the scalar
// alert_id); a root of another name still gets none. The arguments are today's; T015 may add the
// run's paths to probeCheck's parameters and adapt every call alike.
func TestProbeCheckBudgetAlert(t *testing.T) {
	c := probeCheck("budget-alert", deps{}, live.API{}, live.Credential{}, nil, nil, "0123456789abcdef0123456789abcdef", runPaths{})
	if c == nil || c.Output != "budget_alert_check" || !slices.Equal(c.Public, []string{"alert_id"}) || c.Sweep == nil || c.Observe == nil {
		t.Fatalf("probeCheck(budget-alert) = %+v, want the budget-alert check with a Sweep and output budget_alert_check", c)
	}
	if c := probeCheck("budget-alert-x", deps{}, live.API{}, live.Credential{}, nil, nil, "", runPaths{}); c != nil {
		t.Errorf("probeCheck(budget-alert-x) = %+v, want none", c)
	}
}

// TestProbeCheckBudgetAlertPaths (spec 007 T015, T012's deferral): the budget-alert check finds
// the run's files through the paths probeCheck hands it. A record copy under RunDir(<run-id>), a
// recovery copy or state under StateDir, each unusable, makes its Sweep refuse before any request
// (the API is unset); with the paths unwired the run would read as pre-create and pass. The
// control: no file at all is pre-create and passes.
func TestProbeCheckBudgetAlertPaths(t *testing.T) {
	const id, project = "20261019T120000Z-ab12", "0123456789abcdef0123456789abcdef"
	cases := []struct {
		name, file, want string // file: relative to the run's record directory ("live/") or state directory ("state/")
	}{
		{"record-copy", "live/" + id + "/baseline.json", "BASELINE_INVALID"},
		{"recovery-copy", "state/baselines/" + id + ".json", "BASELINE_INVALID"},
		{"state", "state/probes/" + id + "/budget-alert.tfstate", "BASELINE_MISSING"},
		{"nothing", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			paths := runPaths{RunDir: func(run string) string { return filepath.Join(base, "live", run) }, StateDir: filepath.Join(base, "state")}
			if tc.file != "" {
				writePrivate(t, filepath.Join(base, tc.file), `{"run_id":`)
			}
			c := probeCheck("budget-alert", deps{}, live.API{}, live.Credential{}, nil, nil, project, paths)
			err := c.Sweep(context.Background(), id, project, io.Discard)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("Sweep with no run file: %v, want a pre-create pass", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("Sweep with %s: %v, want a refusal carrying %s", tc.file, err, tc.want)
			}
		})
	}
}

// TestProbeAcceptLostBaselineUsage (spec 007 T015): --accept-lost-baseline goes with --cleanup
// only; with a probe root it is a usage error before the guard.
func TestProbeAcceptLostBaselineUsage(t *testing.T) {
	w := newWorld(t, true)
	for _, args := range [][]string{
		{"probe", "--reviewed-sha", fakeHead, "--accept-lost-baseline", "tests/live/probes/a"},
		{"probe", "--reviewed-sha", fakeHead, "--accept-lost-baseline"},
		{"chain", "--reviewed-sha", fakeHead, "--accept-lost-baseline", "all"},
	} {
		if code, stderr := w.run(args...); code != 2 || !strings.Contains(stderr, "usage") {
			t.Errorf("lz-live %q: exit %d, stderr %q; want 2 with usage", args, code, stderr)
		}
	}
	if n := w.gitCalls(t); n != 0 {
		t.Errorf("a usage error ran the guard (%d git calls)", n)
	}
}

// TestReadSummary (spec 007 T026, FR-007 owner recovery): a refused line typed back is described by
// its length, as empty, and with control characters (an arrow key), never shown.
func TestReadSummary(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"", "read an empty line"},
		{"notTheRunIdSecret", "read 17 characters"},
		{"\x1b[D", "read 3 characters, with control characters"},
		{"Zürich", "read 6 characters"},
	} {
		if got := readSummary(c.line); got != c.want {
			t.Errorf("readSummary(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// TestAcceptedCheck (spec 007 T015, FR-007 owner recovery): under --accept-lost-baseline the
// budget-alert check runs without its Sweep (it judges by the lost baseline; the owner removed the
// alerts by hand), and the check itself is left unchanged; every other root keeps its Sweep.
func TestAcceptedCheck(t *testing.T) {
	c := probeCheck("budget-alert", deps{}, live.API{}, live.Credential{}, nil, nil, "", runPaths{})
	got := acceptedCheck("budget-alert", c)
	if got == nil || got.Sweep != nil || got.Observe == nil || got.Output != c.Output || c.Sweep == nil {
		t.Errorf("acceptedCheck(budget-alert) = %+v (original Sweep nil %v), want the check without its Sweep, the original untouched", got, c.Sweep == nil)
	}
	other := probeCheck("swift-reach", deps{}, live.API{}, live.Credential{}, nil, nil, "", runPaths{})
	if got := acceptedCheck("swift-reach", other); got == nil || got.Sweep == nil {
		t.Errorf("acceptedCheck(swift-reach) = %+v, want its Sweep kept", got)
	}
	if got := acceptedCheck("network", nil); got != nil {
		t.Errorf("acceptedCheck(network, nil) = %+v, want nil", got)
	}
}

// TestProbeCheckIamURN (spec 003 T034): the iam-urn root gets the iam-urn check with a Sweep; a
// root of another name still gets none.
func TestProbeCheckIamURN(t *testing.T) {
	c := probeCheck("iam-urn", deps{}, live.API{}, live.Credential{}, nil, nil, "", runPaths{})
	if c == nil || c.Output != "urn_check" || c.Sweep == nil || c.Observe == nil || c.Undo == nil || c.Validate == nil {
		t.Fatalf("probeCheck(iam-urn) = %+v, want the iam-urn check with a Sweep", c)
	}
	if c := probeCheck("iam-urn-x", deps{}, live.API{}, live.Credential{}, nil, nil, "", runPaths{}); c != nil {
		t.Errorf("probeCheck(iam-urn-x) = %+v, want none", c)
	}
}
