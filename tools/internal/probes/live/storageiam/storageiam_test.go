package storageiam

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T100/T101 against a fake OVHcloud API and a fake S3 endpoint that answer as T010 run
// 20261012T072729Z-0646 showed — the P25 identity refused its own bucket A (403 `missing IAM
// permissions: publicCloudProject:apiovh:region/storage/get`), which is the expected answer since
// D107 (P25 refuted) — with knobs for the worlds the live run may meet: bucket A or B allowed, the
// tenant allowed GET /me or the IAM write (its policy listed late or under another name), a
// refusal naming another action or none, the state object plaintext or refused, a failing undo. Refusals follow the 403 body of kb manage-and-operate/iam/troubleshooting.mdx:
// 21-31 (`details.unauthorizedActionsByIAM`) with the message T010 recorded, and echo the request's
// secrets. No test calls the OVHcloud API or reads ~/.config/ovh-lz/.

const (
	runID      = "20261012T072729Z-0646"
	project    = "0123456789abcdef0123456789abcdef"
	tenantID   = "EU.tenantprobe"
	tenantSK   = "tenant-secret-1f2e3d4c5b6a"
	p25ID      = "EU.p25probe"
	p25SK      = "p25-secret-6a5b4c3d2e1f"
	adminID    = "EU.sandboxadmin"
	adminSK    = "admin-secret-0f9e8d7c6b5a"
	s3AK       = "AKP1STATEUSER001"
	s3SK       = "p1-secret-key-9a8b7c6d5e4f"
	identity   = "urn:v1:eu:identity:credential:ab12345-ovh/oauth2-0123456789abcdef"
	bucketA    = "lzprobe-p25a-20261012t072729z-0646"
	bucketB    = "lzprobe-p25b-20261012t072729z-0646"
	stateBkt   = "lzprobe-p1-20261012t072729z-0646"
	writeName  = "lzprobe-p9-write-" + runID
	otherPol   = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	echo       = "SERVERTEXT"
	envelope   = `{"serial":3,"lineage":"l","meta":{"key_provider.pbkdf2.main":"eyJ4IjoxfQ=="},"encrypted_data":"Y2lwaGVydGV4dA==","encryption_version":"v0"}`
	plainState = `{"version":4,"terraform_version":"1.13.0","serial":3,"lineage":"l","resources":[{"type":"ovh_cloud_project_storage","name":"p9"}]}`
)

var (
	secrets = []string{tenantSK, p25SK, adminSK, s3SK, s3AK, "tok-tenant", "tok-p25", "tok-admin"}
	now     = time.Date(2026, 10, 12, 7, 30, 0, 0, time.UTC)
)

func testEnv() map[string]string {
	return map[string]string{
		"bucket_a": bucketA, "bucket_b": bucketB, "project_id": project, "project_urn": "urn:v1:eu:resource:publicCloudProject:" + project,
		"region": "GRA", "run_id": runID, "state_bucket": stateBkt, "state_key": StateKey, "tenant_identity": identity,
		"tenant_client_id": tenantID, "tenant_client_secret": tenantSK, "p25_client_id": p25ID, "p25_client_secret": p25SK,
		"s3_key_id": s3AK, "s3_secret": s3SK,
	}
}

type call struct{ who, method, path string }

type policy struct{ id, name string }

// world is the fake API and S3 endpoint. Knobs: answers maps "<who> <method> <path>" to a status
// (with a refusal body naming action), writeAllowed accepts the tenant's POST /v2/iam/policy and
// stores the policy, the state object's body, status and SSE header, the undo's delete status and
// whether a deleted policy stays listed.
type world struct {
	t      *testing.T
	api    *httptest.Server
	s3     *httptest.Server
	mu     sync.Mutex
	calls  []call
	posted [][]byte

	answers      map[string]answer
	writeAllowed bool
	policies     []policy
	stateBody    string
	stateStatus  int
	sse          string
	s3Hangup     bool
	deleteStatus int
	keepDeleted  bool
	listStatus   int
	listFailOnce bool     // the next listing answers 500
	deleteFailID string   // the delete of this id answers 500
	hidden       bool     // an accepted write's policy is not (yet) listed
	renameTo     string   // an accepted write's policy is listed under this name
	written      []string // the ids accepted writes created
	bound        []string // client ids Bind saw, in order
	bindRefuse   string   // Bind refuses this client id
}

type answer struct {
	status int
	action string // the action a 403 names ("" none; "-" an empty body)
}

