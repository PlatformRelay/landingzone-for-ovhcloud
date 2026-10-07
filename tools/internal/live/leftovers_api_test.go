package live

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Read-only API-client leftover listing (T083; FR-011, FR-013; research R12). ovhcloud 0.15.0 has
// no `api` command (P18 refuted, evidence/T009.md), so the leftover check lists every matrix kind
// through lz-live's own OAuth2 client (binding.go) with the credential the run binds: GET only,
// pagination followed, every failure an error and never zero leftovers, no ovhcloud process.
//
// The fake API below serves the synthetic listings of tests/fixtures/ovhcloud/synthetic/ over
// HTTP. Its protocol follows the sources, not the code under test:
//   - token: the client-credentials grant as binding.go and the fake of fake_test.go model it;
//   - versions: /iam/policy and /iam/resource/{urn} exist only in APIv2 (kb/api/v2/iam.json; no
//     /iam path in kb/api/v1), everything else the matrix lists is APIv1; API.BaseURL is the v1
//     base (".../v1"), the v2 base is its sibling (".../v2", apiv2.mdx: eu.api.ovh.com/v2/...);
//   - pagination: kb/mirror/.../manage-and-operate/api/apiv2.mdx *Pagination*: the cursor goes in
//     the X-Pagination-Cursor request header, the next one comes back in X-Pagination-Cursor-Next,
//     whose absence marks the last page. Cursors are opaque. The fake applies the same protocol to
//     v1 paths (whether v1 listings paginate this way is UNVERIFIED; T084's captures qualify it).

const (
	listClientID = "EU.lister0admin01"
	listSecret   = "lzlist-SECRET-2c9e71b0a4d3"
	listToken    = "lzlist-TOKEN-8d1f0e6a5b27"
)

var listCred = Credential{Endpoint: "ovh-eu", ClientID: listClientID, ClientSecret: listSecret}

// listCall is one request the fake listing API received.
type listCall struct {
	Method, Version, Route, Cursor, Auth, RawQuery string
	Status                                         int
}

// listFault makes the fake answer one route badly (every page, or only page Page).
type listFault struct {
	Kind   string // status, redirect, truncated-length, truncated-chunked, malformed, empty, reset
	Status int
	Page   int
}

type fakeListAPI struct {
	*httptest.Server
	elsewhere *httptest.Server // a redirect target; must never be asked

	mu         sync.Mutex
	responses  map[string][]json.RawMessage
	faults     map[string]listFault
	tokenFault string            // status, no-token
	cursors    map[string]string // issued cursor -> route#page
	calls      []listCall
	elseCalls  int
}

func newFakeListAPI(t *testing.T, responses map[string][]json.RawMessage) *fakeListAPI {
	t.Helper()
	f := &fakeListAPI{responses: responses, faults: map[string]listFault{}, cursors: map[string]string{}}
	f.elsewhere = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.elseCalls++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	t.Cleanup(f.elsewhere.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/oauth2/token", f.token)
	mux.HandleFunc("/v1/", f.api)
	mux.HandleFunc("/v2/", f.api)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeListAPI) API() API {
	return API{TokenURL: f.URL + "/auth/oauth2/token", BaseURL: f.URL + "/v1", HTTP: f.Client()}
}

func (f *fakeListAPI) record(c listCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakeListAPI) Calls() []listCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeListAPI) ElsewhereCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.elseCalls
}

func (f *fakeListAPI) token(w http.ResponseWriter, r *http.Request) {
	c := listCall{Method: r.Method, Route: "token", RawQuery: r.URL.RawQuery}
	defer func() { f.record(c) }()
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.ParseForm() != nil ||
		r.PostForm.Get("grant_type") != "client_credentials" {
		c.Status = http.StatusBadRequest
		http.Error(w, `{"error":"invalid_request"}`, c.Status)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	c.Auth = id
	if id != listClientID || secret != listSecret {
		c.Status = http.StatusUnauthorized
		http.Error(w, `{"error":"invalid_client"}`, c.Status)
		return
	}
	switch f.tokenFault {
	case "status": // the answer echoes the credential: it must never reach an error
		c.Status = http.StatusInternalServerError
		http.Error(w, `{"error":"server_error","error_description":"client `+listClientID+` secret `+listSecret+`"}`, c.Status)
		return
	case "no-token":
		c.Status = http.StatusOK
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token_type":"Bearer","expires_in":3599}`))
		return
	}
	c.Status = http.StatusOK
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"access_token": listToken, "token_type": "Bearer", "expires_in": 3599, "scope": "all"})
}

