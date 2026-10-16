package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T031: the token-residual check (T032) against a fake OVHcloud API (OAuth2 token endpoint,
// /v1/me/api/oauth2/client, /v2/iam/policy and the one granted read), a fake clock and a fake
// sleep that advances it. How long an access token stays usable after its client is deleted is
// UNVERIFIED (005/T010 observed create and delete only); the fakes answer the worlds the live run
// (T026) may meet. The token lifetime is the token answer's expires_in; 3599 s follows spec 005's
// fixtures (tools/internal/live/leftovers_api_test.go), UNVERIFIED for the sandbox. The client and
// policy shapes follow tools/internal/live/bootstrap.go (createAdmin) and its fake. No test calls
// OVHcloud or reads ~/.config/ovh-lz/.
//
// What the tests fix for T032, beyond data-model.md: the check creates the OAuth2 client
// (flow CLIENT_CREDENTIALS) and the policy, both named lzprobe-token-residual-<run id>, the policy
// naming the client's identity; it takes one token with the client's credentials, retries the
// granted read (GET <read_path> on the v1 API) with that token as the control until it is allowed,
// deletes the client, then repeats the same read with the same token. The record is
// observations.json with check "token-residual", run_id, issued_at and deleted_at (RFC 3339),
// expires_in (seconds), control and polls (each {at, status, outage}), observed, reason,
// lower_seconds and upper_seconds. ResidualWindow's rules beyond the task text: a status other than
// 2xx, 401 or 403 (or no answer) is an outage; valid-until-expiry needs the poll just before the
// first refusal to be allowed; with no allowed poll before the refusal, Lower is zero.

const (
	rrRun      = "20261016T111500Z-3c4d"
	rrOtherRun = "20261015T090000Z-9f8e"
	rrProject  = "0123456789abcdef0123456789abcdef"
	rrName     = "lzprobe-token-residual-" + rrRun
	rrReadPath = "/cloud/project/" + rrProject
	rrAccount  = "xx1111-ovh"
	rrAdminID  = "EU.residualsweepadmin"
	rrAdminSK  = "Kq3wEr7tYu1iOp5aSd9f"
	rrAdminTok = "tok-rradmin-Hj6k"
	rrInterval = 30 * time.Second
	rrSettle   = 2 * time.Minute
	// The read's IAM action: GET /cloud/project/{serviceName} requires publicCloudProject:apiovh:get
	// (kb api/v1/cloud.json, iamActions); the project's URN as tools/internal/stacks writes it.
	rrAction     = "publicCloudProject:apiovh:get"
	rrProjectURN = "urn:v1:eu:resource:publicCloudProject:" + rrProject
)

// rrForeign are clients and policies the check must never touch: another run's, a longer name
// with ours as its prefix, a name differing only in case, and the sandbox admin.
var rrForeign = []string{"lzprobe-token-residual-" + rrOtherRun, rrName + "-x", strings.ToLower(rrName), "lz-sandbox-admin"}

func rrEnv() map[string]string {
	return map[string]string{"run_id": rrRun, "project_id": rrProject, "read_path": rrReadPath}
}

type rrReq struct {
	who, method, path string
	tok               string // the bearer token, or the client id of a token request
	at                time.Time
	status            int // a read's answer
}

func (r rrReq) String() string { return r.who + " " + r.method + " " + r.path }

type rrClient struct{ id, name, identity, secret string }

type rrPolicy struct {
	id, name   string
	identities []string
	grants     bool // allows exactly rrAction on exactly rrProjectURN
}

type rrWorld struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	start    time.Time
	clock    time.Time
	deadline time.Time // zero: none; Sleep past it returns context.DeadlineExceeded
	sleeps   int
	reqs     []rrReq
	clients  []rrClient
	policies []rrPolicy
	n        int // ids issued
	tokens   []string
	secrets  []string // every client secret issued
	deleted  bool     // a client of ours was deleted
	postDel  int      // reads after the delete

	expiresIn  string // the raw expires_in of a probe token answer; "": the field is missing
	control    []int  // statuses of the reads before the delete, in turn; then 200
	after      func(n int, at, expiry time.Time) int
	expiry     time.Time
	createLost bool // POST client creates, then answers 502
	echo       bool // a refused read's body echoes the bearer token
	echoed     int  // refused reads answered with the echo
	deleteFail int  // the first client DELETE answers this status, once
	keep       bool // deletes are acknowledged but not effective
	pageSize   int  // > 0: GET /v2/iam/policy answers pages of this size
}

func newRRWorld(t *testing.T) *rrWorld {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	w := &rrWorld{t: t, start: now, clock: now, expiresIn: "3599"}
	w.after = func(n int, at, expiry time.Time) int {
		if at.Before(expiry) {
			return http.StatusOK
		}
		return http.StatusUnauthorized
	}
	w.srv = httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *rrWorld) now() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.clock
}

func (w *rrWorld) sleep(ctx context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sleeps++
	if w.sleeps > 20000 {
		return errors.New("fake sleep: more than 20000 sleeps")
	}
	if !w.deadline.IsZero() && w.clock.Add(d).After(w.deadline) {
		w.clock = w.deadline
		return context.DeadlineExceeded
	}
	w.clock = w.clock.Add(d)
	return ctx.Err()
}

func (w *rrWorld) options() Options {
	return Options{
		API:      live.API{TokenURL: w.srv.URL + "/token", BaseURL: w.srv.URL + "/v1", HTTP: w.srv.Client()},
		Admin:    live.Credential{Endpoint: "ovh-eu", ClientID: rrAdminID, ClientSecret: rrAdminSK},
		Now:      w.now,
		Sleep:    w.sleep,
		Interval: rrInterval,
		Settle:   rrSettle,
	}
}

// ctx is the run's context: with the world's deadline when it has one (the fake clock starts at
// the real time, so ctx.Deadline() and Options.Now agree).
func (w *rrWorld) ctx(t *testing.T) context.Context {
	if w.deadline.IsZero() {
		return context.Background()
	}
	ctx, cancel := context.WithDeadline(context.Background(), w.deadline)
	t.Cleanup(cancel)
	return ctx
}

func (w *rrWorld) seen() []rrReq {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.reqs)
}

func (w *rrWorld) addClient(name string) rrClient {
	w.n++
	c := rrClient{id: fmt.Sprintf("EU.lzfake%04d", w.n), name: name, secret: fmt.Sprintf("Zx8cVb%04dMq6wEr4tYp", w.n)}
	c.identity = "urn:v1:eu:identity:credential:" + rrAccount + "/oauth2-" + c.id
	w.clients = append(w.clients, c)
	w.secrets = append(w.secrets, c.secret)
	return c
}

func (w *rrWorld) addPolicy(name string, identities ...string) rrPolicy {
	w.n++
	p := rrPolicy{id: fmt.Sprintf("00000000-0000-4000-8000-%012d", w.n), name: name, identities: identities}
	w.policies = append(w.policies, p)
	return p
}

// foreign seeds the clients and policies of rrForeign.
func (w *rrWorld) foreign() {
	for _, n := range rrForeign {
		c := w.addClient(n)
		w.addPolicy(n, c.identity)
	}
}

func (w *rrWorld) names() (clients, policies []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range w.clients {
		clients = append(clients, c.name)
	}
	for _, p := range w.policies {
		policies = append(policies, p.name)
	}
	slices.Sort(clients)
	slices.Sort(policies)
	return clients, policies
}