var tokens = map[string]string{"tok-tenant": "tenant", "tok-p25": "p25", "tok-admin": "admin"}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, stateBody: envelope, stateStatus: 200, deleteStatus: 204, listStatus: 200, answers: map[string]answer{
		"p25 GET /v1/cloud/project/" + project + "/region/GRA/storage/" + bucketA: {403, "publicCloudProject:apiovh:region/storage/get"},
		"p25 GET /v1/cloud/project/" + project + "/region/GRA/storage/" + bucketB: {403, "publicCloudProject:apiovh:region/storage/get"},
		"tenant GET /v1/me":          {403, "account:apiovh:me/get"},
		"tenant POST /v2/iam/policy": {403, "account:apiovh:iam/policy/create"},
	}, policies: []policy{{otherPol, "lz-sandbox-admin"}}}
	w.api = httptest.NewServer(http.HandlerFunc(w.serveAPI))
	t.Cleanup(w.api.Close)
	w.s3 = httptest.NewTLSServer(http.HandlerFunc(w.serveS3))
	t.Cleanup(w.s3.Close)
	return w
}

func (w *world) serveAPI(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if r.URL.Path == "/token" {
		q, _ := url.ParseQuery(string(body))
		for id, tok := range map[string]string{tenantID + " " + tenantSK: "tok-tenant", p25ID + " " + p25SK: "tok-p25", adminID + " " + adminSK: "tok-admin"} {
			if q.Get("client_id")+" "+q.Get("client_secret") == id {
				fmt.Fprintf(rw, `{"access_token":%q}`, tok)
				return
			}
		}
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	who := tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, call{who, r.Method, r.URL.Path})
	refuse := func(status int, action string) {
		rw.WriteHeader(status)
		if action == "-" {
			return
		}
		msg := "User not granted for this request " + echo + " " + strings.Join(secrets, " ")
		if action != "" {
			msg = "missing IAM permissions: " + action
		}
		b, _ := json.Marshal(map[string]any{"class": "Client::Forbidden", "message": msg,
			"details": map[string]string{"unauthorizedActionsByAuthentication": "", "unauthorizedActionsByIAM": action}})
		rw.Write(b)
	}
	key := who + " " + r.Method + " " + r.URL.Path
	switch {
	case who == "":
		refuse(401, "")
	case who == "tenant" && r.Method == http.MethodPost && r.URL.Path == "/v2/iam/policy":
		w.posted = append(w.posted, body)
		if !w.writeAllowed {
			a := w.answers[key]
			refuse(a.status, a.action)
			return
		}
		var p struct{ Name string }
		_ = json.Unmarshal(body, &p)
		id := fmt.Sprintf("3f2504e0-4f89-41d3-9a0c-%012d", len(w.policies))
		if w.renameTo != "" {
			p.Name = w.renameTo
		}
		w.policies = append(w.policies, policy{id, p.Name})
		w.written = append(w.written, id)
		fmt.Fprintf(rw, `{"id":%q,"name":%q}`, id, p.Name)
	case who == "admin" && r.Method == http.MethodGet && r.URL.Path == "/v2/iam/policy":
		if w.listFailOnce {
			w.listFailOnce = false
			refuse(500, "-")
			return
		}
		if w.listStatus != 200 {
			refuse(w.listStatus, "account:apiovh:iam/policy/get")
			return
		}
		var out []map[string]any
		for _, p := range w.policies {
			if w.hidden && slices.Contains(w.written, p.id) {
				continue
			}
			out = append(out, map[string]any{"id": p.id, "name": p.name, "identities": []string{identity}})
		}
		if out == nil {
			out = []map[string]any{}
		}
		json.NewEncoder(rw).Encode(out)
	case who == "admin" && r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v2/iam/policy/"):
		id := strings.TrimPrefix(r.URL.Path, "/v2/iam/policy/")
		if id == w.deleteFailID {
			refuse(500, "-")
			return
		}
		if w.deleteStatus == http.StatusNotFound {
			// Gone already (expired, or deleted meanwhile).
			w.policies = slices.DeleteFunc(w.policies, func(p policy) bool { return p.id == id })
		}
		if w.deleteStatus >= 300 {
			refuse(w.deleteStatus, "account:apiovh:iam/policy/delete")
			return
		}
		if !w.keepDeleted {
			w.policies = slices.DeleteFunc(w.policies, func(p policy) bool { return p.id == id })
		}
		rw.WriteHeader(w.deleteStatus)
	default:
		a, ok := w.answers[key]
		if !ok {
			refuse(403, "-")
			return
		}
		if a.status >= 300 {
			refuse(a.status, a.action)
			return
		}
		fmt.Fprint(rw, `{"name":"x","tags":{"lz:tenant":"lzprobe-a"}}`)
	}
}