// cursorFor is an opaque cursor for page of route (not a page number a client could guess).
func cursorFor(route string, page int) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s#%d", route, page)
	return fmt.Sprintf("opaque-%016x", h.Sum64())
}

func (f *fakeListAPI) api(w http.ResponseWriter, r *http.Request) {
	version, route, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	route = "/" + route
	c := listCall{Method: r.Method, Version: version, Route: route, Cursor: r.Header.Get("X-Pagination-Cursor"),
		Auth: r.Header.Get("Authorization"), RawQuery: r.URL.RawQuery}
	answer := func(status int, body string) {
		c.Status = status
		f.record(c)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}
	if n := len(f.Calls()); n > 500 {
		answer(http.StatusInternalServerError, `{"message":"fake: too many requests (a pagination loop?)"}`)
		return
	}
	if c.Auth != "Bearer "+listToken {
		answer(http.StatusUnauthorized, `{"message":"Invalid credentials"}`)
		return
	}
	if r.Method != http.MethodGet {
		answer(http.StatusMethodNotAllowed, `{"message":"read-only fake"}`)
		return
	}
	if want := map[bool]string{true: "v2", false: "v1"}[strings.HasPrefix(route, "/iam/")]; version != want {
		answer(http.StatusNotFound, `{"message":"no such path in this API version"}`)
		return
	}
	f.mu.Lock()
	pages, known := f.responses[route]
	fault, faulty := f.faults[route]
	issued, cursorKnown := f.cursors[c.Cursor]
	f.mu.Unlock()
	if !known {
		answer(http.StatusNotFound, `{"message":"fake: no such path"}`)
		return
	}
	page := 1
	if c.Cursor != "" {
		if !cursorKnown || !strings.HasPrefix(issued, route+"#") {
			answer(http.StatusBadRequest, `{"message":"invalid cursor"}`)
			return
		}
		fmt.Sscanf(strings.TrimPrefix(issued, route+"#"), "%d", &page)
	}
	if page > len(pages) {
		answer(http.StatusBadRequest, `{"message":"fake: no such page"}`)
		return
	}
	body := []byte(pages[page-1])
	var raw struct {
		Raw *string `json:"_raw"`
	}
	if json.Unmarshal(body, &raw) == nil && raw.Raw != nil {
		body = []byte(*raw.Raw)
	}
	if faulty && (fault.Page == 0 || fault.Page == page) {
		f.fault(w, r, &c, fault, body)
		return
	}
	if page < len(pages) {
		next := cursorFor(route, page+1)
		f.mu.Lock()
		f.cursors[next] = fmt.Sprintf("%s#%d", route, page+1)
		f.mu.Unlock()
		w.Header().Set("X-Pagination-Cursor-Next", next)
	}
	answer(http.StatusOK, string(body))
}

