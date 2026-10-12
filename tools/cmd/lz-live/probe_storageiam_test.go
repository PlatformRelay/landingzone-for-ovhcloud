package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// T100 through the entry: `lz-live probe` runs the storage-iam probe's check (package
// probes/live/storageiam) after the root's apply with lz-live's API client, the bound identities
// and its S3 client, writes observations.json, and fails the run when an observation differs from
// the expected one, while the expected denials pass. Since D107 (T101) the expected denials
// include the bucket-A read T010 run 20261012T072729Z-0646 saw refused (P25 refuted); a bucket-A
// read that is allowed fails the run. Its undo lists the IAM policies with the admin's credential
// before the destroy.

const (
	iamProject = "0123456789abcdef0123456789abcdef"
	iamP1AK    = "AKENTRYP1STATE01"
	iamP1SK    = "entry-p1-secret-2b3c4d5e6f7a"
	iamRun     = "20261006T120000Z-0000" // the root's TF_VAR_run_id as published (the fake tofu's output)
)

// iamS3 is the P1 bucket: the companion's state object, an encryption envelope, for the P1 key.
type iamS3 struct {
	mu   sync.Mutex
	seen []string
}

func (w *probeWorld) withIAMS3(t *testing.T) *iamS3 {
	t.Helper()
	f := &iamS3{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p1 := strings.Contains(r.Header.Get("Authorization"), "Credential="+iamP1AK+"/")
		f.seen = append(f.seen, fmt.Sprintf("%v %s %s", p1, r.Method, r.URL.Path))
		if !p1 || r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/storage-iam-companion.tfstate") {
			rw.WriteHeader(http.StatusForbidden)
			fmt.Fprint(rw, "<Error><Code>AccessDenied</Code></Error>")
			return
		}
		fmt.Fprint(rw, `{"serial":1,"lineage":"l","meta":{},"encrypted_data":"Y2lwaGVy","encryption_version":"v0"}`)
	}))
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

// storageIAMRoot adds the storage-iam root (no companion: the order with one is the run core's,
// internal/live).
func (w *probeWorld) storageIAMRoot(t *testing.T) {
	t.Helper()
	root := filepath.Join(w.checkout, "tests", "live", "probes", "storage-iam")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte("# probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (w *probeWorld) publishIAMCheck(t *testing.T, runID string) {
	t.Helper()
	run := strings.ToLower(runID)
	raw, _ := json.Marshal(map[string]any{"iam_check": map[string]any{"sensitive": true, "type": []any{"map", "string"}, "value": map[string]string{
		"bucket_a": "lzprobe-p25a-" + run, "bucket_b": "lzprobe-p25b-" + run, "project_id": iamProject,
		"project_urn": "urn:v1:eu:resource:publicCloudProject:" + iamProject, "region": "GRA", "run_id": runID,
		"state_bucket": "lzprobe-p1-" + run, "state_key": "storage-iam-companion.tfstate",
		"tenant_identity":  "urn:v1:eu:identity:credential:demo/oauth2-probeidentity",
		"tenant_client_id": "EU.probeidentity", "tenant_client_secret": identitySecret,
		"p25_client_id": "EU.probep25", "p25_client_secret": p25Secret, "s3_key_id": iamP1AK, "s3_secret": iamP1SK}}})
	writePrivate(t, filepath.Join(filepath.Dir(w.git), "outputs.json"), string(raw))
}

// iamAnswers answers the check's API calls as T010 run 14 did (bucket A refused), except where
// allowA leaves bucket A readable; every answer records who asked for what.
func (w *probeWorld) iamAnswers(allowA bool, seen *[]string) {
	var mu sync.Mutex
	w.apiHook = func(rw http.ResponseWriter, r *http.Request) bool {
		who := tokenClients[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		refuse := func(action string) bool {
			rw.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(rw, `{"class":"Client::Forbidden","message":"missing IAM permissions: %s","details":{"unauthorizedActionsByIAM":%q}}`, action, action)
			return true
		}
		mu.Lock()
		defer mu.Unlock()
		storage := "/v1/cloud/project/" + iamProject + "/region/GRA/storage/"
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, storage+"lzprobe-p25a-"):
			*seen = append(*seen, who+" read A")
			if !allowA {
				return refuse("publicCloudProject:apiovh:region/storage/get")
			}
			fmt.Fprint(rw, `{"name":"a"}`)
			return true
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, storage+"lzprobe-p25b-"):
			*seen = append(*seen, who+" read B")
			return refuse("publicCloudProject:apiovh:region/storage/get")
		case r.Method == http.MethodGet && r.URL.Path == "/v1/me":
			*seen = append(*seen, who+" GET /me")
			return refuse("account:apiovh:me/get")
		case r.Method == http.MethodPost && r.URL.Path == "/v2/iam/policy":
			*seen = append(*seen, who+" POST /iam/policy")
			return refuse("account:apiovh:iam/policy/create")
		case r.Method == http.MethodGet && r.URL.Path == "/v2/iam/policy":
			*seen = append(*seen, who+" list policies")
		}
		return false
	}
}