func (w *world) serveS3(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	who := "unknown"
	if strings.Contains(r.Header.Get("Authorization"), "Credential="+s3AK+"/") {
		who = "s3"
	}
	w.calls = append(w.calls, call{who, r.Method, r.URL.Path})
	if w.s3Hangup {
		if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
		return
	}
	if who != "s3" || r.Method != http.MethodGet || r.URL.Path != "/"+stateBkt+"/"+StateKey {
		rw.WriteHeader(403)
		fmt.Fprint(rw, "<Error><Code>AccessDenied</Code></Error>")
		return
	}
	if w.sse != "" {
		rw.Header().Set("X-Amz-Server-Side-Encryption", w.sse)
	}
	if w.stateStatus != 200 {
		rw.WriteHeader(w.stateStatus)
		fmt.Fprintf(rw, "<Error><Code>AccessDenied</Code><Message>%s %s</Message></Error>", echo, s3SK)
		return
	}
	fmt.Fprint(rw, w.stateBody)
}

func (w *world) options() Options {
	pool := x509.NewCertPool()
	pool.AddCert(w.s3.Certificate())
	addr := w.s3.Listener.Addr().String()
	return Options{
		API:   live.API{TokenURL: w.api.URL + "/token", BaseURL: w.api.URL + "/v1"},
		Admin: live.Credential{Endpoint: "ovh-eu", ClientID: adminID, ClientSecret: adminSK},
		Bind: func(ctx context.Context, c live.Credential) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.bound = append(w.bound, c.ClientID)
			w.calls = append(w.calls, call{"bind", c.Endpoint, c.ClientID})
			if c.ClientID == w.bindRefuse {
				// live.Bind's refusal names accounts, never the credential (binding.go).
				return &live.Refusal{Condition: live.CondAccount, Detail: `credential account "other", bound "mine"`}
			}
			return nil
		},
		HTTP: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}},
		Now: func() time.Time { return now },
	}
}

func (w *world) seen() []call {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.calls)
}

func runObserve(t *testing.T, w *world, env map[string]string) (Record, []byte, string, error) {
	t.Helper()
	var term bytes.Buffer
	raw, err := Check(w.options()).Observe(context.Background(), env, &term)
	var rec Record
	if raw == nil {
		t.Fatalf("Observe returned no record (error %v)", err)
	}
	if jerr := json.Unmarshal(raw, &rec); jerr != nil {
		t.Fatalf("record %s: %v", raw, jerr)
	}
	return rec, raw, term.String(), err
}

func noLeak(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		for _, bad := range append([]string{echo, "<Error", "{\"class\"", envelope[:20], "Y2lwaGVydGV4dA"}, secrets...) {
			if strings.Contains(s, bad) {
				t.Errorf("%s carries %q", what, bad)
			}
		}
	}
}

type want struct {
	step, credential, expected, observed string
	status                               int
	asExpected                           bool
}

func wantSteps(t *testing.T, got []Observation, want []want) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d observations, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Step != w.step || g.Credential != w.credential || g.Expected != w.expected || g.Observed != w.observed || g.Status != w.status || g.AsExpected != w.asExpected {
			t.Errorf("observation %d = %+v, want %+v", i, g, w)
		}
	}
}

var expected = []want{
	{"p25-read-bucket-a", "p25", "refused", "refused", 403, true}, // D107: P25 refuted, run 20261012T072729Z-0646
	{"p25-read-bucket-b", "p25", "refused", "refused", 403, true},
	{"p26-get-me", "tenant", "refused", "refused", 403, true},
	{"p9-iam-write", "tenant", "refused", "refused", 403, true},
	{"p3-state-object", "s3", "encrypted", "encrypted", 200, true},
}

func with(i int, edit func(*want)) []want {
	out := slices.Clone(expected)
	edit(&out[i])
	return out
}

