package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T089: `lz-live bootstrap` passes the state, publish and verify phases as Rest
// (live.NewBootstrapState) with the live S3 store of the account bucket: region and endpoint from
// the manifest's spec.state, the reviewed commit as the envelope's revision, tofu from PATH, the
// checkout's schemas/outputs. The S3 store's own behaviour is pinned in internal/live
// (objectstore_test.go); here a fake S3 endpoint shows the entry's store reaches the derived host.

// entryS3 is a TLS S3 endpoint every dial of its client reaches; it keeps objects by path and
// records the host, method, path and Authorization of each request.
type entryS3 struct {
	srv     *httptest.Server
	mu      sync.Mutex
	objects map[string][]byte
	seen    []string // "<host> <method> <path> <authorization>"
}

func newEntryS3(t *testing.T) *entryS3 {
	t.Helper()
	f := &entryS3{objects: map[string][]byte{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.seen = append(f.seen, r.Host+" "+r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		switch r.Method {
		case http.MethodPut:
			f.objects[r.URL.Path] = body
		case http.MethodGet:
			data, ok := f.objects[r.URL.Path]
			if !ok {
				rw.WriteHeader(http.StatusNotFound)
				return
			}
			rw.Write(data)
		default:
			rw.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *entryS3) client() *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(f.srv.Certificate())
	addr := f.srv.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}}
}

// captureBoot records the options lz-live hands live.Bootstrap and runs nothing.
func captureBoot(got *live.BootstrapOptions, n *int) func(context.Context, live.BootstrapOptions) ([]live.PhaseResult, error) {
	return func(_ context.Context, o live.BootstrapOptions) ([]live.PhaseResult, error) {
		*got = o
		*n++
		return nil, nil
	}
}

// TestBootstrapEntryRest: the entry passes Rest, and Rest is the state phases' (it reports the
// `state` phase: an account directory without the passphrase file fails there, before any tofu,
// API or S3 request).
func TestBootstrapEntryRest(t *testing.T) {
	w := newBootstrapWorld(t)
	var got live.BootstrapOptions
	var n int
	w.boot = captureBoot(&got, &n)
	s3 := newEntryS3(t)
	w.s3 = s3.client()
	if code, stderr := w.run("bootstrap", "--reviewed-sha", fakeHead); code != 0 || n != 1 {
		t.Fatalf("exit %d, stderr %q, Bootstrap called %d times; want 0 and once", code, stderr, n)
	}
	if got.Rest == nil {
		t.Fatal("lz-live bootstrap passes no Rest: state, publish and verify do not run")
	}
	if got.Checkout != w.checkout || got.Endpoint != "ovh-eu" || got.Org != "lz" {
		t.Errorf("Bootstrap options checkout %q endpoint %q org %q", got.Checkout, got.Endpoint, got.Org)
	}
	err := got.Rest(context.Background(), live.BootstrapAccount{ID: "xx000001-ovh", Dir: t.TempDir()})
	if err == nil {
		t.Error("Rest without a passphrase file: no error")
	}
	if out := w.stdout.String(); !strings.Contains(out, "LZ-LIVE state bootstrap fail") {
		t.Errorf("stdout %q: Rest did not report the state phase", out)
	}
	if w.calls.Load() != 0 || len(s3.seen) != 0 {
		t.Errorf("%d API and %d S3 requests before the state phase could start", w.calls.Load(), len(s3.seen))
	}
	// With the passphrase file and account.env present, the default Rest gets past its own checks
	// (manifest, region) with the entry's options and asks the entry's API for the admin's token,
	// which the fake refuses: the phase fails there, before any tofu or S3 request (review r1).
	dir := t.TempDir()
	for name, body := range map[string]string{"state-passphrase.env": "TF_VAR_state_passphrase=p0123456789abcdef\n", "account.env": "OVH_ENDPOINT=ovh-eu\nLZ_PROJECT_ID_STATE=0123456789abcdef0123456789abcdef\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	from := w.stdout.Len()
	if err := got.Rest(context.Background(), live.BootstrapAccount{ID: "xx000001-ovh", Dir: dir}); err == nil {
		t.Error("Rest against a refusing API: no error")
	}
	out := w.stdout.String()[from:]
	if !strings.Contains(out, "LZ-LIVE state bootstrap fail") || strings.Contains(out, "region") {
		t.Errorf("stdout %q: want the state phase failing at the token, not on its options", out)
	}
	if w.calls.Load() == 0 || len(s3.seen) != 0 {
		t.Errorf("%d API and %d S3 requests; want the entry's API asked for a token and no S3 request", w.calls.Load(), len(s3.seen))
	}
}

// TestBootstrapEntryStateOptions: the phases get the manifest's region as the API names it (GRA),
// the reviewed commit, tofu from PATH, the checkout and its schemas/outputs, the bootstrap API,
// and a store that opens https://s3.gra.io.cloud.ovh.net with state.env's keys. The options are
// the ones the entry itself built and turned into the Rest it handed Bootstrap (review r1: not the
// helper called on its own).
func TestBootstrapEntryStateOptions(t *testing.T) {
	w := newBootstrapWorld(t)
	s3 := newEntryS3(t)
	w.s3 = s3.client()
	if err := os.MkdirAll(filepath.Join(w.checkout, "schemas", "outputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.checkout, "schemas", "outputs", "probe.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got live.BootstrapOptions
	var n, built, restCalls int
	var o live.StateOptions
	w.boot = captureBoot(&got, &n)
	w.state = func(so live.StateOptions) func(context.Context, live.BootstrapAccount) error {
		o = so
		built++
		return func(context.Context, live.BootstrapAccount) error { restCalls++; return nil }
	}
	if code, stderr := w.run("bootstrap", "--reviewed-sha", fakeHead); code != 0 || n != 1 || built != 1 {
		t.Fatalf("exit %d, stderr %q, Bootstrap called %d times, state options built %d times; want 0, once, once", code, stderr, n, built)
	}
	if got.Rest == nil || got.Rest(context.Background(), live.BootstrapAccount{}) != nil || restCalls != 1 {
		t.Fatal("the Rest handed to Bootstrap is not the one built from these state options")
	}
	if o.Region != "GRA" || o.Revision != fakeHead || o.Tofu != w.tofu || o.Checkout != w.checkout || o.Manifest == nil || o.Manifest.Org != "lz" || o.API.BaseURL != got.API.BaseURL || o.API.BaseURL == "" || o.Stdout == nil {
		t.Errorf("state options region %q revision %q tofu %q checkout %q api %q (Bootstrap's %q)", o.Region, o.Revision, o.Tofu, o.Checkout, o.API.BaseURL, got.API.BaseURL)
	}
	if o.Schemas == nil {
		t.Fatal("no schemas")
	}
	if _, err := fs.ReadFile(o.Schemas, "probe.json"); err != nil {
		t.Errorf("schemas are not the checkout's schemas/outputs: %v", err)
	}
	if _, err := o.Store(map[string]string{"AWS_ACCESS_KEY_ID": "AKENTRY"}); err == nil {
		t.Error("a store without the secret key: no error")
	}
	store, err := o.Store(map[string]string{"AWS_ACCESS_KEY_ID": "AKENTRY", "AWS_SECRET_ACCESS_KEY": "SKENTRY0secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("lz-bkt-state", "account/x/outputs.json", []byte("{}")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if data, err := store.Get("lz-bkt-state", "account/x/outputs.json"); err != nil || string(data) != "{}" {
		t.Errorf("Get = %q, %v", data, err)
	}
	for _, s := range s3.seen {
		if !strings.HasPrefix(s, "s3.gra.io.cloud.ovh.net ") || !strings.Contains(s, " /lz-bkt-state/account/x/outputs.json AWS4-HMAC-SHA256 Credential=AKENTRY/") || !strings.Contains(s, "/gra/s3/aws4_request") {
			t.Errorf("S3 request %q, want the derived host, path-style, signed for gra/s3 with state.env's key", s)
		}
		if strings.Contains(s, "SKENTRY0secret") {
			t.Errorf("S3 request %q carries the secret", s)
		}
	}
	if len(s3.seen) != 2 {
		t.Errorf("%d S3 requests, want PUT and GET", len(s3.seen))
	}
}

// TestBootstrapEntryStateRefusals: without tofu on PATH, or with a manifest whose spec.state.endpoint
// is not the endpoint its region derives (the S3 backend and the store would diverge), the run
// stops before the phases start: exit 1, no API call, no terminal.
func TestBootstrapEntryStateRefusals(t *testing.T) {
	for name, mutate := range map[string]func(*bootstrapWorld){
		"no-tofu": func(w *bootstrapWorld) { w.tofu = "" },
		"endpoint-differs": func(w *bootstrapWorld) {
			p := filepath.Join(w.checkout, "stacks", "deployments.yaml")
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(raw), "https://s3.gra.io.cloud.ovh.net", "https://s3.gra.storage.example")), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newBootstrapWorld(t)
			var got live.BootstrapOptions
			var n int
			w.boot = captureBoot(&got, &n)
			mutate(w)
			code, stderr := w.run("bootstrap", "--reviewed-sha", fakeHead)
			if code != 1 || n != 0 {
				t.Errorf("exit %d, stderr %q, Bootstrap called %d times; want 1 and never", code, stderr, n)
			}
			if w.calls.Load() != 0 || w.terminals.Load() != 0 {
				t.Errorf("%d API calls, %d terminals", w.calls.Load(), w.terminals.Load())
			}
		})
	}
}