func (w *rrWorld) leaks(texts ...string) []string {
	w.mu.Lock()
	bad := append([]string{rrAdminSK, rrAdminTok}, w.secrets...)
	bad = append(bad, w.tokens...)
	w.mu.Unlock()
	var out []string
	for _, s := range texts {
		for _, b := range bad {
			if strings.Contains(s, b) && !slices.Contains(out, b) {
				out = append(out, b)
			}
		}
	}
	return out
}

func (w *rrWorld) serve(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.mu.Lock()
	defer w.mu.Unlock()
	rw.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/token" {
		w.token(rw, body)
		return
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	who := "?"
	switch {
	case tok == rrAdminTok:
		who = "admin"
	case slices.Contains(w.tokens, tok):
		who = "probe"
	}
	w.reqs = append(w.reqs, rrReq{who, r.Method, r.URL.Path, tok, w.clock, 0})
	switch who {
	case "admin":
		w.admin(rw, r, body)
	case "probe":
		w.read(rw, r)
	default:
		rw.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(rw, `{"message":"Invalid token"}`)
	}
}

// token answers the client-credentials grant: the admin's, or a live client's of this world.
func (w *rrWorld) token(rw http.ResponseWriter, body []byte) {
	f, _ := url.ParseQuery(string(body))
	id, secret := f.Get("client_id"), f.Get("client_secret")
	if f.Get("grant_type") == "client_credentials" && id == rrAdminID && secret == rrAdminSK {
		w.reqs = append(w.reqs, rrReq{"admin", "TOKEN", "/token", id, w.clock, 0})
		fmt.Fprintf(rw, `{"access_token":%q,"token_type":"Bearer","expires_in":3599,"scope":"all"}`, rrAdminTok)
		return
	}
	w.reqs = append(w.reqs, rrReq{"probe", "TOKEN", "/token", id, w.clock, 0})
	i := slices.IndexFunc(w.clients, func(c rrClient) bool { return c.id == id && c.secret == secret })
	if f.Get("grant_type") != "client_credentials" || i < 0 {
		rw.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(rw, `{"error":"invalid_client"}`)
		return
	}
	tok := fmt.Sprintf("atk%04dRt9LmQzW", len(w.tokens)+1)
	w.tokens = append(w.tokens, tok)
	ans := fmt.Sprintf(`{"access_token":%q,"token_type":"Bearer","scope":"all"`, tok)
	if w.expiresIn != "" {
		ans += `,"expires_in":` + w.expiresIn
		if s, err := strconv.Atoi(w.expiresIn); err == nil {
			w.expiry = w.clock.Add(time.Duration(s) * time.Second)
		}
	}
	fmt.Fprint(rw, ans+"}")
}

// read answers the probe token's requests: only GET <read_path> is granted, and only while a policy
// names the client's identity.
func (w *rrWorld) read(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/v1"+rrReadPath {
		rw.WriteHeader(http.StatusForbidden)
		fmt.Fprint(rw, `{"message":"This call has not been granted"}`)
		return
	}
	status := http.StatusOK
	if w.deleted {
		w.postDel++
		status = w.after(w.postDel, w.clock, w.expiry)
	} else {
		granted := false
		for _, c := range w.clients {
			for _, p := range w.policies {
				if c.name == rrName && p.name == rrName && p.grants && slices.Contains(p.identities, c.identity) {
					granted = true
				}
			}
		}
		switch {
		case !granted:
			status = http.StatusForbidden
		case len(w.control) > 0:
			status, w.control = w.control[0], w.control[1:]
		}
	}
	w.reqs[len(w.reqs)-1].status = status
	rw.WriteHeader(status)
	switch {
	case status == http.StatusOK:
		fmt.Fprintf(rw, `{"project_id":%q,"status":"ok"}`, rrProject)
	case w.echo:
		w.echoed++
		fmt.Fprintf(rw, `{"message":"token %s refused"}`, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	default:
		fmt.Fprint(rw, `{"message":"scripted answer"}`)
	}
}

// adminDo sends one request as the sandbox admin, as a process outside the check would (a run
// that died after it).
func (w *rrWorld) adminDo(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, w.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rrAdminTok)
	resp, err := w.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (w *rrWorld) admin(rw http.ResponseWriter, r *http.Request, body []byte) {
	p := r.URL.Path
	notFound := func() {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"not found"}`)
	}
	const cl, pol = "/v1/me/api/oauth2/client", "/v2/iam/policy"
	switch {
	case r.Method == http.MethodGet && p == cl:
		ids := []string{}
		for _, c := range w.clients {
			ids = append(ids, c.id)
		}
		json.NewEncoder(rw).Encode(ids)
	case r.Method == http.MethodGet && strings.HasPrefix(p, cl+"/"):
		id := strings.TrimPrefix(p, cl+"/")
		i := slices.IndexFunc(w.clients, func(c rrClient) bool { return c.id == id })
		if i < 0 {
			notFound()
			return
		}
		c := w.clients[i]
		json.NewEncoder(rw).Encode(map[string]any{"clientId": c.id, "name": c.name, "description": c.name,
			"flow": "CLIENT_CREDENTIALS", "callbackUrls": []string{}, "identity": c.identity})
	case r.Method == http.MethodPost && p == cl:
		var req struct {
			Flow string `json:"flow"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &req); err != nil || req.Name == "" || req.Flow != "CLIENT_CREDENTIALS" {
			rw.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(rw, `{"message":"invalid client request"}`)
			return
		}
		c := w.addClient(req.Name)
		if w.createLost {
			rw.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(rw).Encode(map[string]string{"clientId": c.id, "clientSecret": c.secret})
	case r.Method == http.MethodDelete && strings.HasPrefix(p, cl+"/"):
		id := strings.TrimPrefix(p, cl+"/")
		i := slices.IndexFunc(w.clients, func(c rrClient) bool { return c.id == id })
		if i < 0 {
			notFound()
			return
		}
		if w.deleteFail != 0 {
			rw.WriteHeader(w.deleteFail)
			fmt.Fprint(rw, `{"message":"scripted delete failure"}`)
			w.deleteFail = 0
			return
		}
		if w.clients[i].name == rrName {
			w.deleted = true
		}
		if !w.keep {
			w.clients = slices.Delete(w.clients, i, i+1)
		}
		fmt.Fprint(rw, `null`)
	case r.Method == http.MethodGet && p == pol:
		out := []map[string]any{}
		for _, q := range w.policies {
			out = append(out, map[string]any{"id": q.id, "name": q.name, "identities": q.identities, "readOnly": false})
		}
		if w.pageSize > 0 {
			start, _ := strconv.Atoi(r.Header.Get("X-Pagination-Cursor"))
			start = min(start, len(out))
			end := min(start+w.pageSize, len(out))
			if end < len(out) {
				rw.Header().Set("X-Pagination-Cursor-Next", strconv.Itoa(end))
			}
			out = out[start:end]
		}
		json.NewEncoder(rw).Encode(out)
	case r.Method == http.MethodPost && p == pol:
		var req struct {
			Name        string   `json:"name"`
			Identities  []string `json:"identities"`
			Resources   []struct{ URN string }
			Permissions struct {
				Allow  []struct{ Action string }
				Except []struct{ Action string }
				Deny   []struct{ Action string }
			}
		}
		if err := json.Unmarshal(body, &req); err != nil || req.Name == "" {
			rw.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(rw, `{"message":"invalid policy"}`)
			return
		}
		q := w.addPolicy(req.Name, req.Identities...)
		pm := req.Permissions
		w.policies[len(w.policies)-1].grants = len(req.Resources) == 1 && req.Resources[0].URN == rrProjectURN &&
			len(pm.Allow) == 1 && pm.Allow[0].Action == rrAction && len(pm.Except) == 0 && len(pm.Deny) == 0
		json.NewEncoder(rw).Encode(map[string]any{"id": q.id, "name": q.name})
	case r.Method == http.MethodDelete && strings.HasPrefix(p, pol+"/"):
		id := strings.TrimPrefix(p, pol+"/")
		i := slices.IndexFunc(w.policies, func(q rrPolicy) bool { return q.id == id })
		if i < 0 {
			notFound()
			return
		}
		if !w.keep {
			w.policies = slices.Delete(w.policies, i, i+1)
		}
		fmt.Fprint(rw, `null`)
	default:
		rw.WriteHeader(http.StatusForbidden)
		fmt.Fprint(rw, `{"message":"This call has not been granted"}`)
	}
}