// fault answers badly. Where a body is sent it is a well-formed one that parses to nothing to
// report (an empty array, or the clean object), so a client that ignores the failure and parses
// the body reports zero leftovers instead of an error.
func (f *fakeListAPI) fault(w http.ResponseWriter, r *http.Request, c *listCall, fault listFault, clean []byte) {
	neutral := []byte("[]")
	if trimmed := strings.TrimSpace(string(clean)); strings.HasPrefix(trimmed, "{") {
		neutral = clean
	}
	c.Status = fault.Status
	f.record(*c)
	hijack := func(raw string) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			panic(err)
		}
		buf.WriteString(raw)
		buf.Flush()
		conn.Close()
	}
	switch fault.Kind {
	case "status":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(fault.Status)
		if fault.Status != http.StatusNoContent {
			w.Write(neutral)
		}
	case "redirect":
		w.Header().Set("Location", f.elsewhere.URL+r.URL.Path)
		w.WriteHeader(fault.Status)
	case "truncated-length": // a complete JSON body, but shorter than the declared length
		hijack(fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(neutral)+64, neutral))
	case "truncated-chunked": // a complete JSON body in a chunked answer without its last chunk
		hijack(fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", len(neutral), neutral))
	case "reset": // the connection closes before any answer
		hijack("")
	case "malformed":
		w.Header().Set("Content-Type", "application/json")
		w.Write(clean[:len(clean)/2])
	case "empty":
		w.Header().Set("Content-Type", "application/json")
	case "echo": // an error answer that echoes the request's credential: never into an error
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(fault.Status)
		fmt.Fprintf(w, `{"message":"rejected %s (client %s, secret %s)"}`, r.Header.Get("Authorization"), listClientID, listSecret)
	default:
		panic("unknown fault " + fault.Kind)
	}
}

// apiCheck is worldCheck's check listing through the fake API instead of the in-memory lister.
func apiCheck(t testing.TB, w syntheticWorld, s syntheticSeed, f *fakeListAPI) (LeftoverCheck, []InventoryEntry) {
	t.Helper()
	c, _, inv := worldCheck(t, w, s)
	c.Lister = nil
	c.API = f.API()
	c.Cred = listCred
	return c, inv
}

// seededResponses is the clean world's listings with a seed's replacements.
func seededResponses(w syntheticWorld, s syntheticSeed) map[string][]json.RawMessage {
	out := map[string][]json.RawMessage{}
	for k, v := range w.Responses {
		out[k] = v
	}
	for k, v := range s.Replace {
		out[k] = v
	}
	return out
}

// setOvhcloud points the check's ovhcloud executable (while that field exists: T084 may drop it)
// at bin, so a check that still shells out runs the logging fake instead of failing early.
func setOvhcloud(c *LeftoverCheck, bin string) {
	if f := reflect.ValueOf(c).Elem().FieldByName("Ovhcloud"); f.IsValid() && f.Kind() == reflect.String {
		f.SetString(bin)
	}
}

// assertNoCredentialIn fails when the secret or the bearer token appears in any of texts.
func assertNoCredentialIn(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if strings.Contains(s, listSecret) || strings.Contains(s, listToken) {
			t.Errorf("%s carries the client secret or the bearer token: %q", what, s)
		}
	}
}

// TestLeftoversAPICleanMatrix: a clean world passes when listed through the API client alone:
// every listing of every matrix kind (both projects, every parent's children, every page) is a
// GET under the right API version with the bearer token of the run's credential, the token comes
// from that credential's client-credentials grant. (Whether ovhcloud is started is a run-level
// question: TestLeftoversAPIRunStartsNoOvhcloud.)
func TestLeftoversAPICleanMatrix(t *testing.T) {
	w, _ := loadSynthetic(t)
	f := newFakeListAPI(t, w.Responses)
	c, inv := apiCheck(t, w, syntheticSeed{}, f)
	rep := c.Check(context.Background(), inv)
	if rep.Outcome != "pass" || len(rep.Leftovers) != 0 || len(rep.Errors) != 0 {
		t.Fatalf("clean listings through the API: outcome %q, leftovers %v, errors %v; want pass", rep.Outcome, leftoverKeys(rep.Leftovers), rep.Errors)
	}

	asked := map[string]bool{}
	tokens := 0
	for _, call := range f.Calls() {
		if call.Route == "token" {
			tokens++
			if call.Method != http.MethodPost || call.Auth != listClientID {
				t.Errorf("token request %s for client %q, want the client-credentials grant of %s", call.Method, call.Auth, listClientID)
			}
			continue
		}
		if call.Method != http.MethodGet {
			t.Errorf("%s %s: the leftover check sent a %s", call.Method, call.Route, call.Method)
		}
		if call.Auth != "Bearer "+listToken {
			t.Errorf("%s: Authorization %q, want the run credential's bearer token", call.Route, call.Auth)
		}
		if call.RawQuery != "" {
			t.Errorf("%s: query %q, want none (the cursor goes in a header)", call.Route, call.RawQuery)
		}
		if call.Status == http.StatusOK {
			page := 1
			if call.Cursor != "" {
				page = 0
				for p := 2; p <= len(w.Responses[call.Route]); p++ {
					if cursorFor(call.Route, p) == call.Cursor {
						page = p
					}
				}
			}
			asked[fmt.Sprintf("%s#%d", call.Route, page)] = true
		}
		if want := map[bool]string{true: "v2", false: "v1"}[strings.HasPrefix(call.Route, "/iam/")]; call.Version != want {
			t.Errorf("%s asked under /%s, want /%s", call.Route, call.Version, want)
		}
	}
	if tokens == 0 {
		t.Error("no token request: the listings did not use the run's credential")
	}
	for path, pages := range w.Responses {
		for p := 1; p <= len(pages); p++ {
			if !asked[fmt.Sprintf("%s#%d", path, p)] {
				t.Errorf("never listed %s page %d through the API", path, p)
			}
		}
	}
	if n := len(rep.Listings["ovh_cloud_project_network_private"]); n != 2 {
		t.Errorf("recorded network listing holds %d items, want 2 (two pages)", n)
	}
	for _, typ := range matrixTypes {
		if !slices.Contains(unlisted, typ) && rep.Listings[typ] == nil {
			t.Errorf("no recorded listing for %s", typ)
		}
	}
}

