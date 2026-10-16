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

// T033: the iam-urn check (T034) against a fake OVHcloud API (OAuth2 token endpoint,
// /v1/me/api/oauth2/client, /v2/iam/policy, /v2/iam/resource and the two controls), a fake clock
// and a fake sleep that advances it. Which URN OVHcloud checks an `account:apiovh:iam/*` action
// against is UNVERIFIED (D109 and 005/T049 run 3 saw `account:apiovh:iam/resource/edit` granted on
// a project URN refused); the fakes answer every world the live run (T027) may meet. The client and
// policy shapes follow tools/internal/live/bootstrap.go (createAdmin) and residual_test.go's fake.
// No test calls OVHcloud or reads ~/.config/ovh-lz/.
//
// What the tests fix for T034, beyond data-model.md:
//   - The probed read is `account:apiovh:iam/policy/get`, GET /v2/iam/policy/{policyId} on the
//     identity's own policy; the probed write is `account:apiovh:iam/resource/edit`,
//     PUT /v2/iam/resource/{resourceURN} on bucket_urn with a body whose tags are the bucket's tags
//     as the admin's listing showed them plus `lzprobe-iam-urn:run-id` = the run id (kb
//     api/v2/iam.json, iamActions; iam.resource.Resource.tags). The controls: the project identity
//     GET /v1/cloud/project/{id} (`publicCloudProject:apiovh:get`, kb api/v1/cloud.json), the
//     account identity GET /v1/me (`account:apiovh:me/get`, observed by spec 005).
//   - Each identity's policy names its client's identity and allows exactly the read, the write
//     and its control on exactly its URN (project_urn or account_urn), with no except or deny.
//   - Before any write the admin lists the bucket as an IAM resource (GET /v2/iam/resource); the
//     write goes only to a listed resource named `bucket` whose URN is bucket_urn.
//   - The record is observations.json with check "iam-urn", run_id, attempts (each {identity
//     "project"|"account", call "control"|"probe", action, at, status, actions, outage,
//     policy_age_seconds}) one to one with the calls made, and results (each {action, observed,
//     reason}), one per probed action. reason (blocked only), in this order: "propagation" (a
//     control that never succeeded within Settle, its last answer a refusal) or "outage" (its last
//     answer none, or other than 2xx, 401 or 403; it wins when the other control's last answer is
//     a refusal); "no owned target"; "outage" (a probed call with
//     no answer, or other than 2xx, 401 or 403); "refused" (both probed calls refused naming the
//     action); "unattributable" (any other refusal, 401 included, or one side named and the other
//     not).
//   - A control is retried, Interval apart, on any answer other than 2xx until Settle is used up.
//   - policy_age_seconds is the seconds from the policy's createdAt (the create answer and the
//     listing) to the call.
//   - A 403 is an attributable denial only when it names the probed action; one naming nothing is
//     not (a URN is claimed only from a refusal the action is named in).
//   - Validate: run_id a run id; project_urn `urn:v1:eu:resource:publicCloudProject:<32 hex>`;
//     account_urn `urn:v1:eu:resource:account:<nic>`; bucket exactly lzprobe-iam-urn-<lower(run
//     id)>; bucket_urn a resource URN without `*`, other than project_urn and account_urn, whose
//     last `:` or `/` segment is the bucket. The bucket URN's form is UNVERIFIED (the fake uses
//     urn:v1:eu:resource:storageS3:<project>/<bucket>), so Validate does not check its type or
//     project: the admin's listing (a resource named `bucket` whose URN is bucket_urn) is the guard.

const (
	uuRun        = "20261016T121500Z-5e6f"
	uuOtherRun   = "20261015T090000Z-9f8e"
	uuProject    = "0123456789abcdef0123456789abcdef"
	uuAccount    = "xx2222-ovh"
	uuProjectURN = "urn:v1:eu:resource:publicCloudProject:" + uuProject
	uuAccountURN = "urn:v1:eu:resource:account:" + uuAccount
	uuBucket     = "lzprobe-iam-urn-20261016t121500z-5e6f" // lower(run id)
	uuBucketURN  = "urn:v1:eu:resource:storageS3:" + uuProject + "/" + uuBucket
	uuOtherBkt   = "lzprobe-iam-urn-20261015t090000z-9f8e"
	uuNameP      = "lzprobe-iam-urn-" + uuRun + "-project"
	uuNameA      = "lzprobe-iam-urn-" + uuRun + "-account"
	uuTagKey     = "lzprobe-iam-urn:run-id"
	uuRead       = "account:apiovh:iam/policy/get"
	uuWrite      = "account:apiovh:iam/resource/edit"
	uuCtlP       = "publicCloudProject:apiovh:get"
	uuCtlA       = "account:apiovh:me/get"
	uuAdminID    = "EU.urnsweepadmin"
	uuAdminSK    = "Pw4eRt8yUi2oPa6sDf0g"
	uuAdminTok   = "tok-uuadmin-Kl7m"
	uuInterval   = 30 * time.Second
	uuSettle     = 2 * time.Minute
)

// uuForeign are clients and policies the check must never touch: another run's, a longer name with
// ours as its prefix, a name differing only in case, the run's bare prefix and the sandbox admin.
var uuForeign = []string{"lzprobe-iam-urn-" + uuOtherRun + "-project", uuNameP + "-x", strings.ToLower(uuNameA), "lzprobe-iam-urn-" + uuRun, "lz-sandbox-admin"}

// The tags the root's resources carry before the run; the check adds and removes only its key.
var (
	uuBucketTags  = map[string]string{"lz:tenant": "lzprobe"}
	uuProjectTags = map[string]string{"lz:managed-in": "account-governance"}
	uuOtherTags   = map[string]string{uuTagKey: uuOtherRun}
)

func uuEnv() map[string]string {
	return map[string]string{"run_id": uuRun, "project_urn": uuProjectURN, "account_urn": uuAccountURN, "bucket": uuBucket, "bucket_urn": uuBucketURN}
}

// uuIdent is what the fake knows of each probe identity: its client and policy name, the URN its
// policy must name, and its control.
var uuIdent = map[string]struct{ name, urn, ctl, ctlPath string }{
	"project": {uuNameP, uuProjectURN, uuCtlP, "/v1/cloud/project/" + uuProject},
	"account": {uuNameA, uuAccountURN, uuCtlA, "/v1/me"},
}

type uuReq struct {
	who, method, path string // who: admin, project, account, probe (another client) or ?
	name              string // a create's name
	at                time.Time
	status            int      // 0: no answer (hung up)
	actions           []string // the actions a refusal named
}

func (r uuReq) String() string { return r.who + " " + r.method + " " + r.path }

type uuPolicy struct {
	id, name   string
	identities []string
	resources  []string
	allow      []string
	other      int // except and deny entries
}

type uuRes struct {
	urn, name string
	tags      map[string]string
	listed    bool
}

// uuAns is a scripted answer to a probed call.
type uuAns struct {
	status  int
	actions []string // the actions a 403 names
	hangup  bool     // close the connection without an answer
}

type uuWorld struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	start     time.Time
	clock     time.Time
	sleeps    int
	reqs      []uuReq
	clients   []rrClient
	policies  []uuPolicy
	created   map[string]time.Time // policy name → createdAt (kept after a delete)
	resources []uuRes
	// buckets (T034): the project's buckets in GRA, as the S3 bucket API lists them (the probe
	// bucket's create answer may have been lost); none unless a case adds them.
	buckets []string
	n       int
	tokens  map[string]string // probe token → client id
	secrets []string

	control    map[string][]int // identity → control statuses once granted, in turn (0: hang up); then 200
	answer     map[string]uuAns // "project read", "account write", …; missing: 200
	clientLost bool             // POST client creates, then answers 502
	policyLost bool             // POST policy creates, then answers 502
	keep       bool             // deletes are acknowledged but not effective
	keepTags   bool             // tag removals are acknowledged but not effective
	pageSize   int              // > 0: GET /v2/iam/policy answers pages of this size
	echo       bool             // a refusal's body echoes the bearer token
	echoed     int
	stray      int // probe calls outside the grant: a write elsewhere, or with other tags
}

func newUUWorld(t *testing.T) *uuWorld {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	w := &uuWorld{t: t, start: now, clock: now, created: map[string]time.Time{}, tokens: map[string]string{},
		control: map[string][]int{}, answer: map[string]uuAns{}}
	w.resources = []uuRes{
		{uuBucketURN, uuBucket, maps.Clone(uuBucketTags), true},
		{"urn:v1:eu:resource:storageS3:" + uuProject + "/" + uuOtherBkt, uuOtherBkt, maps.Clone(uuOtherTags), true},
		{uuProjectURN, uuProject, maps.Clone(uuProjectTags), true},
	}
	// No keep-alive: a hang-up is never undone by net/http re-sending on a reused connection.
	w.srv = httptest.NewUnstartedServer(http.HandlerFunc(w.serve))
	w.srv.Config.SetKeepAlivesEnabled(false)
	w.srv.Start()
	t.Cleanup(w.srv.Close)
	return w
}