// rrPoll and rrRecord read observations.json as T032 writes it (the JSON names are the contract;
// fields T032 adds are ignored).
type rrPoll struct {
	At     time.Time `json:"at"`
	Status int       `json:"status"`
	Outage bool      `json:"outage"`
}

type rrRecord struct {
	Check     string          `json:"check"`
	RunID     string          `json:"run_id"`
	IssuedAt  time.Time       `json:"issued_at"`
	ExpiresIn json.RawMessage `json:"expires_in"`
	DeletedAt *time.Time      `json:"deleted_at"`
	Control   []rrPoll        `json:"control"`
	Polls     []rrPoll        `json:"polls"`
	Observed  string          `json:"observed"`
	Reason    string          `json:"reason"`
	Lower     int64           `json:"lower_seconds"`
	Upper     int64           `json:"upper_seconds"`
}

// rrTrace is what the fake saw of one Observe: the probe token requests, the probe reads before
// and after the client delete, and the delete itself.
type rrTrace struct {
	reqs      []rrReq
	tokenReqs []int // indexes of probe token requests
	controls  []rrReq
	polls     []rrReq
	del       int // index of the first DELETE of a client of ours; -1: none
	delAt     time.Time
}

func (w *rrWorld) trace() rrTrace {
	reqs := w.seen()
	tr := rrTrace{reqs: reqs, del: -1}
	for i, r := range reqs {
		switch {
		case r.who == "probe" && r.method == "TOKEN":
			tr.tokenReqs = append(tr.tokenReqs, i)
		case r.who == "admin" && r.method == http.MethodDelete && strings.HasPrefix(r.path, "/v1/me/api/oauth2/client/") && tr.del < 0:
			tr.del, tr.delAt = i, r.at
		case r.who == "probe" && r.method == http.MethodGet && r.path == "/v1"+rrReadPath:
			if tr.del < 0 {
				tr.controls = append(tr.controls, r)
			} else {
				tr.polls = append(tr.polls, r)
			}
		}
	}
	return tr
}

func rrParse(t *testing.T, raw []byte, err error) rrRecord {
	t.Helper()
	if raw == nil {
		t.Fatalf("Observe wrote no record (err %v)", err)
	}
	var rec rrRecord
	if jerr := json.Unmarshal(raw, &rec); jerr != nil {
		t.Fatalf("record does not decode: %v", jerr)
	}
	if rec.Check != "token-residual" || rec.RunID != rrRun {
		t.Errorf("record check %q run_id %q, want token-residual and %s", rec.Check, rec.RunID, rrRun)
	}
	if !slices.Contains([]string{"refused-before-expiry", "valid-until-expiry", "blocked"}, rec.Observed) {
		t.Errorf("observed %q is outside the check's set", rec.Observed)
	}
	return rec
}

func rrObserve(t *testing.T, w *rrWorld, o Options) (raw []byte, term string, err error) {
	t.Helper()
	var buf bytes.Buffer
	raw, err = TokenResidual(o).Observe(w.ctx(t), rrEnv(), &buf)
	if leaks := w.leaks(string(raw), buf.String(), fmt.Sprint(err)); len(leaks) > 0 {
		t.Errorf("record, terminal or error carry %q", leaks)
	}
	return raw, buf.String(), err
}

// wantOneToken: exactly one probe token, issued before the delete, and every probe read uses it.
func wantOneToken(t *testing.T, w *rrWorld, tr rrTrace) {
	t.Helper()
	if len(tr.tokenReqs) != 1 {
		t.Fatalf("probe token requests = %d, want exactly one (reqs %v)", len(tr.tokenReqs), tr.reqs)
	}
	if tr.del >= 0 && tr.tokenReqs[0] > tr.del {
		t.Errorf("the token was issued after the client delete")
	}
	w.mu.Lock()
	tok := w.tokens[0]
	w.mu.Unlock()
	for _, r := range append(slices.Clone(tr.controls), tr.polls...) {
		if r.tok != tok {
			t.Errorf("read at %s used another token than the one issued", r.at.Format(time.RFC3339))
		}
	}
}

// wantOrder: the requests hold want, in order, as a subsequence ("who METHOD path", a path ending
// in "/" matching any id under it). Requests in between (such as reading the created client's
// identity) are T032's choice.
func wantOrder(t *testing.T, reqs []rrReq, want []string) {
	t.Helper()
	i := 0
	for _, r := range reqs {
		if i == len(want) {
			break
		}
		w := want[i]
		if got := r.String(); got == w || strings.HasSuffix(w, "/") && strings.HasPrefix(got, w) {
			i++
		}
	}
	if i < len(want) {
		t.Fatalf("requests miss %q (step %d of %d, in order): %v", want[i], i+1, len(want), reqs)
	}
}

// wantRecorded: the record's control and polls are exactly the reads the fake answered, in order,
// each with its time, status and outage flag (an answer other than 2xx, 401 or 403).
func wantRecorded(t *testing.T, rec rrRecord, tr rrTrace) {
	t.Helper()
	cmp := func(kind string, got []rrPoll, want []rrReq) {
		if len(got) != len(want) {
			t.Errorf("record %s has %d entries, the fake answered %d reads", kind, len(got), len(want))
			return
		}
		for i, r := range want {
			outage := !(r.status >= 200 && r.status <= 299 || r.status == 401 || r.status == 403)
			if g := got[i]; !g.At.Equal(r.at) || g.Status != r.status || g.Outage != outage {
				t.Errorf("record %s[%d] = %+v, want at %s status %d outage %v", kind, i, g, r.at.Format(time.RFC3339), r.status, outage)
			}
		}
	}
	cmp("control", rec.Control, tr.controls)
	cmp("polls", rec.Polls, tr.polls)
}

// wantBounded: no poll later than the expiry plus one interval.
func wantBounded(t *testing.T, w *rrWorld, tr rrTrace) {
	t.Helper()
	w.mu.Lock()
	exp := w.expiry
	w.mu.Unlock()
	for _, p := range tr.polls {
		if p.at.After(exp.Add(rrInterval)) {
			t.Errorf("poll at expiry%+v, later than the expiry plus one interval", p.at.Sub(exp))
			return
		}
	}
}

func secs(d time.Duration) int64 { return int64(d / time.Second) }