func TestProbeEntryStorageIAM(t *testing.T) {
	for _, tc := range []struct {
		name   string
		allowA bool
	}{{"pass", false}, {"bucket-a-allowed", true}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newProbeWorld(t)
			s3 := w.withIAMS3(t)
			w.storageIAMRoot(t)
			var seen []string
			w.iamAnswers(tc.allowA, &seen)
			w.publishIAMCheck(t, iamRun)
			code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/storage-iam")
			id := runIDOf(t, stdout)
			raw, err := os.ReadFile(filepath.Join(w.checkout, ".local", "live", id, "observations.json"))
			if err != nil {
				t.Fatalf("no observations.json: %v\n%s\n%s", err, stdout, stderr)
			}
			var rec struct {
				Check        string `json:"check"`
				Observations []struct {
					Step       string   `json:"step"`
					Actions    []string `json:"actions"`
					AsExpected bool     `json:"as_expected"`
				} `json:"observations"`
				Findings []string `json:"findings"`
			}
			if json.Unmarshal(raw, &rec) != nil || rec.Check != "storage-iam" || len(rec.Observations) != 5 {
				t.Fatalf("observations.json %s: want the storage-iam check's five observations", raw)
			}
			sum, _ := os.ReadFile(filepath.Join(w.checkout, ".local", "live", id, "summary.json"))
			var s struct{ Outcome, Error string }
			_ = json.Unmarshal(sum, &s)
			if tc.allowA {
				if code == 0 || s.Outcome != "fail" || !strings.Contains(s.Error, "p25-read-bucket-a: expected refused, observed allowed 200") || !strings.Contains(s.Error, "P25 holds after all") {
					t.Errorf("exit %d, summary %s: want fail naming p25-read-bucket-a allowed and that P25 holds after all", code, sum)
				}
				if !slices.Equal(rec.Findings, []string{"p25-read-bucket-a"}) || !strings.Contains(stdout, "LZ-LIVE finding p25-read-bucket-a: expected refused, observed allowed 200; P25 holds after all") {
					t.Errorf("findings %v, stdout:\n%s\nwant the allowed read as the one finding, printed", rec.Findings, stdout)
				}
			} else {
				// T010 run 20261012T072729Z-0646's answers, bucket A refused included (D107): pass.
				if code != 0 || s.Outcome != "pass" || len(rec.Findings) != 0 || !rec.Observations[0].AsExpected ||
					!slices.Equal(rec.Observations[0].Actions, []string{"publicCloudProject:apiovh:region/storage/get"}) {
					t.Errorf("exit %d, summary %s, observations %s: want pass with bucket A refused 403 naming region/storage/get\n%s\n%s", code, sum, raw, stdout, stderr)
				}
			}
			// The other expected denials pass in both runs.
			for _, o := range rec.Observations[1:] {
				if !o.AsExpected {
					t.Errorf("observation %s not as expected: %s", o.Step, raw)
				}
			}
			// Each identity asks for its own: the P25 identity reads, the tenant calls /me and the
			// write, the admin lists in the undo; the P1 key reads the state object.
			want := []string{"EU.probep25 read A", "EU.probep25 read B", "EU.probeidentity GET /me", "EU.probeidentity POST /iam/policy", "EU.sandboxadmin list policies"}
			if len(seen) < len(want) || !slices.Equal(seen[:len(want)], want) {
				t.Errorf("check calls %v, want %v first", seen, want)
			}
			if len(s3.seen) != 1 || !strings.HasPrefix(s3.seen[0], "true GET /lzprobe-p1-") {
				t.Errorf("S3 requests %v: want the P1 key's one read of the state object", s3.seen)
			}
			// The identities were bound before the check used them (G13).
			if len(*w.identityAsked) == 0 || len(*w.p25Asked) == 0 {
				t.Error("the check used an identity it did not bind")
			}
			for _, sec := range []string{identitySecret, p25Secret, iamP1SK, iamP1AK} {
				if strings.Contains(stdout+stderr+string(raw)+string(sum), sec) {
					t.Errorf("a check credential reached the terminal or the record")
				}
			}
			w.noSecret(t, stdout, stderr)
		})
	}
}