func (w *uuWorld) now() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.clock
}

func (w *uuWorld) sleep(ctx context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sleeps++
	if w.sleeps > 20000 {
		return errors.New("fake sleep: more than 20000 sleeps")
	}
	w.clock = w.clock.Add(d)
	return ctx.Err()
}

func (w *uuWorld) options() Options {
	return Options{
		API:      live.API{TokenURL: w.srv.URL + "/token", BaseURL: w.srv.URL + "/v1", HTTP: w.srv.Client()},
		Admin:    live.Credential{Endpoint: "ovh-eu", ClientID: uuAdminID, ClientSecret: uuAdminSK},
		Now:      w.now,
		Sleep:    w.sleep,
		Interval: uuInterval,
		Settle:   uuSettle,
	}
}

func (w *uuWorld) seen() []uuReq {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.reqs)
}

func (w *uuWorld) addClient(name string) rrClient {
	w.n++
	c := rrClient{id: fmt.Sprintf("EU.lzurn%04d", w.n), name: name, secret: fmt.Sprintf("Hn5jKm%04dWq2eRt6yUi", w.n)}
	c.identity = "urn:v1:eu:identity:credential:" + uuAccount + "/oauth2-" + c.id
	w.clients = append(w.clients, c)
	w.secrets = append(w.secrets, c.secret)
	return c
}

func (w *uuWorld) addPolicy(p uuPolicy) uuPolicy {
	w.n++
	p.id = fmt.Sprintf("00000000-0000-4000-8000-%012d", w.n)
	w.policies = append(w.policies, p)
	w.created[p.name] = w.clock
	return p
}

// grantFor is the policy the check must create for an identity.
func (w *uuWorld) grantFor(who string, c rrClient) uuPolicy {
	id := uuIdent[who]
	return uuPolicy{name: id.name, identities: []string{c.identity}, resources: []string{id.urn}, allow: []string{uuRead, uuWrite, id.ctl}}
}

func (w *uuWorld) foreign() {
	for _, n := range uuForeign {
		c := w.addClient(n)
		w.addPolicy(uuPolicy{name: n, identities: []string{c.identity}, resources: []string{uuProjectURN}, allow: []string{uuRead}})
	}
}

// tagBucket sets the run's key on the probe bucket, as an accepted write does.
func (w *uuWorld) tagBucket() {
	w.resources[0].tags[uuTagKey] = uuRun
}

func (w *uuWorld) names() (clients, policies []string) {
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

func (w *uuWorld) tags(urn string) map[string]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range w.resources {
		if r.urn == urn {
			return maps.Clone(r.tags)
		}
	}
	return nil
}

func (w *uuWorld) leaks(texts ...string) []string {
	w.mu.Lock()
	bad := append([]string{uuAdminSK, uuAdminTok}, w.secrets...)
	for tok := range w.tokens {
		bad = append(bad, tok)
	}
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

func (w *uuWorld) serve(rw http.ResponseWriter, r *http.Request) {
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
	if tok == uuAdminTok {
		who = "admin"
	} else if id, ok := w.tokens[tok]; ok {
		who = "probe"
		for _, c := range w.clients {
			if c.id == id && c.name == uuNameP {
				who = "project"
			} else if c.id == id && c.name == uuNameA {
				who = "account"
			}
		}
	}
	var named struct{ Name string }
	json.Unmarshal(body, &named)
	w.reqs = append(w.reqs, uuReq{who: who, method: r.Method, path: r.URL.Path, name: named.Name, at: w.clock})
	switch who {
	case "admin":
		w.admin(rw, r, body)
	case "project", "account":
		w.probe(rw, r, who, tok, body)
	case "probe":
		w.stray++
		w.reply(rw, http.StatusForbidden, nil, tok, "")
	default:
		w.reqs[len(w.reqs)-1].status = http.StatusUnauthorized
		rw.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(rw, `{"message":"Invalid token"}`)
	}
}

// token answers the client-credentials grant: the admin's, or a live client's of this world.
func (w *uuWorld) token(rw http.ResponseWriter, body []byte) {
	f, _ := url.ParseQuery(string(body))
	id, secret := f.Get("client_id"), f.Get("client_secret")
	if f.Get("grant_type") == "client_credentials" && id == uuAdminID && secret == uuAdminSK {
		w.reqs = append(w.reqs, uuReq{who: "admin", method: "TOKEN", path: "/token", at: w.clock})
		fmt.Fprintf(rw, `{"access_token":%q,"token_type":"Bearer","expires_in":3599,"scope":"all"}`, uuAdminTok)
		return
	}
	w.reqs = append(w.reqs, uuReq{who: "probe", method: "TOKEN", path: "/token", name: id, at: w.clock})
	i := slices.IndexFunc(w.clients, func(c rrClient) bool { return c.id == id && c.secret == secret })
	if f.Get("grant_type") != "client_credentials" || i < 0 {
		rw.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(rw, `{"error":"invalid_client"}`)
		return
	}
	tok := fmt.Sprintf("atk%04dUrN7xQpLs", len(w.tokens)+1)
	w.tokens[tok] = id
	fmt.Fprintf(rw, `{"access_token":%q,"token_type":"Bearer","expires_in":3599,"scope":"all"}`, tok)
}

// reply answers the last request: a 2xx with body, or a refusal naming actions (as OVHcloud's
// details.unauthorizedActionsByIAM; kb manage-and-operate/iam/troubleshooting.mdx).
func (w *uuWorld) reply(rw http.ResponseWriter, status int, actions []string, tok, body string) {
	last := &w.reqs[len(w.reqs)-1]
	last.status, last.actions = status, slices.Sorted(slices.Values(actions))
	rw.WriteHeader(status)
	switch {
	case status >= 200 && status <= 299:
		fmt.Fprint(rw, body)
	case w.echo:
		w.echoed++
		fmt.Fprintf(rw, `{"message":"token %s refused","details":{"unauthorizedActionsByIAM":%q}}`, tok, strings.Join(actions, ","))
	default:
		fmt.Fprintf(rw, `{"class":"Client::Forbidden","message":"This call has not been granted","details":{"unauthorizedActionsByIAM":%q}}`, strings.Join(actions, ","))
	}
}

func (w *uuWorld) hangup(rw http.ResponseWriter) {
	w.reqs[len(w.reqs)-1].status = 0
	hj, ok := rw.(http.Hijacker)
	if !ok {
		w.t.Errorf("fake: cannot hijack")
		return
	}
	if conn, _, err := hj.Hijack(); err == nil {
		conn.Close()
	}
}

// granted: a policy named for the identity names its client's identity and allows exactly the
// read, the write and its control on exactly its URN.
func (w *uuWorld) granted(who string) int {
	id := uuIdent[who]
	ci := slices.IndexFunc(w.clients, func(c rrClient) bool { return c.name == id.name })
	if ci < 0 {
		return -1
	}
	want := []string{uuRead, uuWrite, id.ctl}
	slices.Sort(want)
	return slices.IndexFunc(w.policies, func(p uuPolicy) bool {
		return p.name == id.name && slices.Equal(p.identities, []string{w.clients[ci].identity}) &&
			slices.Equal(p.resources, []string{id.urn}) && slices.Equal(slices.Sorted(slices.Values(p.allow)), want) && p.other == 0
	})
}

// probe answers a probe identity: its control, its own policy (the read) and the bucket (the write).
func (w *uuWorld) probe(rw http.ResponseWriter, r *http.Request, who, tok string, body []byte) {
	id := uuIdent[who]
	pol := w.granted(who)
	const res = "/v2/iam/resource/"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == id.ctlPath:
		if pol < 0 {
			w.reply(rw, http.StatusForbidden, []string{id.ctl}, tok, "")
			return
		}
		st := http.StatusOK
		if q := w.control[who]; len(q) > 0 {
			st, w.control[who] = q[0], q[1:]
		}
		if st == 0 { // scripted: no answer
			w.hangup(rw)
			return
		}
		var acts []string
		if st == http.StatusForbidden {
			acts = []string{id.ctl}
		}
		w.reply(rw, st, acts, tok, `{"ok":true}`)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/iam/policy/"):
		if pol < 0 || r.URL.Path != "/v2/iam/policy/"+w.policies[pol].id {
			w.stray++
			w.reply(rw, http.StatusForbidden, nil, tok, "")
			return
		}
		w.scripted(rw, who+" read", tok, fmt.Sprintf(`{"id":%q,"name":%q}`, w.policies[pol].id, id.name))
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, res):
		urn := strings.TrimPrefix(r.URL.Path, res)
		i := slices.IndexFunc(w.resources, func(x uuRes) bool { return x.urn == urn })
		var req struct {
			Tags map[string]string `json:"tags"`
		}
		json.Unmarshal(body, &req)
		var want map[string]string
		if i >= 0 {
			want = maps.Clone(w.resources[i].tags)
			want[uuTagKey] = uuRun
		}
		if urn != uuBucketURN || i < 0 || !maps.Equal(req.Tags, want) {
			w.stray++
			w.reply(rw, http.StatusForbidden, nil, tok, "")
			return
		}
		if pol < 0 {
			w.reply(rw, http.StatusForbidden, []string{uuWrite}, tok, "")
			return
		}
		if st := w.scripted(rw, who+" write", tok, `{}`); st >= 200 && st <= 299 {
			w.resources[i].tags = req.Tags
		}
	default:
		w.stray++
		w.reply(rw, http.StatusForbidden, nil, tok, "")
	}
}