// TestLeftoversAPISeeded: one seeded leftover per kind, listed through the API, is reported
// exactly.
func TestLeftoversAPISeeded(t *testing.T) {
	w, seeds := loadSynthetic(t)
	for name, s := range seeds.Seeds {
		t.Run(name, func(t *testing.T) {
			f := newFakeListAPI(t, seededResponses(w, s))
			c, inv := apiCheck(t, w, s, f)
			rep := c.Check(context.Background(), inv)
			if got, want := leftoverKeys(rep.Leftovers), leftoverKeys(s.Expect); rep.Outcome != "fail" || !slices.Equal(got, want) || len(rep.Errors) != 0 {
				t.Errorf("outcome %q, leftovers %v, errors %v; want fail with exactly %v and no error", rep.Outcome, got, rep.Errors, want)
			}
		})
	}
}

// TestLeftoversAPIGetOnly: the client sends GET and nothing else; any other method is refused
// before a request (no token request, no API request), whether or not it holds a token yet.
func TestLeftoversAPIGetOnly(t *testing.T) {
	w, _ := loadSynthetic(t)
	const path = "/me/identity/group"
	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead,
		http.MethodOptions, http.MethodConnect, http.MethodTrace, "get", "PROPFIND"}
	ctx := context.Background()

	t.Run("before-any-token", func(t *testing.T) {
		f := newFakeListAPI(t, w.Responses)
		l := newAPILister(f.API(), listCred)
		for _, m := range methods {
			if _, _, err := l.request(ctx, m, path, ""); err == nil {
				t.Errorf("%s %s: no error, want a refusal", m, path)
			}
		}
		if calls := f.Calls(); len(calls) != 0 {
			t.Errorf("refused methods reached the fake: %+v", calls)
		}
	})
	t.Run("after-a-get", func(t *testing.T) {
		f := newFakeListAPI(t, w.Responses)
		l := newAPILister(f.API(), listCred)
		body, _, err := l.request(ctx, http.MethodGet, path, "")
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		var got, want []string
		if json.Unmarshal(body, &got) != nil || json.Unmarshal(w.Responses[path][0], &want) != nil || !slices.Equal(got, want) {
			t.Fatalf("GET %s = %s, want %s", path, body, w.Responses[path][0])
		}
		if body, _, err := l.Get(ctx, path, ""); err != nil || len(body) == 0 {
			t.Fatalf("Get %s: %v", path, err)
		}
		before := len(f.Calls())
		for _, m := range methods {
			if _, _, err := l.request(ctx, m, path, ""); err == nil {
				t.Errorf("%s %s: no error, want a refusal", m, path)
			} else {
				assertNoCredentialIn(t, m+" refusal", err.Error())
			}
		}
		if calls := f.Calls(); len(calls) != before {
			t.Errorf("refused methods reached the fake: %+v", calls[before:])
		}
	})
}