func TestResidualWindow(t *testing.T) {
	t0 := time.Date(2026, 10, 16, 11, 20, 0, 0, time.UTC)
	exp := 10 * time.Minute
	deleted := t0.Add(time.Minute)
	expiry := t0.Add(exp) // deleted + 9m
	ok := Poll{At: t0.Add(30 * time.Second), Status: 200}
	p := func(after time.Duration, status int) Poll { return Poll{At: deleted.Add(after), Status: status} }
	pe := func(after time.Duration) Poll {
		return Poll{At: deleted.Add(after), Err: errors.New("connection reset")}
	}
	// allowed: 200 every 30 s from 30 s up to and including last.
	allowed := func(last time.Duration) []Poll {
		var out []Poll
		for d := 30 * time.Second; d <= last; d += 30 * time.Second {
			out = append(out, p(d, 200))
		}
		return out
	}
	untilExp := expiry.Sub(deleted) // 9m
	cases := []struct {
		name     string
		exp      time.Duration
		control  Poll
		polls    []Poll
		deadline bool
		want     Residual
		exposes  string
	}{
		{"refused 401 after three allowed", exp, ok, append(allowed(90*time.Second), p(120*time.Second, 401)), false,
			Residual{"refused-before-expiry", "", 90 * time.Second, 120 * time.Second, exp}, "a window reported as one point or as revocation at delete"},
		{"refused 403 after three allowed", exp, ok, append(allowed(90*time.Second), p(120*time.Second, 403)), false,
			Residual{"refused-before-expiry", "", 90 * time.Second, 120 * time.Second, exp}, "403 not read as a refusal"},
		{"refused at the first poll", exp, ok, []Poll{p(30*time.Second, 401)}, false,
			Residual{"refused-before-expiry", "", 0, 30 * time.Second, exp}, "a lower bound invented without an allowed poll"},
		{"refused exactly at the expiry", exp, ok, append(allowed(untilExp-30*time.Second), p(untilExp, 401)), false,
			Residual{"valid-until-expiry", "", untilExp - 30*time.Second, untilExp, exp}, "an off-by-one at the boundary"},
		{"refused a second before the expiry", exp, ok, append(allowed(untilExp-30*time.Second), p(untilExp-time.Second, 401)), false,
			Residual{"refused-before-expiry", "", untilExp - 30*time.Second, untilExp - time.Second, exp}, "an off-by-one at the boundary"},
		{"refused after the expiry", exp, ok, append(allowed(untilExp-30*time.Second), p(untilExp+20*time.Second, 401)), false,
			Residual{"valid-until-expiry", "", untilExp - 30*time.Second, untilExp + 20*time.Second, exp}, "an expired token counted as revoked"},
		{"outages between allowed polls", exp, ok, []Poll{p(30*time.Second, 200), p(60*time.Second, 503), p(90*time.Second, 200), pe(120 * time.Second), p(150*time.Second, 401)}, false,
			Residual{"refused-before-expiry", "", 90 * time.Second, 150 * time.Second, exp}, "an outage counted as the refusal, or as validity"},
		{"an outage, then a refusal at the expiry", exp, ok, append(allowed(untilExp-60*time.Second), p(untilExp-30*time.Second, 503), p(untilExp, 401)), false,
			Residual{"blocked", "outages", untilExp - 60*time.Second, 0, exp}, "validity inferred across an unobserved stretch before the expiry"},
		{"only outages after the last allowed poll", exp, ok, append(allowed(2*time.Minute), p(150*time.Second, 503), p(180*time.Second, 502), pe(210*time.Second), p(untilExp+30*time.Second, 503)), false,
			Residual{"blocked", "outages", 2 * time.Minute, 0, exp}, "validity inferred across an unobserved tail"},
		{"allowed after the expiry", exp, ok, append(allowed(untilExp-30*time.Second), p(untilExp+10*time.Second, 200)), false,
			Residual{Observed: "blocked", Reason: "expiry"}, "a token outliving its stated expiry reported as a window"},
		{"allowed after the expiry, then refused", exp, ok, append(allowed(untilExp-30*time.Second), p(untilExp+10*time.Second, 200), p(untilExp+40*time.Second, 401)), false,
			Residual{Observed: "blocked", Reason: "expiry"}, "a token outliving its stated expiry reported as valid-until-expiry"},
		{"deadline while allowed", exp, ok, allowed(4 * time.Minute), true,
			Residual{"blocked", "deadline", 4 * time.Minute, 0, exp}, "a deadline stop read as a result"},
		{"control refused", exp, Poll{At: ok.At, Status: 403}, append(allowed(90*time.Second), p(120*time.Second, 401)), false,
			Residual{Observed: "blocked", Reason: "control"}, "a policy not yet in effect read as revocation"},
		{"control without an answer", exp, Poll{At: ok.At, Err: errors.New("timeout")}, []Poll{p(30*time.Second, 401)}, false,
			Residual{Observed: "blocked", Reason: "control"}, "a control outage read as a working route"},
		{"expires_in zero", 0, ok, []Poll{p(30*time.Second, 401)}, false, Residual{Observed: "blocked", Reason: "expiry"}, "a window judged against an unvalidated expiry"},
		{"expires_in negative", -time.Second, ok, []Poll{p(30*time.Second, 401)}, false, Residual{Observed: "blocked", Reason: "expiry"}, "a window judged against an unvalidated expiry"},
		{"expires_in above 24 h", 24*time.Hour + time.Second, ok, []Poll{p(30*time.Second, 401)}, false, Residual{Observed: "blocked", Reason: "expiry"}, "a window judged against an unvalidated expiry"},
		{"expires_in 24 h exactly (control)", 24 * time.Hour, ok, []Poll{p(30*time.Second, 200), p(60*time.Second, 401)}, false,
			Residual{"refused-before-expiry", "", 30 * time.Second, 60 * time.Second, 24 * time.Hour}, "a valid 24 h lifetime refused"},
	}
	for _, c := range cases {
		got := ResidualWindow(t0, deleted, c.exp, c.control, c.polls, c.deadline)
		if c.want.Reason == "control" || c.want.Reason == "expiry" {
			// expiry: an invalid expires_in, or an allowed poll after the expiry.
			// Bounds are not claimed when the window was never measurable.
			if got.Observed != c.want.Observed || got.Reason != c.want.Reason {
				t.Errorf("%s: got %q/%q, want %q/%q (exposes %s)", c.name, got.Observed, got.Reason, c.want.Observed, c.want.Reason, c.exposes)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v (exposes %s)", c.name, got, c.want, c.exposes)
		}
	}
}

func TestResidualObserve(t *testing.T) {
	t.Run("refused-before-expiry", func(t *testing.T) {
		// Exposes a window reported as a single point, or as revocation at delete time; and
		// a record that cannot be compared with the lifetime later.
		w := newRRWorld(t)
		w.control = []int{403} // the delete comes one interval after the token, so the bounds tell them apart
		w.after = func(n int, at, expiry time.Time) int {
			if n <= 3 {
				return 200
			}
			return 401
		}
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		wantOrder(t, tr.reqs, []string{
			"admin TOKEN /token",
			"admin POST /v1/me/api/oauth2/client",
			"admin POST /v2/iam/policy",
			"probe TOKEN /token",
			"probe GET /v1" + rrReadPath, // the control, refused
			"probe GET /v1" + rrReadPath, // the control, allowed
			"admin DELETE /v1/me/api/oauth2/client/",
			"probe GET /v1" + rrReadPath, // polls
		})
		wantOneToken(t, w, tr)
		if tr.del < 0 || len(tr.polls) < 4 {
			t.Fatalf("delete at %d, %d polls after it; want the client deleted and at least four polls", tr.del, len(tr.polls))
		}
		if err != nil {
			t.Errorf("Observe = %v, want nil", err)
		}
		rec := rrParse(t, raw, err)
		issued := tr.reqs[tr.tokenReqs[0]].at
		if !rec.IssuedAt.Equal(issued) || string(rec.ExpiresIn) != "3599" {
			t.Errorf("issued_at %s expires_in %s, want %s and 3599", rec.IssuedAt, rec.ExpiresIn, issued)
		}
		if rec.DeletedAt == nil || !rec.DeletedAt.Equal(tr.delAt) {
			t.Errorf("deleted_at %v, want %s", rec.DeletedAt, tr.delAt)
		}
		wantBounded(t, w, tr)
		lower, upper := secs(tr.polls[2].at.Sub(tr.delAt)), secs(tr.polls[3].at.Sub(tr.delAt))
		if rec.Observed != "refused-before-expiry" || rec.Lower != lower || rec.Upper != upper || lower >= upper {
			t.Errorf("observed %q lower %d upper %d, want refused-before-expiry, %d and %d", rec.Observed, rec.Lower, rec.Upper, lower, upper)
		}
		wantRecorded(t, rec, tr)
	})

	t.Run("valid-until-expiry", func(t *testing.T) {
		// Exposes a deleted client's token counted as revoked when it simply expired, and
		// unbounded polling.
		w := newRRWorld(t)
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		wantOneToken(t, w, tr)
		if tr.del < 0 || len(tr.polls) == 0 {
			t.Fatalf("no client delete or no poll after it: %v", tr.reqs)
		}
		wantBounded(t, w, tr)
		rec := rrParse(t, raw, err)
		if rec.Observed != "valid-until-expiry" {
			t.Errorf("observed %q, want valid-until-expiry", rec.Observed)
		}
		last := tr.polls[len(tr.polls)-1]
		if w.expiry.After(last.at) {
			t.Errorf("last poll %s before the expiry %s: the refusal was not observed", last.at, w.expiry)
		}
		if len(tr.polls) < 2 {
			t.Fatalf("%d polls after the delete, want allowed polls and then the refusal", len(tr.polls))
		}
		if prev := tr.polls[len(tr.polls)-2]; prev.status != 200 || prev.at.Before(w.expiry.Add(-rrInterval)) {
			t.Errorf("the poll before the refusal (%d at expiry%+v) is not an allowed poll within one interval before the expiry", prev.status, prev.at.Sub(w.expiry))
		}
		wantRecorded(t, rec, tr)
	})

	t.Run("allowed-past-expiry", func(t *testing.T) {
		// Exposes unbounded polling: the read is never refused.
		w := newRRWorld(t)
		w.after = func(int, time.Time, time.Time) int { return 200 }
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		if tr.del < 0 || len(tr.polls) == 0 {
			t.Fatalf("no client delete or no poll after it: %v", tr.reqs)
		}
		wantBounded(t, w, tr)
		if !slices.ContainsFunc(tr.polls, func(p rrReq) bool { return p.status == 200 && p.at.After(w.expiry) }) {
			t.Errorf("no allowed poll after the expiry was observed, so nothing shows the token outlived it")
		}
		if rec := rrParse(t, raw, err); rec.Observed != "blocked" || rec.Reason != "expiry" {
			t.Errorf("observed %q reason %q, want blocked and expiry: the token outlived its stated expiry", rec.Observed, rec.Reason)
		}
	})

	t.Run("outages", func(t *testing.T) {
		// Exposes an outage counted as the refusal (005/T095), or an outage gap counted as validity.
		w := newRRWorld(t)
		w.control = []int{403}
		w.after = func(n int, at, expiry time.Time) int {
			return []int{0, 200, 503, 200, 503, 401}[min(n, 5)]
		}
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		if tr.del < 0 || len(tr.polls) < 5 {
			t.Fatalf("delete at %d, %d polls after it; want at least five", tr.del, len(tr.polls))
		}
		rec := rrParse(t, raw, err)
		wantBounded(t, w, tr)
		lower, upper := secs(tr.polls[2].at.Sub(tr.delAt)), secs(tr.polls[4].at.Sub(tr.delAt))
		if rec.Observed != "refused-before-expiry" || rec.Lower != lower || rec.Upper != upper {
			t.Errorf("observed %q lower %d upper %d, want refused-before-expiry, %d and %d", rec.Observed, rec.Lower, rec.Upper, lower, upper)
		}
		wantRecorded(t, rec, tr)
	})

	t.Run("tail-outages", func(t *testing.T) {
		// Exposes validity inferred across an unobserved tail.
		w := newRRWorld(t)
		w.after = func(n int, at, expiry time.Time) int {
			if n <= 2 {
				return 200
			}
			return 503
		}
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		if tr.del < 0 || len(tr.polls) < 3 {
			t.Fatalf("delete at %d, %d polls after it; want at least three", tr.del, len(tr.polls))
		}
		wantBounded(t, w, tr)
		if last := tr.polls[len(tr.polls)-1]; last.at.Before(w.expiry) {
			t.Errorf("last poll at expiry%+v: stopped before the expiry", last.at.Sub(w.expiry))
		}
		rec := rrParse(t, raw, err)
		wantRecorded(t, rec, tr)
		if lower := secs(tr.polls[1].at.Sub(tr.delAt)); rec.Observed != "blocked" || rec.Reason != "outages" || rec.Lower != lower || rec.Upper != 0 {
			t.Errorf("observed %q reason %q lower %d upper %d, want blocked, outages, %d and 0", rec.Observed, rec.Reason, rec.Lower, rec.Upper, lower)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		// Exposes polling past the deadline, or a deadline stop read as a result.
		w := newRRWorld(t)
		w.deadline = w.start.Add(20 * time.Minute)
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		if tr.del < 0 || len(tr.polls) == 0 {
			t.Fatalf("no client delete or no poll after it: %v", tr.reqs)
		}
		for _, p := range tr.polls {
			if p.at.After(w.deadline) {
				t.Errorf("poll at deadline%+v, after the deadline", p.at.Sub(w.deadline))
			}
		}
		last := tr.polls[len(tr.polls)-1]
		if last.at.Before(w.deadline.Add(-rrInterval)) {
			t.Errorf("last poll at deadline%+v: stopped before the deadline", last.at.Sub(w.deadline))
		}
		rec := rrParse(t, raw, err)
		wantRecorded(t, rec, tr)
		if lower := secs(last.at.Sub(tr.delAt)); rec.Observed != "blocked" || rec.Reason != "deadline" || rec.Lower != lower || rec.Upper != 0 {
			t.Errorf("observed %q reason %q lower %d upper %d, want blocked, deadline, %d and 0", rec.Observed, rec.Reason, rec.Lower, rec.Upper, lower)
		}
	})

	t.Run("expiry-invalid", func(t *testing.T) {
		// Exposes a window judged against an expiry nobody validated.
		for _, e := range []string{"", "0", "-5", "86401", "null", `"3599"`} {
			w := newRRWorld(t)
			w.expiresIn = e
			raw, _, err := rrObserve(t, w, w.options())
			tr := w.trace()
			if len(tr.tokenReqs) != 1 {
				t.Errorf("expires_in %q: %d probe token requests, want one", e, len(tr.tokenReqs))
			}
			if tr.del >= 0 {
				t.Errorf("expires_in %q: the client was deleted (%v), want a stop before the delete", e, tr.reqs[tr.del])
			}
			if raw == nil {
				t.Errorf("expires_in %q: no record (err %v)", e, err)
				continue
			}
			if rec := rrParse(t, raw, err); rec.Observed != "blocked" || rec.Reason != "expiry" {
				t.Errorf("expires_in %q: observed %q reason %q, want blocked and expiry", e, rec.Observed, rec.Reason)
			}
		}
	})

	t.Run("control-settles", func(t *testing.T) {
		// Exposes a run wasted on a policy still propagating.
		w := newRRWorld(t)
		w.control = []int{403, 403}
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		if len(tr.controls) != 3 || tr.del < 0 {
			t.Fatalf("%d control reads, delete at %d; want three (403, 403, 200), then the delete: %v", len(tr.controls), tr.del, tr.reqs)
		}
		wantOneToken(t, w, tr)
		if span := tr.controls[2].at.Sub(tr.controls[0].at); span > rrSettle {
			t.Errorf("control retries spanned %s, beyond Settle %s", span, rrSettle)
		}
		rec := rrParse(t, raw, err)
		var got []int
		for _, c := range rec.Control {
			got = append(got, c.Status)
		}
		if !slices.Equal(got, []int{403, 403, 200}) {
			t.Errorf("record control %v, want each attempt [403 403 200]", got)
		}
		wantRecorded(t, rec, tr)
	})

	t.Run("control-never", func(t *testing.T) {
		// Exposes a policy not yet in effect read as revocation.
		w := newRRWorld(t)
		w.control = slices.Repeat([]int{403}, 1000)
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		if len(tr.controls) < 2 {
			t.Errorf("%d control reads, want retries within Settle", len(tr.controls))
		}
		if tr.del >= 0 {
			t.Errorf("the client was deleted (%v) although the control never succeeded", tr.reqs[tr.del])
		}
		if n := len(tr.controls); n > 0 {
			if span := tr.controls[n-1].at.Sub(tr.controls[0].at); span > rrSettle || span < rrSettle-rrInterval {
				t.Errorf("control retries spanned %s, want Settle %s used up (within one interval)", span, rrSettle)
			}
		}
		if len(tr.controls) == 0 {
			return
		}
		rec := rrParse(t, raw, err)
		wantRecorded(t, rec, tr)
		if rec.Observed != "blocked" || rec.Reason != "control" {
			t.Errorf("observed %q reason %q, want blocked and control", rec.Observed, rec.Reason)
		}
	})

	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("delete-fails-%d", status), func(t *testing.T) {
			// Exposes a window measured without a delete.
			w := newRRWorld(t)
			w.deleteFail = status
			raw, _, err := rrObserve(t, w, w.options())
			tr := w.trace()
			if tr.del < 0 {
				t.Fatalf("no client delete attempted: %v", tr.reqs)
			}
			if err == nil {
				t.Errorf("Observe = nil after the delete answered %d, want an error", status)
			}
			if len(tr.polls) != 0 {
				t.Errorf("%d reads after the failed delete, want none", len(tr.polls))
			}
			var rec rrRecord
			if raw != nil && json.Unmarshal(raw, &rec) == nil && (rec.Observed == "refused-before-expiry" || rec.Observed == "valid-until-expiry") {
				t.Errorf("observed %q after a failed delete, want no window", rec.Observed)
			}
			// Undo still runs and removes the client and the policy.
			if err := TokenResidual(w.options()).Undo(context.Background(), rrEnv(), io.Discard); err != nil {
				t.Errorf("Undo = %v, want nil", err)
			}
			if cl, pol := w.names(); len(cl) != 0 || len(pol) != 0 {
				t.Errorf("after Undo clients %q policies %q remain", cl, pol)
			}
		})
	}
}