func (w *uuWorld) scripted(rw http.ResponseWriter, key, tok, body string) int {
	a, ok := w.answer[key]
	if !ok {
		a = uuAns{status: http.StatusOK}
	}
	if a.hangup {
		w.hangup(rw)
		return 0
	}
	w.reply(rw, a.status, a.actions, tok, body)
	return a.status
}

// adminDo sends one request as the sandbox admin, as a process outside the check would (a run
// that died after it).
func (w *uuWorld) adminDo(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, w.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+uuAdminTok)
	resp, err := w.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (w *uuWorld) admin(rw http.ResponseWriter, r *http.Request, body []byte) {
	p := r.URL.Path
	notFound := func() {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"not found"}`)
	}
	const cl, pol, res = "/v1/me/api/oauth2/client", "/v2/iam/policy", "/v2/iam/resource"
	polJSON := func(q uuPolicy) map[string]any {
		var rs []map[string]string
		for _, u := range q.resources {
			rs = append(rs, map[string]string{"urn": u})
		}
		return map[string]any{"id": q.id, "name": q.name, "identities": q.identities, "resources": rs,
			"readOnly": false, "createdAt": w.created[q.name].Format(time.RFC3339)}
	}
	resJSON := func(x uuRes) map[string]any {
		return map[string]any{"id": "00000000-0000-4000-9000-000000000001", "urn": x.urn, "name": x.name, "displayName": x.name,
			"type": strings.Split(strings.TrimPrefix(x.urn, "urn:v1:eu:resource:"), ":")[0], "owner": uuAccount, "tags": x.tags}
	}
	switch {
	case r.Method == http.MethodGet && p == cl:
		ids := []string{}
		for _, c := range w.clients {
			ids = append(ids, c.id)
		}
		json.NewEncoder(rw).Encode(ids)
	case r.Method == http.MethodGet && strings.HasPrefix(p, cl+"/"):
		i := slices.IndexFunc(w.clients, func(c rrClient) bool { return c.id == strings.TrimPrefix(p, cl+"/") })
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
		if w.clientLost {
			rw.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(rw).Encode(map[string]string{"clientId": c.id, "clientSecret": c.secret})
	case r.Method == http.MethodDelete && strings.HasPrefix(p, cl+"/"):
		i := slices.IndexFunc(w.clients, func(c rrClient) bool { return c.id == strings.TrimPrefix(p, cl+"/") })
		if i < 0 {
			notFound()
			return
		}
		if !w.keep {
			w.clients = slices.Delete(w.clients, i, i+1)
		}
		fmt.Fprint(rw, `null`)
	case r.Method == http.MethodGet && p == pol:
		out := []map[string]any{}
		for _, q := range w.policies {
			out = append(out, polJSON(q))
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
	case r.Method == http.MethodGet && strings.HasPrefix(p, pol+"/"):
		i := slices.IndexFunc(w.policies, func(q uuPolicy) bool { return q.id == strings.TrimPrefix(p, pol+"/") })
		if i < 0 {
			notFound()
			return
		}
		json.NewEncoder(rw).Encode(polJSON(w.policies[i]))
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
		q := uuPolicy{name: req.Name, identities: req.Identities, other: len(req.Permissions.Except) + len(req.Permissions.Deny)}
		for _, x := range req.Resources {
			q.resources = append(q.resources, x.URN)
		}
		for _, a := range req.Permissions.Allow {
			q.allow = append(q.allow, a.Action)
		}
		q = w.addPolicy(q)
		if w.policyLost {
			rw.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(rw).Encode(polJSON(q))
	case r.Method == http.MethodDelete && strings.HasPrefix(p, pol+"/"):
		i := slices.IndexFunc(w.policies, func(q uuPolicy) bool { return q.id == strings.TrimPrefix(p, pol+"/") })
		if i < 0 {
			notFound()
			return
		}
		if !w.keep {
			w.policies = slices.Delete(w.policies, i, i+1)
		}
		fmt.Fprint(rw, `null`)
	case r.Method == http.MethodGet && p == res:
		q := r.URL.Query()
		out := []map[string]any{}
		for _, x := range w.resources {
			if x.listed && (len(q["resourceName"]) == 0 || slices.Contains(q["resourceName"], x.name)) &&
				(len(q["resourceURN"]) == 0 || slices.Contains(q["resourceURN"], x.urn)) {
				out = append(out, resJSON(x))
			}
		}
		json.NewEncoder(rw).Encode(out)
	case strings.HasPrefix(p, res+"/"):
		rest := strings.TrimPrefix(p, res+"/")
		urn, key, tag := rest, "", false
		if i := strings.LastIndex(rest, "/tag/"); i >= 0 && r.Method == http.MethodDelete {
			urn, key, tag = rest[:i], rest[i+len("/tag/"):], true
		}
		i := slices.IndexFunc(w.resources, func(x uuRes) bool { return x.urn == urn && x.listed })
		if i < 0 {
			notFound()
			return
		}
		switch {
		case r.Method == http.MethodGet:
			json.NewEncoder(rw).Encode(resJSON(w.resources[i]))
		case r.Method == http.MethodPut:
			var req struct {
				Tags map[string]string `json:"tags"`
			}
			json.Unmarshal(body, &req)
			if !w.keep && !w.keepTags {
				w.resources[i].tags = req.Tags
			}
			json.NewEncoder(rw).Encode(resJSON(w.resources[i]))
		case tag:
			if _, ok := w.resources[i].tags[key]; !ok {
				notFound()
				return
			}
			if !w.keep && !w.keepTags {
				delete(w.resources[i].tags, key)
			}
			fmt.Fprint(rw, `null`)
		default:
			rw.WriteHeader(http.StatusMethodNotAllowed)
		}
	case strings.HasPrefix(p, "/v1/cloud/project/"+uuProject+"/region/GRA/storage"):
		// T034: the bucket listing and delete a Sweep needs (kb api/v1/cloud.json; the T030
		// operations ListBuckets and DeleteBucket).
		name := strings.TrimPrefix(strings.TrimPrefix(p, "/v1/cloud/project/"+uuProject+"/region/GRA/storage"), "/")
		i := slices.Index(w.buckets, name)
		switch {
		case r.Method == http.MethodGet && name == "":
			out := []map[string]string{}
			for _, b := range w.buckets {
				out = append(out, map[string]string{"name": b})
			}
			json.NewEncoder(rw).Encode(out)
		case r.Method == http.MethodDelete && i >= 0:
			if !w.keep {
				w.buckets = slices.Delete(w.buckets, i, i+1)
			}
			fmt.Fprint(rw, `null`)
		case r.Method == http.MethodDelete:
			notFound()
		default:
			rw.WriteHeader(http.StatusMethodNotAllowed)
		}
	default:
		rw.WriteHeader(http.StatusForbidden)
		fmt.Fprint(rw, `{"message":"This call has not been granted"}`)
	}
}

// uuAttempt, uuResult and uuRecord read observations.json as T034 writes it (the JSON names are the
// contract; fields T034 adds are ignored).
type uuAttempt struct {
	Identity  string    `json:"identity"`
	Call      string    `json:"call"`
	Action    string    `json:"action"`
	At        time.Time `json:"at"`
	Status    int       `json:"status"`
	Actions   []string  `json:"actions"`
	Outage    bool      `json:"outage"`
	PolicyAge *int64    `json:"policy_age_seconds"`
}

type uuResult struct {
	Action   string `json:"action"`
	Observed string `json:"observed"`
	Reason   string `json:"reason"`
}

type uuRecord struct {
	Check    string      `json:"check"`
	RunID    string      `json:"run_id"`
	Attempts []uuAttempt `json:"attempts"`
	Results  []uuResult  `json:"results"`
}

func uuParse(t *testing.T, raw []byte, err error) uuRecord {
	t.Helper()
	if raw == nil {
		t.Fatalf("Observe wrote no record (err %v)", err)
	}
	var rec uuRecord
	if jerr := json.Unmarshal(raw, &rec); jerr != nil {
		t.Fatalf("record does not decode: %v", jerr)
	}
	if rec.Check != "iam-urn" || rec.RunID != uuRun {
		t.Errorf("record check %q run_id %q, want iam-urn and %s", rec.Check, rec.RunID, uuRun)
	}
	for _, r := range rec.Results {
		if !slices.Contains([]string{"project", "account", "either", "blocked"}, r.Observed) {
			t.Errorf("result %+v: observed is outside the check's set", r)
		}
		if r.Observed == "blocked" && !slices.Contains([]string{"propagation", "no owned target", "outage", "unattributable", "refused"}, r.Reason) {
			t.Errorf("result %+v: reason is outside the check's set", r)
		}
	}
	return rec
}

func uuObserve(t *testing.T, w *uuWorld) (raw []byte, term string, err error) {
	t.Helper()
	var buf bytes.Buffer
	raw, err = IamURN(w.options()).Observe(context.Background(), uuEnv(), &buf)
	if leaks := w.leaks(string(raw), buf.String(), fmt.Sprint(err)); len(leaks) > 0 {
		t.Errorf("record, terminal or error carry %q", leaks)
	}
	return raw, buf.String(), err
}

// uuKind is what a probe identity's request is: its control or a probed call, and the action.
func uuKind(r uuReq) (call, action string) {
	switch {
	case r.method == http.MethodGet && r.path == uuIdent[r.who].ctlPath:
		return "control", uuIdent[r.who].ctl
	case r.method == http.MethodGet && strings.HasPrefix(r.path, "/v2/iam/policy/"):
		return "probe", uuRead
	case r.method == http.MethodPut && strings.HasPrefix(r.path, "/v2/iam/resource/"):
		return "probe", uuWrite
	}
	return "?", "?"
}

// probes are the probe identities' calls, in order.
func (w *uuWorld) probes() []uuReq {
	var out []uuReq
	for _, r := range w.seen() {
		if (r.who == "project" || r.who == "account") && r.method != "TOKEN" {
			out = append(out, r)
		}
	}
	return out
}

func (w *uuWorld) calls(who, call, action string) []uuReq {
	var out []uuReq
	for _, r := range w.probes() {
		if c, a := uuKind(r); r.who == who && c == call && (action == "" || a == action) {
			out = append(out, r)
		}
	}
	return out
}

// uuWantSequence: per identity, the admin creates its client and then its policy, and the identity's
// first call is its control; a probed call comes only after that identity's control succeeded; the
// bucket is listed as an IAM resource before any write; no probe call strays outside its grant.
func uuWantSequence(t *testing.T, w *uuWorld) {
	t.Helper()
	reqs := w.seen()
	find := func(f func(uuReq) bool) int { return slices.IndexFunc(reqs, f) }
	for _, who := range []string{"project", "account"} {
		name := uuIdent[who].name
		c := find(func(r uuReq) bool {
			return r.who == "admin" && r.method == http.MethodPost && r.path == "/v1/me/api/oauth2/client" && r.name == name
		})
		p := find(func(r uuReq) bool {
			return r.who == "admin" && r.method == http.MethodPost && r.path == "/v2/iam/policy" && r.name == name
		})
		f := find(func(r uuReq) bool { return r.who == who })
		if c < 0 || p < 0 || f < 0 || !(c < p && p < f) {
			t.Fatalf("%s: client create at %d, policy create at %d, first call at %d; want them in that order (reqs %v)", who, c, p, f, reqs)
		}
		if call, _ := uuKind(reqs[f]); call != "control" {
			t.Errorf("%s: first call %s, want its control", who, reqs[f])
		}
		ok := false
		for _, r := range reqs {
			if r.who != who {
				continue
			}
			call, _ := uuKind(r)
			switch {
			case call == "control" && r.status >= 200 && r.status <= 299:
				ok = true
			case call == "probe" && !ok:
				t.Errorf("%s: probed call %s at %s before its control succeeded", who, r, r.at.Format(time.RFC3339))
			}
		}
	}
	list := find(func(r uuReq) bool {
		return r.who == "admin" && r.method == http.MethodGet && strings.HasPrefix(r.path, "/v2/iam/resource")
	})
	write := find(func(r uuReq) bool { return r.method == http.MethodPut && r.who != "admin" })
	if list < 0 || write >= 0 && write < list {
		t.Errorf("bucket listing at %d, first write at %d: want the bucket listed as an IAM resource first", list, write)
	}
	uuWantNoStray(t, w)
}

// uuWantNoStray: no probe call outside its grant, and the retained project's and the other run's
// tags untouched.
func uuWantNoStray(t *testing.T, w *uuWorld) {
	t.Helper()
	w.mu.Lock()
	stray := w.stray
	w.mu.Unlock()
	if stray != 0 {
		t.Errorf("%d probe calls outside the grant (another resource, other tags, another policy): %v", stray, w.probes())
	}
	if got := w.tags(uuProjectURN); !maps.Equal(got, uuProjectTags) {
		t.Errorf("retained project tags %v, want %v untouched", got, uuProjectTags)
	}
	if got := w.tags("urn:v1:eu:resource:storageS3:" + uuProject + "/" + uuOtherBkt); !maps.Equal(got, uuOtherTags) {
		t.Errorf("the other run's bucket tags %v, want %v untouched", got, uuOtherTags)
	}
}

// uuWantRecorded: the record's attempts are exactly the probe identities' calls the fake answered, in
// order, each with its identity, call, action, time, status, named actions, outage flag (no answer,
// or other than 2xx, 401 or 403) and the seconds since that identity's policy was created.
func uuWantRecorded(t *testing.T, w *uuWorld, rec uuRecord) {
	t.Helper()
	want := w.probes()
	if len(rec.Attempts) != len(want) {
		t.Errorf("record has %d attempts, the fake answered %d calls: %v", len(rec.Attempts), len(want), want)
		return
	}
	w.mu.Lock()
	created := maps.Clone(w.created)
	w.mu.Unlock()
	for i, r := range want {
		call, action := uuKind(r)
		outage := !(r.status >= 200 && r.status <= 299 || r.status == 401 || r.status == 403)
		g := rec.Attempts[i]
		if g.Identity != r.who || g.Call != call || g.Action != action || !g.At.Equal(r.at) || g.Status != r.status ||
			!slices.Equal(slices.Sorted(slices.Values(g.Actions)), r.actions) || g.Outage != outage {
			t.Errorf("attempt %d = %+v, want %s %s %s at %s status %d actions %v outage %v", i, g, r.who, call, action, r.at.Format(time.RFC3339), r.status, r.actions, outage)
		}
		c, ok := created[uuIdent[r.who].name]
		if age := secs(r.at.Sub(c)); !ok || g.PolicyAge == nil || *g.PolicyAge != age {
			t.Errorf("attempt %d policy_age_seconds %v, want %d (seconds since %s's policy was created)", i, g.PolicyAge, age, r.who)
		}
	}
}

// uuWantResults: exactly one result per probed action, each as given ("observed/reason").
func uuWantResults(t *testing.T, rec uuRecord, read, write string) {
	t.Helper()
	got := map[string]string{}
	for _, r := range rec.Results {
		if _, dup := got[r.Action]; dup {
			t.Errorf("two results for %s", r.Action)
		}
		got[r.Action] = r.Observed
		if r.Observed == "blocked" {
			got[r.Action] += "/" + r.Reason
		}
	}
	if len(got) != 2 || got[uuRead] != read || got[uuWrite] != write {
		t.Errorf("results %v, want %s: %s and %s: %s", rec.Results, uuRead, read, uuWrite, write)
	}
}

// uuWantSpaced: consecutive control retries are at least one Interval apart (no busy retry).
func uuWantSpaced(t *testing.T, ctl []uuReq) {
	t.Helper()
	for i := 1; i < len(ctl); i++ {
		if gap := ctl[i].at.Sub(ctl[i-1].at); gap < uuInterval {
			t.Errorf("control retry %d came %s after the previous one, want at least Interval %s", i, gap, uuInterval)
			return
		}
	}
}

func uuRefuse(action string) uuAns {
	return uuAns{status: http.StatusForbidden, actions: []string{action}}
}

func TestJudgeURN(t *testing.T) {
	ok := URNAnswer{Status: 200}
	no := func(actions ...string) URNAnswer { return URNAnswer{Status: 403, Actions: actions} }
	cases := []struct {
		name          string
		action        string
		pc, p, ac, a  URNAnswer
		want, exposes string
	}{
		{"project refused, account allowed", uuWrite, ok, no(uuWrite), ok, ok, "account", "a judge that reads the URN from one identity alone"},
		{"account refused, project allowed", uuWrite, ok, ok, ok, no(uuWrite), "project", "a judge hard-wired to D109's expectation"},
		{"both allowed", uuRead, ok, ok, ok, ok, "either", "a URN claimed where the action is not URN-bound"},
		{"both refused", uuRead, ok, no(uuRead), ok, no(uuRead), "blocked", "a URN read from two refusals"},
		{"a refusal naming the action among others", uuRead, ok, no(uuCtlP, uuRead), ok, ok, "account", "an over-strict reading of the refusal"},
		{"project control refused", uuWrite, no(uuCtlP), no(uuWrite), ok, ok, "blocked", "D109's false account from a policy that never propagated"},
		{"account control refused", uuWrite, ok, ok, no(uuCtlA), no(uuWrite), "blocked", "a verdict judged before the policy was in effect"},
		{"account control without an answer", uuRead, ok, no(uuRead), URNAnswer{Err: errors.New("connection reset")}, ok, "blocked", "a control outage read as a working route"},
		{"project control 503", uuRead, URNAnswer{Status: 503}, no(uuRead), ok, ok, "blocked", "a control outage read as a working route"},
		{"project 503", uuRead, ok, URNAnswer{Status: 503}, ok, ok, "blocked", "an outage read as a URN"},
		{"account transport error", uuRead, ok, ok, ok, URNAnswer{Err: errors.New("EOF")}, "blocked", "an outage read as a URN"},
		{"project 403 naming another action", uuRead, ok, no("account:apiovh:iam/policy/edit"), ok, ok, "blocked", "an unrelated refusal counted for the probed action"},
		{"project 403 naming the other probed action", uuWrite, ok, no(uuRead), ok, ok, "blocked", "one action's refusal applied to another"},
		{"project 403 naming nothing", uuWrite, ok, no(), ok, ok, "blocked", "an unattributable refusal counted for the probed action"},
		{"project 403 naming nothing, account 403 naming the action", uuWrite, ok, no(), ok, no(uuWrite), "blocked", "a URN read from a nameless refusal"},
		{"account 401", uuWrite, ok, ok, ok, URNAnswer{Status: 401}, "blocked", "an authentication failure read as a URN"},
	}
	for _, c := range cases {
		if got := JudgeURN(c.action, c.pc, c.p, c.ac, c.a); got != c.want {
			t.Errorf("%s: JudgeURN = %q, want %q (exposes %s)", c.name, got, c.want, c.exposes)
		}
	}
}

func TestURNObserve(t *testing.T) {
	verdict := func(t *testing.T, w *uuWorld, read, write string) uuRecord {
		t.Helper()
		raw, _, err := uuObserve(t, w)
		uuWantSequence(t, w)
		if err != nil {
			t.Errorf("Observe = %v, want nil", err)
		}
		rec := uuParse(t, raw, err)
		uuWantRecorded(t, w, rec)
		uuWantResults(t, rec, read, write)
		return rec
	}

	t.Run("account", func(t *testing.T) {
		// Exposes a judge that reads the URN from one identity alone.
		w := newUUWorld(t)
		w.answer["project read"], w.answer["project write"] = uuRefuse(uuRead), uuRefuse(uuWrite)
		verdict(t, w, "account", "account")
		for _, who := range []string{"project", "account"} {
			if n, m := len(w.calls(who, "probe", uuRead)), len(w.calls(who, "probe", uuWrite)); n != 1 || m != 1 {
				t.Errorf("%s: %d reads and %d writes, want one of each", who, n, m)
			}
		}
		want := maps.Clone(uuBucketTags)
		want[uuTagKey] = uuRun
		if got := w.tags(uuBucketURN); !maps.Equal(got, want) {
			t.Errorf("bucket tags %v after the accepted write, want %v (its tags kept, the run's key added)", got, want)
		}
	})

	t.Run("project", func(t *testing.T) {
		// Exposes a check hard-wired to D109's expectation.
		w := newUUWorld(t)
		w.answer["account read"], w.answer["account write"] = uuRefuse(uuRead), uuRefuse(uuWrite)
		verdict(t, w, "project", "project")
	})

	t.Run("either", func(t *testing.T) {
		// Exposes a URN claimed where the action is not URN-bound.
		w := newUUWorld(t)
		verdict(t, w, "either", "either")
	})

	t.Run("per-action", func(t *testing.T) {
		// Exposes one action's URN applied to another: the read is allowed on both, the write
		// only on the account (D109's world).
		w := newUUWorld(t)
		w.answer["project write"] = uuRefuse(uuWrite)
		verdict(t, w, "either", "account")
	})

	t.Run("control-settles", func(t *testing.T) {
		// Exposes a verdict judged before the policy was in effect.
		w := newUUWorld(t)
		w.control["project"] = []int{403, 403}
		w.answer["project write"] = uuRefuse(uuWrite)
		rec := verdict(t, w, "either", "account")
		ctl := w.calls("project", "control", "")
		var got []int
		for _, c := range ctl {
			got = append(got, c.status)
		}
		if !slices.Equal(got, []int{403, 403, 200}) {
			t.Fatalf("project control statuses %v, want [403 403 200]", got)
		}
		if span := ctl[2].at.Sub(ctl[0].at); span > uuSettle {
			t.Errorf("control retries spanned %s, beyond Settle %s", span, uuSettle)
		}
		uuWantSpaced(t, ctl)
		for _, r := range w.calls("project", "probe", "") {
			if r.at.Before(ctl[2].at) {
				t.Errorf("probed call %s before the control succeeded", r)
			}
		}
		var ages []int64
		for _, a := range rec.Attempts {
			if a.Identity == "project" && a.Call == "control" && a.PolicyAge != nil {
				ages = append(ages, *a.PolicyAge)
			}
		}
		if len(ages) != 3 || ages[0] >= ages[2] {
			t.Errorf("project control policy ages %v, want three, growing", ages)
		}
	})

	t.Run("control-never", func(t *testing.T) {
		// Exposes D109's false `account` from a policy that never propagated: the account's
		// control is refused until Settle is used up, whatever the probed calls answer.
		w := newUUWorld(t)
		w.control["account"] = slices.Repeat([]int{403}, 1000)
		w.answer["project read"], w.answer["project write"] = uuRefuse(uuRead), uuRefuse(uuWrite)
		raw, _, err := uuObserve(t, w)
		ctl := w.calls("account", "control", "")
		if len(ctl) < 2 {
			t.Fatalf("%d account control calls, want retries within Settle (reqs %v)", len(ctl), w.seen())
		}
		if span := ctl[len(ctl)-1].at.Sub(ctl[0].at); span > uuSettle || span < uuSettle-uuInterval {
			t.Errorf("control retries spanned %s, want Settle %s used up (within one interval)", span, uuSettle)
		}
		uuWantSpaced(t, ctl)
		if p := w.calls("account", "probe", ""); len(p) != 0 {
			t.Errorf("account probed calls %v although its control never succeeded (a probed call is made only after it)", p)
		}
		uuWantNoStray(t, w)
		rec := uuParse(t, raw, err)
		uuWantRecorded(t, w, rec)
		uuWantResults(t, rec, "blocked/propagation", "blocked/propagation")
	})

	t.Run("control-outage", func(t *testing.T) {
		// Exposes a control outage read as propagation (or as a working route), and a hung-up
		// control re-sent without the Interval: the account's control answers 503, or nothing,
		// until Settle is used up. With the project's control refused at the same time, the
		// outage wins (the header's order).
		for _, v := range []struct {
			name     string
			st       int
			refusedP bool
		}{{"503", http.StatusServiceUnavailable, false}, {"hang-up", 0, false}, {"503-and-project-refused", http.StatusServiceUnavailable, true}} {
			t.Run(v.name, func(t *testing.T) {
				w := newUUWorld(t)
				w.control["account"] = slices.Repeat([]int{v.st}, 1000)
				if v.refusedP {
					w.control["project"] = slices.Repeat([]int{403}, 1000)
				}
				raw, _, err := uuObserve(t, w)
				ctl := w.calls("account", "control", "")
				if len(ctl) < 2 {
					t.Fatalf("%d account control calls, want retries within Settle", len(ctl))
				}
				if span := ctl[len(ctl)-1].at.Sub(ctl[0].at); span > uuSettle || span < uuSettle-uuInterval {
					t.Errorf("retries spanned %s, want Settle %s used up (within one interval)", span, uuSettle)
				}
				uuWantSpaced(t, ctl)
				if p := w.calls("account", "probe", ""); len(p) != 0 {
					t.Errorf("account probed calls %v although its control never succeeded", p)
				}
				uuWantNoStray(t, w)
				rec := uuParse(t, raw, err)
				uuWantRecorded(t, w, rec)
				uuWantResults(t, rec, "blocked/outage", "blocked/outage")
			})
		}
	})

	t.Run("mixed-refusal", func(t *testing.T) {
		// Exposes a nameless refusal counted as a refusal of the action: the project's write is
		// refused naming nothing, the account's naming the action.
		w := newUUWorld(t)
		w.answer["project write"] = uuAns{status: http.StatusForbidden}
		w.answer["account write"] = uuRefuse(uuWrite)
		raw, _, err := uuObserve(t, w)
		uuWantSequence(t, w)
		rec := uuParse(t, raw, err)
		uuWantRecorded(t, w, rec)
		uuWantResults(t, rec, "either", "blocked/unattributable")
	})

	t.Run("both-refused", func(t *testing.T) {
		// Exposes a URN read from two refusals.
		w := newUUWorld(t)
		w.answer["project read"], w.answer["account read"] = uuRefuse(uuRead), uuRefuse(uuRead)
		raw, _, err := uuObserve(t, w)
		uuWantSequence(t, w)
		rec := uuParse(t, raw, err)
		uuWantRecorded(t, w, rec)
		uuWantResults(t, rec, "blocked/refused", "either")
	})

	t.Run("outage-5xx", func(t *testing.T) {
		// Exposes an outage read as a URN.
		w := newUUWorld(t)
		w.answer["project read"] = uuAns{status: http.StatusServiceUnavailable}
		w.answer["account write"] = uuRefuse(uuWrite)
		raw, _, err := uuObserve(t, w)
		uuWantSequence(t, w)
		rec := uuParse(t, raw, err)
		uuWantRecorded(t, w, rec)
		uuWantResults(t, rec, "blocked/outage", "project")
	})

	t.Run("outage-transport", func(t *testing.T) {
		// Exposes a transport error read as a URN.
		w := newUUWorld(t)
		w.answer["account write"] = uuAns{hangup: true}
		w.answer["account read"] = uuRefuse(uuRead)
		raw, _, err := uuObserve(t, w)
		uuWantSequence(t, w)
		if n := len(w.calls("account", "probe", uuWrite)); n != 1 {
			t.Errorf("account write sent %d times, want once (a re-send hides the hang-up)", n)
		}
		rec := uuParse(t, raw, err)
		uuWantRecorded(t, w, rec)
		uuWantResults(t, rec, "project", "blocked/outage")
	})

	t.Run("unattributable", func(t *testing.T) {
		// Exposes an unrelated refusal counted for the probed action: the read's refusal names
		// another action, the write's names none.
		w := newUUWorld(t)
		w.answer["project read"] = uuRefuse("account:apiovh:iam/policy/edit")
		w.answer["project write"] = uuAns{status: http.StatusForbidden}
		raw, _, err := uuObserve(t, w)
		uuWantSequence(t, w)
		rec := uuParse(t, raw, err)
		uuWantRecorded(t, w, rec)
		uuWantResults(t, rec, "blocked/unattributable", "blocked/unattributable")
	})

	t.Run("no-owned-target", func(t *testing.T) {
		// Exposes a fallback to the retained project, or to whatever the listing returns: the
		// bucket is not listed as an IAM resource (UNVERIFIED that OVHcloud lists S3 buckets), or
		// listed under its name with another URN.
		for _, variant := range []string{"not listed", "another urn"} {
			t.Run(variant, func(t *testing.T) { uuNoOwnedTarget(t, variant) })
		}
	})
}

// uuNoOwnedTarget is one variant of TestURNObserve/no-owned-target.
func uuNoOwnedTarget(t *testing.T, variant string) {
	w := newUUWorld(t)
	if variant == "not listed" {
		w.resources[0].listed = false
	} else {
		w.resources[0].urn = "urn:v1:eu:resource:storageS3:" + uuProject + "/" + uuBucket + "-other"
	}
	raw, _, err := uuObserve(t, w)
	reqs := w.seen()
	if !slices.ContainsFunc(reqs, func(r uuReq) bool {
		return r.who == "admin" && r.method == http.MethodGet && strings.HasPrefix(r.path, "/v2/iam/resource")
	}) {
		t.Fatalf("%s: the bucket was never looked up as an IAM resource: %v", variant, reqs)
	}
	if slices.ContainsFunc(reqs, func(r uuReq) bool { return r.method == http.MethodPut }) {
		t.Errorf("%s: a write was attempted: %v", variant, reqs)
	}
	// The read is still observed: each identity's control succeeded and its read was made.
	for _, who := range []string{"project", "account"} {
		ok := slices.ContainsFunc(w.calls(who, "control", ""), func(r uuReq) bool { return r.status == http.StatusOK })
		if n := len(w.calls(who, "probe", uuRead)); !ok || n != 1 {
			t.Errorf("%s: %s control succeeded %v, %d reads; want a successful control and one read", variant, who, ok, n)
		}
	}
	uuWantNoStray(t, w)
	rec := uuParse(t, raw, err)
	uuWantRecorded(t, w, rec)
	uuWantResults(t, rec, "either", "blocked/no owned target")
}

// uuAdminOnly: every request of a cleanup is the sandbox admin's.
func uuAdminOnly(t *testing.T, reqs []uuReq) {
	t.Helper()
	for _, r := range reqs {
		if r.who != "admin" {
			t.Errorf("cleanup sent %s: want the sandbox admin only", r)
		}
	}
}

// uuMutation: a delete, or a tag write by the admin.
func uuMutation(r uuReq) bool { return r.method == http.MethodDelete || r.method == http.MethodPut }

// uuRelisted: clients, policies and the bucket's resource are listed before the first change and
// again after the last.
func uuRelisted(t *testing.T, reqs []uuReq) {
	t.Helper()
	first, last := -1, -1
	for i, r := range reqs {
		if uuMutation(r) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	for _, path := range []string{"/v1/me/api/oauth2/client", "/v2/iam/policy", "/v2/iam/resource"} {
		list := func(r uuReq) bool {
			return r.method == http.MethodGet && (r.path == path || path == "/v2/iam/resource" && strings.HasPrefix(r.path, path))
		}
		if i := slices.IndexFunc(reqs, list); i < 0 || first >= 0 && i > first {
			t.Errorf("%s not listed before the first change: %v", path, reqs)
		}
		if last >= 0 && !slices.ContainsFunc(reqs[last+1:], list) {
			t.Errorf("%s not listed again after the last change: %v", path, reqs)
		}
	}
}

// uuClean: only the foreign clients and policies are left, the probe bucket carries its own tags
// without the run's key, and the other resources are untouched.
func uuClean(t *testing.T, w *uuWorld) {
	t.Helper()
	foreign := slices.Sorted(slices.Values(uuForeign))
	if cl, pol := w.names(); !slices.Equal(cl, foreign) || !slices.Equal(pol, foreign) {
		t.Errorf("clients %q policies %q left, want only the foreign %q", cl, pol, foreign)
	}
	if got := w.tags(uuBucketURN); !maps.Equal(got, uuBucketTags) {
		t.Errorf("bucket tags %v, want %v (the run's key removed, the rest kept)", got, uuBucketTags)
	}
	uuWantNoStray(t, w)
}

func TestURNUndo(t *testing.T) {
	t.Run("after-observe", func(t *testing.T) {
		// Exposes a client, policy or tag key left behind, or a foreign one removed.
		w := newUUWorld(t)
		w.foreign()
		if _, _, err := uuObserve(t, w); err != nil {
			t.Logf("Observe = %v", err)
		}
		before := len(w.seen())
		var term bytes.Buffer
		err := IamURN(w.options()).Undo(context.Background(), uuEnv(), &term)
		undo := w.seen()[before:]
		uuAdminOnly(t, undo)
		uuRelisted(t, undo)
		if err != nil {
			t.Errorf("Undo = %v, want nil", err)
		}
		uuClean(t, w)
		if leaks := w.leaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 {
			t.Errorf("terminal or error carry %q", leaks)
		}
	})

	t.Run("still-listed", func(t *testing.T) {
		// Exposes an Undo that trusts an acknowledged delete.
		w := newUUWorld(t)
		c := w.addClient(uuNameP)
		w.addPolicy(w.grantFor("project", c))
		w.tagBucket()
		w.keep = true
		before := len(w.seen())
		err := IamURN(w.options()).Undo(context.Background(), uuEnv(), io.Discard)
		reqs := w.seen()[before:]
		uuAdminOnly(t, reqs)
		uuRelisted(t, reqs)
		if !slices.ContainsFunc(reqs, uuMutation) {
			t.Errorf("Undo changed nothing: %v", reqs)
		}
		if err == nil || !strings.Contains(err.Error(), uuNameP) {
			t.Errorf("Undo = %v, want an error naming what is left (%s)", err, uuNameP)
		}
	})

	t.Run("tag-still-set", func(t *testing.T) {
		// Exposes an Undo that checks clients and policies again but not the bucket's tag key:
		// they are deleted, the tag removal is acknowledged and does not take.
		w := newUUWorld(t)
		c := w.addClient(uuNameP)
		w.addPolicy(w.grantFor("project", c))
		w.tagBucket()
		w.keepTags = true
		before := len(w.seen())
		err := IamURN(w.options()).Undo(context.Background(), uuEnv(), io.Discard)
		reqs := w.seen()[before:]
		uuAdminOnly(t, reqs)
		uuRelisted(t, reqs)
		if cl, pol := w.names(); len(cl)+len(pol) != 0 {
			t.Errorf("clients %q policies %q left, want both deleted", cl, pol)
		}
		if err == nil || !strings.Contains(err.Error(), uuTagKey) {
			t.Errorf("Undo = %v, want an error naming the tag key left on the bucket (%s)", err, uuTagKey)
		}
	})
}

func TestURNSweep(t *testing.T) {
	// Sweep has the signature of ProbeCheck's Sweep field (data-model.md; the field lands in T038).
	var _ func(context.Context, string, string, io.Writer) error = Options{}.sweepURN

	sweep := func(t *testing.T, w *uuWorld) ([]uuReq, error) {
		t.Helper()
		before := len(w.seen())
		var term bytes.Buffer
		// A fresh process: new Options, only the run id and the project.
		err := w.options().sweepURN(context.Background(), uuRun, uuProject, &term)
		reqs := w.seen()[before:]
		uuAdminOnly(t, reqs)
		if leaks := w.leaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 {
			t.Errorf("terminal or error carry %q", leaks)
		}
		return reqs, err
	}
	wantSwept := func(t *testing.T, w *uuWorld, reqs []uuReq, err error, changes int) {
		t.Helper()
		uuRelisted(t, reqs)
		if err != nil {
			t.Errorf("sweep = %v, want nil", err)
		}
		var got int
		for _, r := range reqs {
			if uuMutation(r) {
				got++
			}
		}
		if got != changes {
			t.Errorf("sweep sent %d deletes and tag writes, want %d: %v", got, changes, reqs)
		}
		uuClean(t, w)
	}

	t.Run("client-only", func(t *testing.T) {
		// Exposes an orphan client: one identity's client created and its policy not.
		w := newUUWorld(t)
		w.foreign()
		w.addClient(uuNameP)
		reqs, err := sweep(t, w)
		wantSwept(t, w, reqs, err, 1)
	})

	t.Run("client-create-lost", func(t *testing.T) {
		// Exposes a client nobody knows the id of: the fake created it, then answered 502 to the
		// process that asked, which then died (no Observe of this check, no output).
		w := newUUWorld(t)
		w.foreign()
		w.clientLost = true
		if st, _ := w.adminDo(t, http.MethodPost, "/v1/me/api/oauth2/client", `{"callbackUrls":[],"flow":"CLIENT_CREDENTIALS","name":"`+uuNameA+`"}`); st != http.StatusBadGateway {
			t.Fatalf("precondition: the create answered %d, want 502", st)
		}
		reqs, err := sweep(t, w)
		wantSwept(t, w, reqs, err, 1)
	})

	t.Run("policy-create-lost", func(t *testing.T) {
		// Exposes a policy nobody knows the id of: its create's answer was lost. The listing comes
		// in pages of one.
		w := newUUWorld(t)
		w.foreign()
		c := w.addClient(uuNameA)
		w.policyLost = true
		body := `{"name":"` + uuNameA + `","identities":["` + c.identity + `"],"resources":[{"urn":"` + uuAccountURN + `"}],"permissions":{"allow":[{"action":"` + uuRead + `"}]}}`
		if st, _ := w.adminDo(t, http.MethodPost, "/v2/iam/policy", body); st != http.StatusBadGateway {
			t.Fatalf("precondition: the create answered %d, want 502", st)
		}
		if _, pol := w.names(); !slices.Contains(pol, uuNameA) {
			t.Fatalf("precondition: the fake did not create the policy: %q", pol)
		}
		w.pageSize = 1
		reqs, err := sweep(t, w)
		wantSwept(t, w, reqs, err, 2)
	})

	t.Run("tag-and-policy-deleted", func(t *testing.T) {
		// Exposes a tag key left on the probe bucket: both clients exist, the project's policy was
		// deleted, the account's was not, and the bucket carries the run's key.
		w := newUUWorld(t)
		w.foreign()
		w.addClient(uuNameP)
		a := w.addClient(uuNameA)
		w.addPolicy(w.grantFor("account", a))
		w.tagBucket()
		reqs, err := sweep(t, w)
		wantSwept(t, w, reqs, err, 4)
	})

	t.Run("still-listed", func(t *testing.T) {
		// Exposes a sweep that trusts an acknowledged delete.
		w := newUUWorld(t)
		c := w.addClient(uuNameA)
		w.addPolicy(w.grantFor("account", c))
		w.tagBucket()
		w.keep = true
		reqs, err := sweep(t, w)
		uuRelisted(t, reqs)
		if !slices.ContainsFunc(reqs, uuMutation) {
			t.Errorf("sweep changed nothing: %v", reqs)
		}
		if err == nil || !strings.Contains(err.Error(), uuNameA) {
			t.Errorf("sweep = %v, want an error naming what is left (%s)", err, uuNameA)
		}
	})

	t.Run("tag-still-set", func(t *testing.T) {
		// Exposes a sweep that checks clients and policies again but not the bucket's tag key.
		w := newUUWorld(t)
		c := w.addClient(uuNameA)
		w.addPolicy(w.grantFor("account", c))
		w.tagBucket()
		w.keepTags = true
		reqs, err := sweep(t, w)
		uuRelisted(t, reqs)
		if cl, pol := w.names(); len(cl)+len(pol) != 0 {
			t.Errorf("clients %q policies %q left, want both deleted", cl, pol)
		}
		if err == nil || !strings.Contains(err.Error(), uuTagKey) {
			t.Errorf("sweep = %v, want an error naming the tag key left on the bucket (%s)", err, uuTagKey)
		}
	})

	t.Run("bad-run-id", func(t *testing.T) {
		// Control (passes on the stub too): a sweep refuses a run id it cannot derive the exact
		// names from, before any request.
		for _, run := range []string{"lzprobe", strings.ToLower(uuRun), uuRun + "/x"} {
			w := newUUWorld(t)
			if err := w.options().sweepURN(context.Background(), run, uuProject, io.Discard); err == nil || len(w.seen()) != 0 {
				t.Errorf("sweep(%q) = %v after %v, want a refusal before any request", run, err, w.seen())
			}
		}
	})
}

// TestURNValidate: Validate refuses published variables the check cannot use, before any request,
// and the check publishes the data-model's output and public keys.
func TestURNValidate(t *testing.T) {
	c := IamURN(Options{})
	if c.Output != "urn_check" {
		t.Errorf("Output = %q, want urn_check", c.Output)
	}
	if want := []string{"account_urn", "bucket", "bucket_urn", "project_urn", "run_id"}; !slices.Equal(slices.Sorted(slices.Values(c.Public)), want) {
		t.Errorf("Public = %q, want %q", c.Public, want)
	}
	if err := c.Validate(uuEnv()); err != nil {
		t.Errorf("Validate refused the run's own variables: %v", err)
	}
	edit := func(k, v string) map[string]string {
		e := uuEnv()
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
	for _, k := range slices.Sorted(maps.Keys(uuEnv())) {
		bad = append(bad, badEnv{"missing " + k, edit(k, "")})
	}
	otherBucketURN := "urn:v1:eu:resource:storageS3:" + uuProject + "/" + uuOtherBkt
	// bucketPair publishes a bucket and the URN that names it, so only the bucket rule can refuse.
	bucketPair := func(b string) map[string]string {
		e := edit("bucket", b)
		e["bucket_urn"] = "urn:v1:eu:resource:storageS3:" + uuProject + "/" + b
		return e
	}
	bad = append(bad, []badEnv{
		{"run_id not a run id", edit("run_id", "lzprobe-iam-urn")},
		{"run_id lower case", edit("run_id", strings.ToLower(uuRun))},
		{"project_urn a wildcard", edit("project_urn", "urn:v1:eu:resource:publicCloudProject:*")},
		{"project_urn not a project", edit("project_urn", uuAccountURN)},
		{"account_urn a wildcard", edit("account_urn", "urn:v1:eu:resource:account:*")},
		{"account_urn not an account", edit("account_urn", uuProjectURN)},
		{"bucket of another run", bucketPair(uuOtherBkt)},
		{"bucket with the run id's case", bucketPair("lzprobe-iam-urn-" + uuRun)},
		{"bucket a longer name", bucketPair(uuBucket + "-x")},
		{"bucket_urn the retained project", edit("bucket_urn", uuProjectURN)},
		{"bucket_urn the account", edit("bucket_urn", uuAccountURN)},
		{"bucket_urn another run's bucket", edit("bucket_urn", otherBucketURN)},
		{"bucket_urn the admin's policy", edit("bucket_urn", "urn:v1:eu:resource:iamPolicy:lz-sandbox-admin")},
		{"bucket_urn a longer name", edit("bucket_urn", uuBucketURN+"-x")},
		{"bucket_urn a wildcard", edit("bucket_urn", "urn:v1:eu:resource:storageS3:*/"+uuBucket)},
		{"bucket_urn not a resource", edit("bucket_urn", "urn:v1:eu:identity:credential:"+uuAccount+"/"+uuBucket)},
	}...)
	for _, b := range bad {
		w := newUUWorld(t)
		c := IamURN(w.options())
		if err := c.Validate(b.env); err == nil {
			t.Errorf("%s: Validate accepted it (exposes a probe writing to the retained project, the admin's policy or another run's resource)", b.name)
		}
		if _, err := c.Observe(context.Background(), b.env, io.Discard); err == nil || len(w.seen()) != 0 {
			t.Errorf("%s: Observe = %v after %v, want a refusal before any request", b.name, err, w.seen())
		}
		if err := c.Undo(context.Background(), b.env, io.Discard); err == nil || len(w.seen()) != 0 {
			t.Errorf("%s: Undo = %v after %v, want a refusal before any request", b.name, err, w.seen())
		}
	}
}

// TestURNLeak: refusals whose bodies echo the token, and the created clients' secrets, reach neither
// the record, the terminal nor the error. The guard is shown first on a leaking mutant, then trusted
// on the check.
func TestURNLeak(t *testing.T) {
	w := newUUWorld(t)
	// The leaking mutant: a check that copies the API's answers into its record and terminal.
	mutant := &live.ProbeCheck{Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
		_, b := w.adminDo(t, http.MethodPost, "/v1/me/api/oauth2/client", `{"callbackUrls":[],"flow":"CLIENT_CREDENTIALS","name":"`+uuNameP+`"}`)
		fmt.Fprintln(term, b)
		return []byte(b), nil
	}}
	var term bytes.Buffer
	raw, _ := mutant.Observe(context.Background(), uuEnv(), &term)
	if leaks := w.leaks(string(raw)); len(leaks) == 0 {
		t.Fatalf("the leak guard missed the mutant's record %s", raw)
	}
	if leaks := w.leaks(term.String()); len(leaks) == 0 {
		t.Fatalf("the leak guard missed the mutant's terminal %s", term.String())
	}

	// The check: a control refusal and a probed refusal echo the token.
	w = newUUWorld(t)
	w.echo = true
	w.control["project"] = []int{403}
	w.answer["project read"] = uuRefuse(uuRead)
	uuObserve(t, w) // fails on any leak
	w.mu.Lock()
	echoed := w.echoed
	w.mu.Unlock()
	if echoed < 2 {
		t.Errorf("%d echoing refusals were read, want the control's and the probed call's (else the guard proves nothing)", echoed)
	}
}

// TestURNFakes: the fakes answer as the cases say (fixture control; passes on the stub by design).
func TestURNFakes(t *testing.T) {
	if want := "lzprobe-iam-urn-" + strings.ToLower(uuRun); uuBucket != want {
		t.Fatalf("uuBucket %q, want %q", uuBucket, want)
	}
	ctx := context.Background()
	w := newUUWorld(t)
	client := w.srv.Client()
	do := func(method, path, tok, body string) (int, string, error) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, w.srv.URL+path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), nil
	}
	must := func(method, path, tok, body string) (int, string) {
		t.Helper()
		st, b, err := do(method, path, tok, body)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return st, b
	}
	grant := func(id, secret string) string {
		t.Helper()
		_, b := must(http.MethodPost, "/token", "", url.Values{"grant_type": {"client_credentials"}, "client_id": {id}, "client_secret": {secret}}.Encode())
		var tok struct {
			AccessToken string `json:"access_token"`
		}
		json.Unmarshal([]byte(b), &tok)
		return tok.AccessToken
	}
	if grant(uuAdminID, uuAdminSK) != uuAdminTok {
		t.Fatalf("admin grant failed")
	}
	_, b := must(http.MethodPost, "/v1/me/api/oauth2/client", uuAdminTok, `{"callbackUrls":[],"flow":"CLIENT_CREDENTIALS","name":"`+uuNameA+`"}`)
	var created struct{ ClientID, ClientSecret string }
	json.Unmarshal([]byte(b), &created)
	_, b = must(http.MethodGet, "/v1/me/api/oauth2/client/"+created.ClientID, uuAdminTok, "")
	var cl struct{ Identity string }
	json.Unmarshal([]byte(b), &cl)
	tok := grant(created.ClientID, created.ClientSecret)
	if tok == "" || cl.Identity == "" {
		t.Fatalf("probe client: token %q identity %q", tok, cl.Identity)
	}
	if st, _ := must(http.MethodGet, "/v1/me", tok, ""); st != 403 {
		t.Errorf("control without a policy = %d, want 403", st)
	}
	policy := func(allow ...string) string {
		var acts []string
		for _, a := range allow {
			acts = append(acts, `{"action":"`+a+`"}`)
		}
		_, b := must(http.MethodPost, "/v2/iam/policy", uuAdminTok, `{"name":"`+uuNameA+`","identities":["`+cl.Identity+`"],"resources":[{"urn":"`+uuAccountURN+`"}],"permissions":{"allow":[`+strings.Join(acts, ",")+`]}}`)
		var p struct {
			ID        string
			CreatedAt string
		}
		json.Unmarshal([]byte(b), &p)
		if _, err := time.Parse(time.RFC3339, p.CreatedAt); err != nil {
			t.Errorf("policy create answer %s: createdAt not RFC 3339", b)
		}
		return p.ID
	}
	wild := policy("account:apiovh:*")
	if st, _ := must(http.MethodGet, "/v1/me", tok, ""); st != 403 {
		t.Errorf("control under a wildcard policy = %d, want 403 (only the exact grant counts)", st)
	}
	must(http.MethodDelete, "/v2/iam/policy/"+wild, uuAdminTok, "")
	own := policy(uuRead, uuWrite, uuCtlA)
	w.control["account"] = []int{403}
	if st, _ := must(http.MethodGet, "/v1/me", tok, ""); st != 403 {
		t.Errorf("scripted control = %d, want 403", st)
	}
	if st, _ := must(http.MethodGet, "/v1/me", tok, ""); st != 200 {
		t.Errorf("control after the script = %d, want 200", st)
	}
	if st, _ := must(http.MethodGet, "/v2/iam/policy/"+own, tok, ""); st != 200 {
		t.Errorf("read of the own policy = %d, want 200", st)
	}
	w.answer["account read"] = uuRefuse(uuRead)
	if st, b := must(http.MethodGet, "/v2/iam/policy/"+own, tok, ""); st != 403 || !strings.Contains(b, uuRead) {
		t.Errorf("scripted read = %d %s, want 403 naming %s", st, b, uuRead)
	}
	_, b = must(http.MethodGet, "/v2/iam/resource?resourceName="+uuBucket, uuAdminTok, "")
	var listed []struct {
		URN  string
		Tags map[string]string
	}
	json.Unmarshal([]byte(b), &listed)
	if len(listed) != 1 || listed[0].URN != uuBucketURN || !maps.Equal(listed[0].Tags, uuBucketTags) {
		t.Fatalf("bucket listing %s, want the bucket alone with its tags", b)
	}
	tagged := maps.Clone(uuBucketTags)
	tagged[uuTagKey] = uuRun
	body, _ := json.Marshal(map[string]any{"tags": tagged})
	if st, _ := must(http.MethodPut, "/v2/iam/resource/"+url.PathEscape(uuBucketURN), tok, string(body)); st != 200 || !maps.Equal(w.tags(uuBucketURN), tagged) {
		t.Errorf("write = %d, tags %v; want 200 and %v", st, w.tags(uuBucketURN), tagged)
	}
	if st, _ := must(http.MethodDelete, "/v2/iam/resource/"+url.PathEscape(uuBucketURN)+"/tag/"+url.PathEscape(uuTagKey), uuAdminTok, ""); st != 200 || !maps.Equal(w.tags(uuBucketURN), uuBucketTags) {
		t.Errorf("tag delete = %d, tags %v; want 200 and %v", st, w.tags(uuBucketURN), uuBucketTags)
	}
	if must(http.MethodPut, "/v2/iam/resource/"+url.PathEscape(uuProjectURN), tok, string(body)); w.stray != 1 || !maps.Equal(w.tags(uuProjectURN), uuProjectTags) {
		t.Errorf("a write to the project: stray %d, tags %v; want it counted and refused", w.stray, w.tags(uuProjectURN))
	}
	w.answer["account write"] = uuAns{hangup: true}
	if _, _, err := do(http.MethodPut, "/v2/iam/resource/"+url.PathEscape(uuBucketURN), tok, string(body)); err == nil {
		t.Errorf("scripted hang-up answered")
	}
	if p := w.probes(); p[len(p)-1].status != 0 || uuKindCall(p[len(p)-1]) != "probe" {
		t.Errorf("the hang-up recorded as %+v", p[len(p)-1])
	}
	// Pagination: pages of one, the cursor in X-Pagination-Cursor-Next.
	w.foreign()
	w.pageSize = 1
	var names []string
	for cur, i := "", 0; i < 20; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, w.srv.URL+"/v2/iam/policy", nil)
		req.Header.Set("Authorization", "Bearer "+uuAdminTok)
		req.Header.Set("X-Pagination-Cursor", cur)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var page []struct{ Name string }
		json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		for _, p := range page {
			names = append(names, p.Name)
		}
		if cur = resp.Header.Get("X-Pagination-Cursor-Next"); cur == "" {
			break
		}
	}
	if len(names) != len(uuForeign)+1 {
		t.Errorf("paged policies %q, want %d", names, len(uuForeign)+1)
	}
}

func uuKindCall(r uuReq) string {
	c, _ := uuKind(r)
	return c
}
