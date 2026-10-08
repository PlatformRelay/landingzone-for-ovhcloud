package live

// T048: the L7 observation collector's KD-1 canary and the chain's reporting of L7 observations
// (spec 005 FR-004, FR-008, FR-010, FR-011, SC-003; research R20; contracts/checks.md V010, guard
// G15; coordinator 2026-10-08: summary.json `blocked` for a blocked chain, known_deviations with
// the deviations observed). The judge's own rows over recorded observation sets are
// TestChainObservations in tools/internal/probes/live/chain (build tag live).
//
// Collector (TestObserveCanary*): against a fake management API that models the storage routes
// of kb/api/v1/cloud.json (POST/GET/DELETE …/region/{r}/storage[/{name}], …/bulkDeleteObjects,
// …/presign, …/object) and enforces each route's IAM action (iamActions in that schema) on the
// fake credentials' policies (testdata/api/credentials.json: the tenant deployer holds the R6
// allowlist, region/storage/delete and bulkDeleteObjects included, which is KD-1). A presigned
// PUT is the API-only way the bootstrap authority (an OAuth2 client without S3 keys) puts the
// canary's object (cloud.storage.PresignedURLInput: method PUT, object). The fake refuses DELETE
// of a non-empty bucket with 409 (S3 BucketNotEmpty semantics; OVHcloud behaviour UNVERIFIED).
//
// Chain (TestObserveChain*): the chain harness of chain_test.go with a fake Observer.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	canaryProject = "f0000000000000000000000000000048"
	canaryRegion  = "GRA"
	canaryRunID   = "20261008T120000Z-0048"
	canaryOrg     = "lz"
)

// canaryCall is one request the fake answered, with what the world held when it arrived.
type canaryCall struct {
	Method, Path, Op, Cred string
	Status                 int
	Bucket                 string   // the bucket the route names ("" for create/list)
	Objects                int      // objects in the created canary when the call arrived (-1: none exists)
	Trap                   []string // the trap's names when the call arrived
}

type canaryAPI struct {
	*httptest.Server
	t       *testing.T
	creds   map[string]apiCredential // by client id
	tokens  map[string]apiCredential // by access token
	trap    *Trap
	mu      sync.Mutex
	buckets map[string]map[string]string // name -> key -> body
	canary  string                       // the bucket the admin created
	calls   []canaryCall
	// answer forces a status: "<credential> <METHOD> <op>" (ops: create list get delete bulk
	// presign objects upload).
	answer map[string]int
	// bulkErrorsOnly: the tenant's bulkDeleteObjects answers 200 with every object in errors.
	bulkErrorsOnly bool
}