// adminOnly: every request of a cleanup is the sandbox admin's.
func adminOnly(t *testing.T, reqs []rrReq) {
	t.Helper()
	for _, r := range reqs {
		if r.who != "admin" {
			t.Errorf("cleanup sent %s: want the sandbox admin only", r)
		}
	}
}

// relisted: both kinds are listed before the first delete and again after the last.
func relisted(t *testing.T, reqs []rrReq) {
	t.Helper()
	first, last := -1, -1
	for i, r := range reqs {
		if r.method == http.MethodDelete {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	for _, path := range []string{"/v1/me/api/oauth2/client", "/v2/iam/policy"} {
		list := func(r rrReq) bool { return r.method == http.MethodGet && r.path == path }
		if i := slices.IndexFunc(reqs, list); i < 0 || first >= 0 && i > first {
			t.Errorf("%s not listed before the first delete: %v", path, reqs)
		}
		if last >= 0 && !slices.ContainsFunc(reqs[last+1:], list) {
			t.Errorf("%s not listed again after the last delete: %v", path, reqs)
		}
	}
}

// wantTried: a cleanup facing a delete that does not take listed, deleted the client and the policy,
// and listed again before it failed.
func wantTried(t *testing.T, reqs []rrReq) {
	t.Helper()
	relisted(t, reqs)
	var cl, pol int
	for _, r := range reqs {
		switch {
		case r.method == http.MethodDelete && strings.HasPrefix(r.path, "/v1/me/api/oauth2/client/"):
			cl++
		case r.method == http.MethodDelete && strings.HasPrefix(r.path, "/v2/iam/policy/"):
			pol++
		}
	}
	if cl == 0 || pol == 0 {
		t.Errorf("deleted %d clients and %d policies, want both tried: %v", cl, pol, reqs)
	}
}

func TestResidualUndo(t *testing.T) {
	t.Run("after-observe", func(t *testing.T) {
		// Exposes a policy left behind, or a foreign client or policy deleted.
		w := newRRWorld(t)
		w.foreign()
		w.after = func(n int, at, expiry time.Time) int { return []int{0, 200, 401}[min(n, 2)] }
		if _, _, err := rrObserve(t, w, w.options()); err != nil {
			t.Logf("Observe = %v", err)
		}
		before := len(w.seen())
		var term bytes.Buffer
		err := TokenResidual(w.options()).Undo(context.Background(), rrEnv(), &term)
		undo := w.seen()[before:]
		adminOnly(t, undo)
		relisted(t, undo)
		if err != nil {
			t.Errorf("Undo = %v, want nil", err)
		}
		cl, pol := w.names()
		if want := slices.Sorted(slices.Values(rrForeign)); !slices.Equal(cl, want) || !slices.Equal(pol, want) {
			t.Errorf("after Undo clients %q policies %q, want only the foreign %q", cl, pol, want)
		}
		if leaks := w.leaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 {
			t.Errorf("terminal or error carry %q", leaks)
		}
	})

	t.Run("still-listed", func(t *testing.T) {
		// Exposes an Undo that trusts an acknowledged delete.
		w := newRRWorld(t)
		c := w.addClient(rrName)
		w.addPolicy(rrName, c.identity)
		w.keep = true
		before := len(w.seen())
		err := TokenResidual(w.options()).Undo(context.Background(), rrEnv(), io.Discard)
		wantTried(t, w.seen()[before:])
		if err == nil || !strings.Contains(err.Error(), rrName) {
			t.Errorf("Undo = %v, want an error naming what is left (%s)", err, rrName)
		}
	})
}

func TestResidualSweep(t *testing.T) {
	// Sweep has the signature of ProbeCheck's Sweep field (data-model.md; the field lands in T038).
	var _ func(context.Context, string, string, io.Writer) error = Options{}.sweep

	foreign := slices.Sorted(slices.Values(rrForeign))
	sweep := func(t *testing.T, w *rrWorld) ([]rrReq, string, error) {
		t.Helper()
		before := len(w.seen())
		var term bytes.Buffer
		// A fresh process: new Options, only the run id and the project.
		err := w.options().sweep(context.Background(), rrRun, rrProject, &term)
		reqs := w.seen()[before:]
		adminOnly(t, reqs)
		if leaks := w.leaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 {
			t.Errorf("terminal or error carry %q", leaks)
		}
		return reqs, term.String(), err
	}
	wantClean := func(t *testing.T, w *rrWorld, reqs []rrReq, err error, deletes int) {
		t.Helper()
		var got int
		for _, r := range reqs {
			if r.method == http.MethodDelete {
				got++
			}
		}
		relisted(t, reqs)
		if err != nil {
			t.Errorf("sweep = %v, want nil", err)
		}
		if got != deletes {
			t.Errorf("sweep sent %d deletes, want %d: %v", got, deletes, reqs)
		}
		if cl, pol := w.names(); !slices.Equal(cl, foreign) || !slices.Equal(pol, foreign) {
			t.Errorf("after sweep clients %q policies %q, want only the foreign %q", cl, pol, foreign)
		}
	}

	t.Run("client-only", func(t *testing.T) {
		// Exposes an orphan client: created, and the policy not.
		w := newRRWorld(t)
		w.foreign()
		w.addClient(rrName)
		reqs, _, err := sweep(t, w)
		wantClean(t, w, reqs, err, 1)
	})

	t.Run("policy-only", func(t *testing.T) {
		// Exposes an orphan policy: created, and the client not (or already deleted). The listing
		// comes in pages of one, ours last.
		w := newRRWorld(t)
		w.foreign()
		w.addPolicy(rrName, "urn:v1:eu:identity:credential:"+rrAccount+"/oauth2-EU.gone")
		w.pageSize = 1
		reqs, _, err := sweep(t, w)
		wantClean(t, w, reqs, err, 1)
	})

	t.Run("create-answer-lost", func(t *testing.T) {
		// Exposes a client nobody knows the id of: the fake created it, then answered 502 to the
		// process that asked, which then died (no Observe of this check, no output).
		w := newRRWorld(t)
		w.foreign()
		w.createLost = true
		if st, _ := w.adminDo(t, http.MethodPost, "/v1/me/api/oauth2/client", `{"callbackUrls":[],"flow":"CLIENT_CREDENTIALS","name":"`+rrName+`"}`); st != http.StatusBadGateway {
			t.Fatalf("precondition: the create answered %d, want 502", st)
		}
		if cl, _ := w.names(); !slices.Contains(cl, rrName) {
			t.Fatalf("precondition: the fake did not create the client: %q", cl)
		}
		reqs, _, err := sweep(t, w)
		wantClean(t, w, reqs, err, 1)
	})

	t.Run("client-deleted-by-observe", func(t *testing.T) {
		// Exposes a sweep that fails on the client Observe already deleted, or leaves the policy:
		// the client and its policy were created and the client deleted, as Observe does before
		// it polls, and the process died there.
		w := newRRWorld(t)
		w.foreign()
		c := w.addClient(rrName)
		w.addPolicy(rrName, c.identity)
		if st, _ := w.adminDo(t, http.MethodDelete, "/v1/me/api/oauth2/client/"+c.id, ""); st != http.StatusOK {
			t.Fatalf("precondition: the client delete answered %d", st)
		}
		reqs, _, err := sweep(t, w)
		wantClean(t, w, reqs, err, 1)
	})

	t.Run("still-listed", func(t *testing.T) {
		// Exposes a sweep that trusts an acknowledged delete.
		w := newRRWorld(t)
		c := w.addClient(rrName)
		w.addPolicy(rrName, c.identity)
		w.keep = true
		reqs, _, err := sweep(t, w)
		wantTried(t, reqs)
		if err == nil || !strings.Contains(err.Error(), rrName) {
			t.Errorf("sweep = %v, want an error naming what is left (%s)", err, rrName)
		}
	})

	t.Run("bad-run-id", func(t *testing.T) {
		// Control (passes on the stub too): a sweep refuses a run id it cannot derive the exact
		// name from, before any request.
		for _, run := range []string{"lzprobe", strings.ToLower(rrRun), rrRun + "/x"} {
			w := newRRWorld(t)
			if err := w.options().sweep(context.Background(), run, rrProject, io.Discard); err == nil || len(w.seen()) != 0 {
				t.Errorf("sweep(%q) = %v after %v, want a refusal before any request", run, err, w.seen())
			}
		}
	})
}

// TestResidualValidate: Validate refuses published variables the check cannot use, before any
// request, and the check publishes the data-model's output and public keys.
func TestResidualValidate(t *testing.T) {
	c := TokenResidual(Options{})
	if c.Output != "residual_check" {
		t.Errorf("Output = %q, want residual_check", c.Output)
	}
	if want := []string{"project_id", "read_path", "run_id"}; !slices.Equal(slices.Sorted(slices.Values(c.Public)), want) {
		t.Errorf("Public = %q, want %q", c.Public, want)
	}
	if err := c.Validate(rrEnv()); err != nil {
		t.Errorf("Validate refused the run's own variables: %v", err)
	}
	edit := func(k, v string) map[string]string {
		e := rrEnv()
		if v == "" {
			delete(e, k)
		} else {
			e[k] = v
		}
		return e
	}
	type badEnv struct {
		name string
		env  map[string]string
	}
	var bad []badEnv
	for _, k := range slices.Sorted(maps.Keys(rrEnv())) {
		bad = append(bad, badEnv{"missing " + k, edit(k, "")})
	}
	bad = append(bad, []badEnv{
		{"run_id not a run id", edit("run_id", "lzprobe-token-residual")},
		{"run_id lower case", edit("run_id", strings.ToLower(rrRun))},
		{"project_id not a project", edit("project_id", "../me")},
		{"read_path of another project", edit("read_path", "/cloud/project/fedcba9876543210fedcba9876543210")},
		{"read_path outside the project", edit("read_path", "/me")},
		{"read_path climbing out", edit("read_path", rrReadPath+"/../../../me")},
		{"read_path with a query", edit("read_path", rrReadPath+"?x=1")},
		{"read_path an absolute URL", edit("read_path", "https://example.com"+rrReadPath)},
	}...)
	for _, b := range bad {
		w := newRRWorld(t)
		c := TokenResidual(w.options())
		if err := c.Validate(b.env); err == nil {
			t.Errorf("%s: Validate accepted it (exposes a check that reads, creates or deletes for a run it cannot name)", b.name)
		}
		if _, err := c.Observe(context.Background(), b.env, io.Discard); err == nil || len(w.seen()) != 0 {
			t.Errorf("%s: Observe = %v after %v, want a refusal before any request", b.name, err, w.seen())
		}
		if err := c.Undo(context.Background(), b.env, io.Discard); err == nil || len(w.seen()) != 0 {
			t.Errorf("%s: Undo = %v after %v, want a refusal before any request", b.name, err, w.seen())
		}
	}
}

// TestResidualLeak: a refused read whose body echoes the token, and the created client's secret,
// reach neither the record, the terminal nor the error. The guard is shown first on a leaking
// mutant, then trusted on the check.
func TestResidualLeak(t *testing.T) {
	w := newRRWorld(t)
	// The leaking mutant: a check that copies the API's answers into its record and terminal.
	mutant := &live.ProbeCheck{Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
		_, b := w.adminDo(t, http.MethodPost, "/v1/me/api/oauth2/client", `{"callbackUrls":[],"flow":"CLIENT_CREDENTIALS","name":"`+rrName+`"}`)
		fmt.Fprintln(term, b)
		return []byte(b), nil
	}}
	var term bytes.Buffer
	raw, _ := mutant.Observe(context.Background(), rrEnv(), &term)
	if leaks := w.leaks(string(raw)); len(leaks) == 0 {
		t.Fatalf("the leak guard missed the mutant's record %s", raw)
	}
	if leaks := w.leaks(term.String()); len(leaks) == 0 {
		t.Fatalf("the leak guard missed the mutant's terminal %s", term.String())
	}

	// The check: the control's refusals and the polls' refusal echo the token.
	w = newRRWorld(t)
	w.echo = true
	w.control = []int{403}
	w.after = func(n int, at, expiry time.Time) int { return []int{0, 200, 401}[min(n, 2)] }
	rrObserve(t, w, w.options()) // fails on any leak
	w.mu.Lock()
	echoed := w.echoed
	w.mu.Unlock()
	if echoed < 2 {
		t.Errorf("%d echoing refusals were read, want the control's and the poll's (else the guard proves nothing)", echoed)
	}
}

// TestResidualFakes: the fakes answer as the cases say (fixture control; passes on the stub by
// design).
func TestResidualFakes(t *testing.T) {
	ctx := context.Background()
	w := newRRWorld(t)
	client := w.srv.Client()
	do := func(method, path, tok, body string, hdr ...string) (int, http.Header, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, w.srv.URL+path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(b)
	}
	grant := func(id, secret string) (int, string) {
		st, _, b := do(http.MethodPost, "/token", "", url.Values{"grant_type": {"client_credentials"}, "client_id": {id}, "client_secret": {secret}}.Encode())
		return st, b
	}
	if st, b := grant(rrAdminID, rrAdminSK); st != 200 || !strings.Contains(b, rrAdminTok) {
		t.Fatalf("admin grant = %d %s", st, b)
	}
	st, _, b := do(http.MethodPost, "/v1/me/api/oauth2/client", rrAdminTok, `{"callbackUrls":[],"flow":"CLIENT_CREDENTIALS","name":"`+rrName+`","description":"x"}`)
	var created struct{ ClientID, ClientSecret string }
	if json.Unmarshal([]byte(b), &created); st != 200 || created.ClientID == "" || created.ClientSecret == "" {
		t.Fatalf("create client = %d %s", st, b)
	}
	_, _, b = do(http.MethodGet, "/v1/me/api/oauth2/client/"+created.ClientID, rrAdminTok, "")
	var cl struct{ Identity string }
	json.Unmarshal([]byte(b), &cl)
	st, tb := grant(created.ClientID, created.ClientSecret)
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if json.Unmarshal([]byte(tb), &tok); st != 200 || tok.AccessToken == "" || tok.ExpiresIn != 3599 {
		t.Fatalf("probe grant = %d %s", st, tb)
	}
	if st, _, _ := do(http.MethodGet, "/v1"+rrReadPath, tok.AccessToken, ""); st != 403 {
		t.Errorf("read without a policy = %d, want 403", st)
	}
	if st, _, _ := do(http.MethodPost, "/v2/iam/policy", rrAdminTok, `{"name":"`+rrName+`","identities":["`+cl.Identity+`"],"resources":[{"urn":"`+rrProjectURN+`"}],"permissions":{"allow":[{"action":"*"}]}}`); st != 200 {
		t.Errorf("create policy = %d", st)
	}
	if st, _, _ := do(http.MethodGet, "/v1"+rrReadPath, tok.AccessToken, ""); st != 403 {
		t.Errorf("read under a wildcard policy = %d, want 403 (only the read's action grants it)", st)
	}
	if st, _, _ := do(http.MethodPost, "/v2/iam/policy", rrAdminTok, `{"name":"`+rrName+`","identities":["`+cl.Identity+`"],"resources":[{"urn":"`+rrProjectURN+`"}],"permissions":{"allow":[{"action":"`+rrAction+`"}]}}`); st != 200 {
		t.Errorf("create policy = %d", st)
	}
	w.control = []int{403}
	if st, _, _ := do(http.MethodGet, "/v1"+rrReadPath, tok.AccessToken, ""); st != 403 {
		t.Errorf("scripted control = %d, want 403", st)
	}
	if st, _, _ := do(http.MethodGet, "/v1"+rrReadPath, tok.AccessToken, ""); st != 200 {
		t.Errorf("control after the script = %d, want 200", st)
	}
	if st, _, _ := do(http.MethodGet, "/v1/me", tok.AccessToken, ""); st != 403 {
		t.Errorf("an ungranted read = %d, want 403", st)
	}
	if st, _, _ := do(http.MethodDelete, "/v1/me/api/oauth2/client/"+created.ClientID, rrAdminTok, ""); st != 200 {
		t.Errorf("delete client = %d", st)
	}
	if st, _ := grant(created.ClientID, created.ClientSecret); st != 401 {
		t.Errorf("a grant for the deleted client = %d, want 401", st)
	}
	if st, _, _ := do(http.MethodGet, "/v1"+rrReadPath, tok.AccessToken, ""); st != 200 {
		t.Errorf("the issued token before its expiry = %d, want 200 (the default world)", st)
	}
	if err := w.sleep(ctx, 3600*time.Second); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	if st, _, _ := do(http.MethodGet, "/v1"+rrReadPath, tok.AccessToken, ""); st != 401 {
		t.Errorf("the issued token after its expiry = %d, want 401", st)
	}
	// Pagination: pages of one, the cursor in X-Pagination-Cursor-Next.
	w.foreign()
	w.pageSize = 1
	var names []string
	for cur, i := "", 0; i < 20; i++ {
		_, h, b := do(http.MethodGet, "/v2/iam/policy", rrAdminTok, "", "X-Pagination-Cursor", cur)
		var page []struct{ Name string }
		json.Unmarshal([]byte(b), &page)
		for _, p := range page {
			names = append(names, p.Name)
		}
		if cur = h.Get("X-Pagination-Cursor-Next"); cur == "" {
			break
		}
	}
	if len(names) != len(rrForeign)+2 {
		t.Errorf("paged policies %q, want %d", names, len(rrForeign)+2)
	}
	// The fake deadline: a sleep past it returns context.DeadlineExceeded at the deadline.
	w2 := newRRWorld(t)
	w2.deadline = w2.start.Add(time.Minute)
	if err := w2.sleep(ctx, 2*time.Minute); !errors.Is(err, context.DeadlineExceeded) || !w2.now().Equal(w2.deadline) {
		t.Errorf("sleep past the deadline = %v at %s, want DeadlineExceeded at the deadline", err, w2.now())
	}
}