// TestLeftoversAPIPagination: the cursor of X-Pagination-Cursor-Next is sent back verbatim in the
// X-Pagination-Cursor header until a page comes without one, and a leftover on the last page of
// a paginated listing is found.
func TestLeftoversAPIPagination(t *testing.T) {
	w, seeds := loadSynthetic(t)
	ctx := context.Background()

	t.Run("cursor-protocol", func(t *testing.T) {
		resp := map[string][]json.RawMessage{"/iam/policy": {
			json.RawMessage(`[{"id":"p-1","name":"one"}]`), json.RawMessage(`[{"id":"p-2","name":"two"}]`), json.RawMessage(`[{"id":"p-3","name":"three"}]`),
		}}
		f := newFakeListAPI(t, resp)
		l := newAPILister(f.API(), listCred)
		cursor := ""
		for page := 1; page <= 3; page++ {
			body, next, err := l.Get(ctx, "/iam/policy", cursor)
			if err != nil {
				t.Fatalf("page %d: %v", page, err)
			}
			if string(body) != string(resp["/iam/policy"][page-1]) {
				t.Errorf("page %d = %s, want %s", page, body, resp["/iam/policy"][page-1])
			}
			calls := f.Calls()
			if last := calls[len(calls)-1]; last.Cursor != cursor || last.RawQuery != "" {
				t.Errorf("page %d asked with cursor header %q and query %q, want header %q and no query", page, last.Cursor, last.RawQuery, cursor)
			}
			wantNext := ""
			if page < 3 {
				wantNext = cursorFor("/iam/policy", page+1)
			}
			if next != wantNext {
				t.Errorf("page %d: next cursor %q, want %q (X-Pagination-Cursor-Next)", page, next, wantNext)
			}
			cursor = next
		}
	})

	t.Run("leftover-on-last-page", func(t *testing.T) {
		s := seeds.Seeds["iam-policy"]
		if len(s.Expect) != 1 {
			t.Fatalf("fixture: seed iam-policy expects %v, want one policy", s.Expect)
		}
		// One policy per page, the seeded leftover last.
		var items, last []json.RawMessage
		for _, page := range s.Replace["/iam/policy"] {
			var ps []json.RawMessage
			if err := json.Unmarshal(page, &ps); err != nil {
				t.Fatal(err)
			}
			for _, p := range ps {
				var id struct {
					ID string `json:"id"`
				}
				json.Unmarshal(p, &id)
				if id.ID == s.Expect[0].ID {
					last = append(last, p)
				} else {
					items = append(items, p)
				}
			}
		}
		if len(last) != 1 || len(items) < 2 {
			t.Fatalf("fixture: seed iam-policy has %d other policies and %d seeded, want ≥2 and 1", len(items), len(last))
		}
		var pages []json.RawMessage
		for _, p := range items {
			pages = append(pages, json.RawMessage("["+string(p)+"]"))
		}
		lastPage := json.RawMessage("[" + string(last[0]) + "]")
		// An empty page that still carries a next cursor is not the end of the listing.
		for name, layout := range map[string][]json.RawMessage{
			"one-per-page":           append(slices.Clone(pages), lastPage),
			"empty-page-before-last": append(slices.Clone(pages), json.RawMessage("[]"), lastPage),
		} {
			t.Run(name, func(t *testing.T) {
				resp := seededResponses(w, s)
				resp["/iam/policy"] = layout
				f := newFakeListAPI(t, resp)
				c, inv := apiCheck(t, w, s, f)
				rep := c.Check(ctx, inv)
				if got, want := leftoverKeys(rep.Leftovers), leftoverKeys(s.Expect); rep.Outcome != "fail" || !slices.Equal(got, want) {
					t.Errorf("outcome %q, leftovers %v, errors %v; want fail with %v from page %d", rep.Outcome, got, rep.Errors, want, len(layout))
				}
				if n := len(rep.Listings["ovh_iam_policy"]); n != len(items)+1 {
					t.Errorf("recorded policy listing holds %d items, want %d (every page)", n, len(items)+1)
				}
			})
		}
	})
}

