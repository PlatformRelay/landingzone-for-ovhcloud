package live

// T042: the bootstrap phases guard, identify, passphrase, admin and revoke (spec 005 FR-010,
// FR-012, SC-005; research R13, R19; contracts/checks.md V008, guards G2, G3, G11, G13) against a
// fake OVHcloud API and a fake terminal. Nothing here reaches the real API or reads
// ~/.config/ovh-lz/: every run gets a fake home under t.TempDir().
//
// The fake API (bootstrapAPI) serves the OAuth2 client-credentials grant and the routes the
// phases need (kb/api/v1/auth.json, me.json, cloud.json; kb/api/v2/iam.json): GET /auth/details,
// GET /me (which identify must never call), GET /cloud/project, GET /auth/currentCredential,
// DELETE /me/api/credential/{id}, GET|POST /me/api/oauth2/client[/{id}], GET|POST|PUT
// /iam/policy[/{id}]. A root-key request must carry X-Ovh-Application, X-Ovh-Consumer,
// X-Ovh-Timestamp and X-Ovh-Signature = "$1$" + SHA1_HEX(AS+"+"+CK+"+"+METHOD+"+"+URL+"+"+BODY+
// "+"+TSTAMP) (kb guide manage-and-operate/api/first-steps.mdx "First API Usage"); the fake
// recomputes it and records a protocol issue whenever an application secret, or an application or
// consumer key outside its header, appears on the wire (research R19: signed in process). OAuth2
// clients are held to the allow actions of their IAM policies only (resources, except, deny,
// conditions and expiry are not evaluated, so a drifted admin stays able to read its own client
// and policy and the drift itself is what the tests observe). Every error answer echoes every fake secret,
// so code that prints the API's text leaks (T083/T084 lesson). Accounts, clients and policies
// come from testdata/bootstrap/world.json (the current sandbox is the T087 capture).
//
// The fake terminal (bsTerminal) answers ReadSecret (echo off: the answer is not in the
// transcript) from a queue of root keys and ReadLine (echo on: prompt and answer in the
// transcript) by the LZ_PROJECT_ID_<REF> the prompt names.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fixture values (testdata/bootstrap/world.json).
const (
	bsOldAccount  = "xx000001-ovh"
	bsNewAccount  = "xx000002-ovh"
	bsOldClient   = "EU.0000000000000001"
	bsOldPolicy   = "00000000-0000-4000-8000-000000000001"
	bsOldProject  = "f0000000000000000000000000000001"
	bsNewProjectA = "f0000000000000000000000000000002"
	bsNewProjectB = "f0000000000000000000000000000003"
	bsOldSecret   = "fakeOldAdminSecret9Kq3vW"
	bsRootFirst   = 4711 // the operator's root keys
	bsRootOther   = 4712 // an unrelated credential of the fresh account
	bsRootSecond  = 4713 // the operator's root keys of a retry
	bsAdminName   = "lz-sandbox-admin"
	bsPassphrase  = "TF_VAR_state_passphrase"
	bsRefState    = "LZ_PROJECT_ID_STATE"
	bsRefDemoDev  = "LZ_PROJECT_ID_DEMO_DEV"
)

// The admin policy as expected (research R13): exactly these actions, on the account and every
// Public Cloud project; the captured lz-sandbox-admin policy has exactly this shape.
var bsAdminActions = []string{"account:apiovh:iam/*", "account:apiovh:me/*", "publicCloudProject:apiovh:*"}

func bsAdminResources(account string) []string {
	return []string{"urn:v1:eu:resource:account:" + account, "urn:v1:eu:resource:publicCloudProject:*"}
}

// ---------------------------------------------------------------- fake API

type bsRootKey struct {
	CredentialID  int64  `json:"credentialId"`
	ApplicationID int64  `json:"applicationId"`
	AK            string `json:"applicationKey"`
	AS            string `json:"applicationSecret"`
	CK            string `json:"consumerKey"`
}