func newCanaryAPI(t *testing.T, tenantPolicy func([]string) []string) *canaryAPI {
	t.Helper()
	var fx apiFixture
	readJSON(t, filepath.Join("testdata", "api", "credentials.json"), &fx)
	f := &canaryAPI{t: t, creds: map[string]apiCredential{}, tokens: map[string]apiCredential{}, buckets: map[string]map[string]string{},
		answer: map[string]int{}, trap: &Trap{}}
	for _, c := range fx.Credentials {
		if c.Name == "tenant-deployer" && tenantPolicy != nil {
			c.Policy = tenantPolicy(slices.Clone(c.Policy))
		}
		f.creds[c.ClientID] = c
		f.tokens["fake-token-"+c.Name] = c
	}
	tok := &fakeAPI{creds: f.creds, tokens: f.tokens}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/oauth2/token", tok.token)
	mux.HandleFunc("/v1/", f.api)
	mux.HandleFunc("/presigned/", f.upload)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func without(drop ...string) func([]string) []string {
	return func(p []string) []string {
		return slices.DeleteFunc(p, func(a string) bool { return slices.Contains(drop, a) })
	}
}

func (f *canaryAPI) cred(name string) Credential {
	for _, c := range f.creds {
		if c.Name == name {
			return Credential{Endpoint: "ovh-eu", ClientID: c.ClientID, ClientSecret: c.ClientSecret}
		}
	}
	f.t.Fatalf("no fake credential %s", name)
	return Credential{}
}

func (f *canaryAPI) options() CanaryOptions {
	return CanaryOptions{API: API{TokenURL: f.URL + "/auth/oauth2/token", BaseURL: f.URL + "/v1", HTTP: f.Client()},
		Admin: f.cred("admin"), Tenant: f.cred("tenant-deployer"), Project: canaryProject, Region: canaryRegion,
		Org: canaryOrg, RunID: canaryRunID, Trap: f.trap}
}

func (f *canaryAPI) Calls() []canaryCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// arrive records a call as it arrives; the caller sets its status.
func (f *canaryAPI) arrive(r *http.Request, op, cred, bucket string) *canaryCall {
	objects := -1
	if b, ok := f.buckets[f.canary]; ok && f.canary != "" {
		objects = len(b)
	}
	var trap []string
	if cred == "tenant-deployer" {
		// Only the tenant's calls are judged against the trap; the trap's own cleanup (admin calls)
		// may hold it while it runs.
		trap = f.trap.Names()
	}
	f.calls = append(f.calls, canaryCall{Method: r.Method, Path: r.URL.Path, Op: op, Cred: cred, Bucket: bucket, Objects: objects, Trap: trap})
	return &f.calls[len(f.calls)-1]
}

func canaryReply(w http.ResponseWriter, call *canaryCall, status int, body any) {
	call.Status = status
	if body == nil {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func (f *canaryAPI) api(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, known := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	prefix := "/v1/cloud/project/" + canaryProject + "/region/" + canaryRegion + "/storage"
	rest, under := strings.CutPrefix(r.URL.Path, prefix)
	var op, action, bucket string
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	switch {
	case !under:
	case rest == "" && r.Method == http.MethodPost:
		op, action = "create", "region/storage/create"
	case rest == "" && r.Method == http.MethodGet:
		op, action = "list", "region/storage/get"
	case len(parts) == 1 && r.Method == http.MethodGet:
		op, action, bucket = "get", "region/storage/get", parts[0]
	case len(parts) == 1 && r.Method == http.MethodDelete:
		op, action, bucket = "delete", "region/storage/delete", parts[0]
	case len(parts) == 2 && parts[1] == "bulkDeleteObjects" && r.Method == http.MethodPost:
		op, action, bucket = "bulk", "region/storage/bulkDeleteObjects", parts[0]
	case len(parts) == 2 && parts[1] == "presign" && r.Method == http.MethodPost:
		op, action, bucket = "presign", "region/storage/presign", parts[0]
	case len(parts) == 2 && parts[1] == "object" && r.Method == http.MethodGet:
		op, action, bucket = "objects", "region/storage/object/get", parts[0]
	}
	if op == "create" {
		// The bucket a create asks for is recorded even when the answer is forced (review r1).
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var in struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &in)
		bucket = in.Name
	}
	call := f.arrive(r, op, c.Name, bucket)
	if !known {
		canaryReply(w, call, http.StatusUnauthorized, map[string]string{"message": "Invalid credentials"})
		return
	}
	if op == "" {
		canaryReply(w, call, http.StatusNotFound, map[string]string{"message": "not modelled by the fake"})
		return
	}
	if code := f.answer[c.Name+" "+r.Method+" "+op]; code != 0 {
		canaryReply(w, call, code, map[string]string{"message": "forced by the test"})
		return
	}
	if !allows(c.Policy, "publicCloudProject:apiovh:"+action) {
		canaryReply(w, call, http.StatusForbidden, map[string]string{"message": "This call has not been granted", "action": action})
		return
	}
	objs, exists := f.buckets[bucket]
	if op != "create" && bucket != "" && !exists {
		canaryReply(w, call, http.StatusNotFound, map[string]string{"message": "no such container"})
		return
	}
	switch op {
	case "create":
		var in struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Name == "" {
			canaryReply(w, call, http.StatusBadRequest, map[string]string{"message": "name required"})
			return
		}
		if _, taken := f.buckets[in.Name]; taken {
			canaryReply(w, call, http.StatusConflict, map[string]string{"message": "container exists"})
			return
		}
		f.buckets[in.Name] = map[string]string{}
		if c.Name == "admin" && f.canary == "" {
			f.canary = in.Name
		}
		canaryReply(w, call, http.StatusOK, map[string]any{"name": in.Name, "region": canaryRegion})
	case "list":
		var out []map[string]any
		for _, n := range slices.Sorted(mapKeys(f.buckets)) {
			out = append(out, map[string]any{"name": n})
		}
		canaryReply(w, call, http.StatusOK, out)
	case "get":
		canaryReply(w, call, http.StatusOK, map[string]any{"name": bucket, "region": canaryRegion, "objectsCount": len(objs), "objects": keyList(objs)})
	case "objects":
		canaryReply(w, call, http.StatusOK, keyList(objs))
	case "delete":
		if len(objs) > 0 {
			canaryReply(w, call, http.StatusConflict, map[string]string{"message": "container not empty"})
			return
		}
		delete(f.buckets, bucket)
		canaryReply(w, call, http.StatusNoContent, nil)
	case "bulk":
		var in struct {
			Objects []struct {
				Key string `json:"key"`
			} `json:"objects"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			canaryReply(w, call, http.StatusBadRequest, map[string]string{"message": "bad body"})
			return
		}
		deleted, errs := []map[string]string{}, []map[string]string{}
		for _, o := range in.Objects {
			if _, ok := objs[o.Key]; ok && !(f.bulkErrorsOnly && c.Name == "tenant-deployer") {
				delete(objs, o.Key)
				deleted = append(deleted, map[string]string{"key": o.Key})
			} else {
				errs = append(errs, map[string]string{"key": o.Key, "code": "AccessDenied"})
			}
		}
		canaryReply(w, call, http.StatusOK, map[string]any{"deleted": deleted, "errors": errs})
	case "presign":
		var in struct {
			Method string `json:"method"`
			Object string `json:"object"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Method != http.MethodPut || in.Object == "" {
			canaryReply(w, call, http.StatusBadRequest, map[string]string{"message": "method PUT and object required"})
			return
		}
		canaryReply(w, call, http.StatusOK, map[string]any{"method": "PUT", "signedHeaders": map[string]string{},
			"url": f.URL + "/presigned/" + bucket + "/" + in.Object + "?X-Amz-Signature=fake"})
	}
}

// upload is the presigned PUT (no bearer token: the URL is the authority).
func (f *canaryAPI) upload(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/presigned/"), "/")
	call := f.arrive(r, "upload", "presigned", name)
	if code := f.answer["presigned PUT upload"]; code != 0 {
		canaryReply(w, call, code, nil)
		return
	}
	objs, ok := f.buckets[name]
	if r.Method != http.MethodPut || !ok || key == "" || r.URL.Query().Get("X-Amz-Signature") == "" {
		canaryReply(w, call, http.StatusForbidden, nil)
		return
	}
	body, _ := io.ReadAll(r.Body)
	objs[key] = string(body)
	canaryReply(w, call, http.StatusOK, nil)
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func keyList(objs map[string]string) []map[string]string {
	out := []map[string]string{}
	for _, k := range slices.Sorted(mapKeys(objs)) {
		out = append(out, map[string]string{"key": k})
	}
	return out
}

// kd1 returns the canary's observations by subject; a second observation of one subject, or one
// of another assertion, is an error.
func kd1(t *testing.T, obs []Observation) map[string]Observation {
	t.Helper()
	out := map[string]Observation{}
	for _, o := range obs {
		if o.Assertion != "kd1-canary" {
			t.Errorf("observation %+v: the canary observes only kd1-canary", o)
			continue
		}
		if _, dup := out[o.Subject]; dup {
			t.Errorf("subject %q observed twice", o.Subject)
		}
		out[o.Subject] = o
	}
	return out
}

// canaryJudge checks what every canary run must hold: the tenant's requests are its two KD-1
// calls on the canary (and reads), never on another bucket; the admin creates one bucket, the
// run's canary, and writes nothing else; the presigned upload goes to the canary; no client
// secret in the observations.
func canaryJudge(t *testing.T, f *canaryAPI, obs []Observation) {
	t.Helper()
	creates := 0
	for _, c := range f.Calls() {
		if c.Op == "create" {
			creates++
			if c.Cred != "admin" || !strings.HasPrefix(c.Bucket, canaryOrg+"-bkt-canary-") || !strings.Contains(c.Bucket, strings.ToLower(canaryRunID)) {
				t.Errorf("create of %q as %s: the only bucket the canary run creates is its canary, by the admin", c.Bucket, c.Cred)
			}
		}
		switch c.Cred {
		case "tenant-deployer":
			if c.Bucket != f.canary || f.canary == "" {
				t.Errorf("tenant %s %s: the tenant deployer touches the canary only (real state buckets are never the target)", c.Method, c.Path)
			}
			if c.Method != http.MethodGet && c.Op != "bulk" && c.Op != "delete" {
				t.Errorf("tenant %s %s: not a KD-1 call", c.Method, c.Path)
			}
		case "admin":
			if c.Method != http.MethodGet && c.Op != "create" && c.Bucket != f.canary {
				t.Errorf("admin %s %s: the bootstrap authority writes only the canary", c.Method, c.Path)
			}
		case "presigned":
			if c.Bucket != f.canary || f.canary == "" {
				t.Errorf("presigned %s %s: the object goes to the canary only", c.Method, c.Path)
			}
		default:
			t.Errorf("%s %s with credential %q: the canary uses the admin and the tenant deployer only", c.Method, c.Path, c.Cred)
		}
	}
	if creates > 1 {
		t.Errorf("%d bucket creates: one canary per run", creates)
	}
	raw, _ := json.Marshal(obs)
	for _, c := range f.creds {
		if strings.Contains(string(raw), c.ClientSecret) {
			t.Errorf("the client secret of %s in the observations", c.Name)
		}
	}
}

// tenantCalls are the tenant deployer's write calls by op.
func tenantCalls(f *canaryAPI) map[string]canaryCall {
	out := map[string]canaryCall{}
	for _, c := range f.Calls() {
		if c.Cred == "tenant-deployer" && c.Method != http.MethodGet {
			out[c.Op] = c
		}
	}
	return out
}

// ---------------------------------------------------------------- collector: the KD-1 canary

// The collector creates the canary with the bootstrap authority in the state project (named
// <org>-bkt-canary-<run>, lowercase), puts one object in it, registers it for the trap once it
// exists and before the tenant's first call, then calls bulkDeleteObjects and DELETE on it through
// the management API as the tenant deployer. Each call observes `denied` (403), `allowed` (2xx
// that did what it asked) or `error` (anything else: never `denied`, so never a pass).
func TestObserveCanary(t *testing.T) {
	cases := []struct {
		name   string
		policy func([]string) []string
		setup  func(f *canaryAPI)
		want   map[string]string // subject -> observed
	}{
		{name: "kd1-reproduced", want: map[string]string{"bulkDeleteObjects": "allowed", "DELETE": "allowed"}},
		{name: "kd1-denied", policy: without("publicCloudProject:apiovh:region/storage/delete", "publicCloudProject:apiovh:region/storage/bulkDeleteObjects"),
			want: map[string]string{"bulkDeleteObjects": "denied", "DELETE": "denied"}},
		{name: "bulk-allowed-delete-denied", policy: without("publicCloudProject:apiovh:region/storage/delete"),
			want: map[string]string{"bulkDeleteObjects": "allowed", "DELETE": "denied"}},
		{name: "bulk-denied-delete-refused-not-empty", policy: without("publicCloudProject:apiovh:region/storage/bulkDeleteObjects"),
			want: map[string]string{"bulkDeleteObjects": "denied", "DELETE": "error"}},
		{name: "tenant-5xx", setup: func(f *canaryAPI) {
			f.answer["tenant-deployer POST bulk"] = http.StatusServiceUnavailable
			f.answer["tenant-deployer DELETE delete"] = http.StatusInternalServerError
		}, want: map[string]string{"bulkDeleteObjects": "error", "DELETE": "error"}},
		{name: "bulk-200-errors-only", setup: func(f *canaryAPI) { f.bulkErrorsOnly = true },
			want: map[string]string{"bulkDeleteObjects": "error", "DELETE": "error"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCanaryAPI(t, tc.policy)
			if tc.setup != nil {
				tc.setup(f)
			}
			obs := ObserveCanary(context.Background(), f.options())
			canaryJudge(t, f, obs)
			got := kd1(t, obs)
			for subj, want := range tc.want {
				if o, ok := got[subj]; !ok || o.Observed != want {
					t.Errorf("kd1-canary %s observed %q (present %t), want %q", subj, o.Observed, ok, want)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("subjects %v, want exactly %v", slices.Sorted(mapKeys(got)), slices.Sorted(mapKeys(tc.want)))
			}
			// The canary: created by the admin in the state project and region, named for the run.
			var created *canaryCall
			for _, c := range f.Calls() {
				if c.Op == "create" {
					c := c
					created = &c
					break
				}
			}
			if created == nil || created.Cred != "admin" || created.Status != http.StatusOK {
				t.Fatalf("canary create %+v, want POST …/cloud/project/%s/region/%s/storage by the admin (bootstrap authority)", created, canaryProject, canaryRegion)
			}
			if want := canaryOrg + "-bkt-canary-"; !strings.HasPrefix(f.canary, want) || !strings.Contains(f.canary, strings.ToLower(canaryRunID)) ||
				f.canary != strings.ToLower(f.canary) || len(f.canary) > 63 {
				t.Errorf("canary %q, want %s<run id, lowercase> (research R20), at most 63 characters", f.canary, want)
			}
			calls := tenantCalls(f)
			for _, op := range []string{"bulk", "delete"} {
				c, ok := calls[op]
				if !ok {
					t.Errorf("no tenant %s on the canary: the KD-1 calls are the tenant deployer's", op)
					continue
				}
				if c.Trap == nil || !slices.Contains(c.Trap, f.canary) {
					t.Errorf("tenant %s arrived with trap %v: the canary is registered for the trap before the tenant's calls", op, c.Trap)
				}
			}
			if c, ok := calls["bulk"]; ok && c.Objects < 1 {
				t.Errorf("tenant bulkDeleteObjects arrived with %d objects in the canary: it holds one object first (R20)", c.Objects)
			}
			if !slices.Contains(f.trap.Names(), f.canary) {
				t.Errorf("trap %v after the canary run, want it to hold %s", f.trap.Names(), f.canary)
			}
		})
	}
}

// A canary that could not be set up is never observed through the tenant: a taken name (409) is
// someone else's bucket, and the trap must not delete it; a failed create or object leaves both
// observations `error`. The tenant deployer makes no call.
func TestObserveCanaryNotSetUp(t *testing.T) {
	cases := []struct {
		name       string
		answer     string
		code       int
		registered bool // the trap holds the canary (it was created by this run)
	}{
		{name: "name-taken", answer: "admin POST create", code: http.StatusConflict},
		{name: "create-fails", answer: "admin POST create", code: http.StatusInternalServerError},
		{name: "presign-fails", answer: "admin POST presign", code: http.StatusInternalServerError, registered: true},
		{name: "upload-fails", answer: "presigned PUT upload", code: http.StatusInternalServerError, registered: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCanaryAPI(t, nil)
			f.answer[tc.answer] = tc.code
			obs := ObserveCanary(context.Background(), f.options())
			canaryJudge(t, f, obs)
			got := kd1(t, obs)
			for _, subj := range []string{"bulkDeleteObjects", "DELETE"} {
				if o := got[subj]; o.Observed != "error" {
					t.Errorf("kd1-canary %s observed %q, want error (the canary was not set up)", subj, o.Observed)
				}
			}
			for _, c := range f.Calls() {
				if c.Cred == "tenant-deployer" {
					t.Errorf("tenant %s %s although the canary was not set up", c.Method, c.Path)
				}
			}
			names := f.trap.Names()
			if tc.registered && len(names) != 1 {
				t.Errorf("trap %v, want the created canary registered", names)
			}
			if !tc.registered && len(names) != 0 {
				t.Errorf("trap %v: a canary this run did not create is never registered for deletion", names)
			}
		})
	}
}

// The trap deletes the canary with the admin credential if it still exists (its object first),
// is content when the tenant already deleted it (KD-1 reproduced), names the canary when its
// delete fails, and runs each cleanup once.
func TestObserveCanaryTrap(t *testing.T) {
	t.Run("canary-left", func(t *testing.T) {
		f := newCanaryAPI(t, without("publicCloudProject:apiovh:region/storage/delete", "publicCloudProject:apiovh:region/storage/bulkDeleteObjects"))
		ObserveCanary(context.Background(), f.options())
		if _, ok := f.buckets[f.canary]; !ok || f.canary == "" {
			t.Fatalf("no canary left after a denied run (canary %q)", f.canary)
		}
		from := len(f.Calls())
		if err := f.trap.Run(context.Background()); err != nil {
			t.Errorf("trap: %v", err)
		}
		if _, ok := f.buckets[f.canary]; ok {
			t.Errorf("canary %s still exists after the trap", f.canary)
		}
		for _, c := range f.Calls()[from:] {
			if c.Cred != "admin" && c.Cred != "presigned" {
				t.Errorf("trap %s %s as %s: the trap deletes with the bootstrap authority", c.Method, c.Path, c.Cred)
			}
			if c.Method != http.MethodGet && c.Bucket != f.canary {
				t.Errorf("trap %s %s: only the canary", c.Method, c.Path)
			}
		}
		from = len(f.Calls())
		if err := f.trap.Run(context.Background()); err != nil {
			t.Errorf("second trap run: %v", err)
		}
		for _, c := range f.Calls()[from:] {
			if c.Method != http.MethodGet {
				t.Errorf("second trap run %s %s: each cleanup runs once", c.Method, c.Path)
			}
		}
	})
	t.Run("canary-gone", func(t *testing.T) {
		f := newCanaryAPI(t, nil)
		ObserveCanary(context.Background(), f.options())
		if _, ok := f.buckets[f.canary]; ok {
			t.Fatalf("canary %s survived an allowed tenant DELETE (fake)", f.canary)
		}
		if err := f.trap.Run(context.Background()); err != nil {
			t.Errorf("trap on a canary the tenant deleted: %v, want nil (nothing left)", err)
		}
	})
	t.Run("delete-fails", func(t *testing.T) {
		f := newCanaryAPI(t, without("publicCloudProject:apiovh:region/storage/delete", "publicCloudProject:apiovh:region/storage/bulkDeleteObjects"))
		ObserveCanary(context.Background(), f.options())
		f.answer["admin DELETE delete"] = http.StatusInternalServerError
		err := f.trap.Run(context.Background())
		if err == nil || !strings.Contains(err.Error(), f.canary) {
			t.Errorf("trap error %v, want one naming %s (it still exists)", err, f.canary)
		}
	})
}

// Trap: every cleanup once; a failure names its cleanup and the others still run.
func TestObserveTrap(t *testing.T) {
	var tr Trap
	ran := map[string]int{}
	tr.Add("one", func(context.Context) error { ran["one"]++; return errors.New("boom") })
	tr.Add("two", func(context.Context) error { ran["two"]++; return nil })
	if got := tr.Names(); !slices.Equal(slices.Sorted(slices.Values(got)), []string{"one", "two"}) {
		t.Errorf("names %v, want one, two", got)
	}
	err := tr.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "one") {
		t.Errorf("trap error %v, want one naming the failed cleanup", err)
	}
	_ = tr.Run(context.Background())
	if ran["one"] != 1 || ran["two"] != 1 {
		t.Errorf("cleanups ran %v, want each once", ran)
	}
}

// ---------------------------------------------------------------- chain: reporting the L7 set

// chainObserver is the chain's fake collector: it logs an "observe" event, registers a cleanup
// that logs a "trap" event, and returns its observations.
type chainObserver struct {
	obs        []Observation
	err        error
	cleanupErr error
	log        string
	runs       []ObserveRun
	// stop, when set, is called once the cleanup is registered; the observer then waits for the
	// run to stop and returns its error (review r1: an interruption after the canary exists).
	stop func()
}

func (o *chainObserver) Observe(ctx context.Context, run ObserveRun) ([]Observation, error) {
	laneAppend(o.log, laneCall{Cmd: "observe"})
	o.runs = append(o.runs, run)
	if run.Trap != nil {
		run.Trap.Add("chain-canary", func(ctx context.Context) error {
			// Exit 1 marks a cleanup handed a context that is already done: a real cleanup's
			// requests would fail (review r2: the trap gets its own time, not the run's).
			exit := 0
			if ctx.Err() != nil {
				exit = 1
			}
			laneAppend(o.log, laneCall{Cmd: "trap", Stack: "chain-canary", Exit: exit})
			return o.cleanupErr
		})
	}
	if o.stop != nil {
		o.stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return nil, errors.New("observer not cancelled by the run's stop")
		}
	}
	return o.obs, o.err
}

// chainPassing is a complete L7 set in which every assertion holds and KD-1 did not reproduce.
func chainPassing() []Observation {
	return []Observation{
		{Assertion: "state-bucket-versioned", Subject: "lz-bkt-acct", Observed: "holds"},
		{Assertion: "state-bucket-versioned", Subject: "lz-bkt-demo", Observed: "holds"},
		{Assertion: "state-object-encrypted", Subject: "lz-bkt-demo/tenants/demo/dev/project/terraform.tfstate", Observed: "holds"},
		{Assertion: "state-lock-contention", Subject: "lz-bkt-demo/tenants/demo/dev/project/terraform.tfstate", Observed: "holds"},
		{Assertion: "bucket-tags", Subject: "lz-bkt-demo", Observed: "holds"},
		{Assertion: "project-tags", Subject: "urn:v1:eu:resource:publicCloudProject:" + laneProject, Observed: "holds"},
		{Assertion: "tenant-iam-write", Subject: "POST /v2/iam/policy", Observed: "denied"},
		{Assertion: "tenant-s3-read", Subject: "lz-bkt-acct", Observed: "denied"},
		{Assertion: "outputs-schema", Subject: "demo-dev-project", Observed: "holds"},
		{Assertion: "deployer-binding", Subject: "platform", Observed: "holds"},
		{Assertion: "deployer-binding", Subject: "tenant", Observed: "holds", Detail: "bound with " + laneTenantSecret},
		{Assertion: "kd1-canary", Subject: "bulkDeleteObjects", Observed: "denied"},
		{Assertion: "kd1-canary", Subject: "DELETE", Observed: "denied"},
	}
}

// chainWith returns chainPassing with f applied to each observation (nil drops it).
func chainWith(f func(o Observation) *Observation) []Observation {
	var out []Observation
	for _, o := range chainPassing() {
		if n := f(o); n != nil {
			out = append(out, *n)
		}
	}
	return out
}

type chainSummary struct {
	RunID           string   `json:"run_id"`
	Outcome         string   `json:"outcome"`
	KnownDeviations []string `json:"known_deviations"`
}

func readChainSummary(t *testing.T, runDir string) chainSummary {
	t.Helper()
	var s chainSummary
	raw, err := os.ReadFile(filepath.Join(runDir, "summary.json"))
	if err != nil {
		t.Errorf("no summary.json: %v", err)
		return s
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Errorf("summary.json: %v", err)
	}
	return s
}

// chainSummaryLine checks the line before the cost reminder.
func chainSummaryLine(t *testing.T, h *laneHarness, runID, outcome, deviations string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(h.term.String()), "\n")
	if want := fmt.Sprintf("LZ-LIVE summary %s %s known-deviations=%s", runID, outcome, deviations); len(lines) < 2 || lines[len(lines)-2] != want {
		t.Errorf("summary line %q, want %q", lines[max(0, len(lines)-2)], want)
	}
}

// The chain observes once every stack is applied, before the destroy-on-exit; it writes the set
// to observations.json (redacted, with the manifest's sandbox flag), judges it, and reports the
// known deviations observed in summary.json and the summary line. A known deviation does not
// change the exit code; a failed or missing assertion, or a failed collector, fails the run (exit
// 1) and the destroy-on-exit still runs; the trap runs once, before the final leftover listing,
// whichever way the run ends after the observer registered it.
func TestObserveChainReported(t *testing.T) {
	cases := []struct {
		name       string
		obs        []Observation
		obsErr     error
		cleanupErr error
		unflagged  bool // the manifest without spec.sandbox.shared_state_project
		interrupt  bool // SIGINT once the observer registered its cleanup
		exit       int
		outcome    string
		deviations []string
		reason     string // in the error
	}{
		{name: "all-pass", obs: chainPassing(), outcome: "pass"},
		{name: "kd1-shared-state-project", obs: chainWith(func(o Observation) *Observation {
			if o.Assertion == "kd1-canary" {
				o.Observed = "allowed"
			}
			return &o
		}), outcome: "pass", deviations: []string{"KD-1"}},
		{name: "kd1-without-flag", unflagged: true, obs: chainWith(func(o Observation) *Observation {
			if o.Assertion == "kd1-canary" {
				o.Observed = "allowed"
			}
			return &o
		}), exit: 1, outcome: "fail", reason: "kd1-canary"},
		{name: "assertion-fails", obs: chainWith(func(o Observation) *Observation {
			if o.Assertion == "tenant-iam-write" {
				o.Observed = "allowed"
			}
			return &o
		}), exit: 1, outcome: "fail", reason: "tenant-iam-write"},
		{name: "assertion-missing", obs: chainWith(func(o Observation) *Observation {
			if o.Assertion == "state-lock-contention" {
				return nil
			}
			return &o
		}), exit: 1, outcome: "fail", reason: "state-lock-contention"},
		{name: "kd1-with-failure", obs: chainWith(func(o Observation) *Observation {
			if o.Assertion == "kd1-canary" || o.Assertion == "tenant-iam-write" {
				o.Observed = "allowed"
			}
			return &o
		}), exit: 1, outcome: "fail", deviations: []string{"KD-1"}, reason: "tenant-iam-write"},
		{name: "interrupted-after-canary", interrupt: true, exit: 1, outcome: "fail", reason: "interrupt"},
		{name: "collector-fails", obsErr: errors.New("collector: listing the state buckets failed"), exit: 1, outcome: "fail", reason: "collector"},
		{name: "trap-fails", obs: chainPassing(), cleanupErr: errors.New("canary still exists"), exit: 1, outcome: "fail", reason: "chain-canary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newChainHarness(t)
			if tc.unflagged {
				h.m.SharedStateProject = false
			}
			o := &chainObserver{obs: tc.obs, err: tc.obsErr, cleanupErr: tc.cleanupErr}
			var sig os.Signal
			if tc.interrupt {
				sig = syscall.SIGINT
			}
			r := h.chain(chainExec{observer: o, signalAtObserve: sig})
			laneExit(t, r.laneRun, tc.exit)
			if tc.reason != "" && (r.err == nil || !strings.Contains(r.err.Error(), tc.reason)) {
				t.Errorf("err %v, want one naming %s", r.err, tc.reason)
			}
			chainDestroyedAfter(t, h, r, chainDestroyOrder, tc.outcome)
			if len(o.runs) != 1 {
				t.Fatalf("observer called %d times, want once", len(o.runs))
			}
			if run := o.runs[0]; run.RunID != r.runID || run.RunDir != r.runDir || run.Manifest == nil || run.Trap == nil {
				t.Errorf("observer got run %q dir %q manifest %t trap %t, want the run's id, directory, manifest and trap", run.RunID, run.RunDir, run.Manifest != nil, run.Trap != nil)
			}
			obsAt := r.index(false, func(c laneCall) bool { return c.Cmd == "observe" })
			lastForward := r.index(true, func(c laneCall) bool { return c.Cmd == "apply" && !c.Destroy })
			firstDestroy := r.index(false, func(c laneCall) bool { return c.Destroy })
			if obsAt < lastForward || firstDestroy >= 0 && obsAt > firstDestroy {
				t.Errorf("observed at event %d (last apply %d, first destroy %d): after every apply, before the destroy-on-exit; events %v", obsAt, lastForward, firstDestroy, laneEvents(r.calls))
			}
			traps := r.events("trap")
			trapAt := r.index(false, func(c laneCall) bool { return c.Cmd == "trap" })
			finalList := -1
			for i, c := range r.calls {
				if i > obsAt && c.Cmd == "list" {
					finalList = i
					break
				}
			}
			if trapAt >= 0 && r.calls[trapAt].Exit != 0 {
				t.Errorf("trap cleanup ran with a context already done: the destroy-on-exit's own time, not the run's")
			}
			if len(traps) != 1 || trapAt < obsAt || finalList >= 0 && trapAt > finalList {
				t.Errorf("trap ran %d times at event %d (observe %d, first final listing %d): once, after the observer registered it and before the leftover check lists",
					len(traps), trapAt, obsAt, finalList)
			}
			// observations.json: the set as observed, with the run id and the manifest's flag; each
			// observation kept as given, the seeded secret redacted from its detail (review r1).
			var set ObservationSet
			raw, err := os.ReadFile(filepath.Join(r.runDir, "observations.json"))
			if tc.obsErr == nil && !tc.interrupt {
				if err != nil || json.Unmarshal(raw, &set) != nil {
					t.Errorf("observations.json unreadable (%v)", err)
				} else if set.RunID != r.runID || set.SharedStateProject != !tc.unflagged || len(set.Observations) != len(tc.obs) {
					t.Errorf("observations.json run %q flag %t %d observations, want %q %t %d", set.RunID, set.SharedStateProject, len(set.Observations), r.runID, !tc.unflagged, len(tc.obs))
				} else {
					for i, want := range tc.obs {
						got := set.Observations[i]
						detailOK := got.Detail == want.Detail
						if strings.Contains(want.Detail, laneTenantSecret) {
							prefix, _, _ := strings.Cut(want.Detail, laneTenantSecret)
							detailOK = strings.HasPrefix(got.Detail, prefix) && !strings.Contains(got.Detail, laneTenantSecret) && got.Detail != prefix
						}
						if got.Assertion != want.Assertion || got.Subject != want.Subject || got.Observed != want.Observed || !detailOK {
							t.Errorf("observations.json[%d] %+v, want %+v (detail redacted, not dropped)", i, got, want)
						}
					}
				}
			}
			s := readChainSummary(t, r.runDir)
			if s.Outcome != tc.outcome || !slices.Equal(s.KnownDeviations, tc.deviations) && !(len(s.KnownDeviations) == 0 && len(tc.deviations) == 0) {
				t.Errorf("summary.json outcome %q known_deviations %v, want %q %v", s.Outcome, s.KnownDeviations, tc.outcome, tc.deviations)
			}
			line := "none"
			if len(tc.deviations) > 0 {
				line = strings.Join(tc.deviations, ",")
			}
			chainSummaryLine(t, h, r.runID, tc.outcome, line)
		})
	}
}

// A chain whose apply failed observes nothing (the slice is incomplete; the run already fails)
// and reports no deviation.
func TestObserveChainNotAfterFailure(t *testing.T) {
	h := newChainHarness(t)
	h.setApplyExit("demo-dev-gra11-runtime", 1)
	o := &chainObserver{obs: chainWith(func(o Observation) *Observation {
		if o.Assertion == "kd1-canary" {
			o.Observed = "allowed"
		}
		return &o
	})}
	r := h.chain(chainExec{observer: o})
	laneExit(t, r.laneRun, 1)
	if len(o.runs) != 0 {
		t.Errorf("observer called %d times after an apply failed", len(o.runs))
	}
	if s := readChainSummary(t, r.runDir); s.Outcome != "fail" || len(s.KnownDeviations) != 0 {
		t.Errorf("summary.json %+v, want fail without deviations", s)
	}
	chainSummaryLine(t, h, r.runID, "fail", "none")
}