// TestLeftoversAPIErrors: a listing error (transport, token), a non-2xx status, a redirect, a
// truncated body (declared length or missing last chunk) and a malformed or empty body are each
// `fail` with an error naming the listing, never a pass with zero leftovers — the fake sends a
// well-formed empty listing wherever it sends a body. No error carries the secret or the token,
// and a redirect is not followed.
func TestLeftoversAPIErrors(t *testing.T) {
	w, _ := loadSynthetic(t)
	routes := []struct {
		name, route string
		page        int
	}{
		{"array", "/cloud/project/p1/network/private", 1},
		{"v2-second-page", "/iam/policy", 2},
		{"child", "/cloud/project/p1/user/101/s3Credentials", 0},
		{"v2-object", "/iam/resource/urn:v1:eu:resource:publicCloudProject:p1", 0},
		{"object", "/me/api/oauth2/client/EU.p1atf0rmdep10y", 0},
	}
	faults := map[string]listFault{
		"400": {Kind: "status", Status: 400}, "401": {Kind: "status", Status: 401}, "403": {Kind: "status", Status: 403},
		"404": {Kind: "status", Status: 404}, "409": {Kind: "status", Status: 409}, "429": {Kind: "status", Status: 429},
		"500": {Kind: "status", Status: 500}, "502": {Kind: "status", Status: 502}, "503": {Kind: "status", Status: 503},
		"204-no-content":    {Kind: "status", Status: 204},
		"302-redirect":      {Kind: "redirect", Status: 302},
		"307-redirect":      {Kind: "redirect", Status: 307},
		"truncated-length":  {Kind: "truncated-length"},
		"truncated-chunked": {Kind: "truncated-chunked"},
		"malformed":         {Kind: "malformed"},
		"empty-body":        {Kind: "empty"},
		"connection-reset":  {Kind: "reset"},
		"500-echoing-token": {Kind: "echo", Status: 500},
		"403-echoing-token": {Kind: "echo", Status: 403},
	}
	for _, r := range routes {
		for name, fault := range faults {
			t.Run(r.name+"/"+name, func(t *testing.T) {
				f := newFakeListAPI(t, w.Responses)
				fault.Page = r.page
				f.faults[r.route] = fault
				c, inv := apiCheck(t, w, syntheticSeed{}, f)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				rep := c.Check(ctx, inv)
				if rep.Outcome != "fail" || len(rep.Errors) == 0 {
					t.Fatalf("outcome %q, leftovers %v, errors %v; want fail with an error", rep.Outcome, leftoverKeys(rep.Leftovers), rep.Errors)
				}
				if !strings.Contains(strings.Join(rep.Errors, "\n"), r.route) {
					t.Errorf("errors %v do not name the listing %s", rep.Errors, r.route)
				}
				assertNoCredentialIn(t, "report errors", rep.Errors...)
				if n := f.ElsewhereCalls(); n != 0 {
					t.Errorf("the redirect was followed (%d requests elsewhere)", n)
				}
			})
		}
	}
	for name, fault := range map[string]string{"token-500": "status", "token-without-access-token": "no-token"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeListAPI(t, w.Responses)
			f.tokenFault = fault
			c, inv := apiCheck(t, w, syntheticSeed{}, f)
			rep := c.Check(context.Background(), inv)
			if rep.Outcome != "fail" || len(rep.Errors) == 0 {
				t.Errorf("outcome %q, errors %v; want fail with an error", rep.Outcome, rep.Errors)
			}
			assertNoCredentialIn(t, "report errors", rep.Errors...)
			assertOnlyTokenAsked(t, f)
		})
	}
	t.Run("credential-rejected", func(t *testing.T) {
		f := newFakeListAPI(t, w.Responses)
		c, inv := apiCheck(t, w, syntheticSeed{}, f)
		c.Cred.ClientSecret = "not-the-secret"
		if rep := c.Check(context.Background(), inv); rep.Outcome != "fail" || len(rep.Errors) == 0 {
			t.Errorf("outcome %q, errors %v; want fail with an error", rep.Outcome, rep.Errors)
		}
		assertOnlyTokenAsked(t, f)
	})
	t.Run("no-api-client", func(t *testing.T) { // guard row: green on the ovhcloud-based check too
		f := newFakeListAPI(t, w.Responses)
		c, inv := apiCheck(t, w, syntheticSeed{}, f)
		// No endpoint: the check must not fall back to a default one (a real OVHcloud endpoint
		// would receive the credential); any request through the given client fails the row.
		var sent []string
		c.API = API{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			sent = append(sent, r.Method+" "+r.URL.Redacted())
			return nil, fmt.Errorf("no request expected")
		})}}
		if rep := c.Check(context.Background(), inv); rep.Outcome != "fail" || len(rep.Errors) == 0 {
			t.Errorf("outcome %q, errors %v; want fail with an error", rep.Outcome, rep.Errors)
		}
		if len(sent) != 0 {
			t.Errorf("a check without an API endpoint sent %v", sent)
		}
		if calls := f.Calls(); len(calls) != 0 {
			t.Errorf("a check without an API endpoint reached the fake: %+v", calls)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// assertOnlyTokenAsked: the check asked the token endpoint for the run's credential, and without
// a token it sent no listing request.
func assertOnlyTokenAsked(t *testing.T, f *fakeListAPI) {
	t.Helper()
	tokens := 0
	for _, c := range f.Calls() {
		if c.Route == "token" {
			tokens++
		} else {
			t.Errorf("listing %s sent without a token", c.Route)
		}
	}
	if tokens == 0 {
		t.Error("no token request: the check never tried the run's credential")
	}
}

// TestLeftoversAPIRunStartsNoOvhcloud: a whole run's leftover check (the runner sets up the child
// environment ovhcloud would run in) lists through the API and starts no ovhcloud process, with
// a fake ovhcloud both on PATH and named in the check.
func TestLeftoversAPIRunStartsNoOvhcloud(t *testing.T) {
	syn, _ := loadSynthetic(t)
	w := newRunWorld(t)
	t.Setenv("PATH", w.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := newFakeListAPI(t, syn.Responses)
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}))
	r.Leftovers.Lister = nil
	r.Leftovers.API = f.API()
	r.Leftovers.Cred = listCred
	setOvhcloud(&r.Leftovers, w.ovhcloud)
	err, _ := execute(t, r)
	if calls := w.calls(t, "ovhcloud.log"); len(calls) != 0 {
		t.Errorf("the run started ovhcloud %d times: %+v", len(calls), calls[0].Args)
	}
	// Started by bare name (argv[0] "ovhcloud"), the fake logs into its working directory.
	for _, dir := range []string{".", r.Stacks[0].Dir} {
		if _, err := os.Stat(dir + "/ovhcloud.log"); err == nil {
			os.Remove(dir + "/ovhcloud.log")
			t.Errorf("the run started ovhcloud by bare name (log in %s)", dir)
		}
	}
	if err != nil {
		t.Errorf("run with a clean world listed through the API: %v", err)
	}
	listed := 0
	for _, c := range f.Calls() {
		if c.Route != "token" && c.Status == http.StatusOK {
			listed++
		}
	}
	if listed == 0 {
		t.Error("the run's leftover check never listed through the API")
	}
	summary, rerr := os.ReadFile(w.runDir + "/summary.json")
	if rerr != nil || !strings.Contains(string(summary), `"outcome":"pass"`) {
		t.Errorf("summary.json = %s (%v), want outcome pass", summary, rerr)
	}
	assertNoCredentialIn(t, "terminal", w.term.String())
	if raw, err := os.ReadFile(w.runDir + "/leftovers.json"); err == nil {
		assertNoCredentialIn(t, "leftovers.json", string(raw))
	}
}