// TestObserve: the five observations in order, each with its own credential; expected denials
// pass; any other answer is a finding that fails Observe, naming the step and the refused actions.
func TestObserve(t *testing.T) {
	t.Run("as-expected", func(t *testing.T) {
		w := newWorld(t)
		w.sse = "AES256"
		rec, raw, term, err := runObserve(t, w, testEnv())
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if rec.Check != "storage-iam" || len(rec.Findings) != 0 {
			t.Errorf("record %s: want check storage-iam, no finding", raw)
		}
		wantSteps(t, rec.Observations, expected)
		// The refusals name their actions; the state object is an encryption envelope with its SSE
		// header recorded.
		for i, a := range []string{"publicCloudProject:apiovh:region/storage/get", "publicCloudProject:apiovh:region/storage/get", "account:apiovh:me/get", "account:apiovh:iam/policy/create"} {
			if got := strings.Join(rec.Observations[i].Actions, ","); got != a {
				t.Errorf("%s actions %q, want %q", rec.Observations[i].Step, got, a)
			}
		}
		if o := rec.Observations[4]; o.SSE != "AES256" || o.Detail != "encryption envelope" {
			t.Errorf("state observation %+v: want SSE AES256, detail encryption envelope", o)
		}
		// Who called what, in order; each identity bound before its first call.
		var order []string
		for _, c := range w.seen() {
			order = append(order, c.who+" "+c.method+" "+c.path)
		}
		wantOrder := []string{
			"bind ovh-eu " + p25ID,
			"p25 GET /v1/cloud/project/" + project + "/region/GRA/storage/" + bucketA,
			"p25 GET /v1/cloud/project/" + project + "/region/GRA/storage/" + bucketB,
			"bind ovh-eu " + tenantID,
			"tenant GET /v1/me",
			"tenant POST /v2/iam/policy",
			"s3 GET /" + stateBkt + "/" + StateKey,
		}
		if !slices.Equal(order, wantOrder) {
			t.Errorf("calls\n%s\nwant\n%s", strings.Join(order, "\n"), strings.Join(wantOrder, "\n"))
		}
		// One IAM write: a harmless policy of the tenant identity on the project, expiring in an hour.
		if len(w.posted) != 1 {
			t.Fatalf("%d IAM writes, want 1", len(w.posted))
		}
		var p struct {
			Name        string
			Identities  []string
			Resources   []struct{ URN string }
			Permissions struct {
				Allow []struct{ Action string }
				Deny  []struct{ Action string }
			}
			ExpiredAt string
		}
		if err := json.Unmarshal(w.posted[0], &p); err != nil {
			t.Fatalf("posted %s: %v", w.posted[0], err)
		}
		if p.Name != writeName || !slices.Equal(p.Identities, []string{identity}) || len(p.Resources) != 1 || p.Resources[0].URN != "urn:v1:eu:resource:publicCloudProject:"+project ||
			len(p.Permissions.Allow) != 1 || p.Permissions.Allow[0].Action != "publicCloudProject:apiovh:region/storage/get" || len(p.Permissions.Deny) != 0 ||
			p.ExpiredAt != now.Add(time.Hour).Format(time.RFC3339) {
			t.Errorf("posted policy %s: want %s, the tenant identity on the project URN, allow region/storage/get only, expiring at %s", w.posted[0], writeName, now.Add(time.Hour).Format(time.RFC3339))
		}
		for _, l := range strings.Split(strings.TrimSpace(term), "\n") {
			if !strings.HasPrefix(l, "LZ-LIVE observe ") {
				t.Errorf("terminal line %q: want LZ-LIVE observe lines only", l)
			}
		}
		noLeak(t, "record and terminal", string(raw), term)
	})

	// D107: T010 run 20261012T072729Z-0646 refuted P25 (bucket A refused); a bucket-A read that is
	// allowed after all is a finding that says so.
	t.Run("bucket-a-allowed", func(t *testing.T) {
		w := newWorld(t)
		w.answers["p25 GET /v1/cloud/project/"+project+"/region/GRA/storage/"+bucketA] = answer{200, ""}
		rec, raw, term, err := runObserve(t, w, testEnv())
		if err == nil || !strings.Contains(err.Error(), "p25-read-bucket-a: expected refused, observed allowed 200") || !strings.Contains(err.Error(), "P25 holds after all") {
			t.Fatalf("Observe: %v; want an error naming the step, the allowed read and that P25 holds after all", err)
		}
		wantSteps(t, rec.Observations, with(0, func(w *want) { w.observed, w.status, w.asExpected = "allowed", 200, false }))
		if !slices.Equal(rec.Findings, []string{"p25-read-bucket-a"}) {
			t.Errorf("findings %v, want [p25-read-bucket-a]", rec.Findings)
		}
		if !strings.Contains(term, "LZ-LIVE finding p25-read-bucket-a: expected refused, observed allowed 200; P25 holds after all: re-open tag-scoped envelopes (D107)") {
			t.Errorf("terminal lacks the finding line:\n%s", term)
		}
		// Expected denials stay passes, and the steps after the finding still ran.
		if strings.Contains(err.Error(), "p25-read-bucket-b") || strings.Contains(err.Error(), "p26-get-me") || strings.Contains(err.Error(), "p9-iam-write") {
			t.Errorf("error %q names an expected denial", err)
		}
		noLeak(t, "record, terminal and error", string(raw), term, err.Error())
	})

	findings := map[string]struct {
		setup func(*world)
		steps []want
		err   string
	}{
		// D107 names the action: a 403 naming another one, or a refusal that is no 403, is not P25's
		// refutation.
		"bucket-a-refusal-names-another-action": {func(w *world) {
			w.answers["p25 GET /v1/cloud/project/"+project+"/region/GRA/storage/"+bucketA] = answer{403, "account:apiovh:me/get"}
		}, with(0, func(w *want) { w.asExpected = false }), "p25-read-bucket-a: expected refused, observed refused 403 account:apiovh:me/get"},
		// Review r1: a 403 naming no action (a proxy, a token scope) is not D107's refutation either.
		"bucket-a-refusal-names-no-action": {func(w *world) {
			w.answers["p25 GET /v1/cloud/project/"+project+"/region/GRA/storage/"+bucketA] = answer{403, ""}
		}, with(0, func(w *want) { w.asExpected = false }), "p25-read-bucket-a: expected refused, observed refused 403; D107 expects a 403 naming publicCloudProject:apiovh:region/storage/get"},
		"bucket-a-refusal-not-403": {func(w *world) {
			w.answers["p25 GET /v1/cloud/project/"+project+"/region/GRA/storage/"+bucketA] = answer{404, "-"}
		}, with(0, func(w *want) { w.status, w.asExpected = 404, false }), "p25-read-bucket-a: expected refused, observed refused 404"},
		"bucket-b-allowed": {func(w *world) {
			w.answers["p25 GET /v1/cloud/project/"+project+"/region/GRA/storage/"+bucketB] = answer{200, ""}
		}, with(1, func(w *want) { w.observed, w.status, w.asExpected = "allowed", 200, false }), "p25-read-bucket-b: expected refused, observed allowed 200"},
		"me-allowed": {func(w *world) { w.answers["tenant GET /v1/me"] = answer{200, ""} },
			with(2, func(w *want) { w.observed, w.status, w.asExpected = "allowed", 200, false }), "p26-get-me: expected refused, observed allowed 200"},
		// A 403 naming another action is not the denial the step observes.
		"refusal-names-another-action": {func(w *world) {
			w.answers["p25 GET /v1/cloud/project/"+project+"/region/GRA/storage/"+bucketB] = answer{403, "account:apiovh:me/get"}
		}, with(1, func(w *want) { w.asExpected = false }), "p25-read-bucket-b: expected refused, observed refused 403 account:apiovh:me/get"},
		// Not a refusal by IAM: a missing bucket.
		"refusal-not-403": {func(w *world) {
			w.answers["p25 GET /v1/cloud/project/"+project+"/region/GRA/storage/"+bucketB] = answer{404, "-"}
		}, with(1, func(w *want) { w.status, w.asExpected = 404, false }), "p25-read-bucket-b: expected refused, observed refused 404"},
		"write-refused-otherwise": {func(w *world) { w.answers["tenant POST /v2/iam/policy"] = answer{400, "-"} },
			with(3, func(w *want) { w.status, w.asExpected = 400, false }), "p9-iam-write: expected refused, observed refused 400"},
		"state-plaintext": {func(w *world) { w.stateBody = plainState },
			with(4, func(w *want) { w.observed, w.asExpected = "plaintext", false }), "p3-state-object: expected encrypted, observed plaintext 200"},
		"state-no-envelope": {func(w *world) { w.stateBody = `{"serial":3}` },
			with(4, func(w *want) { w.observed, w.asExpected = "plaintext", false }), "p3-state-object: expected encrypted, observed plaintext 200"},
		"state-not-json": {func(w *world) { w.stateBody = "\x00\x01" + echo },
			with(4, func(w *want) { w.observed, w.asExpected = "unreadable", false }), "p3-state-object: expected encrypted, observed unreadable 200"},
		"state-refused": {func(w *world) { w.stateStatus = 403 },
			with(4, func(w *want) { w.observed, w.status, w.asExpected = "refused", 403, false }), "p3-state-object: expected encrypted, observed refused 403"},
	}
	for name, tc := range findings {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			tc.setup(w)
			rec, raw, term, err := runObserve(t, w, testEnv())
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("Observe: %v; want an error holding %q", err, tc.err)
			}
			wantSteps(t, rec.Observations, tc.steps)
			step := strings.SplitN(tc.err, ":", 2)[0]
			if !slices.Equal(rec.Findings, []string{step}) || !strings.Contains(term, "LZ-LIVE finding "+step+": ") {
				t.Errorf("findings %v, terminal:\n%s\nwant the one finding %s, printed", rec.Findings, term, step)
			}
			noLeak(t, "record, terminal and error", string(raw), term, err.Error())
		})
	}

	// An accepted IAM write is a finding, the run fails, and the undo (the same check, as lz-live
	// runs it) deletes the policy once: by the id the POST returned and by name (T101, T100 gap 6),
	// also when the listing does not (yet) show it or shows it under another name.
	for name, setup := range map[string]func(*world){
		"write-allowed":          func(*world) {},
		"write-allowed-unlisted": func(w *world) { w.hidden = true },
		"write-allowed-renamed":  func(w *world) { w.renameTo = strings.ToLower(writeName) },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.writeAllowed = true
			setup(w)
			c := Check(w.options())
			var term bytes.Buffer
			raw, err := c.Observe(context.Background(), testEnv(), &term)
			var rec Record
			if jerr := json.Unmarshal(raw, &rec); jerr != nil {
				t.Fatalf("record %s: %v", raw, jerr)
			}
			if err == nil || !strings.Contains(err.Error(), "p9-iam-write: expected refused, observed allowed 200") || !strings.Contains(err.Error(), "undo deletes") {
				t.Fatalf("Observe: %v; want the write named as a finding the undo deletes", err)
			}
			wantSteps(t, rec.Observations, with(3, func(w *want) { w.observed, w.status, w.asExpected = "allowed", 200, false }))
			created := w.written[0]
			if rec.Observations[3].PolicyID != created {
				t.Errorf("p9-iam-write policy_id %q, want the id the POST returned, %s", rec.Observations[3].PolicyID, created)
			}
			noLeak(t, "record, terminal and error", string(raw), term.String(), err.Error())
			if err := c.Undo(context.Background(), testEnv(), &term); err != nil {
				t.Fatalf("Undo: %v", err)
			}
			if !slices.Equal(w.policies, []policy{{otherPol, "lz-sandbox-admin"}}) {
				t.Errorf("policies after the undo %v: want only the other policy", w.policies)
			}
			var deletes []string
			for _, c := range w.seen() {
				if c.method == http.MethodDelete {
					deletes = append(deletes, c.path)
				}
			}
			if !slices.Equal(deletes, []string{"/v2/iam/policy/" + created}) {
				t.Errorf("deletes %v, want the created policy once", deletes)
			}
			if !strings.Contains(term.String(), "LZ-LIVE undo removed 1 IAM policies named "+writeName+" or created by the write") {
				t.Errorf("terminal lacks the undo line:\n%s", term.String())
			}
		})
	}

	// Review r1: a listing that fails does not keep the undo from deleting the id it knows; the
	// second listing decides: failing too, or showing a policy of the name the first one missed, the
	// undo fails (lz-live keeps the root); clear, it passes.
	for name, tc := range map[string]struct {
		setup func(*world)
		err   string
	}{
		"write-allowed-listing-fails":      {func(w *world) { w.listStatus = 500 }, "ListPolicies answered 500"},
		"write-allowed-listing-fails-once": {func(w *world) { w.listFailOnce = true }, ""},
		"write-allowed-listing-fails-once-name-left": {func(w *world) {
			w.listFailOnce = true
			w.policies = append(w.policies, policy{"3f2504e0-4f89-41d3-9a0c-000000000007", writeName})
		}, "1 policies named " + writeName + " remain"},
		// Review r2: a delete acknowledged but not effective, of a policy listed under a normalised
		// name, is found by its id in the second listing.
		"write-allowed-renamed-still-listed": {func(w *world) {
			w.keepDeleted = true
			w.policies[len(w.policies)-1].name = strings.ToLower(writeName) // the write's, set after Observe
		}, "1 policies named " + writeName + " remain"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.writeAllowed = true
			c := Check(w.options())
			if _, err := c.Observe(context.Background(), testEnv(), io.Discard); err == nil {
				t.Fatal("an accepted write passed")
			}
			created := w.written[0]
			tc.setup(w)
			err := c.Undo(context.Background(), testEnv(), io.Discard)
			if tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Fatalf("Undo: %v; want %q", err, tc.err)
			}
			var deletes []string
			for _, c := range w.seen() {
				if c.method == http.MethodDelete {
					deletes = append(deletes, c.path)
				}
			}
			if !slices.Equal(deletes, []string{"/v2/iam/policy/" + created}) {
				t.Errorf("deletes %v, want the created policy although the listing failed", deletes)
			}
			noLeak(t, "error", fmt.Sprint(err))
		})
	}

	// Review r2: a refused delete does not stop the others; the undo still fails.
	t.Run("write-allowed-first-delete-fails", func(t *testing.T) {
		w := newWorld(t)
		w.writeAllowed = true
		c := Check(w.options())
		if _, err := c.Observe(context.Background(), testEnv(), io.Discard); err == nil {
			t.Fatal("an accepted write passed")
		}
		created := w.written[0]
		w.policies = append(w.policies, policy{"3f2504e0-4f89-41d3-9a0c-000000000007", writeName})
		w.deleteFailID = created
		err := c.Undo(context.Background(), testEnv(), io.Discard)
		if err == nil || !strings.Contains(err.Error(), "DeletePolicy answered 500") {
			t.Fatalf("Undo: %v; want the refused delete", err)
		}
		var deletes []string
		for _, c := range w.seen() {
			if c.method == http.MethodDelete {
				deletes = append(deletes, c.path)
			}
		}
		if !slices.Contains(deletes, "/v2/iam/policy/3f2504e0-4f89-41d3-9a0c-000000000007") || !slices.Contains(deletes, "/v2/iam/policy/"+created) {
			t.Errorf("deletes %v, want both policies tried", deletes)
		}
		noLeak(t, "error", err.Error())
	})

	// G13: an identity is bound before use; one bound to another account makes no call.
	t.Run("bind-refused", func(t *testing.T) {
		w := newWorld(t)
		w.bindRefuse = p25ID
		rec, raw, term, err := runObserve(t, w, testEnv())
		if err == nil || !strings.Contains(err.Error(), "p25") {
			t.Fatalf("Observe: %v; want the P25 identity's binding named", err)
		}
		for _, c := range w.seen() {
			if c.who == "p25" {
				t.Errorf("the P25 identity called %s %s after its binding failed", c.method, c.path)
			}
		}
		if rec.Observations[0].Observed != "not-run" || rec.Observations[1].Observed != "not-run" || !rec.Observations[2].AsExpected {
			t.Errorf("observations %+v: want both P25 steps not-run, the rest observed", rec.Observations)
		}
		noLeak(t, "record, terminal and error", string(raw), term, err.Error())
	})

	t.Run("token-refused", func(t *testing.T) {
		w := newWorld(t)
		env := testEnv()
		env["tenant_client_secret"] = "wrong-" + tenantSK
		rec, raw, term, err := runObserve(t, w, env)
		if err == nil || rec.Observations[2].Observed != "not-run" || rec.Observations[3].Observed != "not-run" {
			t.Fatalf("Observe: %v, %+v; want the tenant steps not-run and an error", err, rec.Observations)
		}
		noLeak(t, "record, terminal and error", string(raw), term, err.Error())
	})

	t.Run("s3-hang-up", func(t *testing.T) {
		w := newWorld(t)
		w.s3Hangup = true
		rec, raw, term, err := runObserve(t, w, testEnv())
		if err == nil || !strings.Contains(err.Error(), "p3-state-object") || rec.Observations[4].Observed != "no-answer" {
			t.Fatalf("Observe: %v, %+v; want p3-state-object no-answer and an error", err, rec.Observations[4])
		}
		noLeak(t, "record, terminal and error", string(raw), term, err.Error())
	})

	t.Run("cancelled", func(t *testing.T) {
		w := newWorld(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		raw, err := Check(w.options()).Observe(ctx, testEnv(), io.Discard)
		var rec Record
		if err == nil || json.Unmarshal(raw, &rec) != nil || len(rec.Observations) != 5 {
			t.Fatalf("Observe: %v, %s; want an error and five not-run observations", err, raw)
		}
		for _, o := range rec.Observations {
			if o.Observed != "not-run" {
				t.Errorf("%s observed %s after cancellation", o.Step, o.Observed)
			}
		}
		if n := len(w.seen()); n != 0 {
			t.Errorf("%d calls after cancellation", n)
		}
	})
}