type bsClient struct {
	ClientID     string   `json:"clientId"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Flow         string   `json:"flow"`
	CallbackURLs []string `json:"callbackUrls"`
	Identity     string   `json:"identity"`
	CreatedAt    string   `json:"createdAt"`
	Secret       string   `json:"secret"` // the fake's own record, never served
}

type bsAccount struct {
	Account     string           `json:"account"`
	Projects    []string         `json:"projects"`
	Clients     []*bsClient      `json:"clients"`
	Policies    []map[string]any `json:"policies"`
	Credentials []bsRootKey      `json:"credentials"`
}

// bsCall is one request the fake answered. Auth is root:<credentialId>, client:<clientId>,
// grant:<clientId> (token request), none, or refused.
type bsCall struct {
	Method, Path, Query, Auth, Body string
	Status                          int
}

type bsBearer struct {
	acct   *bsAccount
	client *bsClient
}

type bootstrapAPI struct {
	srv      *httptest.Server
	mu       sync.Mutex
	accounts []*bsAccount
	tokens   map[string]bsBearer
	revoked  map[int64]bool
	rejected map[string]bool // client ids whose grant is refused
	hidden   map[string]bool // client ids GET /me/api/oauth2/client/{id} answers 404 for
	fail     map[string]int  // "METHOD /v1/path" → injected status
	calls    []bsCall
	issues   []string // protocol violations
	secrets  []string // every secret the fake knows, echoed in each error answer
	created  []*bsClient
	policies []map[string]any // created through POST /iam/policy
	log      func(string)
	onAdmin  func(account string) // called before every admin client or policy request
}

func newBootstrapAPI(t *testing.T) *bootstrapAPI {
	t.Helper()
	var w struct {
		Accounts []*bsAccount `json:"accounts"`
	}
	readJSON(t, filepath.Join("testdata", "bootstrap", "world.json"), &w)
	f := &bootstrapAPI{
		accounts: w.Accounts,
		tokens:   map[string]bsBearer{},
		revoked:  map[int64]bool{},
		rejected: map[string]bool{},
		hidden:   map[string]bool{},
		fail:     map[string]int{},
	}
	for _, a := range f.accounts {
		for _, c := range a.Clients {
			f.secrets = append(f.secrets, c.Secret)
		}
		for _, k := range a.Credentials {
			f.secrets = append(f.secrets, k.AK, k.AS, k.CK)
		}
	}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *bootstrapAPI) API() API {
	return API{TokenURL: f.srv.URL + "/auth/oauth2/token", BaseURL: f.srv.URL + "/v1", HTTP: f.srv.Client()}
}

func (f *bootstrapAPI) account(id string) *bsAccount {
	for _, a := range f.accounts {
		if a.Account == id {
			return a
		}
	}
	return nil
}

func (f *bootstrapAPI) rootKey(id int64) bsRootKey {
	for _, a := range f.accounts {
		for _, k := range a.Credentials {
			if k.CredentialID == id {
				return k
			}
		}
	}
	panic(fmt.Sprintf("no credential %d", id))
}

// Calls returns the calls answered since call number from.
func (f *bootstrapAPI) Calls(from int) []bsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls[from:])
}

func (f *bootstrapAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *bootstrapAPI) Issues() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.issues)
}

func (f *bootstrapAPI) Created() ([]*bsClient, []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.created), slices.Clone(f.policies)
}

func (f *bootstrapAPI) Revoked(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoked[id]
}

// errBody is an error answer that echoes every secret the fake knows.
func (f *bootstrapAPI) errBody(msg string) []byte {
	b, _ := json.Marshal(map[string]any{"class": "Client::Forbidden", "message": msg + " (" + strings.Join(f.secrets, " ") + ")"})
	return b
}

func (f *bootstrapAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wireCheck(r, body)
	c := bsCall{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body)}
	status, out := f.serve(r, body, &c)
	c.Status = status
	f.calls = append(f.calls, c)
	if f.log != nil {
		f.log(fmt.Sprintf("api %s %s %s %d", c.Method, c.Path, c.Auth, status))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(out)
}

// wireCheck records an application secret anywhere in a request, and an application or consumer
// key anywhere but its own header.
func (f *bootstrapAPI) wireCheck(r *http.Request, body []byte) {
	var hdr, other strings.Builder
	for k, vs := range r.Header {
		for _, v := range vs {
			fmt.Fprintf(&hdr, "%s: %s\n", k, v)
			if k != "X-Ovh-Application" && k != "X-Ovh-Consumer" {
				fmt.Fprintf(&other, "%s: %s\n", k, v)
			}
		}
	}
	all := r.URL.String() + "\n" + hdr.String() + "\n" + string(body)
	outside := r.URL.String() + "\n" + other.String() + "\n" + string(body)
	for _, a := range f.accounts {
		for _, k := range a.Credentials {
			if strings.Contains(all, k.AS) {
				f.issues = append(f.issues, fmt.Sprintf("%s %s: application secret of credential %d on the wire", r.Method, r.URL.Path, k.CredentialID))
			}
			if strings.Contains(outside, k.AK) || strings.Contains(outside, k.CK) {
				f.issues = append(f.issues, fmt.Sprintf("%s %s: key of credential %d outside its own header", r.Method, r.URL.Path, k.CredentialID))
			}
		}
	}
}

func (f *bootstrapAPI) serve(r *http.Request, body []byte, c *bsCall) (int, []byte) {
	switch r.URL.Path {
	case "/auth/oauth2/token":
		return f.grant(r, body, c)
	case "/v1/auth/time":
		c.Auth = "none"
		return http.StatusOK, []byte(strconv.FormatInt(time.Now().Unix(), 10))
	}
	acct, client, root, status := f.authenticate(r, body, c)
	if status != 0 {
		return status, f.errBody("authentication failed")
	}
	if s, ok := f.fail[r.Method+" "+r.URL.Path]; ok {
		return s, f.errBody("injected failure")
	}
	if f.onAdmin != nil && (strings.HasPrefix(r.URL.Path, "/v1/me/api/oauth2/client") || strings.HasPrefix(r.URL.Path, "/v2/iam/policy")) {
		f.onAdmin(acct.Account)
	}
	may := func(action string) bool {
		if root != nil {
			return true
		}
		var actions []string
		for _, p := range acct.Policies {
			if slices.Contains(bsStrings(p["identities"]), client.Identity) {
				actions = append(actions, bsActions(p, "allow")...)
			}
		}
		return allows(actions, action)
	}
	ok := func(v any) (int, []byte) {
		b, _ := json.Marshal(v)
		return http.StatusOK, b
	}
	forbidden := func() (int, []byte) { return http.StatusForbidden, f.errBody("not allowed") }
	notFound := func() (int, []byte) { return http.StatusNotFound, f.errBody("not found") }
	path, method := r.URL.Path, r.Method
	switch {
	case method == http.MethodGet && path == "/v1/auth/details":
		m := "oauth2_client_credentials"
		if root != nil {
			m = "account"
		}
		return ok(map[string]any{"account": acct.Account, "allowedRoutes": nil, "description": "fake", "identities": []string{}, "method": m, "roles": nil, "user": nil})
	case method == http.MethodGet && path == "/v1/me":
		if !may("account:apiovh:me/get") {
			return forbidden()
		}
		return ok(map[string]any{"nichandle": acct.Account})
	case method == http.MethodGet && path == "/v1/auth/currentCredential":
		if root == nil {
			return http.StatusBadRequest, f.errBody("not an application credential")
		}
		return ok(map[string]any{"credentialId": root.CredentialID, "applicationId": root.ApplicationID, "status": "validated",
			"creation": "2026-10-08T07:00:00Z", "expiration": "2026-10-09T07:00:00Z", "lastUse": "2026-10-08T07:05:00Z",
			"allowedIPs": nil, "ovhSupport": false, "rules": []map[string]string{{"method": "GET", "path": "/*"}, {"method": "POST", "path": "/*"}, {"method": "DELETE", "path": "/*"}}})
	case method == http.MethodDelete && strings.HasPrefix(path, "/v1/me/api/credential/"):
		if !may("account:apiovh:me/api/credential/delete") {
			return forbidden()
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(path, "/v1/me/api/credential/"), 10, 64)
		if err != nil {
			return http.StatusBadRequest, f.errBody("bad id")
		}
		for _, k := range acct.Credentials {
			if k.CredentialID == id {
				f.revoked[id] = true
				return http.StatusOK, []byte("null")
			}
		}
		return notFound()
	case method == http.MethodGet && path == "/v1/cloud/project":
		if !may("publicCloudProject:apiovh:get") {
			return forbidden()
		}
		return ok(acct.Projects)
	case method == http.MethodGet && path == "/v1/me/api/oauth2/client":
		if !may("account:apiovh:me/api/oauth2/client/get") {
			return forbidden()
		}
		ids := []string{}
		for _, cl := range acct.Clients {
			if !f.hidden[cl.ClientID] {
				ids = append(ids, cl.ClientID)
			}
		}
		return ok(ids)
	case method == http.MethodGet && strings.HasPrefix(path, "/v1/me/api/oauth2/client/"):
		if !may("account:apiovh:me/api/oauth2/client/get") {
			return forbidden()
		}
		id := strings.TrimPrefix(path, "/v1/me/api/oauth2/client/")
		for _, cl := range acct.Clients {
			if cl.ClientID == id && !f.hidden[id] {
				return ok(map[string]any{"clientId": cl.ClientID, "name": cl.Name, "description": cl.Description, "flow": cl.Flow,
					"callbackUrls": cl.CallbackURLs, "identity": cl.Identity, "createdAt": cl.CreatedAt})
			}
		}
		return notFound()
	case method == http.MethodPost && path == "/v1/me/api/oauth2/client":
		if !may("account:apiovh:me/api/oauth2/client/create") {
			return forbidden()
		}
		var req struct {
			CallbackURLs []string `json:"callbackUrls"`
			Description  string   `json:"description"`
			Flow         string   `json:"flow"`
			Name         string   `json:"name"`
		}
		if err := json.Unmarshal(body, &req); err != nil || req.Name == "" || req.Flow == "" {
			return http.StatusBadRequest, f.errBody("invalid client request")
		}
		n := len(f.created) + 1
		cl := &bsClient{ClientID: fmt.Sprintf("EU.fakecreated%04d", n), Name: req.Name, Description: req.Description, Flow: req.Flow,
			CallbackURLs: req.CallbackURLs, CreatedAt: "2026-10-08T07:10:00Z", Secret: fmt.Sprintf("fakeCreatedSecret%04dZr8Lq", n)}
		cl.Identity = "urn:v1:eu:identity:credential:" + acct.Account + "/oauth2-" + cl.ClientID
		acct.Clients = append(acct.Clients, cl)
		f.created = append(f.created, cl)
		f.secrets = append(f.secrets, cl.Secret)
		return ok(map[string]string{"clientId": cl.ClientID, "clientSecret": cl.Secret})
	case method == http.MethodGet && path == "/v2/iam/policy":
		if !may("account:apiovh:iam/policy/get") {
			return forbidden()
		}
		want := r.URL.Query()["identity"]
		out := []map[string]any{}
		for _, p := range acct.Policies {
			if len(want) == 0 || slices.ContainsFunc(bsStrings(p["identities"]), func(s string) bool { return slices.Contains(want, s) }) {
				out = append(out, p)
			}
		}
		return ok(out)
	case method == http.MethodGet && strings.HasPrefix(path, "/v2/iam/policy/"):
		if !may("account:apiovh:iam/policy/get") {
			return forbidden()
		}
		id := strings.TrimPrefix(path, "/v2/iam/policy/")
		for _, p := range acct.Policies {
			if p["id"] == id {
				return ok(p)
			}
		}
		return notFound()
	case method == http.MethodPost && path == "/v2/iam/policy":
		if !may("account:apiovh:iam/policy/create") {
			return forbidden()
		}
		var p map[string]any
		if err := json.Unmarshal(body, &p); err != nil || p["name"] == nil {
			return http.StatusBadRequest, f.errBody("invalid policy")
		}
		p["id"] = fmt.Sprintf("00000000-0000-4000-8000-0000000001%02d", len(f.policies)+1)
		p["owner"] = acct.Account
		p["readOnly"] = false
		p["createdAt"] = "2026-10-08T07:11:00Z"
		acct.Policies = append(acct.Policies, p)
		f.policies = append(f.policies, p)
		return ok(p)
	case method == http.MethodPut && strings.HasPrefix(path, "/v2/iam/policy/"):
		if !may("account:apiovh:iam/policy/edit") {
			return forbidden()
		}
		id := strings.TrimPrefix(path, "/v2/iam/policy/")
		for i, p := range acct.Policies {
			if p["id"] == id {
				var upd map[string]any
				if err := json.Unmarshal(body, &upd); err != nil {
					return http.StatusBadRequest, f.errBody("invalid policy")
				}
				for _, k := range []string{"id", "owner", "readOnly", "createdAt"} {
					upd[k] = p[k]
				}
				acct.Policies[i] = upd
				return ok(upd)
			}
		}
		return notFound()
	}
	return notFound()
}

// grant is the OAuth2 client-credentials grant (OVHcloud guide "Authenticate to the API with a
// service account"; binding.go's token request).
func (f *bootstrapAPI) grant(r *http.Request, body []byte, c *bsCall) (int, []byte) {
	form, err := parseForm(body)
	if err != nil || r.Method != http.MethodPost || form["grant_type"] != "client_credentials" {
		c.Auth = "refused"
		return http.StatusBadRequest, f.errBody("bad grant")
	}
	c.Auth = "grant:" + form["client_id"]
	for _, a := range f.accounts {
		for _, cl := range a.Clients {
			if cl.ClientID == form["client_id"] && cl.Secret == form["client_secret"] && !f.rejected[cl.ClientID] {
				tok := fmt.Sprintf("fake-bootstrap-token-%d", len(f.tokens)+1)
				f.tokens[tok] = bsBearer{acct: a, client: cl}
				b, _ := json.Marshal(map[string]any{"access_token": tok, "token_type": "Bearer", "expires_in": 3600, "scope": "all"})
				return http.StatusOK, b
			}
		}
	}
	return http.StatusUnauthorized, f.errBody("invalid_client")
}

func parseForm(body []byte) (map[string]string, error) {
	out := map[string]string{}
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	for k, v := range vals {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out, nil
}

// authenticate identifies a bearer token or a signed root-key request; status is non-zero on a
// refusal.
func (f *bootstrapAPI) authenticate(r *http.Request, body []byte, c *bsCall) (*bsAccount, *bsClient, *bsRootKey, int) {
	if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		b, known := f.tokens[tok]
		if !known {
			c.Auth = "refused"
			return nil, nil, nil, http.StatusUnauthorized
		}
		c.Auth = "client:" + b.client.ClientID
		return b.acct, b.client, nil, 0
	}
	ak := r.Header.Get("X-Ovh-Application")
	if ak == "" {
		c.Auth = "none"
		return nil, nil, nil, http.StatusUnauthorized
	}
	for _, a := range f.accounts {
		for i := range a.Credentials {
			k := &a.Credentials[i]
			if k.AK != ak {
				continue
			}
			ts := r.Header.Get("X-Ovh-Timestamp")
			u := "http://" + r.Host + r.URL.RequestURI()
			sum := sha1.Sum([]byte(k.AS + "+" + k.CK + "+" + r.Method + "+" + u + "+" + string(body) + "+" + ts))
			if r.Header.Get("X-Ovh-Consumer") != k.CK || ts == "" || r.Header.Get("X-Ovh-Signature") != "$1$"+hex.EncodeToString(sum[:]) {
				c.Auth = "refused"
				f.issues = append(f.issues, fmt.Sprintf("%s %s: bad signature for credential %d", r.Method, r.URL.Path, k.CredentialID))
				return nil, nil, nil, http.StatusForbidden
			}
			if f.revoked[k.CredentialID] {
				c.Auth = fmt.Sprintf("root-revoked:%d", k.CredentialID) // a use of the keys after revocation
				return nil, nil, nil, http.StatusForbidden
			}
			c.Auth = fmt.Sprintf("root:%d", k.CredentialID)
			return a, nil, k, 0
		}
	}
	c.Auth = "refused"
	return nil, nil, nil, http.StatusForbidden
}

func bsStrings(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	if l, ok := v.([]string); ok {
		out = append(out, l...)
	}
	return out
}

// bsActions lists a policy's permissions.<kind>[].action.
func bsActions(p map[string]any, kind string) []string {
	perms, _ := p["permissions"].(map[string]any)
	var out []string
	if l, ok := perms[kind].([]any); ok {
		for _, x := range l {
			if m, ok := x.(map[string]any); ok {
				if s, ok := m["action"].(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

func bsResources(p map[string]any) []string {
	var out []string
	if l, ok := p["resources"].([]any); ok {
		for _, x := range l {
			if m, ok := x.(map[string]any); ok {
				if s, ok := m["urn"].(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- fake terminal

type termRead struct {
	Kind   string // secret or line
	Prompt string
}

type bsTerminal struct {
	mu         sync.Mutex
	secrets    []string          // ReadSecret answers, in order
	lines      map[string]string // LZ_PROJECT_ID_<REF> → ReadLine answer
	abort      string            // the reference whose prompt the operator aborts
	transcript strings.Builder
	reads      []termRead
	shownFirst string        // what the operator had seen when the first line prompt came
	shown      func() string // the run's stdout so far
	log        func(string)
}

var errOperatorAbort = errors.New("operator aborted the prompt")

func (f *bsTerminal) ReadSecret(prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, termRead{"secret", prompt})
	f.transcript.WriteString(prompt + "\n") // echo off: the answer is not shown
	if f.log != nil {
		f.log("terminal secret")
	}
	if len(f.secrets) == 0 {
		return "", io.EOF
	}
	s := f.secrets[0]
	f.secrets = f.secrets[1:]
	return s, nil
}

func (f *bsTerminal) ReadLine(prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.ContainsFunc(f.reads, func(r termRead) bool { return r.Kind == "line" }) && f.shown != nil {
		f.shownFirst = f.transcript.String() + f.shown()
	}
	f.reads = append(f.reads, termRead{"line", prompt})
	if f.log != nil {
		f.log("terminal line")
	}
	ref := ""
	for k := range f.lines {
		if strings.Contains(prompt, k) && len(k) > len(ref) {
			ref = k
		}
	}
	if f.abort != "" && strings.Contains(prompt, f.abort) {
		f.transcript.WriteString(prompt + "^C\n")
		return "", errOperatorAbort
	}
	if ref == "" {
		f.transcript.WriteString(prompt + "\n")
		return "", fmt.Errorf("fake terminal: unexpected prompt %q", prompt)
	}
	f.transcript.WriteString(prompt + f.lines[ref] + "\n") // echo on
	return f.lines[ref], nil
}

func (f *bsTerminal) Reads() []termRead {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reads)
}

func (f *bsTerminal) Transcript() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transcript.String()
}

// queueRoot queues the three root keys of credential id, in the order AK, AS, CK.
func (f *bsTerminal) queueRoot(k bsRootKey) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets = append(f.secrets, k.AK, k.AS, k.CK)
}

// ---------------------------------------------------------------- harness

type bsHarness struct {
	t        *testing.T
	base     string
	root     string // the fake ~/.config/ovh-lz
	checkout string
	bin      string // recording tofu, ovhcloud, curl, wget, openssl first on PATH
	api      *bootstrapAPI
	term     *bsTerminal
	stdout   *syncBuffer
	org      string
	guardErr error
	// stateFn is the test's state phase (Rest); nil succeeds.
	stateFn  func(ctx context.Context) error
	cancel   context.CancelFunc
	mu       sync.Mutex
	events   []string
	restSeen []BootstrapAccount
	issues   []string // what the Rest hook found wrong
	from     int      // first API call of the current run
}

func newBSHarness(t *testing.T) *bsHarness {
	t.Helper()
	base := t.TempDir()
	h := &bsHarness{t: t, base: base, org: "lz", stdout: &syncBuffer{}}
	h.root = filepath.Join(base, "home", ".config", "ovh-lz")
	bsMkdirPrivate(t, h.root)
	h.checkout = filepath.Join(base, "checkout")
	if err := os.MkdirAll(h.checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.checkout, "README.md"), []byte("checkout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(base, "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	bsWrite(t, filepath.Join(h.root, "live.env"), "LZ_OWNER_CHECKOUT="+h.checkout+"\nLZ_AGENT_WORKTREE_ROOT="+agents+"\n")
	h.bin = filepath.Join(base, "bin")
	if err := os.MkdirAll(h.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Recorders for the children a bootstrap could start with the root keys (the CLIs it could
	// reach the API through): each logs its argv and environment to bin/children.log.
	for _, name := range []string{"tofu", "ovhcloud", "curl", "wget", "openssl"} {
		script := "#!/bin/sh\n{ printf 'argv: %s\\n' \"$0 $*\"; env; } >> '" + filepath.Join(h.bin, "children.log") + "'\nexit 1\n"
		if err := os.WriteFile(filepath.Join(h.bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", h.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// The fake home, so an implementation that ignores ConfigRoot finds no real ~/.config/ovh-lz/.
	t.Setenv("HOME", filepath.Join(base, "home"))
	h.api = newBootstrapAPI(t)
	h.api.log = h.event
	// Phase order observed, not self-reported: the admin phase's requests come after the passphrase.
	h.api.onAdmin = func(account string) {
		if _, err := os.Stat(filepath.Join(h.accountDir(account), "state-passphrase.env")); err != nil {
			h.issue("admin client or policy request for %s before its passphrase exists", account)
		}
	}
	h.term = &bsTerminal{lines: map[string]string{}, shown: h.stdout.String, log: h.event}
	return h
}

func bsMkdirPrivate(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func bsWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func bsRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (h *bsHarness) event(e string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
}

func (h *bsHarness) Events() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.events)
}

func (h *bsHarness) accountDir(account string) string {
	return filepath.Join(h.root, "accounts", account)
}

// seedCurrentSandbox writes the current sandbox's files (data-model *Account binding*): the admin
// credential and the binding with its project references; no passphrase yet (T044's first run).
func (h *bsHarness) seedCurrentSandbox() {
	h.t.Helper()
	bsWrite(h.t, filepath.Join(h.root, "sandbox.env"), "OVH_ENDPOINT=ovh-eu\nOVH_CLIENT_ID="+bsOldClient+"\nOVH_CLIENT_SECRET="+bsOldSecret+"\n")
	bsMkdirPrivate(h.t, filepath.Join(h.root, "accounts"))
	bsMkdirPrivate(h.t, h.accountDir(bsOldAccount))
	bsWrite(h.t, filepath.Join(h.accountDir(bsOldAccount), "account.env"),
		"LZ_ACCOUNT_ID="+bsOldAccount+"\nOVH_ENDPOINT=ovh-eu\nLZ_ORG=lz\n"+bsRefState+"="+bsOldProject+"\n"+bsRefDemoDev+"="+bsOldProject+"\n")
}

// seedPreviousAccount is the current sandbox after its own bootstrap: also its passphrase and
// the recorded admin ids (journey b's previous account).
func (h *bsHarness) seedPreviousAccount() {
	h.t.Helper()
	h.seedCurrentSandbox()
	dir := h.accountDir(bsOldAccount)
	bsWrite(h.t, filepath.Join(dir, "account.env"), bsRead(h.t, filepath.Join(dir, "account.env"))+
		"LZ_ADMIN_CLIENT_ID="+bsOldClient+"\nLZ_ADMIN_POLICY_ID="+bsOldPolicy+"\n")
	bsWrite(h.t, filepath.Join(dir, "state-passphrase.env"), bsPassphrase+"=fakeOldPassphrase3c1d8e\n")
}

// answerRefs answers the project-reference prompts of the fresh account.
func (h *bsHarness) answerRefs() {
	h.term.lines[bsRefState] = bsNewProjectA
	h.term.lines[bsRefDemoDev] = bsNewProjectB
}

func (h *bsHarness) rootSecrets() []string {
	var out []string
	for _, id := range []int64{bsRootFirst, bsRootOther, bsRootSecond} {
		k := h.api.rootKey(id)
		out = append(out, k.AK, k.AS, k.CK)
	}
	return out
}

func (h *bsHarness) options(fresh bool) BootstrapOptions {
	return BootstrapOptions{
		ConfigRoot:   h.root,
		Checkout:     h.checkout,
		Endpoint:     "ovh-eu",
		Org:          h.org,
		ProjectRefs:  []string{"STATE", "DEMO_DEV"},
		FreshAccount: fresh,
		API:          h.api.API(),
		Terminal:     h.term,
		Stdout:       h.stdout,
		Guard: func() error {
			h.event("guard")
			return h.guardErr
		},
		Rest: h.rest,
	}
}

func (h *bsHarness) run(fresh bool) ([]PhaseResult, error) {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.cancel = cancel
	h.from = h.api.count()
	h.event("run")
	// The process's own stdout and stderr are captured too: an implementation writing there
	// instead of the injected Stdout would otherwise escape the leak scan.
	outFile, oerr := os.OpenFile(filepath.Join(h.base, "process-stdout.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	errFile, eerr := os.OpenFile(filepath.Join(h.base, "process-stderr.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if oerr != nil || eerr != nil {
		h.t.Fatal(oerr, eerr)
	}
	savedOut, savedErr, savedLog := os.Stdout, os.Stderr, log.Writer()
	os.Stdout, os.Stderr = outFile, errFile
	log.SetOutput(errFile) // the standard logger holds the original stderr
	defer func() {
		os.Stdout, os.Stderr = savedOut, savedErr
		log.SetOutput(savedLog)
		outFile.Close()
		errFile.Close()
	}()
	done := make(chan struct{})
	var rs []PhaseResult
	var err error
	go func() {
		defer close(done)
		rs, err = Bootstrap(ctx, h.options(fresh))
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		h.t.Fatal("Bootstrap did not return within 60s")
	}
	h.t.Logf("phases %v err %v", rs, err)
	return rs, err
}

// rest stands in for the state, publish and verify phases (T056/T057): the first phase that
// writes encrypted state, and the one that starts children.
func (h *bsHarness) rest(ctx context.Context, a BootstrapAccount) error {
	h.event("rest")
	h.mu.Lock()
	h.restSeen = append(h.restSeen, a)
	h.mu.Unlock()
	pass := filepath.Join(a.Dir, "state-passphrase.env")
	if vals, err := ReadCredentialFile(pass); err != nil {
		h.issue("passphrase file before the state phase: %v", err)
	} else if vals[bsPassphrase] == "" {
		h.issue("passphrase file before the state phase holds no %s", bsPassphrase)
	}
	env := strings.Join(os.Environ(), "\n")
	seen := fmt.Sprintf("%+v", a)
	for _, s := range h.rootSecrets() {
		if strings.Contains(env, s) {
			h.issue("the process environment holds a root key during the state phase (a child would inherit it)")
		}
		if strings.Contains(seen, s) {
			h.issue("the state phase was handed a root key")
		}
	}
	if h.stateFn != nil {
		return h.stateFn(ctx)
	}
	return nil
}

func (h *bsHarness) issue(format string, a ...any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.issues = append(h.issues, fmt.Sprintf(format, a...))
}

func (h *bsHarness) Rest() ([]BootstrapAccount, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.restSeen), slices.Clone(h.issues)
}

// ---------------------------------------------------------------- assertions

func bsPhases(rs []PhaseResult) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Phase)
	}
	return out
}

func bsResult(rs []PhaseResult, phase string) (PhaseResult, bool) {
	for _, r := range rs {
		if r.Phase == phase {
			return r, true
		}
	}
	return PhaseResult{}, false
}

func bsWantStatus(t *testing.T, rs []PhaseResult, phase, want string) {
	t.Helper()
	r, ok := bsResult(rs, phase)
	if !ok {
		t.Errorf("phase %s: not reported (phases %v)", phase, bsPhases(rs))
		return
	}
	if r.Status != want {
		t.Errorf("phase %s: status %q (%s), want %q", phase, r.Status, r.Detail, want)
	}
}

func bsNoBlocked(t *testing.T, rs []PhaseResult) {
	t.Helper()
	for _, r := range rs {
		if r.Status == StatusBlocked {
			t.Errorf("phase %s blocked (%s): a fresh run never stops before the admin credential exists", r.Phase, r.Detail)
		}
	}
}

// bsWrites lists the calls that change the account (anything but GET, token requests excepted).
func bsWrites(calls []bsCall) []string {
	var out []string
	for _, c := range calls {
		if c.Method != http.MethodGet && c.Path != "/auth/oauth2/token" {
			out = append(out, c.Method+" "+c.Path)
		}
	}
	return out
}

// bsRootCalls lists the calls made with root keys, also those refused because the keys were
// already revoked.
func bsRootCalls(calls []bsCall) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c.Auth, "root:") || strings.HasPrefix(c.Auth, "root-revoked:") {
			out = append(out, c.Method+" "+c.Path+" "+c.Auth)
		}
	}
	return out
}

// bsNoMe: identify reads the account from GET /auth/details (P26), never GET /me.
func bsNoMe(t *testing.T, calls []bsCall) {
	t.Helper()
	for _, c := range calls {
		if c.Path == "/v1/me" {
			t.Errorf("GET /me called (%s): the account comes from GET /auth/details (P26)", c.Auth)
		}
	}
}

func bsFound(calls []bsCall, method, path, auth string) bool {
	return slices.ContainsFunc(calls, func(c bsCall) bool {
		return c.Method == method && c.Path == path && c.Auth == auth && c.Status == http.StatusOK
	})
}

// bsFiles lists every entry under dir with its permission bits ("rel 0600", "rel/ 0700").
func bsFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			rel += "/"
		}
		out = append(out, fmt.Sprintf("%s %04o", rel, fi.Mode().Perm()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// bsNoLeak checks that no secret appears in the run's stdout, the terminal transcript, the error,
// the phase results, the process environment, a child's log, the checkout, or any file under the
// config root except the files allowed for it (G2, G3; SC-005). Root keys are allowed nowhere.
func bsNoLeak(t *testing.T, h *bsHarness, rs []PhaseResult, runErr error, allowed map[string][]string) {
	t.Helper()
	secrets := map[string][]string{}
	for _, s := range h.rootSecrets() {
		secrets[s] = nil
	}
	for s, files := range allowed {
		secrets[s] = files
	}
	streams := map[string]string{
		"stdout":              h.stdout.String(),
		"terminal transcript": h.term.Transcript(),
		"phase results":       fmt.Sprintf("%+v", rs),
		"process environment": strings.Join(os.Environ(), "\n"),
	}
	for _, name := range []string{"process-stdout.txt", "process-stderr.txt"} {
		raw, _ := os.ReadFile(filepath.Join(h.base, name))
		streams[strings.TrimSuffix(name, ".txt")] = string(raw)
	}
	if runErr != nil {
		streams["error"] = runErr.Error()
	}
	for name, text := range streams {
		for s := range secrets {
			if strings.Contains(text, s) {
				t.Errorf("a secret (%.6s…) reached the %s", s, name)
			}
		}
	}
	scan := func(dir string, rel func(string) string) {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for s, ok := range secrets {
				if strings.Contains(string(raw), s) && !slices.Contains(ok, rel(p)) {
					t.Errorf("a secret (%.6s…) was written to %s", s, p)
				}
			}
			return nil
		})
	}
	scan(h.root, func(p string) string { r, _ := filepath.Rel(h.root, p); return r })
	scan(h.checkout, func(string) string { return "" })
	scan(h.bin, func(string) string { return "" })
	if logs, _ := filepath.Glob(filepath.Join(h.bin, "*.log")); len(logs) > 0 {
		t.Errorf("the phases started a child process: %v", logs)
	}
	if issues := h.api.Issues(); len(issues) > 0 {
		t.Errorf("protocol issues on the wire:\n%s", strings.Join(issues, "\n"))
	}
	if _, issues := h.Rest(); len(issues) > 0 {
		t.Errorf("harness saw (state phase, admin requests):\n%s", strings.Join(issues, "\n"))
	}
}

// bsExit checks the exit code the contract gives the error (contracts/checks.md lz-live: 0 pass,
// 1 fail, 2 blocked, 3 refused).
func bsExit(t *testing.T, err error, want int) {
	t.Helper()
	if got := ExitCode(err); got != want {
		t.Errorf("exit %d (%v), want %d", got, err, want)
	}
}

// ---------------------------------------------------------------- tests

// TestBootstrapAdminGuardFirst: the guard runs before anything else; its refusal ends the run
// before a credential is read, a prompt shown, the API called or a file written.
func TestBootstrapAdminGuardFirst(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("fresh=%v", fresh), func(t *testing.T) {
			h := newBSHarness(t)
			h.seedCurrentSandbox()
			h.term.queueRoot(h.api.rootKey(bsRootFirst))
			h.guardErr = refuse(CondDirty, "fake dirty tree")
			before := treeSnapshot(t, h.root)
			watch := watchEvents(t, h.root, h.accountDir(bsOldAccount))
			rs, err := h.run(fresh)
			events := watch()
			var r *Refusal
			if !errors.As(err, &r) || r.Condition != CondDirty {
				t.Errorf("err %v, want the guard's refusal", err)
			}
			bsExit(t, err, RefusalExit)
			if ev := h.Events(); len(ev) < 2 || ev[1] != "guard" {
				t.Errorf("events %v: the guard is the first phase", ev)
			}
			if calls := h.api.Calls(h.from); len(calls) > 0 {
				t.Errorf("API called after a guard refusal: %v", calls)
			}
			if reads := h.term.Reads(); len(reads) > 0 {
				t.Errorf("terminal read after a guard refusal: %v", reads)
			}
			if rest, _ := h.Rest(); len(rest) > 0 {
				t.Error("later phases ran after a guard refusal")
			}
			if after := treeSnapshot(t, h.root); !bsEqual(before, after) {
				t.Error("files changed after a guard refusal")
			}
			for _, e := range events {
				if e.Mask&evOpen != 0 && e.Name != "" {
					t.Errorf("%s/%s opened after a guard refusal: no credential is read before the guard admits", e.Dir, e.Name)
				}
			}
			for _, p := range rs {
				if p.Phase != PhaseGuard && (p.Status == StatusRan || p.Status == StatusUnchanged) {
					t.Errorf("phase %s reported %s after a guard refusal", p.Phase, p.Status)
				}
			}
		})
	}
}

func bsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// TestBootstrapAdminPhaseOrder: guard, identify, passphrase, admin (then the state phase, then
// revoke with --fresh-account); the passphrase exists before the state phase, the first that
// writes encrypted state; the state phase runs with the admin credential, never the root keys.
func TestBootstrapAdminPhaseOrder(t *testing.T) {
	t.Run("current-sandbox", func(t *testing.T) {
		h := newBSHarness(t)
		h.seedCurrentSandbox()
		rs, err := h.run(false)
		if err != nil {
			t.Errorf("err %v", err)
		}
		if got, want := bsPhases(rs), []string{PhaseGuard, PhaseIdentify, PhasePassphrase, PhaseAdmin}; !slices.Equal(got, want) {
			t.Errorf("phases %v, want %v", got, want)
		}
		ev := h.Events()
		if len(ev) < 2 || ev[1] != "guard" {
			t.Errorf("events %v: the guard runs first", ev)
		}
		rest, issues := h.Rest()
		if len(rest) != 1 {
			t.Fatalf("state phase ran %d times, want once after admin", len(rest))
		}
		if len(issues) > 0 {
			t.Errorf("state phase saw: %v", issues)
		}
		want := BootstrapAccount{ID: bsOldAccount, Dir: h.accountDir(bsOldAccount), Admin: Credential{Endpoint: "ovh-eu", ClientID: bsOldClient, ClientSecret: bsOldSecret}}
		if rest[0] != want {
			t.Errorf("state phase got %+v, want the bound account and the sandbox.env credential", rest[0].ID)
		}
		if i := slices.Index(ev, "rest"); i < 0 || slices.ContainsFunc(ev[i:], func(e string) bool { return strings.HasPrefix(e, "api ") && strings.Contains(e, "/oauth2/client") }) {
			t.Errorf("events %v: the state phase runs after the admin checks", ev)
		}
	})
	t.Run("fresh-account", func(t *testing.T) {
		h := newBSHarness(t)
		h.term.queueRoot(h.api.rootKey(bsRootFirst))
		h.answerRefs()
		rs, err := h.run(true)
		if err != nil {
			t.Errorf("err %v", err)
		}
		if got, want := bsPhases(rs), []string{PhaseGuard, PhaseIdentify, PhasePassphrase, PhaseAdmin, PhaseRevoke}; !slices.Equal(got, want) {
			t.Errorf("phases %v, want %v", got, want)
		}
		ev := h.Events()
		if len(ev) < 2 || ev[1] != "guard" {
			t.Errorf("events %v: the guard runs first", ev)
		}
		rest, issues := h.Rest()
		if len(rest) != 1 {
			t.Fatalf("state phase ran %d times, want once", len(rest))
		}
		if len(issues) > 0 {
			t.Errorf("state phase saw: %v", issues)
		}
		i := slices.Index(ev, "rest")
		j := slices.IndexFunc(ev, func(e string) bool { return strings.HasPrefix(e, "api DELETE /v1/me/api/credential/") })
		if i < 0 || j < i {
			t.Errorf("events %v: revoke runs after the state phase", ev)
		}
		created, _ := h.api.Created()
		if len(created) == 1 && rest[0].Admin.ClientID != created[0].ClientID {
			t.Errorf("state phase runs as %q, want the created admin client", rest[0].Admin.ClientID)
		}
	})
}

// TestBootstrapAdminCurrentSandbox: on the current sandbox (T009/T087 capture) the first run
// keeps the binding, creates the passphrase, reports admin unchanged and records the admin ids;
// a second run changes nothing. No root key is asked for or accepted without --fresh-account.
func TestBootstrapAdminCurrentSandbox(t *testing.T) {
	h := newBSHarness(t)
	h.seedCurrentSandbox()
	h.term.queueRoot(h.api.rootKey(bsRootFirst)) // offered, never to be read
	rs, err := h.run(false)
	if err != nil {
		t.Errorf("first run: %v", err)
	}
	bsWantStatus(t, rs, PhaseIdentify, StatusUnchanged)
	bsWantStatus(t, rs, PhasePassphrase, StatusRan)
	bsWantStatus(t, rs, PhaseAdmin, StatusUnchanged)
	if _, ok := bsResult(rs, PhaseRevoke); ok {
		t.Error("revoke reported without --fresh-account")
	}
	calls := h.api.Calls(h.from)
	if w := bsWrites(calls); len(w) > 0 {
		t.Errorf("the run changed the account: %v", w)
	}
	if r := bsRootCalls(calls); len(r) > 0 {
		t.Errorf("root keys used without --fresh-account: %v", r)
	}
	if !bsFound(calls, http.MethodGet, "/v1/auth/details", "client:"+bsOldClient) {
		t.Error("identify did not read the account from GET /auth/details with the sandbox.env credential")
	}
	bsNoMe(t, calls)
	if reads := h.term.Reads(); len(reads) > 0 {
		t.Errorf("terminal read without --fresh-account: %v", reads)
	}
	env := readEnvFile(t, filepath.Join(h.accountDir(bsOldAccount), "account.env"))
	want := map[string]string{"LZ_ACCOUNT_ID": bsOldAccount, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz", bsRefState: bsOldProject,
		bsRefDemoDev: bsOldProject, "LZ_ADMIN_CLIENT_ID": bsOldClient, "LZ_ADMIN_POLICY_ID": bsOldPolicy}
	if !bsEqual(env, want) {
		t.Errorf("account.env %v, want %v (binding kept, admin ids recorded for the leftover exemption)", env, want)
	}
	if got := bsRead(t, filepath.Join(h.root, "sandbox.env")); !strings.Contains(got, "OVH_CLIENT_SECRET="+bsOldSecret) {
		t.Error("sandbox.env changed on the current sandbox")
	}
	bsNoLeak(t, h, rs, err, map[string][]string{bsOldSecret: {"sandbox.env"}})

	before := treeSnapshot(t, h.root)
	rs, err = h.run(false)
	if err != nil {
		t.Errorf("second run: %v", err)
	}
	for _, p := range []string{PhaseIdentify, PhasePassphrase, PhaseAdmin} {
		bsWantStatus(t, rs, p, StatusUnchanged)
	}
	if w := bsWrites(h.api.Calls(h.from)); len(w) > 0 {
		t.Errorf("second run changed the account: %v", w)
	}
	if !bsEqual(before, treeSnapshot(t, h.root)) {
		t.Error("second run changed files")
	}
}

// TestBootstrapAdminDrift: admin is `unchanged` only when the credential works and the client and
// its policy are exactly as expected; a drifted policy is `fail` naming the difference (repair is
// the --fresh-account path, never the admin editing its own policy); a missing client or policy is
// `blocked` without --fresh-account.
func TestBootstrapAdminDrift(t *testing.T) {
	admin := func(f *bootstrapAPI) map[string]any {
		for _, p := range f.account(bsOldAccount).Policies {
			if p["id"] == bsOldPolicy {
				return p
			}
		}
		panic("no admin policy")
	}
	allow := func(p map[string]any) []any { return p["permissions"].(map[string]any)["allow"].([]any) }
	cases := []struct {
		name   string
		mutate func(f *bootstrapAPI)
		status string
		detail []string // any of these names the difference
	}{
		{"extra-action", func(f *bootstrapAPI) {
			p := admin(f)
			p["permissions"].(map[string]any)["allow"] = append(allow(p), map[string]any{"action": "account:apiovh:*"})
		}, StatusFail, []string{"account:apiovh:*"}},
		{"missing-action", func(f *bootstrapAPI) {
			p := admin(f)
			p["permissions"].(map[string]any)["allow"] = allow(p)[:2]
		}, StatusFail, []string{"publicCloudProject:apiovh:*"}},
		{"extra-resource", func(f *bootstrapAPI) {
			p := admin(f)
			p["resources"] = append(p["resources"].([]any), map[string]any{"urn": "urn:v1:eu:resource:dnsZone:*"})
		}, StatusFail, []string{"urn:v1:eu:resource:dnsZone:*"}},
		{"missing-resource", func(f *bootstrapAPI) {
			p := admin(f)
			p["resources"] = p["resources"].([]any)[1:]
		}, StatusFail, []string{"urn:v1:eu:resource:account:" + bsOldAccount}},
		{"except-action", func(f *bootstrapAPI) {
			p := admin(f)
			p["permissions"].(map[string]any)["except"] = []any{map[string]any{"action": "account:apiovh:iam/policy/delete"}}
		}, StatusFail, []string{"account:apiovh:iam/policy/delete"}},
		{"deny-action", func(f *bootstrapAPI) {
			p := admin(f)
			p["permissions"].(map[string]any)["deny"] = []any{map[string]any{"action": "account:apiovh:me/api/oauth2/client/delete"}}
		}, StatusFail, []string{"account:apiovh:me/api/oauth2/client/delete"}},
		{"conditions", func(f *bootstrapAPI) {
			admin(f)["conditions"] = map[string]any{"operator": "MATCH", "values": map[string]any{"date(Europe/Paris).WeekDay": "monday"}}
		}, StatusFail, []string{"condition"}},
		{"expiry", func(f *bootstrapAPI) {
			admin(f)["expiredAt"] = "2026-12-31T00:00:00Z"
		}, StatusFail, []string{"expir"}},
		{"client-flow", func(f *bootstrapAPI) {
			f.account(bsOldAccount).Clients[0].Flow = "AUTHORIZATION_CODE"
		}, StatusFail, []string{"AUTHORIZATION_CODE"}},
		{"permissions-group", func(f *bootstrapAPI) {
			p := admin(f)
			p["permissionsGroups"] = []any{map[string]any{"urn": "urn:v1:eu:permissionsGroup:ovhLegacy:ovh-default-owner"}}
		}, StatusFail, []string{"ovh-default-owner"}},
		{"second-policy", func(f *bootstrapAPI) {
			a := f.account(bsOldAccount)
			a.Policies = append(a.Policies, map[string]any{"id": "00000000-0000-4000-8000-000000000009", "owner": bsOldAccount, "name": "lz-extra",
				"readOnly": false, "identities": []any{"urn:v1:eu:identity:credential:" + bsOldAccount + "/oauth2-" + bsOldClient},
				"resources":         []any{map[string]any{"urn": "urn:v1:eu:resource:account:" + bsOldAccount}},
				"permissions":       map[string]any{"allow": []any{map[string]any{"action": "account:apiovh:*"}}},
				"permissionsGroups": []any{}, "createdAt": "2026-10-02T00:00:00Z"})
		}, StatusFail, []string{"lz-extra", "account:apiovh:*", "00000000-0000-4000-8000-000000000009"}},
		{"policy-missing", func(f *bootstrapAPI) {
			a := f.account(bsOldAccount)
			a.Policies = slices.DeleteFunc(a.Policies, func(p map[string]any) bool { return p["id"] == bsOldPolicy })
		}, StatusBlocked, []string{"--fresh-account"}},
		{"policy-for-another-identity", func(f *bootstrapAPI) {
			admin(f)["identities"] = []any{"urn:v1:eu:identity:credential:" + bsOldAccount + "/oauth2-EU.0000000000000077"}
		}, StatusBlocked, []string{"--fresh-account"}},
		{"client-missing", func(f *bootstrapAPI) { f.hidden[bsOldClient] = true }, StatusBlocked, []string{"--fresh-account"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newBSHarness(t)
			h.seedCurrentSandbox()
			tc.mutate(h.api)
			rs, err := h.run(false)
			r, ok := bsResult(rs, PhaseAdmin)
			if !ok || r.Status != tc.status {
				t.Errorf("admin %+v, want %s", r, tc.status)
			}
			if !slices.ContainsFunc(tc.detail, func(d string) bool { return strings.Contains(r.Detail, d) }) {
				t.Errorf("admin detail %q names none of %v", r.Detail, tc.detail)
			}
			if tc.status == StatusFail {
				bsExit(t, err, 1)
			} else {
				bsExit(t, err, 2)
			}
			if rest, _ := h.Rest(); len(rest) > 0 {
				t.Error("the state phase ran after the admin check failed")
			}
			calls := h.api.Calls(h.from)
			if w := bsWrites(calls); len(w) > 0 {
				t.Errorf("the run changed the account (repair needs --fresh-account and root keys): %v", w)
			}
			if r := bsRootCalls(calls); len(r) > 0 {
				t.Errorf("root keys used without --fresh-account: %v", r)
			}
			if reads := h.term.Reads(); len(reads) > 0 {
				t.Errorf("terminal read without --fresh-account: %v", reads)
			}
			bsNoLeak(t, h, rs, err, map[string][]string{bsOldSecret: {"sandbox.env"}})
		})
	}
}

// TestBootstrapAdminBlockedWithoutFresh: a rejected sandbox.env credential, or none at all, is
// `blocked` (exit 2) naming --fresh-account; no root key is asked for.
func TestBootstrapAdminBlockedWithoutFresh(t *testing.T) {
	t.Run("credential-rejected", func(t *testing.T) {
		h := newBSHarness(t)
		h.seedCurrentSandbox()
		h.api.rejected[bsOldClient] = true
		h.term.queueRoot(h.api.rootKey(bsRootFirst))
		rs, err := h.run(false)
		bsExit(t, err, 2)
		blocked := slices.ContainsFunc(rs, func(r PhaseResult) bool {
			return (r.Phase == PhaseIdentify || r.Phase == PhaseAdmin) && r.Status == StatusBlocked && strings.Contains(r.Detail, "--fresh-account")
		})
		if !blocked {
			t.Errorf("phases %+v: a rejected credential is blocked at identify or admin, naming --fresh-account", rs)
		}
		if r, ok := bsResult(rs, PhaseAdmin); ok && r.Status == StatusUnchanged {
			t.Error("admin unchanged with a rejected credential")
		}
		if rest, _ := h.Rest(); len(rest) > 0 {
			t.Error("the state phase ran with a rejected credential")
		}
		if reads := h.term.Reads(); len(reads) > 0 {
			t.Errorf("terminal read without --fresh-account: %v", reads)
		}
		calls := h.api.Calls(h.from)
		if w := bsWrites(calls); len(w) > 0 {
			t.Errorf("the run changed the account: %v", w)
		}
		if r := bsRootCalls(calls); len(r) > 0 {
			t.Errorf("root keys used without --fresh-account: %v", r)
		}
		bsNoLeak(t, h, rs, err, map[string][]string{bsOldSecret: {"sandbox.env"}})
	})
	t.Run("missing-reference", func(t *testing.T) {
		// R13 identify: without --fresh-account a missing project reference is blocked (the owner
		// fills it in account.env and re-runs); nothing is prompted and no later phase runs.
		h := newBSHarness(t)
		h.seedCurrentSandbox()
		p := filepath.Join(h.accountDir(bsOldAccount), "account.env")
		bsWrite(t, p, strings.Replace(bsRead(t, p), bsRefDemoDev+"="+bsOldProject+"\n", "", 1))
		h.term.queueRoot(h.api.rootKey(bsRootFirst))
		h.answerRefs()
		rs, err := h.run(false)
		bsExit(t, err, 2)
		if r, ok := bsResult(rs, PhaseIdentify); !ok || r.Status != StatusBlocked || !strings.Contains(r.Detail, bsRefDemoDev) {
			t.Errorf("identify %+v: a missing reference without --fresh-account is blocked naming %s", r, bsRefDemoDev)
		}
		if reads := h.term.Reads(); len(reads) > 0 {
			t.Errorf("terminal read without --fresh-account: %v", reads)
		}
		if rest, _ := h.Rest(); len(rest) > 0 {
			t.Error("the state phase ran with a project reference missing")
		}
		if r := bsRootCalls(h.api.Calls(h.from)); len(r) > 0 {
			t.Errorf("root keys used without --fresh-account: %v", r)
		}
		bsNoLeak(t, h, rs, err, map[string][]string{bsOldSecret: {"sandbox.env"}})
	})
	t.Run("no-credential", func(t *testing.T) {
		h := newBSHarness(t)
		h.term.queueRoot(h.api.rootKey(bsRootFirst))
		before := treeSnapshot(t, h.root)
		rs, err := h.run(false)
		bsExit(t, err, 2)
		if r, ok := bsResult(rs, PhaseIdentify); !ok || r.Status != StatusBlocked || !strings.Contains(r.Detail, "--fresh-account") {
			t.Errorf("identify %+v: without sandbox.env and without --fresh-account it is blocked naming the flag", r)
		}
		if reads := h.term.Reads(); len(reads) > 0 {
			t.Errorf("terminal read without --fresh-account: %v", reads)
		}
		if !bsEqual(before, treeSnapshot(t, h.root)) {
			t.Error("files written by a blocked run")
		}
		if calls := h.api.Calls(h.from); len(calls) > 0 {
			t.Errorf("API called without a credential: %v", calls)
		}
	})
}

// TestBootstrapAdminRefusesBindingMismatch (G13): a binding that disagrees with the credential's
// account, its endpoint or the manifest org is refused (exit 3) before anything is written; with
// --fresh-account the root credential is still revoked.
func TestBootstrapAdminRefusesBindingMismatch(t *testing.T) {
	cases := []struct {
		name, cond string
		setup      func(h *bsHarness)
	}{
		{"account", CondAccount, func(h *bsHarness) {
			p := filepath.Join(h.accountDir(bsOldAccount), "account.env")
			bsWrite(h.t, p, strings.Replace(bsRead(h.t, p), "LZ_ACCOUNT_ID="+bsOldAccount, "LZ_ACCOUNT_ID=xx000009-ovh", 1))
		}},
		{"endpoint", CondEndpoint, func(h *bsHarness) {
			p := filepath.Join(h.accountDir(bsOldAccount), "account.env")
			bsWrite(h.t, p, strings.Replace(bsRead(h.t, p), "OVH_ENDPOINT=ovh-eu", "OVH_ENDPOINT=ovh-ca", 1))
		}},
		{"credential-endpoint", CondEndpoint, func(h *bsHarness) {
			p := filepath.Join(h.root, "sandbox.env")
			bsWrite(h.t, p, strings.Replace(bsRead(h.t, p), "OVH_ENDPOINT=ovh-eu", "OVH_ENDPOINT=ovh-ca", 1))
		}},
		{"org", CondOrg, func(h *bsHarness) { h.org = "acme" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newBSHarness(t)
			h.seedCurrentSandbox()
			tc.setup(h)
			before := treeSnapshot(t, h.root)
			rs, err := h.run(false)
			var r *Refusal
			if !errors.As(err, &r) || r.Condition != tc.cond {
				t.Errorf("err %v, want refusal %q", err, tc.cond)
			}
			bsExit(t, err, RefusalExit)
			if !bsEqual(before, treeSnapshot(t, h.root)) {
				t.Error("files written despite the binding mismatch (passphrase or account.env)")
			}
			if rest, _ := h.Rest(); len(rest) > 0 {
				t.Error("the state phase ran despite the binding mismatch")
			}
			if w := bsWrites(h.api.Calls(h.from)); len(w) > 0 {
				t.Errorf("the run changed the account: %v", w)
			}
			for _, p := range rs {
				if (p.Phase == PhasePassphrase || p.Phase == PhaseAdmin) && (p.Status == StatusRan || p.Status == StatusUnchanged) {
					t.Errorf("phase %s %s after a binding refusal", p.Phase, p.Status)
				}
			}
			bsNoLeak(t, h, rs, err, map[string][]string{bsOldSecret: {"sandbox.env"}})
		})
	}
	for _, fc := range []struct{ name, cond, account, endpoint, org string }{
		{"fresh-org", CondOrg, bsNewAccount, "ovh-eu", "other"},
		{"fresh-account", CondAccount, "xx000009-ovh", "ovh-eu", "lz"},
		{"fresh-endpoint", CondEndpoint, bsNewAccount, "ovh-ca", "lz"},
	} {
		t.Run(fc.name, func(t *testing.T) { freshMismatch(t, fc.cond, fc.account, fc.endpoint, fc.org) })
	}
}

// freshMismatch: a binding for the root keys' account that disagrees (account id, endpoint or org)
// is refused before anything is written, and the root credential is still revoked.
func freshMismatch(t *testing.T, cond, account, endpoint, org string) {
	t.Helper()
	h := newBSHarness(t)
	bsMkdirPrivate(t, h.accountDir(bsNewAccount))
	bsWrite(t, filepath.Join(h.accountDir(bsNewAccount), "account.env"),
		"LZ_ACCOUNT_ID="+account+"\nOVH_ENDPOINT="+endpoint+"\nLZ_ORG="+org+"\n"+bsRefState+"="+bsNewProjectA+"\n"+bsRefDemoDev+"="+bsNewProjectB+"\n")
	h.term.queueRoot(h.api.rootKey(bsRootFirst))
	h.answerRefs()
	before := treeSnapshot(t, h.root)
	rs, err := h.run(true)
	var r *Refusal
	if !errors.As(err, &r) || r.Condition != cond {
		t.Errorf("err %v, want refusal %q", err, cond)
	}
	bsExit(t, err, RefusalExit)
	if !bsEqual(before, treeSnapshot(t, h.root)) {
		t.Error("files written despite the binding mismatch")
	}
	if created, _ := h.api.Created(); len(created) > 0 {
		t.Error("admin created despite the binding mismatch")
	}
	bsWantStatus(t, rs, PhaseRevoke, StatusRan)
	if !h.api.Revoked(bsRootFirst) {
		t.Error("root credential left valid after a refused fresh run")
	}
	bsNoLeak(t, h, rs, err, nil)
}

// freshDone checks a completed fresh-account run for the new account with root credential id:
// projects listed and references prompted with echo on after the keys were read with echo off,
// client and policy created through the API as expected, sandbox.env and account.env written,
// the root credential revoked, nothing else revoked.
func freshDone(t *testing.T, h *bsHarness, rs []PhaseResult, err error, root int64) *bsClient {
	t.Helper()
	if err != nil {
		t.Errorf("err %v", err)
	}
	if got, want := bsPhases(rs), []string{PhaseGuard, PhaseIdentify, PhasePassphrase, PhaseAdmin, PhaseRevoke}; !slices.Equal(got, want) {
		t.Errorf("phases %v, want %v", got, want)
	}
	bsNoBlocked(t, rs)
	bsWantStatus(t, rs, PhaseAdmin, StatusRan)
	bsWantStatus(t, rs, PhaseRevoke, StatusRan)
	auth := fmt.Sprintf("root:%d", root)
	calls := h.api.Calls(h.from)

	reads := h.term.Reads()
	if len(reads) < 3 || reads[0].Kind != "secret" || reads[1].Kind != "secret" || reads[2].Kind != "secret" {
		t.Errorf("terminal reads %v: the three root keys come first, with echo off", reads)
	}
	if n := len(slices.DeleteFunc(slices.Clone(reads), func(r termRead) bool { return r.Kind != "secret" })); n != 3 {
		t.Errorf("%d secret reads, want 3 (AK, AS, CK)", n)
	}
	for _, ref := range []string{bsRefState, bsRefDemoDev} {
		n := 0
		for _, r := range reads {
			if r.Kind == "line" && strings.Contains(r.Prompt, ref) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%d echo-on prompts name %s, want 1", n, ref)
		}
	}
	for _, p := range []string{bsNewProjectA, bsNewProjectB} {
		if !strings.Contains(h.term.shownFirst, p) {
			t.Errorf("project %s not listed before the first reference prompt", p)
		}
	}
	if !bsFound(calls, http.MethodGet, "/v1/cloud/project", auth) {
		t.Error("projects not listed with the root keys (GET /cloud/project)")
	}
	if !bsFound(calls, http.MethodGet, "/v1/auth/details", auth) {
		t.Error("identify did not read the account from GET /auth/details with the root keys")
	}
	bsNoMe(t, calls)

	created, policies := h.api.Created()
	var client *bsClient
	for _, c := range created {
		if c.Identity != "" && strings.Contains(c.Identity, bsNewAccount) {
			client = c
		}
	}
	if client == nil || !bsFound(calls, http.MethodPost, "/v1/me/api/oauth2/client", auth) {
		t.Fatalf("no admin client created with the root keys (created %d)", len(created))
	}
	if client.Name != bsAdminName || client.Flow != "CLIENT_CREDENTIALS" || client.CallbackURLs == nil || len(client.CallbackURLs) != 0 {
		t.Errorf("client %+v: want name %s, flow CLIENT_CREDENTIALS, callbackUrls []", *client, bsAdminName)
	}
	var policy map[string]any
	for _, p := range policies {
		if slices.Contains(bsStrings(p["identities"]), client.Identity) {
			if policy != nil {
				t.Error("two policies created for the admin client")
			}
			policy = p
		}
	}
	if policy == nil || !bsFound(calls, http.MethodPost, "/v2/iam/policy", auth) {
		t.Fatal("no admin policy created with the root keys")
	}
	if ids := bsStrings(policy["identities"]); len(ids) != 1 {
		t.Errorf("policy identities %v, want only the admin client", ids)
	}
	if got := bsSorted(bsActions(policy, "allow")); !slices.Equal(got, bsSorted(bsAdminActions)) {
		t.Errorf("policy actions %v, want %v", got, bsAdminActions)
	}
	if got := bsSorted(bsResources(policy)); !slices.Equal(got, bsSorted(bsAdminResources(bsNewAccount))) {
		t.Errorf("policy resources %v, want %v", got, bsAdminResources(bsNewAccount))
	}
	if ex, deny := bsActions(policy, "except"), bsActions(policy, "deny"); len(ex)+len(deny) > 0 {
		t.Errorf("policy except %v deny %v, want none", ex, deny)
	}
	if g, _ := policy["permissionsGroups"].([]any); len(g) > 0 {
		t.Errorf("policy permissionsGroups %v, want none", g)
	}

	if !bsFound(calls, http.MethodGet, "/v1/auth/currentCredential", auth) {
		t.Error("revoke did not look up the current credential")
	}
	if !bsFound(calls, http.MethodDelete, fmt.Sprintf("/v1/me/api/credential/%d", root), auth) || !h.api.Revoked(root) {
		t.Errorf("root credential %d not revoked", root)
	}
	if h.api.Revoked(bsRootOther) {
		t.Error("an unrelated credential was revoked")
	}
	if r := bsRootCalls(calls); len(r) == 0 || !strings.HasPrefix(r[len(r)-1], "DELETE /v1/me/api/credential/") {
		t.Errorf("root calls %v: the revocation is the last use of the root keys", r)
	}

	sandbox := filepath.Join(h.root, "sandbox.env")
	if got, want := readEnvFile(t, sandbox), map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": client.ClientID, "OVH_CLIENT_SECRET": client.Secret}; !bsEqual(got, want) {
		t.Errorf("sandbox.env keys %v, want the new admin credential", bsKeys(got))
	}
	acct := readEnvFile(t, filepath.Join(h.accountDir(bsNewAccount), "account.env"))
	policyID, _ := policy["id"].(string)
	want := map[string]string{"LZ_ACCOUNT_ID": bsNewAccount, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz", bsRefState: bsNewProjectA,
		bsRefDemoDev: bsNewProjectB, "LZ_ADMIN_CLIENT_ID": client.ClientID, "LZ_ADMIN_POLICY_ID": policyID}
	if !bsEqual(acct, want) {
		t.Errorf("account.env %v, want %v", acct, want)
	}
	if vals := readEnvFile(t, filepath.Join(h.accountDir(bsNewAccount), "state-passphrase.env")); vals[bsPassphrase] == "" {
		t.Error("no passphrase for the new account")
	}
	rest, _ := h.Rest()
	if len(rest) == 0 || rest[len(rest)-1].Admin != (Credential{Endpoint: "ovh-eu", ClientID: client.ClientID, ClientSecret: client.Secret}) || rest[len(rest)-1].ID != bsNewAccount {
		t.Error("the state phase did not run as the new admin of the new account")
	}
	return client
}

func bsSorted(s []string) []string {
	out := slices.Clone(s)
	sort.Strings(out)
	return out
}

func bsKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// bsWritesOf counts the writes of name in dir: a rename into place or a close after writing (a
// create alone is not counted, so a temporary file renamed into place counts once, a second
// in-place write counts again).
func bsWritesOf(events []fsEvent, dir, name string) int {
	n := 0
	for _, e := range events {
		if e.Dir == dir && e.Name == name && e.Mask&(evMovedTo|evCloseWrite) != 0 {
			n++
		}
	}
	return n
}

// TestBootstrapAdminFreshFromEmpty: journey (a) from an empty directory (only live.env) — root
// keys read with echo off, projects listed and references prompted with echo on, admin client and
// policy created, sandbox.env written once, root credential revoked; a second run without the flag
// is unchanged.
func TestBootstrapAdminFreshFromEmpty(t *testing.T) {
	h := newBSHarness(t)
	h.term.queueRoot(h.api.rootKey(bsRootFirst))
	h.answerRefs()
	watch := watchEvents(t, h.root)
	rs, err := h.run(true)
	events := watch()
	client := freshDone(t, h, rs, err, bsRootFirst)
	bsWantStatus(t, rs, PhaseIdentify, StatusRan)
	bsWantStatus(t, rs, PhasePassphrase, StatusRan)
	if fsEventsObservable {
		if n := bsWritesOf(events, h.root, "sandbox.env"); n != 1 {
			t.Errorf("sandbox.env written %d times, want once", n)
		}
	}
	files := bsFiles(t, h.root)
	want := []string{"accounts/ 0700", "accounts/" + bsNewAccount + "/ 0700", "accounts/" + bsNewAccount + "/account.env 0600",
		"accounts/" + bsNewAccount + "/state-passphrase.env 0600", "live.env 0600", "sandbox.env 0600"}
	if !slices.Equal(files, want) {
		t.Errorf("files %v, want %v (credential files 0600, directories 0700, no deployer file)", files, want)
	}
	if c := bsFiles(t, h.checkout); !slices.Equal(c, []string{"README.md 0644"}) {
		t.Errorf("checkout gained files: %v", c)
	}
	bsNoLeak(t, h, rs, err, map[string][]string{client.Secret: {"sandbox.env"}})

	before := treeSnapshot(t, h.root)
	nreads := len(h.term.Reads())
	rs, err = h.run(false)
	if err != nil {
		t.Errorf("second run: %v", err)
	}
	if got, want := bsPhases(rs), []string{PhaseGuard, PhaseIdentify, PhasePassphrase, PhaseAdmin}; !slices.Equal(got, want) {
		t.Errorf("second run phases %v, want %v", got, want)
	}
	for _, p := range []string{PhaseIdentify, PhasePassphrase, PhaseAdmin} {
		bsWantStatus(t, rs, p, StatusUnchanged)
	}
	if len(h.term.Reads()) != nreads {
		t.Error("second run read the terminal")
	}
	calls := h.api.Calls(h.from)
	if r := bsRootCalls(calls); len(r) > 0 {
		t.Errorf("second run used root keys: %v", r)
	}
	if w := bsWrites(calls); len(w) > 0 {
		t.Errorf("second run changed the account: %v", w)
	}
	if !bsEqual(before, treeSnapshot(t, h.root)) {
		t.Error("second run changed files")
	}
}

// TestBootstrapAdminFreshMigration: journey (b) with the previous account's files present — the
// old sandbox.env moves to accounts/<old>/sandbox.env before anything is written, and none of the
// old account's files is read afterwards.
func TestBootstrapAdminFreshMigration(t *testing.T) {
	h := newBSHarness(t)
	h.seedPreviousAccount()
	oldDir := h.accountDir(bsOldAccount)
	oldSandbox := bsRead(t, filepath.Join(h.root, "sandbox.env"))
	oldFiles := treeSnapshot(t, oldDir)
	h.term.queueRoot(h.api.rootKey(bsRootFirst))
	h.answerRefs()
	accounts := filepath.Join(h.root, "accounts")
	watch := watchEvents(t, h.root, accounts, oldDir)
	rs, err := h.run(true)
	ev := watch()
	client := freshDone(t, h, rs, err, bsRootFirst)
	if got := bsRead(t, filepath.Join(oldDir, "sandbox.env")); got != oldSandbox {
		t.Error("the previous account's sandbox.env was not moved to accounts/<old>/sandbox.env unchanged")
	}
	after := treeSnapshot(t, oldDir)
	delete(after, "sandbox.env")
	if !bsEqual(after, oldFiles) {
		t.Error("the previous account's files changed")
	}
	files := bsFiles(t, h.root)
	want := []string{"accounts/ 0700", "accounts/" + bsOldAccount + "/ 0700", "accounts/" + bsOldAccount + "/account.env 0600",
		"accounts/" + bsOldAccount + "/sandbox.env 0600", "accounts/" + bsOldAccount + "/state-passphrase.env 0600",
		"accounts/" + bsNewAccount + "/ 0700", "accounts/" + bsNewAccount + "/account.env 0600",
		"accounts/" + bsNewAccount + "/state-passphrase.env 0600", "live.env 0600", "sandbox.env 0600"}
	if !slices.Equal(files, want) {
		t.Errorf("files %v, want %v", files, want)
	}
	if fsEventsObservable {
		moved := slices.IndexFunc(ev, func(e fsEvent) bool {
			return e.Dir == oldDir && e.Name == "sandbox.env" && e.Mask&(evMovedTo|evCloseWrite) != 0
		})
		if moved < 0 {
			t.Fatal("no move of sandbox.env into the previous account's directory observed")
		}
		for _, e := range ev[:moved] {
			if e.Dir != oldDir && e.Mask&(evCreate|evMovedTo|evCloseWrite) != 0 {
				t.Errorf("%s/%s written before the previous sandbox.env was moved", e.Dir, e.Name)
			}
		}
		for _, e := range ev[moved+1:] {
			if e.Dir == oldDir && e.Mask&evOpen != 0 {
				t.Errorf("the previous account's %q read after the move", e.Name)
			}
		}
		if n := bsWritesOf(ev, h.root, "sandbox.env"); n != 1 {
			t.Errorf("sandbox.env written %d times, want once", n)
		}
	}
	bsNoLeak(t, h, rs, err, map[string][]string{client.Secret: {"sandbox.env"}, bsOldSecret: {"accounts/" + bsOldAccount + "/sandbox.env"}})
}

// TestBootstrapAdminFreshFailureRevokes: journey (c) — once root keys were entered, revoke runs on
// every exit path (state failure after admin, the operator aborting a reference prompt after
// identify, an admin API failure, an interrupt); a retry resumes without --fresh-account when admin
// had completed, with it (new root keys) when it had not.
func TestBootstrapAdminFreshFailureRevokes(t *testing.T) {
	stateFails := errors.New("fake state failure")
	cases := []struct {
		name    string
		migrate bool
		setup   func(h *bsHarness)
		admin   bool // admin had completed when the run failed
		retry   bool
	}{
		{"empty/after-admin", false, func(h *bsHarness) { h.stateFn = func(context.Context) error { return stateFails } }, true, true},
		{"empty/after-identify", false, func(h *bsHarness) { h.term.abort = bsRefDemoDev }, false, true},
		{"migration/after-admin", true, func(h *bsHarness) { h.stateFn = func(context.Context) error { return stateFails } }, true, true},
		{"migration/after-identify", true, func(h *bsHarness) { h.term.abort = bsRefDemoDev }, false, true},
		{"empty/identify-api-failure", false, func(h *bsHarness) { h.api.fail["GET /v1/auth/details"] = http.StatusInternalServerError }, false, false},
		// The client exists, its policy does not: the most likely partial admin.
		{"empty/admin-api-failure", false, func(h *bsHarness) { h.api.fail["POST /v2/iam/policy"] = http.StatusInternalServerError }, false, true},
		{"empty/interrupted", false, func(h *bsHarness) {
			h.stateFn = func(ctx context.Context) error {
				h.cancel() // SIGINT
				<-ctx.Done()
				return ctx.Err()
			}
		}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newBSHarness(t)
			allowed := map[string][]string{}
			oldSandbox := ""
			if tc.migrate {
				h.seedPreviousAccount()
				oldSandbox = bsRead(t, filepath.Join(h.root, "sandbox.env"))
				allowed[bsOldSecret] = []string{"sandbox.env", "accounts/" + bsOldAccount + "/sandbox.env"}
			}
			h.term.queueRoot(h.api.rootKey(bsRootFirst))
			h.answerRefs()
			tc.setup(h)
			rs, err := h.run(true)
			if err == nil {
				t.Error("the failed run returned no error")
			}
			if ExitCode(err) == 0 {
				t.Error("the failed run exits 0")
			}
			if p := bsPhases(rs); len(p) == 0 || p[len(p)-1] != PhaseRevoke {
				t.Errorf("phases %v: revoke is the last phase on every exit path", p)
			}
			bsWantStatus(t, rs, PhaseRevoke, StatusRan)
			calls := h.api.Calls(h.from)
			if !bsFound(calls, http.MethodDelete, fmt.Sprintf("/v1/me/api/credential/%d", bsRootFirst), fmt.Sprintf("root:%d", bsRootFirst)) || !h.api.Revoked(bsRootFirst) {
				t.Error("root credential left valid after a failed fresh run")
			}
			if r := bsRootCalls(calls); len(r) == 0 || !strings.HasPrefix(r[len(r)-1], "DELETE /v1/me/api/credential/") {
				t.Errorf("root calls %v: the revocation is the last use of the root keys", r)
			}
			if h.api.Revoked(bsRootOther) {
				t.Error("an unrelated credential was revoked")
			}
			created, _ := h.api.Created()
			for _, c := range created {
				allowed[c.Secret] = []string{"sandbox.env"}
			}
			if tc.name == "empty/admin-api-failure" {
				if _, err := os.Lstat(filepath.Join(h.root, "sandbox.env")); err == nil {
					t.Error("sandbox.env written for an admin client without its policy")
				}
			}
			if !tc.admin && tc.name != "empty/admin-api-failure" {
				if len(created) > 0 {
					t.Error("admin client created although identify did not complete")
				}
				got := ""
				if raw, err := os.ReadFile(filepath.Join(h.root, "sandbox.env")); err == nil {
					got = string(raw)
				}
				if got != "" && got != oldSandbox {
					t.Error("a new sandbox.env written although identify was aborted")
				}
			}
			bsNoLeak(t, h, rs, err, allowed)
			if !tc.retry {
				return
			}

			h.stateFn = nil
			h.term.abort = ""
			clear(h.api.fail)
			restBefore, _ := h.Rest()
			// resumed checks that the retry reached the state phase as the admin sandbox.env names.
			resumed := func() {
				t.Helper()
				rest, _ := h.Rest()
				sb := readEnvFile(t, filepath.Join(h.root, "sandbox.env"))
				want := Credential{Endpoint: sb["OVH_ENDPOINT"], ClientID: sb["OVH_CLIENT_ID"], ClientSecret: sb["OVH_CLIENT_SECRET"]}
				if len(rest) != len(restBefore)+1 || rest[len(rest)-1].Admin != want || rest[len(rest)-1].ID != bsNewAccount {
					t.Error("the retry did not reach the state phase as the admin in sandbox.env")
				}
			}
			if tc.admin {
				rs, err = h.run(false)
				if err != nil {
					t.Errorf("retry: %v", err)
				}
				for _, p := range []string{PhaseIdentify, PhasePassphrase, PhaseAdmin} {
					bsWantStatus(t, rs, p, StatusUnchanged)
				}
				if _, ok := bsResult(rs, PhaseRevoke); ok {
					t.Error("retry without --fresh-account reported revoke")
				}
				if n := len(slices.DeleteFunc(h.term.Reads(), func(r termRead) bool { return r.Kind != "secret" })); n != 3 {
					t.Errorf("%d root-key reads in total, want the first run's 3", n)
				}
				if r := bsRootCalls(h.api.Calls(h.from)); len(r) > 0 {
					t.Errorf("retry used root keys: %v", r)
				}
				resumed()
				if sb := readEnvFile(t, filepath.Join(h.root, "sandbox.env")); len(created) != 1 || sb["OVH_CLIENT_ID"] != created[0].ClientID || sb["OVH_CLIENT_SECRET"] != created[0].Secret {
					t.Error("retry did not resume from the new sandbox.env")
				}
			} else {
				h.term.queueRoot(h.api.rootKey(bsRootSecond))
				rs, err = h.run(true)
				if err != nil {
					t.Errorf("retry: %v", err)
				}
				bsNoBlocked(t, rs)
				bsWantStatus(t, rs, PhaseAdmin, StatusRan)
				bsWantStatus(t, rs, PhaseRevoke, StatusRan)
				if !h.api.Revoked(bsRootSecond) {
					t.Error("retry left its root credential valid")
				}
				// The admin the retry leaves in sandbox.env is a complete one: its client is bound
				// to exactly one policy, the expected one (an orphan from the failed run may remain;
				// what to do with it is not specified).
				sb := readEnvFile(t, filepath.Join(h.root, "sandbox.env"))
				var bound []map[string]any
				for _, p := range h.api.account(bsNewAccount).Policies {
					if slices.Contains(bsStrings(p["identities"]), "urn:v1:eu:identity:credential:"+bsNewAccount+"/oauth2-"+sb["OVH_CLIENT_ID"]) {
						bound = append(bound, p)
					}
				}
				if len(bound) != 1 || !slices.Equal(bsSorted(bsActions(bound[0], "allow")), bsSorted(bsAdminActions)) ||
					!slices.Equal(bsSorted(bsResources(bound[0])), bsSorted(bsAdminResources(bsNewAccount))) ||
					len(bsActions(bound[0], "except"))+len(bsActions(bound[0], "deny")) > 0 {
					t.Errorf("sandbox.env names client %q with %d policies, want one expected admin policy", sb["OVH_CLIENT_ID"], len(bound))
				}
				secretOK := false
				for _, c := range h.api.account(bsNewAccount).Clients {
					secretOK = secretOK || c.ClientID == sb["OVH_CLIENT_ID"] && c.Secret == sb["OVH_CLIENT_SECRET"]
				}
				if !secretOK || sb["OVH_ENDPOINT"] != "ovh-eu" {
					t.Error("sandbox.env does not hold that client's own secret")
				}
				resumed()
				if tc.name == "empty/admin-api-failure" {
					// identify and passphrase had completed before the admin failure (R13: completed
					// phases report unchanged on the retry).
					bsWantStatus(t, rs, PhaseIdentify, StatusUnchanged)
					bsWantStatus(t, rs, PhasePassphrase, StatusUnchanged)
				}
				// The admin left behind works on its own: a run without the flag is unchanged.
				rs2, err2 := h.run(false)
				if err2 != nil {
					t.Errorf("run after the retry: %v", err2)
				}
				for _, p := range []string{PhaseIdentify, PhasePassphrase, PhaseAdmin} {
					bsWantStatus(t, rs2, p, StatusUnchanged)
				}
				if tc.migrate {
					if got := bsRead(t, filepath.Join(h.accountDir(bsOldAccount), "sandbox.env")); got != oldSandbox {
						t.Error("the previous account's sandbox.env lost on retry")
					}
				}
			}
			created, _ = h.api.Created()
			for _, c := range created {
				allowed[c.Secret] = []string{"sandbox.env"}
			}
			if tc.migrate {
				allowed[bsOldSecret] = []string{"accounts/" + bsOldAccount + "/sandbox.env"}
			}
			bsNoLeak(t, h, rs, err, allowed)
		})
	}
}

// TestBootstrapAdminRevokeFailure: a failed revocation is `fail` (exit 1) and tells the operator
// to delete the credential in the Control Panel (research R13).
func TestBootstrapAdminRevokeFailure(t *testing.T) {
	h := newBSHarness(t)
	h.term.queueRoot(h.api.rootKey(bsRootFirst))
	h.answerRefs()
	h.api.fail[fmt.Sprintf("DELETE /v1/me/api/credential/%d", bsRootFirst)] = http.StatusInternalServerError
	rs, err := h.run(true)
	bsExit(t, err, 1)
	r, _ := bsResult(rs, PhaseRevoke)
	if r.Status != StatusFail {
		t.Errorf("revoke %+v, want fail", r)
	}
	if !strings.Contains(r.Detail+h.stdout.String()+fmt.Sprint(err), "Control Panel") {
		t.Error("a failed revocation does not name the Control Panel step")
	}
	created, _ := h.api.Created()
	allowed := map[string][]string{}
	for _, c := range created {
		allowed[c.Secret] = []string{"sandbox.env"}
	}
	bsNoLeak(t, h, rs, err, allowed)
}

// TestBootstrapAdminPassphrase (G11): created once with fresh randomness before the state phase,
// never overwritten; a passphrase file others can read is refused, not rewritten.
func TestBootstrapAdminPassphrase(t *testing.T) {
	t.Run("kept", func(t *testing.T) {
		h := newBSHarness(t)
		h.seedPreviousAccount()
		p := filepath.Join(h.accountDir(bsOldAccount), "state-passphrase.env")
		before := bsRead(t, p)
		rs, err := h.run(false)
		if err != nil {
			t.Errorf("err %v", err)
		}
		bsWantStatus(t, rs, PhasePassphrase, StatusUnchanged)
		if bsRead(t, p) != before {
			t.Error("passphrase overwritten")
		}
		bsNoLeak(t, h, rs, err, map[string][]string{bsOldSecret: {"sandbox.env"}, "fakeOldPassphrase3c1d8e": {"accounts/" + bsOldAccount + "/state-passphrase.env"}})
	})
	t.Run("created", func(t *testing.T) {
		var values []string
		for range 2 {
			h := newBSHarness(t)
			h.seedCurrentSandbox()
			rs, err := h.run(false)
			if err != nil {
				t.Errorf("err %v", err)
			}
			bsWantStatus(t, rs, PhasePassphrase, StatusRan)
			p := filepath.Join(h.accountDir(bsOldAccount), "state-passphrase.env")
			vals, rerr := ReadCredentialFile(p)
			if rerr != nil {
				t.Fatalf("passphrase file: %v", rerr)
			}
			if got := bsKeys(vals); !slices.Equal(got, []string{bsPassphrase}) {
				t.Errorf("passphrase file keys %v", got)
			}
			v := vals[bsPassphrase]
			if len(v) < 43 { // 32 random bytes: 43 base64 or 64 hex characters
				t.Errorf("passphrase of %d characters, want 32 random bytes", len(v))
			}
			values = append(values, v)
			bsNoLeak(t, h, rs, err, map[string][]string{bsOldSecret: {"sandbox.env"}, v: {"accounts/" + bsOldAccount + "/state-passphrase.env"}})
		}
		if len(values) == 2 && values[0] == values[1] {
			t.Error("two accounts got the same passphrase")
		}
	})
	t.Run("open-mode", func(t *testing.T) {
		h := newBSHarness(t)
		h.seedPreviousAccount()
		p := filepath.Join(h.accountDir(bsOldAccount), "state-passphrase.env")
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
		before := bsRead(t, p)
		rs, err := h.run(false)
		var r *Refusal
		if !errors.As(err, &r) || r.Condition != CondFileMode {
			t.Errorf("err %v, want refusal %q", err, CondFileMode)
		}
		if fi, _ := os.Stat(p); fi == nil || fi.Mode().Perm() != 0o644 || bsRead(t, p) != before {
			t.Error("an open passphrase file was rewritten or silently tightened, not reported")
		}
		if rest, _ := h.Rest(); len(rest) > 0 {
			t.Error("the state phase ran with an open passphrase file")
		}
		_ = rs
	})
}

// TestBootstrapAdminCredentialFiles (G3): a sandbox.env others can read is refused before the
// credential is sent anywhere; a config root inside the checkout (directly or through a symlink)
// is refused before anything is read or written.
func TestBootstrapAdminCredentialFiles(t *testing.T) {
	t.Run("sandbox-0644", func(t *testing.T) {
		h := newBSHarness(t)
		h.seedCurrentSandbox()
		if err := os.Chmod(filepath.Join(h.root, "sandbox.env"), 0o644); err != nil {
			t.Fatal(err)
		}
		before := treeSnapshot(t, h.root)
		rs, err := h.run(false)
		var r *Refusal
		if !errors.As(err, &r) || r.Condition != CondFileMode {
			t.Errorf("err %v, want refusal %q", err, CondFileMode)
		}
		if calls := h.api.Calls(h.from); len(calls) > 0 {
			t.Errorf("credential of an open file sent to the API: %v", calls)
		}
		if !bsEqual(before, treeSnapshot(t, h.root)) {
			t.Error("files written after the refusal")
		}
		if rest, _ := h.Rest(); len(rest) > 0 {
			t.Error("the state phase ran")
		}
		_ = rs
	})
	inside := func(h *bsHarness, viaLink bool) {
		dir := filepath.Join(h.checkout, ".config", "ovh-lz")
		bsMkdirPrivate(h.t, dir)
		if err := os.Chmod(filepath.Join(h.checkout, ".config"), 0o700); err != nil {
			h.t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(h.root, "live.env"), filepath.Join(dir, "live.env")); err != nil {
			h.t.Fatal(err)
		}
		if err := os.Remove(h.root); err != nil {
			h.t.Fatal(err)
		}
		if viaLink {
			if err := os.Symlink(dir, h.root); err != nil {
				h.t.Fatal(err)
			}
		} else {
			h.root = dir
		}
		h.seedCurrentSandbox()
	}
	for _, link := range []bool{false, true} {
		t.Run(fmt.Sprintf("config-root-in-checkout/symlink=%v", link), func(t *testing.T) {
			h := newBSHarness(t)
			inside(h, link)
			before := treeSnapshot(t, h.checkout)
			h.term.queueRoot(h.api.rootKey(bsRootFirst))
			h.answerRefs()
			for _, fresh := range []bool{false, true} {
				rs, err := h.run(fresh)
				var r *Refusal
				if !errors.As(err, &r) {
					t.Errorf("fresh=%v: err %v, want a refusal of a config root inside the checkout", fresh, err)
				}
				bsExit(t, err, RefusalExit)
				if calls := h.api.Calls(h.from); len(calls) > 0 {
					t.Errorf("fresh=%v: API called: %v", fresh, calls)
				}
				if reads := h.term.Reads(); len(reads) > 0 {
					t.Errorf("fresh=%v: root keys read for a config root inside the checkout: %v", fresh, reads)
				}
				if !bsEqual(before, treeSnapshot(t, h.checkout)) {
					t.Errorf("fresh=%v: files written inside the checkout", fresh)
				}
				_ = rs
			}
		})
	}
}
