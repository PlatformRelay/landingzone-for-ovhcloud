package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// T100: the probe check's OVHcloud API client (apicheck.go). Its gate admits five named operations,
// each with its method and path fixed and its parameters checked by shape before any request; of
// an answer it keeps the status, the IAM actions a refusal names, a created policy's uuid and a
// policy listing's (id, name) pairs; errors carry no answer text, token or secret.

const (
	apiTestClient  = "EU.apicheckclient"
	apiTestSecret  = "apicheck-secret-6d5c4b3a2f1e"
	apiTestToken   = "apicheck-token-0a1b2c3d4e5f"
	apiTestProject = "0123456789abcdef0123456789abcdef"
	apiTestPolicy  = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
)

type apiSeen struct {
	Method, Path, Query, Auth, ContentType string
	Body                                   []byte
}

// fakeCheckAPI is the OVHcloud API for the checker: the token endpoint answers the test client, every
// other request is recorded and answered by answer (nil: 200 `{}`).
type fakeCheckAPI struct {
	mu     sync.Mutex
	seen   []apiSeen
	tokens int
	answer func(rw http.ResponseWriter, r *http.Request)
	srv    *httptest.Server
}

func newFakeCheckAPI(t *testing.T) *fakeCheckAPI {
	t.Helper()
	f := &fakeCheckAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/token" {
			f.mu.Lock()
			f.tokens++
			f.mu.Unlock()
			q, _ := urlValues(string(body))
			if q["client_id"] != apiTestClient || q["client_secret"] != apiTestSecret {
				rw.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(rw, `{"access_token":%q}`, apiTestToken)
			return
		}
		f.mu.Lock()
		f.seen = append(f.seen, apiSeen{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
		answer := f.answer
		f.mu.Unlock()
		if answer == nil {
			fmt.Fprint(rw, "{}")
			return
		}
		answer(rw, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func urlValues(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, kv := range strings.Split(s, "&") {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out, nil
}

func (f *fakeCheckAPI) api() API {
	return API{TokenURL: f.srv.URL + "/token", BaseURL: f.srv.URL + "/v1"}
}

func (f *fakeCheckAPI) requests() []apiSeen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

func newTestAPIChecker(t *testing.T, f *fakeCheckAPI) *APIChecker {
	t.Helper()
	c, err := NewAPIChecker(context.Background(), f.api(), Credential{Endpoint: "ovh-eu", ClientID: apiTestClient, ClientSecret: apiTestSecret})
	if err != nil {
		t.Fatalf("NewAPIChecker: %v", err)
	}
	return c
}

// apiNoLeak: an error names no answer text, token, secret or URL.
func apiNoLeak(t *testing.T, msg string) {
	t.Helper()
	for _, bad := range []string{"SERVERTEXT", apiTestToken, apiTestSecret, "http://", "127.0.0.1"} {
		if strings.Contains(msg, bad) {
			t.Errorf("error %q carries %q", msg, bad)
		}
	}
}

// TestAPICheckerOperations: each operation goes out with its method and path (v1 or v2), the
// identity's bearer token, a body only for CreatePolicy (JSON content type).
func TestAPICheckerOperations(t *testing.T) {
	t.Parallel()
	policy := []byte(`{"name":"lzprobe-p9-write-x"}`)
	cases := []struct {
		call         APICall
		method, path string
	}{
		{APICall{Op: APIGetStorage, Project: apiTestProject, Region: "GRA", Name: "lzprobe-p25a-20261012t072729z-0646"}, "GET", "/v1/cloud/project/" + apiTestProject + "/region/GRA/storage/lzprobe-p25a-20261012t072729z-0646"},
		{APICall{Op: APIGetMe}, "GET", "/v1/me"},
		{APICall{Op: APICreatePolicy, Body: policy}, "POST", "/v2/iam/policy"},
		{APICall{Op: APIListPolicies, Name: "lzprobe-p9-write-a"}, "GET", "/v2/iam/policy"},
		{APICall{Op: APIDeletePolicy, ID: apiTestPolicy}, "DELETE", "/v2/iam/policy/" + apiTestPolicy},
	}
	for _, tc := range cases {
		t.Run(string(tc.call.Op), func(t *testing.T) {
			f := newFakeCheckAPI(t)
			if tc.call.Op == APIListPolicies {
				f.answer = func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, "[]") }
			}
			got, err := newTestAPIChecker(t, f).Do(context.Background(), tc.call)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			if got.Status != http.StatusOK {
				t.Errorf("status %d, want 200", got.Status)
			}
			reqs := f.requests()
			if len(reqs) != 1 {
				t.Fatalf("%d requests, want 1", len(reqs))
			}
			r := reqs[0]
			if r.Method != tc.method || r.Path != tc.path || r.Auth != "Bearer "+apiTestToken {
				t.Errorf("sent %s %s (auth %q), want %s %s with the bearer token", r.Method, r.Path, r.Auth, tc.method, tc.path)
			}
			if tc.call.Op == APICreatePolicy {
				if string(r.Body) != string(policy) || r.ContentType != "application/json" {
					t.Errorf("body %q (%s), want the policy as JSON", r.Body, r.ContentType)
				}
			} else if len(r.Body) != 0 {
				t.Errorf("body %q on %s", r.Body, tc.call.Op)
			}
		})
	}
}

// TestAPICheckerGate: anything outside the five operations, or an operation with a part it does
// not take or a part of the wrong shape, is refused before any request.
func TestAPICheckerGate(t *testing.T) {
	t.Parallel()
	ok := APICall{Op: APIGetStorage, Project: apiTestProject, Region: "GRA", Name: "lzprobe-p25a-x"}
	with := func(edit func(*APICall)) APICall { c := ok; edit(&c); return c }
	refused := map[string]APICall{
		"unknown op":              {Op: "DeleteStorage", Project: apiTestProject, Region: "GRA", Name: "b-1"},
		"empty op":                {},
		"project not hex":         with(func(c *APICall) { c.Project = "../../me" }),
		"project short":           with(func(c *APICall) { c.Project = "0123" }),
		"region lower":            with(func(c *APICall) { c.Region = "gra" }),
		"region with slash":       with(func(c *APICall) { c.Region = "GRA/../x" }),
		"name not a bucket":       with(func(c *APICall) { c.Name = "A/../../me" }),
		"name empty":              with(func(c *APICall) { c.Name = "" }),
		"storage with id":         with(func(c *APICall) { c.ID = apiTestPolicy }),
		"storage with body":       with(func(c *APICall) { c.Body = []byte("{}") }),
		"me with project":         {Op: APIGetMe, Project: apiTestProject},
		"me with name":            {Op: APIGetMe, Name: "b-1"},
		"me with body":            {Op: APIGetMe, Body: []byte("{}")},
		"create without body":     {Op: APICreatePolicy},
		"create with bad JSON":    {Op: APICreatePolicy, Body: []byte("{")},
		"create with id":          {Op: APICreatePolicy, Body: []byte("{}"), ID: apiTestPolicy},
		"create too big":          {Op: APICreatePolicy, Body: []byte(`"` + strings.Repeat("x", 64<<10) + `"`)},
		"list with id":            {Op: APIListPolicies, Name: "lzprobe-p9-write-a", ID: apiTestPolicy},
		"list without name":       {Op: APIListPolicies},
		"list with a spaced name": {Op: APIListPolicies, Name: "lzprobe p9"},
		"list with body":          {Op: APIListPolicies, Name: "lzprobe-p9-write-a", Body: []byte("{}")},
		// T101 review r2: the ids a listing also keeps are uuids, and only a listing takes them.
		"list keeping a non-uuid": {Op: APIListPolicies, Name: "lzprobe-p9-write-a", Keep: []string{apiTestPolicy, "../x"}},
		"me keeping an id":        {Op: APIGetMe, Keep: []string{apiTestPolicy}},
		"delete keeping an id":    {Op: APIDeletePolicy, ID: apiTestPolicy, Keep: []string{apiTestPolicy}},
		"delete without id":       {Op: APIDeletePolicy},
		"delete with a path id":   {Op: APIDeletePolicy, ID: "../policy"},
		"delete with upper uuid":  {Op: APIDeletePolicy, ID: strings.ToUpper(apiTestPolicy)},
		"delete with body":        {Op: APIDeletePolicy, ID: apiTestPolicy, Body: []byte("{}")},
		"delete with project":     {Op: APIDeletePolicy, ID: apiTestPolicy, Project: apiTestProject},
		"storage without project": with(func(c *APICall) { c.Project = "" }),
	}
	for name, call := range refused {
		t.Run(name, func(t *testing.T) {
			f := newFakeCheckAPI(t)
			if _, err := newTestAPIChecker(t, f).Do(context.Background(), call); err == nil {
				t.Error("no refusal")
			}
			if n := len(f.requests()); n != 0 {
				t.Errorf("%d requests before the refusal, want 0", n)
			}
		})
	}
}

// TestAPICheckerAnswers: any status is an answer (no redirect followed); a refusal keeps only the
// action-shaped tokens it names; a created policy keeps its uuid; a listing reads every page and
// keeps (id, name); errors (oversized, unexpected listing, too many pages, transport) carry no
// answer text.
func TestAPICheckerAnswers(t *testing.T) {
	t.Parallel()
	echo := "SERVERTEXT " + apiTestToken + " " + apiTestSecret
	forbidden := func(message, byIAM string) string {
		b, _ := json.Marshal(map[string]any{"class": "Client::Forbidden", "message": message,
			"details": map[string]string{"unauthorizedActionsByAuthentication": "", "unauthorizedActionsByIAM": byIAM}})
		return string(b)
	}
	answers := []struct {
		name, body string
		status     int
		actions    []string
	}{
		// kb manage-and-operate/iam/troubleshooting.mdx:21-31.
		{"details", forbidden("User not granted for this request "+echo, "vps:apiovh:reboot"), 403, []string{"vps:apiovh:reboot"}},
		// T010 rows 10 and 14: the message names the action.
		{"message", `{"class":"Client::Forbidden","message":"missing IAM permissions: publicCloudProject:apiovh:region/storage/get"}`, 403, []string{"publicCloudProject:apiovh:region/storage/get"}},
		{"both-deduplicated", forbidden("missing IAM permissions: account:apiovh:me/get", "account:apiovh:me/get"), 403, []string{"account:apiovh:me/get"}},
		{"several", forbidden("missing IAM permissions: account:apiovh:iam/policy/create, account:apiovh:me/get", ""), 403, []string{"account:apiovh:iam/policy/create", "account:apiovh:me/get"}},
		{"echo-dropped", forbidden(echo, echo+" x:apiovh:"+apiTestToken), 403, nil},
		{"no-json", echo, 403, nil},
		{"empty", "", 403, nil},
		{"not-found", `{"class":"Client::NotFound","message":"` + echo + `"}`, 404, nil},
		{"redirect", "", 302, nil},
	}
	for _, a := range answers {
		t.Run(a.name, func(t *testing.T) {
			f := newFakeCheckAPI(t)
			f.answer = func(rw http.ResponseWriter, r *http.Request) {
				if a.status == 302 {
					rw.Header().Set("Location", "https://elsewhere.example/v1/me")
				}
				rw.WriteHeader(a.status)
				fmt.Fprint(rw, a.body)
			}
			got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIGetMe})
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			if got.Status != a.status || !slices.Equal(got.Actions, a.actions) || got.ID != "" || got.Policies != nil {
				t.Errorf("answer %+v, want status %d, actions %v, nothing else", got, a.status, a.actions)
			}
			if n := len(f.requests()); n != 1 {
				t.Errorf("%d requests, want 1 (no redirect followed)", n)
			}
		})
	}

	t.Run("created-id", func(t *testing.T) {
		for body, want := range map[string]string{
			`{"id":"` + apiTestPolicy + `","name":"x"}`: apiTestPolicy,
			`{"id":"` + echo + `"}`:                     "",
			`{}`:                                        "",
			echo:                                        "",
		} {
			f := newFakeCheckAPI(t)
			f.answer = func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, body) }
			got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APICreatePolicy, Body: []byte("{}")})
			if err != nil || got.Status != 200 || got.ID != want {
				t.Errorf("create answered %q: %+v, %v; want id %q", body, got, err, want)
			}
		}
	})

	t.Run("listing-pages", func(t *testing.T) {
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Pagination-Cursor") == "" {
				rw.Header().Set("X-Pagination-Cursor-Next", "c2")
				fmt.Fprintf(rw, `[{"id":%q,"name":"lzprobe-p9-write-a","identities":["x"]}]`, apiTestPolicy)
				return
			}
			if r.Header.Get("X-Pagination-Cursor") != "c2" {
				rw.WriteHeader(400)
				return
			}
			// Review r1: foreign policies of any shape (a name with a space or non-ASCII, a
			// non-uuid id; iam.json puts no pattern on name) are skipped, never an error.
			fmt.Fprint(rw, `[{"id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8","name":"other"},{"id":"not-a-uuid","name":"My policy é"},`+
				`{"id":"6ba7b810-9dad-11d1-80b4-00c04fd430c9","name":"lzprobe-p9-write-a"},{"id":"6ba7b810-9dad-11d1-80b4-00c04fd430ca","name":"lzprobe-p9-write-ab"}]`)
		}
		got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListPolicies, Name: "lzprobe-p9-write-a"})
		want := []APIPolicy{{apiTestPolicy, "lzprobe-p9-write-a"}, {"6ba7b810-9dad-11d1-80b4-00c04fd430c9", "lzprobe-p9-write-a"}}
		if err != nil || got.Status != 200 || !slices.Equal(got.Policies, want) {
			t.Errorf("listing %+v, %v; want the named policies of both pages %v", got, err, want)
		}
	})

	// T101 review r2: a listing also keeps the policies whose id it was asked to keep, whatever
	// their name (a name OVHcloud normalised), and still skips the others.
	t.Run("listing-keeps-ids", func(t *testing.T) {
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(rw, `[{"id":%q,"name":"renamed"},{"id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8","name":"other"},{"id":"6ba7b810-9dad-11d1-80b4-00c04fd430c9","name":"lzprobe-p9-write-a"}]`, apiTestPolicy)
		}
		got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListPolicies, Name: "lzprobe-p9-write-a", Keep: []string{apiTestPolicy}})
		want := []APIPolicy{{apiTestPolicy, "renamed"}, {"6ba7b810-9dad-11d1-80b4-00c04fd430c9", "lzprobe-p9-write-a"}}
		if err != nil || !slices.Equal(got.Policies, want) {
			t.Errorf("listing %+v, %v; want the kept id and the named policy %v", got, err, want)
		}
	})

	// T113 (T101 gap 6): a listing keeps each kept policy's createdAt, read from the v2 policy listing
	// as the sandbox answered it (T087's captured listing, run 20261007T133803Z-aeee; iam.json
	// iam.policy.Response.createdAt); one without a readable createdAt has none.
	t.Run("listing-created-at", func(t *testing.T) {
		captured, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "ovhcloud", "captured", "20261007T133803Z-aeee", "listings", "ovh_iam_policy.json"))
		if err != nil {
			t.Fatal(err)
		}
		var entries []map[string]any
		if err := json.Unmarshal(captured, &entries); err != nil || len(entries) == 0 {
			t.Fatalf("captured listing: %v", err)
		}
		// The captured admin policy, then two of the asked name: no createdAt, an unreadable one.
		page, _ := json.Marshal(append(entries[:1:1],
			map[string]any{"id": apiTestPolicy, "name": "lz-sandbox-admin"},
			map[string]any{"id": "6ba7b810-9dad-11d1-80b4-00c04fd430c9", "name": "lz-sandbox-admin", "createdAt": "yesterday"}))
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) { rw.Write(page) }
		got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListPolicies, Name: "lz-sandbox-admin"})
		want := map[string]time.Time{"00000000-0000-4000-8000-000000000001": time.Date(2026, 10, 1, 20, 2, 44, 305461000, time.UTC)}
		if err != nil || len(got.Policies) != 3 || len(got.CreatedAt) != 1 || !got.CreatedAt["00000000-0000-4000-8000-000000000001"].Equal(want["00000000-0000-4000-8000-000000000001"]) {
			t.Errorf("listing %+v, %v; want three policies and the captured one's createdAt %v only", got, err, want)
		}
	})

	t.Run("listing-refused", func(t *testing.T) {
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(403)
			fmt.Fprint(rw, forbidden("missing IAM permissions: account:apiovh:iam/policy/get", ""))
		}
		got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListPolicies, Name: "lzprobe-p9-write-a"})
		if err != nil || got.Status != 403 || got.Policies != nil || !slices.Equal(got.Actions, []string{"account:apiovh:iam/policy/get"}) {
			t.Errorf("refused listing %+v, %v; want status 403, the action, no policies", got, err)
		}
	})

	faults := map[string]func(rw http.ResponseWriter, r *http.Request){
		"oversized": func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(403)
			fmt.Fprint(rw, strings.Repeat("x", maxBody+1))
		},
		"listing-not-a-list": func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, `{"message":"`+echo+`"}`) },
		// The named policy without a usable id cannot be deleted: an error, not a skip.
		"listing-bad-id": func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprint(rw, `[{"id":"`+echo+`","name":"lzprobe-p9-write-a"}]`)
		},
		"listing-name-not-a-string": func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, `[{"id":"x","name":1}]`) },
		// Review r1: `null` is no listing (two of them read as empty would pass the undo).
		"listing-null":         func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, "null") },
		"listing-null-entries": func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, "[null]") },
		"listing-endless": func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("X-Pagination-Cursor-Next", "again")
			fmt.Fprint(rw, "[]")
		},
		"reset": func(rw http.ResponseWriter, r *http.Request) {
			if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
		},
	}
	for name, fault := range faults {
		t.Run(name, func(t *testing.T) {
			f := newFakeCheckAPI(t)
			f.answer = fault
			_, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListPolicies, Name: "lzprobe-p9-write-a"})
			if err == nil {
				t.Fatal("no error")
			}
			apiNoLeak(t, err.Error())
			if !strings.Contains(err.Error(), "/iam/policy") {
				t.Errorf("error %q does not name the request", err)
			}
		})
	}
}