// TestUndo: with the admin's credential only, every policy named lzprobe-p9-write-<run id> is
// deleted and the listing read again; nothing else is touched; a refused listing or delete, or a
// policy still listed, is an error (lz-live keeps the root).
func TestUndo(t *testing.T) {
	cases := map[string]struct {
		setup  func(*world)
		err    string
		remain []policy
	}{
		"none": {func(w *world) {}, "", []policy{{otherPol, "lz-sandbox-admin"}}},
		"two-of-the-name": {func(w *world) {
			w.policies = append(w.policies, policy{"3f2504e0-4f89-41d3-9a0c-000000000007", writeName}, policy{"3f2504e0-4f89-41d3-9a0c-000000000008", writeName})
		}, "", []policy{{otherPol, "lz-sandbox-admin"}}},
		// Another run's write and a prefix sibling stay.
		"other-names-stay": {func(w *world) {
			w.policies = append(w.policies, policy{"3f2504e0-4f89-41d3-9a0c-000000000007", "lzprobe-p9-write-20261012T080000Z-aaaa"}, policy{"3f2504e0-4f89-41d3-9a0c-000000000008", writeName + "x"})
		}, "", []policy{{otherPol, "lz-sandbox-admin"}, {"3f2504e0-4f89-41d3-9a0c-000000000007", "lzprobe-p9-write-20261012T080000Z-aaaa"}, {"3f2504e0-4f89-41d3-9a0c-000000000008", writeName + "x"}}},
		// Review r1: a foreign policy of any shape is no reason to fail the undo.
		"foreign-policy-any-shape": {func(w *world) { w.policies = append(w.policies, policy{"not-a-uuid", "My policy é"}) },
			"", []policy{{otherPol, "lz-sandbox-admin"}, {"not-a-uuid", "My policy é"}}},
		"delete-404": {func(w *world) {
			w.policies = append(w.policies, policy{"3f2504e0-4f89-41d3-9a0c-000000000007", writeName})
			w.deleteStatus = 404
		}, "", []policy{{otherPol, "lz-sandbox-admin"}}},
		"delete-refused": {func(w *world) {
			w.policies = append(w.policies, policy{"3f2504e0-4f89-41d3-9a0c-000000000007", writeName})
			w.deleteStatus = 403
		}, "DeletePolicy answered 403", nil},
		"still-listed": {func(w *world) {
			w.policies = append(w.policies, policy{"3f2504e0-4f89-41d3-9a0c-000000000007", writeName})
			w.keepDeleted = true
		}, "1 policies named " + writeName + " remain", nil},
		"listing-refused": {func(w *world) { w.listStatus = 403 }, "ListPolicies answered 403", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			tc.setup(w)
			var term bytes.Buffer
			err := Check(w.options()).Undo(context.Background(), testEnv(), &term)
			if tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Fatalf("Undo: %v, want %q", err, tc.err)
			}
			if tc.remain != nil && !slices.Equal(w.policies, tc.remain) {
				t.Errorf("policies after the undo %v, want %v", w.policies, tc.remain)
			}
			calls := w.seen()
			lists := 0
			for _, c := range calls {
				if c.who != "admin" {
					t.Errorf("undo call %+v not as the admin", c)
				}
				if c.method == http.MethodGet {
					lists++
				}
				if c.method == http.MethodDelete && !strings.Contains(c.path, "3f2504e0-4f89-41d3-9a0c-00000000000") {
					t.Errorf("undo deleted %s", c.path)
				}
			}
			if tc.err == "" && lists != 2 {
				t.Errorf("%d listings, want 2 (before and after the deletes)", lists)
			}
			// Review r1: a 404 removed nothing, so it does not count.
			if want := map[string]string{"delete-404": "removed 0 ", "two-of-the-name": "removed 2 "}[name]; want != "" && !strings.Contains(term.String(), want) {
				t.Errorf("terminal %q lacks %q", term.String(), want)
			}
			noLeak(t, "terminal and error", term.String(), fmt.Sprint(err))
		})
	}
	t.Run("only-the-run's-name", func(t *testing.T) {
		w := newWorld(t)
		w.policies = append(w.policies, policy{"3f2504e0-4f89-41d3-9a0c-000000000007", writeName})
		if err := Check(w.options()).Undo(context.Background(), testEnv(), io.Discard); err != nil {
			t.Fatal(err)
		}
		var deletes []string
		for _, c := range w.seen() {
			if c.method == http.MethodDelete {
				deletes = append(deletes, c.path)
			}
		}
		if !slices.Equal(deletes, []string{"/v2/iam/policy/3f2504e0-4f89-41d3-9a0c-000000000007"}) {
			t.Errorf("deletes %v, want the run's write only", deletes)
		}
	})
}

// TestEnv: the published variables are refused unless every key is present, non-empty and the only
// ones, and the names and URNs are the run's own; refusals name keys, never values.
func TestEnv(t *testing.T) {
	edits := map[string]func(map[string]string){
		"missing": func(e map[string]string) { delete(e, "s3_secret") },
		"extra":   func(e map[string]string) { e["admin_secret"] = "x" },
		"empty":   func(e map[string]string) { e["p25_client_secret"] = "" },
		"run-id":  func(e map[string]string) { e["run_id"] = "now" },
		"project": func(e map[string]string) { e["project_id"] = "../me" },
		"project-urn": func(e map[string]string) {
			e["project_urn"] = "urn:v1:eu:resource:publicCloudProject:ffffffffffffffffffffffffffffffff"
		},
		"bucket-a":            func(e map[string]string) { e["bucket_a"] = bucketB },
		"bucket-b":            func(e map[string]string) { e["bucket_b"] = "lzprobe-p25b-20261012t080000z-aaaa" },
		"state-bucket":        func(e map[string]string) { e["state_bucket"] = "lzprobe-state-20261012t072729z-0646" },
		"state-key":           func(e map[string]string) { e["state_key"] = "other.tfstate" },
		"region":              func(e map[string]string) { e["region"] = "gra/../x" },
		"tenant-identity":     func(e map[string]string) { e["tenant_identity"] = "urn:v1:eu:resource:x" },
		"identity-with-space": func(e map[string]string) { e["tenant_identity"] = "urn:v1:eu:identity:a b" },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			env := testEnv()
			edit(env)
			c := Check(Options{})
			err := c.Validate(env)
			if err == nil {
				t.Fatal("no refusal")
			}
			noLeak(t, "error", err.Error())
			if _, err := c.Observe(context.Background(), env, io.Discard); err == nil {
				t.Error("Observe ran on refused variables")
			}
			if err := c.Undo(context.Background(), env, io.Discard); err == nil {
				t.Error("Undo ran on refused variables")
			}
		})
	}
	if err := Check(Options{}).Validate(testEnv()); err != nil {
		t.Errorf("Validate(the root's variables): %v", err)
	}
	if got := slices.Sorted(maps.Keys(testEnv())); !slices.Equal(got, slices.Sorted(slices.Values(append(slices.Clone(Public),
		"p25_client_id", "p25_client_secret", "s3_key_id", "s3_secret", "tenant_client_id", "tenant_client_secret")))) {
		t.Errorf("test env keys %v drift from Public", got)
	}
}