// TestAPICheckerToken: a refused credential is an error naming the token endpoint, never a value;
// the secret goes only to the token endpoint, the token only in Authorization.
func TestAPICheckerToken(t *testing.T) {
	t.Parallel()
	f := newFakeCheckAPI(t)
	_, err := NewAPIChecker(context.Background(), f.api(), Credential{Endpoint: "ovh-eu", ClientID: apiTestClient, ClientSecret: "wrong-" + apiTestSecret})
	if err == nil || !strings.Contains(err.Error(), "token endpoint") {
		t.Errorf("refused credential: %v, want an error naming the token endpoint", err)
	}
	apiNoLeak(t, fmt.Sprint(err))
	c := newTestAPIChecker(t, f)
	for _, call := range []APICall{{Op: APIGetMe}, {Op: APICreatePolicy, Body: []byte(`{"name":"x"}`)}, {Op: APIDeletePolicy, ID: apiTestPolicy}} {
		if _, err := c.Do(context.Background(), call); err != nil {
			t.Fatalf("%s: %v", call.Op, err)
		}
	}
	for _, r := range f.requests() {
		if strings.Contains(r.Path+r.Query+string(r.Body), apiTestSecret) || strings.Contains(r.Path+r.Query+string(r.Body), apiTestToken) {
			t.Errorf("%s %s carries the secret or the token outside Authorization", r.Method, r.Path)
		}
	}
	if f.tokens != 2 {
		t.Errorf("%d token requests, want one per checker (2)", f.tokens)
	}
}

// TestAPICheckerSweepOps (spec 003 T030): the seven operations a probe's Sweep sends, each with its
// method and v1 path (kb/api/v1/cloud.json), a body only for BulkDeleteObjects
// (cloud.StorageObjectDeleteBulk); the gate refuses wrong parts before any request; a listing keeps
// only names, object keys and version ids, or user ids and descriptions, and refuses another shape;
// a ListBuckets 404 is read against the region's services.
func TestAPICheckerSweepOps(t *testing.T) {
	t.Parallel()
	const bucket = "lzprobe-swift-reach-20261016t101500z-1a2b-a"
	base := "/v1/cloud/project/" + apiTestProject
	objs := []APIObject{{Key: "lzprobe-swift-reach/seed", VersionID: "null"}, {Key: "lzprobe-swift-reach/write-a-other"}}
	t.Run("operations", func(t *testing.T) {
		for _, tc := range []struct {
			call                APICall
			method, path, query string
			answer              string
		}{
			{APICall{Op: APIListRegions, Project: apiTestProject}, "GET", base + "/region", "", `["GRA","RBX-ARCHIVE"]`},
			{APICall{Op: APIListBuckets, Project: apiTestProject, Region: "RBX-ARCHIVE"}, "GET", base + "/region/RBX-ARCHIVE/storage", "", `[{"name":"b-1","region":"RBX-ARCHIVE"}]`},
			{APICall{Op: APIListObjects, Project: apiTestProject, Region: "GRA", Name: bucket}, "GET", base + "/region/GRA/storage/" + bucket + "/object", "withVersions=true", `[{"key":"k","versionId":"v1","isLatest":true}]`},
			{APICall{Op: APIBulkDeleteObjects, Project: apiTestProject, Region: "GRA", Name: bucket, Objects: objs}, "POST", base + "/region/GRA/storage/" + bucket + "/bulkDeleteObjects", "", `{"deleted":[],"errors":[]}`},
			{APICall{Op: APIDeleteBucket, Project: apiTestProject, Region: "GRA", Name: bucket}, "DELETE", base + "/region/GRA/storage/" + bucket, "", `null`},
			{APICall{Op: APIListUsers, Project: apiTestProject}, "GET", base + "/user", "", `[{"id":101,"description":"lzprobe-x","username":"user-1"}]`},
			{APICall{Op: APIDeleteUser, Project: apiTestProject, ID: "101"}, "DELETE", base + "/user/101", "", `null`},
		} {
			t.Run(string(tc.call.Op), func(t *testing.T) {
				f := newFakeCheckAPI(t)
				f.answer = func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, tc.answer) }
				got, err := newTestAPIChecker(t, f).Do(context.Background(), tc.call)
				if err != nil || got.Status != http.StatusOK {
					t.Fatalf("Do = %+v, %v", got, err)
				}
				reqs := f.requests()
				if len(reqs) != 1 {
					t.Fatalf("%d requests, want 1", len(reqs))
				}
				r := reqs[0]
				if r.Method != tc.method || r.Path != tc.path || r.Query != tc.query || r.Auth != "Bearer "+apiTestToken {
					t.Errorf("sent %s %s?%s (auth %q), want %s %s?%s with the bearer token", r.Method, r.Path, r.Query, r.Auth, tc.method, tc.path, tc.query)
				}
				switch tc.call.Op {
				case APIBulkDeleteObjects:
					if string(r.Body) != `{"objects":[{"key":"lzprobe-swift-reach/seed","versionId":"null"},{"key":"lzprobe-swift-reach/write-a-other"}]}` || r.ContentType != "application/json" {
						t.Errorf("body %s (%s), want the objects as cloud.StorageObjectDeleteBulk", r.Body, r.ContentType)
					}
				default:
					if len(r.Body) != 0 {
						t.Errorf("body %q on %s", r.Body, tc.call.Op)
					}
				}
				want := map[APIOp]string{
					APIListRegions: "[GRA RBX-ARCHIVE]", APIListBuckets: "[b-1]", APIListObjects: "[{k v1}]", APIListUsers: "[{101 lzprobe-x}]",
				}[tc.call.Op]
				var kept string
				switch {
				case got.Names != nil:
					kept = fmt.Sprint(got.Names)
				case got.Objects != nil:
					kept = fmt.Sprint(got.Objects)
				case got.Users != nil:
					kept = fmt.Sprint(got.Users)
				}
				if kept != want {
					t.Errorf("kept %q, want %q", kept, want)
				}
			})
		}
	})

	t.Run("gate", func(t *testing.T) {
		long := make([]APIObject, maxBulk+1)
		for i := range long {
			long[i] = APIObject{Key: fmt.Sprintf("k%d", i)}
		}
		for name, call := range map[string]APICall{
			"regions without project":   {Op: APIListRegions},
			"regions with region":       {Op: APIListRegions, Project: apiTestProject, Region: "GRA"},
			"buckets lower region":      {Op: APIListBuckets, Project: apiTestProject, Region: "gra"},
			"buckets region path":       {Op: APIListBuckets, Project: apiTestProject, Region: "GRA/../x"},
			"buckets with name":         {Op: APIListBuckets, Project: apiTestProject, Region: "GRA", Name: bucket},
			"objects bad bucket":        {Op: APIListObjects, Project: apiTestProject, Region: "GRA", Name: "A/../me"},
			"objects without region":    {Op: APIListObjects, Project: apiTestProject, Name: bucket},
			"bulk without objects":      {Op: APIBulkDeleteObjects, Project: apiTestProject, Region: "GRA", Name: bucket},
			"bulk empty objects":        {Op: APIBulkDeleteObjects, Project: apiTestProject, Region: "GRA", Name: bucket, Objects: []APIObject{}},
			"bulk too many":             {Op: APIBulkDeleteObjects, Project: apiTestProject, Region: "GRA", Name: bucket, Objects: long},
			"bulk empty key":            {Op: APIBulkDeleteObjects, Project: apiTestProject, Region: "GRA", Name: bucket, Objects: []APIObject{{Key: ""}}},
			"bulk control key":          {Op: APIBulkDeleteObjects, Project: apiTestProject, Region: "GRA", Name: bucket, Objects: []APIObject{{Key: "a\nb"}}},
			"bulk spaced version":       {Op: APIBulkDeleteObjects, Project: apiTestProject, Region: "GRA", Name: bucket, Objects: []APIObject{{Key: "k", VersionID: "a b"}}},
			"delete bucket with id":     {Op: APIDeleteBucket, Project: apiTestProject, Region: "GRA", Name: bucket, ID: "1"},
			"delete bucket objects":     {Op: APIDeleteBucket, Project: apiTestProject, Region: "GRA", Name: bucket, Objects: objs},
			"users with name":           {Op: APIListUsers, Project: apiTestProject, Name: bucket},
			"delete user uuid":          {Op: APIDeleteUser, Project: apiTestProject, ID: apiTestPolicy},
			"delete user path":          {Op: APIDeleteUser, Project: apiTestProject, ID: "1/../2"},
			"delete user no project":    {Op: APIDeleteUser, ID: "101"},
			"objects on a storage read": {Op: APIGetStorage, Project: apiTestProject, Region: "GRA", Name: bucket, Objects: objs},
			"storage dashed region":     {Op: APIGetStorage, Project: apiTestProject, Region: "EU-WEST-PAR", Name: bucket},
		} {
			t.Run(name, func(t *testing.T) {
				f := newFakeCheckAPI(t)
				if _, err := newTestAPIChecker(t, f).Do(context.Background(), call); err == nil {
					t.Error("no refusal")
				}
				if n := len(f.requests()); n != 0 {
					t.Errorf("%d requests before the refusal, want 0", n)
				}
			})
		}
	})

	t.Run("bad-listings", func(t *testing.T) {
		echo := "SERVERTEXT " + apiTestToken + " " + apiTestSecret
		for _, tc := range []struct {
			call APICall
			body string
		}{
			{APICall{Op: APIListRegions, Project: apiTestProject}, `{"message":"` + echo + `"}`},
			{APICall{Op: APIListRegions, Project: apiTestProject}, `["GRA",""]`},
			{APICall{Op: APIListRegions, Project: apiTestProject}, `["GRA","` + echo + `"]`},
			{APICall{Op: APIListRegions, Project: apiTestProject}, `["gra"]`},
			{APICall{Op: APIListRegions, Project: apiTestProject}, `null`},
			{APICall{Op: APIListBuckets, Project: apiTestProject, Region: "GRA"}, `[{"name":""}]`},
			{APICall{Op: APIListBuckets, Project: apiTestProject, Region: "GRA"}, `[null]`},
			{APICall{Op: APIListObjects, Project: apiTestProject, Region: "GRA", Name: bucket}, `[{"versionId":"v"}]`},
			{APICall{Op: APIListObjects, Project: apiTestProject, Region: "GRA", Name: bucket}, `[{"key":"a` + "\\u0000" + `b"}]`},
			{APICall{Op: APIListUsers, Project: apiTestProject}, `[{"id":"../1","description":"x"}]`},
			{APICall{Op: APIListUsers, Project: apiTestProject}, `[{"description":"x"}]`},
			{APICall{Op: APIListUsers, Project: apiTestProject}, echo},
		} {
			f := newFakeCheckAPI(t)
			f.answer = func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, tc.body) }
			got, err := newTestAPIChecker(t, f).Do(context.Background(), tc.call)
			if err == nil {
				t.Errorf("%s %s: accepted as %+v", tc.call.Op, tc.body, got)
			}
			apiNoLeak(t, fmt.Sprint(err))
		}
	})

	t.Run("buckets-404", func(t *testing.T) {
		for _, tc := range []struct {
			name, detail string
			detailStatus int
			none, err    bool
		}{
			{"cold-archive", `{"name":"RBX-ARCHIVE","services":[{"name":"storage-s3-coldarchive","status":"UP"}]}`, 200, true, false},
			{"compute-only", `{"name":"GRA11","services":[{"name":"instance","status":"UP"}]}`, 200, true, false},
			{"bucket-service", `{"name":"GRA","services":[{"name":"storage-s3-standard","status":"UP"}]}`, 200, false, false},
			{"no-services", `{"name":"GRA"}`, 200, false, true},
			{"detail-refused", `{"message":"SERVERTEXT"}`, 403, false, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newFakeCheckAPI(t)
				f.answer = func(rw http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/storage") {
						rw.WriteHeader(http.StatusNotFound)
						fmt.Fprint(rw, `{"message":"SERVERTEXT"}`)
						return
					}
					rw.WriteHeader(tc.detailStatus)
					fmt.Fprint(rw, tc.detail)
				}
				got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListBuckets, Project: apiTestProject, Region: "GRA"})
				if (err != nil) != tc.err || err == nil && (got.Status != 404 || got.NoBucketService != tc.none || got.Names != nil) {
					t.Errorf("Do = %+v, %v; want status 404, no bucket service %v, error %v", got, err, tc.none, tc.err)
				}
				apiNoLeak(t, fmt.Sprint(err))
				if reqs := f.requests(); len(reqs) != 2 || reqs[1].Path != base+"/region/GRA" {
					t.Errorf("requests %v, want the listing then the region's detail", reqs)
				}
			})
		}
	})
}
