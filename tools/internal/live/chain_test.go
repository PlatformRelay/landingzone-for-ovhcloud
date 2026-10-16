package live

// T046: `lz-live chain -- all` and `lz-live destroy -- <instance>` (spec 005 FR-009, FR-010,
// FR-011, SC-005; research R12, R21; contracts/checks.md V007, guards G7, G8, G9) against the
// plan/apply lane's fakes (apply_test.go: fake tofu of the generated roots, fake state buckets,
// the real run locks, a fake GET /auth/details; the harness judges every run for secrets,
// authority, binding, lock lifetime and that every apply applies the file its `show -json` judged)
// and T087's captured listings (tests/fixtures/ovhcloud/captured/, run 20261007T133803Z-aeee)
// served by the fake listing API (leftovers_api_test.go), rewritten to the lane's account,
// project and admin client.
//
// The chain applies the selected set in run order under the account and tenant locks and
// publishes each envelope; from a destroy-on-exit that also fires on an apply failure, SIGINT,
// SIGTERM and the run deadline it destroys the ephemeral stacks whose apply started (after a
// complete run: runtime and project-network) in reverse run order, each a saved `plan -destroy`
// judged by protect.go and applied as that file, and never a retained instance; it appends the
// inventory as each apply_complete event arrives; then it runs the leftover check over the
// captured listings with the retained instances' states (read with `tofu show -json`) and the
// admin exemption by recorded id, and fails on any leftover or listing error.
//
// "Six stacks" (tasks.md T046): the manifest's six; account-bootstrap is bootstrap:account's
// (T058 decision 1), so the fake account starts as bootstrap:account leaves it (account-bootstrap
// applied, its state present) and the chain applies the other five.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// laneEphemeral are the sandbox manifest's ephemeral rows (data-model *Stage table*).
var laneEphemeral = map[string]bool{"demo-dev-gra11-network": true, "demo-dev-gra11-runtime": true}

// chainRetained are the retained rows; chainDestroyOrder the ephemeral ones in reverse run order.
var (
	chainRetained     = []string{"account-bootstrap", "account-governance", "demo-state", "demo-dev-project"}
	chainDestroyOrder = []string{"demo-dev-gra11-runtime", "demo-dev-gra11-network"}
)

// chainAdminPolicy is the captured admin policy id (provenance.json exempt.policy_id), recorded
// in account.env as LZ_ADMIN_POLICY_ID by the bootstrap admin phase.
const chainAdminPolicy = "00000000-0000-4000-8000-000000000001"

// chainResource is one resource a stack creates: its apply_complete event and, for a retained
// stack, its object in the state (`show -json`) and in the listings.
type chainResource struct {
	addr, typ, id string
	values        map[string]any // state attributes (provider docs, kb mirror terraform-provider-ovh/docs/resources)
}

// chainResources are what each stack's apply creates. The state attribute holding the listing's
// id per type (provider docs): ovh_cloud_project_storage `name`, ovh_cloud_project_user `id`,
// ovh_cloud_project_user_s3_credential `access_key_id`, ovh_cloud_project_user_s3_policy
// `user_id`, ovh_me_api_oauth2_client `client_id`, ovh_iam_policy `id`, ovh_me_identity_group
// `name`, ovh_cloud_project_alerting `id`, ovh_cloud_project_network_private(_subnet) `id`;
// ovh_iam_resource_tags holds the tag keys in `tags`. The states also hold the secrets the
// provider keeps there (client and S3 secrets): the chain must keep them out of every output.
func chainResources() map[string][]chainResource {
	urn := "urn:v1:eu:resource:publicCloudProject:" + laneProject
	return map[string][]chainResource{
		"account-bootstrap": {
			{"module.bootstrap.module.state_backend.ovh_cloud_project_storage.this", tStorage, "lz-bkt-state", map[string]any{"name": "lz-bkt-state", "region": "GRA"}},
			{`module.bootstrap.module.state_backend.ovh_cloud_project_user.s3["platform"]`, tUser, "4711", map[string]any{"id": "4711", "description": "lz-account-platform-s3"}},
			{`module.bootstrap.module.state_backend.ovh_cloud_project_user_s3_credential.s3["platform"]`, tCred, laneAccountAK, map[string]any{"user_id": "4711", "access_key_id": laneAccountAK, "secret_access_key": laneAccountSK}},
			{`module.bootstrap.module.state_backend.ovh_cloud_project_user_s3_policy.s3["platform"]`, tS3Pol, "4711", map[string]any{"user_id": "4711", "policy": "{}"}},
		},
		"account-governance": {
			{"module.account_governance.module.identity.ovh_me_api_oauth2_client.platform_deployer", tClient, lanePlatformID, map[string]any{"client_id": lanePlatformID, "name": "lz-platform-deployer", "client_secret": lanePlatformSecret}},
			{`module.account_governance.module.identity.ovh_me_api_oauth2_client.tenant_deployer["demo"]`, tClient, laneTenantID, map[string]any{"client_id": laneTenantID, "name": "lz-demo-deployer", "client_secret": laneTenantSecret}},
			{"module.account_governance.module.identity.ovh_iam_policy.platform_deployer", tPolicy, "00000000-0000-4000-8000-0000000000b1", map[string]any{"id": "00000000-0000-4000-8000-0000000000b1", "name": "lz-platform-deployer"}},
			{`module.account_governance.module.identity.ovh_iam_policy.tenant_deployer["demo"]`, tPolicy, "00000000-0000-4000-8000-0000000000b2", map[string]any{"id": "00000000-0000-4000-8000-0000000000b2", "name": "lz-demo-deployer"}},
			{`module.account_governance.module.identity.ovh_me_identity_group.tenant["demo"]`, tGroup, "lz-demo-deployers", map[string]any{"name": "lz-demo-deployers"}},
		},
		"demo-state": {
			{"module.tenant_state.module.state_backend.ovh_cloud_project_storage.this", tStorage, "lz-demo-bkt-state", map[string]any{"name": "lz-demo-bkt-state", "region": "GRA"}},
			{`module.tenant_state.module.state_backend.ovh_cloud_project_user.s3["tenant"]`, tUser, "4712", map[string]any{"id": "4712", "description": "lz-demo-tenant-s3"}},
			{`module.tenant_state.module.state_backend.ovh_cloud_project_user.s3["platform"]`, tUser, "4713", map[string]any{"id": "4713", "description": "lz-demo-platform-s3"}},
			{`module.tenant_state.module.state_backend.ovh_cloud_project_user_s3_credential.s3["tenant"]`, tCred, laneTenantAK, map[string]any{"user_id": "4712", "access_key_id": laneTenantAK, "secret_access_key": laneTenantSK}},
			{`module.tenant_state.module.state_backend.ovh_cloud_project_user_s3_credential.s3["platform"]`, tCred, lanePlatformAK, map[string]any{"user_id": "4713", "access_key_id": lanePlatformAK, "secret_access_key": lanePlatformSK}},
			{`module.tenant_state.module.state_backend.ovh_cloud_project_user_s3_policy.s3["tenant"]`, tS3Pol, "4712", map[string]any{"user_id": "4712", "policy": "{}"}},
			{`module.tenant_state.module.state_backend.ovh_cloud_project_user_s3_policy.s3["platform"]`, tS3Pol, "4713", map[string]any{"user_id": "4713", "policy": "{}"}},
		},
		"demo-dev-project": {
			{"module.project.ovh_iam_resource_tags.project", tTags, urn, map[string]any{"id": urn, "urn": urn, "tags": map[string]string{"lz:org": "lz", "lz:tenant": "demo", "lz-retained-tag": "demo"}}},
			{"module.project.ovh_cloud_project_alerting.budget[0]", tAlert, "al-retained", map[string]any{"id": "al-retained", "name": "lz-demo-dev-budget"}},
		},
		"demo-dev-gra11-network": {
			{"module.project_network.module.network.ovh_cloud_project_network_private.this", tNetwork, "pn-0000058_0", nil},
			{"module.project_network.module.network.ovh_cloud_project_network_private_subnet.this", tSubnet, "sn-chain-58", nil},
		},
		"demo-dev-gra11-runtime": {
			{"module.runtime.ovh_cloud_project_storage.this", tStorage, "lz-demo-dev-gra11-bkt-runtime", nil},
		},
	}
}

// chainStateDoc is a retained stack's state as `tofu show -json` prints it: the resources under
// the root's one module call, a nested module's resources in a nested child module.
func chainStateDoc(rs []chainResource) map[string]any {
	type mod struct {
		Address   string           `json:"address"`
		Resources []map[string]any `json:"resources,omitempty"`
		Children  []*mod           `json:"child_modules,omitempty"`
	}
	top := map[string]*mod{}
	var order []string
	for _, r := range rs {
		parts := strings.Split(r.addr, ".")
		// module.<a>[.module.<b>].<type>.<name>
		outer := parts[0] + "." + parts[1]
		m, ok := top[outer]
		if !ok {
			m = &mod{Address: outer}
			top[outer] = m
			order = append(order, outer)
		}
		res := map[string]any{"address": r.addr, "mode": "managed", "type": r.typ, "name": strings.Split(parts[len(parts)-1], "[")[0], "values": r.values}
		if len(parts) > 4 && parts[2] == "module" {
			inner := outer + ".module." + parts[3]
			var c *mod
			for _, x := range m.Children {
				if x.Address == inner {
					c = x
				}
			}
			if c == nil {
				c = &mod{Address: inner}
				m.Children = append(m.Children, c)
			}
			c.Resources = append(c.Resources, res)
			continue
		}
		m.Resources = append(m.Resources, res)
	}
	var children []*mod
	for _, k := range order {
		children = append(children, top[k])
	}
	return map[string]any{"format_version": "1.0", "terraform_version": "1.13.0",
		"values": map[string]any{"root_module": map[string]any{"child_modules": children}}}
}

// newChainHarness is the lane harness as bootstrap:account leaves the account (account-bootstrap
// applied: its state; account.env with the admin policy id), every stack creating its resources,
// each retained stack's state holding them, each ephemeral stack's destroy plan deleting its own.
func newChainHarness(t *testing.T) *laneHarness {
	t.Helper()
	h := newLaneHarness(t)
	h.destroys = true
	h.writeConfig("accounts/"+laneAccount+"/account.env", map[string]string{"LZ_ACCOUNT_ID": laneAccount, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz",
		"LZ_PROJECT_ID_STATE": laneProject, "LZ_PROJECT_ID_DEMO_DEV": laneProject, "LZ_ADMIN_CLIENT_ID": laneAdminID, "LZ_ADMIN_POLICY_ID": chainAdminPolicy})
	raw, _ := json.Marshal(laneState{PassSHA: passSHA(lanePassphrase), Serial: 1})
	if err := os.WriteFile(filepath.Join(h.world.States, "account-bootstrap.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	res := chainResources()
	for path, st := range h.world.Stacks {
		st.Creates = nil
		for _, r := range res[st.ID] {
			st.Creates = append(st.Creates, laneCreate{Addr: r.addr, Type: r.typ, ID: r.id})
		}
		if laneEphemeral[st.ID] {
			st.DestroyPlan = h.destroyDoc(h.row(st.ID), false)
		} else {
			st.StateDoc = h.writeFake("state-"+st.ID+".json", chainStateDoc(res[st.ID]))
		}
		h.world.Stacks[path] = st
	}
	h.save()
	return h
}

func (h *laneHarness) writeFake(name string, v any) string {
	h.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		h.t.Fatal(err)
	}
	p := filepath.Join(h.base, "fake", name)
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		h.t.Fatal(err)
	}
	return p
}

// destroyDoc is the stack's `plan -destroy` document: T063's `create` capture under the root's
// module call with every action `delete` (errored: marked errored, which protect.go refuses).
func (h *laneHarness) destroyDoc(in stacks.Instance, errored bool) string {
	h.t.Helper()
	raw, err := os.ReadFile(h.planDoc(in, "create"))
	if err != nil {
		h.t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		h.t.Fatal(err)
	}
	for _, rc := range doc["resource_changes"].([]any) {
		ch := rc.(map[string]any)["change"].(map[string]any)
		ch["actions"] = []string{"delete"}
		ch["before"], ch["after"] = ch["after"], nil
	}
	if errored {
		doc["errored"] = true
	}
	return h.writeFake(fmt.Sprintf("destroy-%s-%t.json", in.ID, errored), doc)
}

// setStack changes one stack of the fake world.
func (h *laneHarness) setStack(id string, f func(*laneStack)) {
	in := h.row(id)
	st := h.world.Stacks[in.Path]
	f(&st)
	h.world.Stacks[in.Path] = st
	h.save()
}

// chainWorld is T087's captured world rewritten to the lane (project laneProject, account
// laneAccount, admin client laneAdminID, the run's id), with the retained instances' resources
// listed as they exist after a chain: the two state buckets in GRA, the three S3 users with their
// credential and policy, the two deployer clients and policies, the tenant's group, the project's
// tags and budget alert. The ephemeral stacks' resources are gone.
func chainWorld(t *testing.T, runID string) (capturedProvenance, syntheticWorld) {
	t.Helper()
	p, w := loadCapturedWorld(t, "lz-")
	swap := strings.NewReplacer(p.Project.ID, laneProject, p.Exempt.ClientID, laneAdminID, "xx000001-ovh", laneAccount)
	resp := map[string][]json.RawMessage{}
	for k, pages := range w.Responses {
		var out []json.RawMessage
		for _, pg := range pages {
			out = append(out, json.RawMessage(swap.Replace(string(pg))))
		}
		resp[swap.Replace(k)] = out
	}
	w.Responses = resp
	p.Project.ID, p.Project.URN = laneProject, swap.Replace(p.Project.URN)
	p.Exempt.ClientID = laneAdminID
	p.RunID, w.RunID = runID, runID
	pr := "/cloud/project/" + laneProject
	one := func(s string) []json.RawMessage { return []json.RawMessage{json.RawMessage(s)} }
	add := func(path, elem string) {
		var arr []json.RawMessage
		if pages, ok := w.Responses[path]; ok {
			if err := json.Unmarshal(pages[0], &arr); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
		}
		raw, _ := json.Marshal(append(arr, json.RawMessage(elem)))
		w.Responses[path] = one(string(raw))
	}
	for _, b := range []string{"lz-bkt-state", "lz-demo-bkt-state"} {
		add(pr+"/region/GRA/storage", `{"name":"`+b+`","region":"GRA","createdAt":"2026-10-08T12:00:00Z","objectsCount":1,"objectsSize":1,"ownerId":1,"virtualHost":"x","arn":"arn:aws:s3:::`+b+`","objects":[]}`)
	}
	for _, u := range []struct{ id, desc, ak string }{{"4711", "lz-account-platform-s3", laneAccountAK}, {"4712", "lz-demo-tenant-s3", laneTenantAK}, {"4713", "lz-demo-platform-s3", lanePlatformAK}} {
		add(pr+"/user", `{"id":`+u.id+`,"username":"user-`+u.id+`","description":"`+u.desc+`","status":"ok","roles":[],"creationDate":"2026-10-08T12:00:00Z"}`)
		w.Responses[pr+"/user/"+u.id+"/s3Credentials"] = one(`[{"access":"` + u.ak + `","userId":` + u.id + `,"tenantId":"t"}]`)
		w.Responses[pr+"/user/"+u.id+"/policy"] = one(`{"policy":"{\"Statement\":[]}"}`)
	}
	for _, c := range []struct{ id, name string }{{lanePlatformID, "lz-platform-deployer"}, {laneTenantID, "lz-demo-deployer"}} {
		add("/me/api/oauth2/client", `"`+c.id+`"`)
		w.Responses["/me/api/oauth2/client/"+c.id] = one(`{"clientId":"` + c.id + `","name":"` + c.name + `"}`)
	}
	for _, pol := range []struct{ id, name string }{{"00000000-0000-4000-8000-0000000000b1", "lz-platform-deployer"}, {"00000000-0000-4000-8000-0000000000b2", "lz-demo-deployer"}} {
		add("/iam/policy", `{"id":"`+pol.id+`","owner":"`+laneAccount+`","name":"`+pol.name+`","readOnly":false,"identities":[],"resources":[],"permissions":{},"permissionsGroups":[],"createdAt":"2026-10-08T12:00:00Z"}`)
	}
	add("/me/identity/group", `"lz-demo-deployers"`)
	w.Responses["/me/identity/group/lz-demo-deployers"] = one(`{"name":"lz-demo-deployers"}`)
	w.Responses["/iam/resource/"+p.Project.URN] = one(`{"urn":"` + p.Project.URN + `","name":"` + laneProject + `","type":"publicCloudProject","tags":{"lz:org":"lz","lz:tenant":"demo","lz-retained-tag":"demo"}}`)
	add(pr+"/alerting", `"al-retained"`)
	w.Responses[pr+"/alerting/al-retained"] = one(`{"id":"al-retained","name":"lz-demo-dev-budget","delay":3600,"monthlyThreshold":10,"email":"user1@example.invalid"}`)
	return p, w
}

// chainAppend is w's answer at path with elem appended, as one page.
func chainAppend(t *testing.T, w syntheticWorld, path, elem string) []json.RawMessage {
	t.Helper()
	var arr []json.RawMessage
	if pages, ok := w.Responses[path]; ok {
		if err := json.Unmarshal(pages[0], &arr); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	raw, _ := json.Marshal(append(arr, json.RawMessage(elem)))
	return []json.RawMessage{json.RawMessage(raw)}
}

// chainLister logs every listing request in the fake world's log (as a "list" event), so the
// harness orders it against the tofu calls.
// Until the run's first apply the API answers the world without the row's seed; from then on
// with it (review r2: a report computed from the baseline listings, with a later GET for show,
// misses the seed).
type chainLister struct {
	inner Lister
	log   string
	late  func() // serves the seeded answers; called once, on the first request after an apply
	done  *bool
	// first runs once, on the run's first listing request (nil: nothing).
	first     func(ctx context.Context)
	firstOnce *sync.Once
}

func (l chainLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	if l.first != nil {
		l.firstOnce.Do(func() { l.first(ctx) })
	}
	chainLate(l.log, l.late, l.done)
	laneAppend(l.log, laneCall{Cmd: "list", Stack: path})
	return l.inner.Get(ctx, path, cursor)
}

// chainLate calls late once, on the first listing request after an apply.
func chainLate(log string, late func(), done *bool) {
	if !*done {
		if raw, err := os.ReadFile(log); err == nil && strings.Contains(string(raw), `"cmd":"apply"`) {
			late()
			*done = true
		}
	}
}

// chainAdminTransport is the production listing path's API (T047, coordinator decision 1: the
// chain lists through lz-live's own read-only client with the sandbox admin credential, bound to
// the account through GET /auth/details before any listing). The token endpoint and GET
// /auth/details go to the lane's fake API (its binding log included); every other request is a
// listing: logged as a "list" event and answered by the fake listing API only when it carries the
// bearer token the lane's fake API issued to the admin credential (any other: 401, exit logged).
type chainAdminTransport struct {
	lane    http.RoundTripper // the lane's fake API, binding log included
	list    *fakeListAPI
	log     string
	late    func()
	done    *bool
	mu      *sync.Mutex
	adminTo string // "Bearer fake-token-<admin name>"
	// bearers (T006; nil: the lane's one admin token) gives the admin credential a distinct token
	// per token request and echoes the listing's token into its answer.
	bearers *chainBearers
}

func (c chainAdminTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/auth/oauth2/token" {
		resp, err := c.lane.RoundTrip(r)
		if err != nil || c.bearers == nil {
			return resp, err
		}
		return c.bearers.issue(resp)
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.URL.Path == "/v1/auth/details" {
		if c.bearers != nil && c.bearers.known(tok) {
			r = r.Clone(r.Context())
			r.Header.Set("Authorization", "Bearer "+c.bearers.base)
		}
		return c.lane.RoundTrip(r)
	}
	c.mu.Lock()
	chainLate(c.log, c.late, c.done)
	c.mu.Unlock()
	authorized := r.Header.Get("Authorization") == c.adminTo
	if c.bearers != nil {
		authorized = c.bearers.known(tok)
	}
	if !authorized {
		laneAppend(c.log, laneCall{Cmd: "list", Stack: r.URL.Path, Exit: http.StatusUnauthorized})
		return &http.Response{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized", Body: io.NopCloser(strings.NewReader(`{"message":"Invalid credentials"}`)),
			Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
	}
	laneAppend(c.log, laneCall{Cmd: "list", Stack: r.URL.Path, Exit: http.StatusOK})
	u, _ := url.Parse(c.list.URL)
	out := r.Clone(r.Context())
	out.URL.Scheme, out.URL.Host, out.Host = u.Scheme, u.Host, u.Host
	out.Header.Set("Authorization", "Bearer "+listToken)
	resp, err := c.list.Client().Transport.RoundTrip(out)
	if err != nil || c.bearers == nil {
		return resp, err
	}
	if err := c.bearers.carry(tok); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return chainRewriteBody(resp, func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(chainEchoBearer), []byte(tok)) })
}

// chainEchoBearer in a listing answer stands for the bearer token the listing request carried
// (chainBearers): a listing that echoes the leftover check's token (spec 006 FR-004).
const chainEchoBearer = "T006-ECHO-BEARER"

// chainBearers (T006): the lane's fake API issues one token per credential, so the binding's
// token (GET /auth/details) and each leftover check's are the same and a test cannot tell which
// was redacted. A token endpoint may issue a new token per request (for OVHcloud UNVERIFIED);
// chainBearers gives the admin credential that behaviour: the n-th token request is answered
// <base>-<n>, mapped back to <base> for the lane's GET /auth/details. Each token a listing
// request carried is recorded, the last also in file, which the fake tofu reads
// (laneStack.PlanSays).
type chainBearers struct {
	mu      sync.Mutex
	base    string   // the token the lane's fake API issues to the admin credential
	file    string   // the last carried token, for the fake tofu
	issued  []string // in order
	carried []string // distinct, in order
}

func (b *chainBearers) issue(resp *http.Response) (*http.Response, error) {
	return chainRewriteBody(resp, func(body []byte) []byte {
		var m map[string]any
		if json.Unmarshal(body, &m) != nil || m["access_token"] != b.base {
			return body
		}
		b.mu.Lock()
		tok := fmt.Sprintf("%s-%d", b.base, len(b.issued)+1)
		b.issued = append(b.issued, tok)
		b.mu.Unlock()
		m["access_token"] = tok
		out, _ := json.Marshal(m)
		return out
	})
}

func (b *chainBearers) known(tok string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.issued, tok)
}

func (b *chainBearers) carry(tok string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !slices.Contains(b.carried, tok) {
		b.carried = append(b.carried, tok)
	}
	return os.WriteFile(b.file, []byte(tok), 0o600)
}

// chainRewriteBody is resp with its body passed through f.
func chainRewriteBody(resp *http.Response, f func([]byte) []byte) (*http.Response, error) {
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	body = f(body)
	resp.Body, resp.ContentLength = io.NopCloser(bytes.NewReader(body)), int64(len(body))
	resp.Header.Del("Content-Length")
	return resp, nil
}

// chainExec is one chain run's setup beyond the fake world.
type chainExec struct {
	target string
	// deadlineAtBlock: the run's deadline is reached once a call blocks (T114; was a 6 s deadline).
	deadlineAtBlock bool
	signal          os.Signal // sent once a call blocks (laneStack.Block); nil: nothing is sent
	seed            syntheticSeed
	faults          map[string]listFault
	// T047 (coordinator decisions 1, 3): early serves the seed from the first request (it is in
	// the baseline); atBlock runs once a call blocks, before the signal; atFinal runs on the first
	// listing request after an apply (the final reconciliation's first listing); production lists
	// without the Lister seam, through ApplyOptions.API with the sandbox admin credential.
	early      bool
	atBlock    func(runDir string)
	atFinal    func(runDir string, f *fakeListAPI)
	production bool
	// bearers (T006): on the production path, a distinct admin token per token request
	// (chainBearers); the run's chainRun.bearers is it.
	bearers bool
	// signalAtBaseline is sent on the first listing request, which then waits for the run to stop
	// (T091: an interrupted baseline is not a blocked run); production must be false.
	signalAtBaseline os.Signal
	// observer is the run's L7 collector (T048; nil: a complete set in which every assertion
	// holds, observe_test.go chainPassing).
	observer *chainObserver
	// signalAtObserve is sent once the observer registered its cleanup (review r1 of T048).
	signalAtObserve os.Signal
	// productionObserver (T062): ChainOptions.Observer nil, the production collector.
	productionObserver bool
	// stateObjects answers the state-object check's GetObject of an instance's state key (T102);
	// an instance not named gets an encryption envelope.
	stateObjects map[string]chainStateAnswer
	// stateBackoff are the waits before the retries of a retained state read (T110; nil:
	// testBackoff, never the production default).
	stateBackoff []time.Duration
	// budget, when set, wraps the run's Lister seam and records what each listing's context has
	// left (T018); it needs production false (the production path has no Lister to wrap).
	budget *budgetLister
	// deadline replaces the harness's run deadline (T018; 0: testWaitCap).
	deadline time.Duration
	// destroyAtBlock: the destroy-on-exit's budget runs out once a call blocks (T018).
	destroyAtBlock bool
	// clock is the retained state reads' waitClock (T019; nil: the wall clock).
	clock waitClock
}

// chainDeadline is the deadline the chain harness runs under (never DefaultDeadline, T114):
// c.deadline, else testWaitCap.
func (c chainExec) chainDeadline() time.Duration {
	if c.deadline > 0 {
		return c.deadline
	}
	return testWaitCap
}

// stateReadBackoff is the retained state read's backoff the chain harness passes (T110).
func (c chainExec) stateReadBackoff() []time.Duration {
	if c.stateBackoff == nil {
		return testBackoff
	}
	return c.stateBackoff
}

// deadlineAt is the channel that reaches the chain's deadline: block when c.deadlineAtBlock, else
// nil (only the deadline's own time).
func (c chainExec) deadlineAt(block chan struct{}) <-chan struct{} {
	if !c.deadlineAtBlock {
		return nil
	}
	return block
}

// chainStateAnswer is the fake S3 endpoint's answer to one state object's GetObject.
type chainStateAnswer struct {
	status int
	body   string
}

// chainEnvelope is a state object as OpenTofu's encryption writes it (capture
// p4-encryption.txt, evidence/T007.md P4: `encrypted_data`, no `resources`).
const chainEnvelope = `{"serial":1,"lineage":"0d1f6c2e","meta":{"key_provider.pbkdf2.lz":"e30="},"encrypted_data":"c2VhbGVk","encryption_version":"v0"}`

// chainS3 is the state-object check's fake S3 endpoint: it admits a bucket's keys only
// (laneBucketKeys), logs every read as an s3get event with the access key, and answers from
// c.stateObjects or with chainEnvelope.
func (h *laneHarness) chainS3(c chainExec) *http.Client {
	f := newFakeS3(h.t)
	f.secretOf = func(ak string) string {
		for _, admitted := range laneBucketKeys {
			if sk, ok := admitted[ak]; ok {
				return sk
			}
		}
		return ""
	}
	f.get = func(ak, bucket, key string) ([]byte, int) {
		laneAppend(h.world.Log, laneCall{Cmd: "s3get", Stack: bucket + "/" + key, Key: ak})
		if _, ok := laneBucketKeys[bucket][ak]; !ok {
			return nil, http.StatusForbidden
		}
		for id, a := range c.stateObjects {
			if in := h.row(id); in.StateBucket == bucket && in.StateKey == key {
				return []byte(a.body), a.status
			}
		}
		return []byte(chainEnvelope), http.StatusOK
	}
	return f.client()
}

// chainRun is a chain run and what the harness saw while it ran.
type chainRun struct {
	laneRun
	runID string
	// invAtBlock is the inventory as it was while the blocked call still ran.
	invAtBlock []InventoryEntry
	// list are the requests the fake listing API received (T091: none writes).
	list []listCall
	// bearers are the admin tokens of the run (chainExec.bearers; nil otherwise).
	bearers *chainBearers
}

func (h *laneHarness) nextRunID() string {
	return fmt.Sprintf("20261008T1200%02dZ-%04x", h.runs+1, h.runs+1)
}

// chain runs Chain once against the captured world and judges it as every lane run.
func (h *laneHarness) chain(c chainExec) chainRun {
	t := h.t
	t.Helper()
	runID := h.nextRunID()
	_, w := chainWorld(t, runID)
	seeded := seededResponses(w, c.seed)
	first := seededResponses(w, syntheticSeed{})
	if c.early {
		first = seeded
	}
	f := newFakeListAPI(t, first)
	runDir := filepath.Join(h.checkout, ".local", "live", runID)
	late := func() {
		f.mu.Lock()
		f.responses = seeded
		f.mu.Unlock()
		if c.atFinal != nil {
			c.atFinal(runDir, f)
		}
	}
	lateDone := false
	for path, fault := range c.faults {
		f.faults[path] = fault
	}
	h.runs++
	from := len(h.logCalls())
	before := laneTree(t, h.checkout, filepath.Join(h.checkout, ".local"))
	h.term.Reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sigs := make(chan os.Signal, 1)
	var invAtBlock []InventoryEntry
	stop, watched, deadlineAt := make(chan struct{}), make(chan struct{}), make(chan struct{})
	destroyAt := make(chan struct{})
	var destroyAtOpt <-chan struct{}
	if c.destroyAtBlock {
		destroyAtOpt = destroyAt
	}
	if c.budget != nil && c.production {
		t.Fatal("chainExec.budget wraps the Lister seam, which the production path does not use")
	}
	go func() {
		defer close(watched)
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			if _, err := os.Stat(h.world.Log + ".blocked"); err != nil {
				continue
			}
			if c.atBlock != nil {
				c.atBlock(runDir)
			}
			// The blocked call printed its apply_complete events first: the inventory holds
			// them while the child still runs, or it is not appended as they arrive. A blocked
			// stack that reports no resource has none to wait for (T114: 3 s spent waiting before).
			var id string
			waitFor(3*time.Second, func() bool { // the file may be seen before its content
				raw, _ := os.ReadFile(h.world.Log + ".blocked")
				id = string(raw)
				return id != ""
			})
			creates := 1 // unknown stack: wait as before
			if in, err := h.m.Row(id); err == nil {
				if st := h.world.Stacks[in.Path]; st.Block == "apply+events" {
					creates = len(st.Creates)
				} else {
					creates = 0 // only "apply+events" prints events before it blocks
				}
			}
			for i := 0; i < 150 && creates > 0; i++ {
				invAtBlock, _ = ReadInventory(runDir)
				if len(invAtBlock) > 0 && invAtBlock[len(invAtBlock)-1].Stack == id {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if c.signal != nil {
				sigs <- c.signal
			}
			if c.deadlineAtBlock {
				close(deadlineAt) // the deadline is reached at the block (T114)
			}
			if c.destroyAtBlock {
				close(destroyAt) // the destroy's budget runs out at the block (T018)
			}
			return
		}
	}()
	target := c.target
	if target == "" {
		target = TargetAll
	}
	api := h.laneAPI()
	cl := chainLister{inner: newAPILister(f.API(), listCred), log: h.world.Log, late: late, done: &lateDone, firstOnce: &sync.Once{}}
	if c.signalAtBaseline != nil {
		cl.first = func(ctx context.Context) {
			sigs <- c.signalAtBaseline
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Second):
			}
		}
	}
	var lister Lister = cl
	if c.budget != nil {
		c.budget.inner, lister = cl, c.budget
	}
	var bearers *chainBearers
	if c.production {
		lister = nil
		if c.bearers {
			bearers = &chainBearers{base: "fake-token-" + laneAPINames[AuthorityBootstrap], file: h.world.Log + ".bearer"}
		}
		api.HTTP = &http.Client{Transport: chainAdminTransport{lane: api.HTTP.Transport, list: f, log: h.world.Log, late: late, done: &lateDone,
			mu: &sync.Mutex{}, adminTo: "Bearer fake-token-" + laneAPINames[AuthorityBootstrap], bearers: bearers}}
	}
	obs := c.observer
	if obs == nil {
		obs = &chainObserver{obs: chainPassing()}
	}
	obs.log = h.world.Log
	if c.signalAtObserve != nil {
		obs.stop = func() { sigs <- c.signalAtObserve }
	}
	var observer Observer = obs
	if c.productionObserver {
		observer = nil
	}
	streams := laneCaptureStd(t)
	err := Chain(ctx, ChainOptions{ApplyOptions: ApplyOptions{Target: target, Checkout: h.checkout, Manifest: h.m, ConfigRoot: h.root, Account: laneAccount,
		RunID: runID, RunDir: runDir, Records: h.recordsDir(), Revision: laneRevision, Tofu: filepath.Join(h.bin, "tofu"), Schemas: laneSchemas(),
		Locks: laneLocks{inner: stacks.DirLocks(h.world.LockDir), log: h.world.Log, after: h.afterLock}, Store: h.store.open, Terminal: &h.term, API: api},
		Deadline: c.chainDeadline(), Signals: sigs, Lister: lister, Observer: observer, S3HTTP: h.chainS3(c), deadlineAt: c.deadlineAt(deadlineAt), destroyAt: destroyAtOpt,
		stateReadBackoff: c.stateReadBackoff(), stateReadClock: c.clock})
	process := streams()
	close(stop)
	<-watched
	_ = os.Remove(h.world.Log + ".blocked")
	_ = os.Remove(h.world.Log + ".bearer")
	r := chainRun{laneRun: laneRun{err: err, calls: h.logCalls()[from:], runDir: runDir, process: process}, runID: runID, invAtBlock: invAtBlock, list: f.Calls(), bearers: bearers}
	h.chainRun = true
	h.judge(r.laneRun, before)
	h.chainRun = false
	return r
}

// applies lists the stacks of the run's successful (or every, all=true) apply calls of plans
// made with -destroy (destroy=true) or without, in order.
func (r laneRun) applies(destroy bool) []string {
	var out []string
	for _, c := range r.tofu() {
		if c.Cmd == "apply" && c.Destroy == destroy {
			out = append(out, c.Stack)
		}
	}
	return out
}

// index of the first (last=false) or last call matching f; -1 when none.
func (r laneRun) index(last bool, f func(laneCall) bool) int {
	at := -1
	for i, c := range r.calls {
		if f(c) {
			at = i
			if !last {
				return at
			}
		}
	}
	return at
}

func (h *laneHarness) stateOf(id string) (laneState, bool) {
	raw, err := os.ReadFile(filepath.Join(h.world.States, id+".json"))
	var s laneState
	return s, err == nil && json.Unmarshal(raw, &s) == nil
}

func chainReport(t *testing.T, runDir string) Report {
	t.Helper()
	var rep Report
	raw, err := os.ReadFile(filepath.Join(runDir, "leftovers.json"))
	if err != nil {
		t.Errorf("no leftovers.json in the run directory: %v", err)
		return rep
	}
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Errorf("leftovers.json: %v", err)
	}
	return rep
}

// chainExpectedInventory is every create of the stacks in order, as inventory.jsonl lines.
func chainExpectedInventory(ids []string) []InventoryEntry {
	res := chainResources()
	var out []InventoryEntry
	for _, id := range ids {
		for _, r := range res[id] {
			out = append(out, InventoryEntry{Stack: id, Address: r.addr, Type: r.typ, ID: r.id})
		}
	}
	return out
}

// chainDestroyedAfter checks the destroy-on-exit: exactly want destroyed (each a saved
// `plan -destroy` applied, exit 0), in that order, after every forward apply; no destroy plan of a
// retained instance; the leftover check listed after the last destroy and wrote its report; the
// summary line carries the outcome and the cost reminder.
func chainDestroyedAfter(t *testing.T, h *laneHarness, r chainRun, want []string, outcome string) {
	t.Helper()
	if got := r.applies(true); !slices.Equal(got, want) {
		t.Errorf("destroyed %v, want %v (reverse run order, each ephemeral stack whose apply started)", got, want)
	}
	for _, c := range r.tofu() {
		if c.Destroy && !laneEphemeral[c.Stack] {
			t.Errorf("tofu %s -destroy on retained instance %s (G7)", c.Cmd, c.Stack)
		}
		if c.Cmd == "apply" && c.Destroy && c.Exit != 0 {
			t.Errorf("destroy apply of %s exited %d", c.Stack, c.Exit)
		}
	}
	lastForward := r.index(true, func(c laneCall) bool { return c.Cmd == "apply" && !c.Destroy })
	firstDestroy := r.index(false, func(c laneCall) bool { return c.Destroy })
	if firstDestroy >= 0 && firstDestroy < lastForward {
		t.Errorf("a destroy before the last apply (events %v)", laneEvents(r.calls))
	}
	// R12: the run's listings are recorded before the first apply (a baseline), and the leftover
	// check reconciles after the last destroy (review r1: a listing before the destroy is allowed).
	lastDestroy := r.index(true, func(c laneCall) bool { return c.Destroy })
	firstForward := r.index(false, func(c laneCall) bool { return c.Cmd == "apply" && !c.Destroy })
	firstList := r.index(false, func(c laneCall) bool { return c.Cmd == "list" })
	lastList := r.index(true, func(c laneCall) bool { return c.Cmd == "list" })
	if firstList < 0 || firstForward >= 0 && firstList > firstForward {
		t.Errorf("no listing before the first apply (R12 records each run's listings before it; events %v)", laneEvents(r.calls))
	}
	if lastList < 0 || lastList < lastDestroy || lastList < r.index(true, func(c laneCall) bool { return c.Cmd == "apply" }) {
		t.Errorf("no leftover listing after the last apply and destroy (G9: the check runs after every chain; events %v)", laneEvents(r.calls))
	}
	if rep := chainReport(t, r.runDir); outcome == "pass" && rep.Outcome != "pass" {
		t.Errorf("leftover report %q (leftovers %v, errors %v), want pass", rep.Outcome, leftoverKeys(rep.Leftovers), rep.Errors)
	}
	// The run ends with exactly the summary and the cost reminder (contracts/checks.md `lz-live`).
	lines := strings.Split(strings.TrimSpace(h.term.String()), "\n")
	tail := lines[max(0, len(lines)-2):]
	if len(tail) != 2 || !strings.HasPrefix(tail[0], "LZ-LIVE summary "+r.runID+" "+outcome+" known-deviations=") ||
		tail[1] != "record approximate cost for run "+r.runID+" in the PR" {
		t.Errorf("terminal does not end with `LZ-LIVE summary %s %s known-deviations=…` and the cost reminder; it ends:\n%s", r.runID, outcome, strings.Join(tail, "\n"))
	}
	for _, id := range want {
		if s, ok := h.stateOf(id); !ok || !s.Empty {
			t.Errorf("state of %s not destroyed", id)
		}
	}
}

// ---------------------------------------------------------------- tests

// A first chain after bootstrap:account: the account and tenant locks held over every call;
// the five lane stacks applied in run order, each envelope published; runtime then project-network
// destroyed (a saved `plan -destroy` each, judged and applied), the retained four never; the
// inventory appended per created resource; the leftover check over the captured listings passes
// with the retained instances' resources and the admin client and policy listed; the summary
// reports pass. Afterwards the two destroyed stacks are selected again (their records are gone),
// the retained ones are not.
func TestChainFirstRunAll(t *testing.T) {
	h := newChainHarness(t)
	r := h.chain(chainExec{})
	laneExit(t, r.laneRun, 0)
	taken := r.events("lock")
	slices.Sort(taken)
	if !slices.Equal(taken, []string{"account", "tenant-demo"}) {
		t.Errorf("locks taken %v, want account and tenant-demo", taken)
	}
	if got := r.applies(false); !slices.Equal(got, laneOrder) {
		t.Errorf("applied %v, want %v in run order (account-bootstrap is bootstrap:account's)", got, laneOrder)
	}
	// Published during the run; whether a destroyed stack's envelope is withdrawn afterwards is
	// not pinned (review r1).
	puts := r.puts()
	for _, id := range laneOrder {
		if key := h.row(id).StateBucket + "/" + stacks.ArtifactKey(id); puts[key] == "" {
			t.Errorf("no envelope %s published for %s", key, id)
		}
	}
	chainDestroyedAfter(t, h, r, chainDestroyOrder, "pass")
	for _, id := range chainRetained {
		if s, ok := h.stateOf(id); !ok || s.Empty {
			t.Errorf("retained instance %s lost its state", id)
		}
	}
	inv, err := ReadInventory(r.runDir)
	if want := chainExpectedInventory(laneOrder); err != nil || !slices.Equal(inv, want) {
		t.Errorf("inventory %v (%v), want one line per created resource in apply order %v", inv, err, want)
	}
	if rep := chainReport(t, r.runDir); len(rep.Leftovers) != 0 || len(rep.Errors) != 0 {
		t.Errorf("leftovers %v, errors %v; want none", leftoverKeys(rep.Leftovers), rep.Errors)
	}
	again := h.run(VerbPlan, TargetAll)
	laneExit(t, again, 0)
	if got := again.stacksOf("plan"); !slices.Equal(got, []string{"demo-dev-gra11-network", "demo-dev-gra11-runtime"}) {
		t.Errorf("after the chain, plan -- all planned %v; want the two destroyed stacks only", got)
	}
}

// G8: the destroy-on-exit fires on an apply failure, SIGINT, SIGTERM and the run deadline: the
// ephemeral stacks whose apply started are destroyed in reverse run order (one that never
// started is not touched), nothing is applied after the failure, the leftover check still runs
// and the summary reports fail. A stopped child gets SIGINT (SIGTERM forwarded as itself or as
// SIGINT), never only a kill; the inventory holds what the interrupted apply had reported before
// it stopped.
func TestChainDestroyOnExit(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(h *laneHarness)
		signal   os.Signal
		deadline bool     // the deadline is reached once the runtime apply blocks
		applied  []string // forward applies that ran (the failing one included)
		destroy  []string
		stopped  string // the signal the blocked apply got ("" when none blocks)
		exit     int    // 0: any non-zero
		reason   string // in the error
	}{
		{name: "runtime-apply-fails", setup: func(h *laneHarness) { h.setApplyExit("demo-dev-gra11-runtime", 1) },
			applied: laneOrder, destroy: chainDestroyOrder, exit: 1},
		{name: "network-apply-fails", setup: func(h *laneHarness) { h.setApplyExit("demo-dev-gra11-network", 1) },
			applied: laneOrder[:4], destroy: []string{"demo-dev-gra11-network"}, exit: 1},
		{name: "governance-apply-fails", setup: func(h *laneHarness) { h.setApplyExit("account-governance", 1) },
			applied: laneOrder[:1], destroy: nil, exit: 1},
		{name: "sigint", setup: func(h *laneHarness) {
			h.setStack("demo-dev-gra11-runtime", func(s *laneStack) { s.Block = "apply+events" })
		},
			signal: syscall.SIGINT, applied: laneOrder, destroy: chainDestroyOrder, stopped: "interrupt"},
		{name: "sigterm", setup: func(h *laneHarness) {
			h.setStack("demo-dev-gra11-runtime", func(s *laneStack) { s.Block = "apply+events" })
		},
			signal: syscall.SIGTERM, applied: laneOrder, destroy: chainDestroyOrder, stopped: "interrupt|terminated"},
		{name: "deadline", setup: func(h *laneHarness) {
			h.setStack("demo-dev-gra11-runtime", func(s *laneStack) { s.Block = "apply+events" })
		},
			deadline: true, applied: laneOrder, destroy: chainDestroyOrder, stopped: "interrupt", reason: "deadline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newChainHarness(t)
			tc.setup(h)
			r := h.chain(chainExec{signal: tc.signal, deadlineAtBlock: tc.deadline})
			if tc.exit != 0 {
				laneExit(t, r.laneRun, tc.exit)
			} else if r.err == nil {
				t.Errorf("exit 0, want a failed run")
			}
			if tc.reason != "" && (r.err == nil || !strings.Contains(r.err.Error(), tc.reason)) {
				t.Errorf("err %v, want it to name the %s", r.err, tc.reason)
			}
			var forward []string
			for _, c := range r.tofu() {
				if c.Cmd == "apply" && !c.Destroy {
					forward = append(forward, c.Stack)
				}
			}
			if !slices.Equal(forward, tc.applied) {
				t.Errorf("applied %v, want %v (nothing after the failure)", forward, tc.applied)
			}
			for _, c := range r.tofu() {
				if c.Destroy || slices.Contains(tc.applied, c.Stack) {
					continue
				}
				// A stack whose apply never started: only a retained one's state is read (the
				// leftover check), nothing else (review r1).
				if slices.Contains(chainRetained, c.Stack) && (c.Cmd == "init" || c.Cmd == "show-state") {
					continue
				}
				t.Errorf("tofu %s on %s, whose apply never started", c.Cmd, c.Stack)
			}
			chainDestroyedAfter(t, h, r, tc.destroy, "fail")
			if tc.stopped != "" {
				blocked := r.index(false, func(c laneCall) bool { return c.Cmd == "apply" && c.Stack == "demo-dev-gra11-runtime" && !c.Destroy })
				if blocked < 0 {
					t.Fatalf("the runtime apply was never stopped by a signal (a kill is never logged)")
				}
				if sig := r.calls[blocked].Signal; !slices.Contains(strings.Split(tc.stopped, "|"), sig) {
					t.Errorf("the runtime apply got %q, want %s", sig, tc.stopped)
				}
				want := chainExpectedInventory([]string{"demo-dev-gra11-runtime"})
				var got []InventoryEntry
				for _, e := range r.invAtBlock {
					if e.Stack == "demo-dev-gra11-runtime" {
						got = append(got, e)
					}
				}
				if !slices.Equal(got, want) {
					t.Errorf("inventory while the runtime apply ran: %v, want %v (appended as each apply_complete arrives)", got, want)
				}
				inv, _ := ReadInventory(r.runDir)
				if all := chainExpectedInventory(laneOrder); !slices.Equal(inv, all) {
					t.Errorf("inventory %v, want %v", inv, all)
				}
			}
		})
	}
}

// G7 on the destroy: every destroy plan goes through protect.go before it is applied. A destroy
// plan the guard refuses (here: marked errored) is not applied, the refusal is reported (exit 3),
// and the destroy goes on with the next ephemeral stack (the run core continues after a failed
// destroy); the forward order of the destroys is never taken. The refused stack's resources stay
// listed, so the leftover check reports them too and the run still exits 3 (review r1).
func TestChainDestroyGuard(t *testing.T) {
	for _, refused := range chainDestroyOrder {
		t.Run(refused, func(t *testing.T) {
			h := newChainHarness(t)
			h.setStack(refused, func(s *laneStack) { s.DestroyPlan = h.destroyDoc(h.row(refused), true) })
			_, w := chainWorld(t, h.nextRunID())
			pr := "/cloud/project/" + laneProject
			seed := syntheticSeed{Replace: map[string][]json.RawMessage{}}
			var expect []Leftover
			if refused == "demo-dev-gra11-runtime" {
				seed.Replace[pr+"/region/GRA/storage"] = chainAppend(t, w, pr+"/region/GRA/storage", `{"name":"lz-demo-dev-gra11-bkt-runtime","region":"GRA","createdAt":"2026-10-08T12:00:00Z","objectsCount":0,"objectsSize":0,"ownerId":1,"virtualHost":"x","arn":"arn:aws:s3:::lz-demo-dev-gra11-bkt-runtime","objects":[]}`)
				expect = []Leftover{{Type: tStorage, ID: "lz-demo-dev-gra11-bkt-runtime"}}
			} else {
				seed.Replace[pr+"/network/private"] = chainAppend(t, w, pr+"/network/private", `{"id":"pn-0000058_0","name":"lz-demo-dev-gra11-net","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}`)
				seed.Replace[pr+"/network/private/pn-0000058_0/subnet"] = []json.RawMessage{json.RawMessage(`[{"id":"sn-chain-58","cidr":"10.20.0.0/24","ipPools":[],"gatewayIp":null}]`)}
				expect = []Leftover{{Type: tNetwork, ID: "pn-0000058_0"}, {Type: tSubnet, ID: "sn-chain-58"}}
			}
			r := h.chain(chainExec{seed: seed})
			laneExit(t, r.laneRun, 3)
			laneCondition(t, r.laneRun, CondRetained)
			var want []string
			for _, id := range chainDestroyOrder {
				if id != refused {
					want = append(want, id)
				}
			}
			if got := r.applies(true); !slices.Equal(got, want) {
				t.Errorf("destroyed %v, want %v (the refused plan not applied, the other destroyed)", got, want)
			}
			planned := r.index(false, func(c laneCall) bool { return c.Cmd == "show-json" && c.Destroy && c.Stack == refused })
			if planned < 0 {
				t.Errorf("the destroy plan of %s was never judged", refused)
			}
			chainDestroyedAfter(t, h, r, want, "fail")
			if got := leftoverKeys(chainReport(t, r.runDir).Leftovers); !slices.Equal(got, leftoverKeys(expect)) {
				t.Errorf("leftovers %v, want exactly the refused stack's %v", got, leftoverKeys(expect))
			}
		})
	}
}

// chainSeed is T087's seed of one kind with the probe prefix renamed to the slice's: the routes,
// the answers and the expected leftovers.
func chainSeed(p capturedProvenance, w syntheticWorld, name string) syntheticSeed {
	s := capturedSeeds(p, w)[name]
	rename := strings.NewReplacer("lzprobe-", "lz-seed-")
	out := syntheticSeed{Replace: map[string][]json.RawMessage{}}
	for k, pages := range s.Replace {
		for _, pg := range pages {
			out.Replace[rename.Replace(k)] = append(out.Replace[rename.Replace(k)], json.RawMessage(rename.Replace(string(pg))))
		}
	}
	for _, e := range s.Expect {
		out.Expect = append(out.Expect, Leftover{Type: e.Type, ID: rename.Replace(e.ID), Name: rename.Replace(e.Name)})
	}
	return out
}

// G9 on the chain: the leftover check runs T065's parser on the captured listings. One seeded
// leftover per kind of the chain (T087's seeds, renamed to the slice prefix; an S3 policy under a
// user absent from every state and a project alert absent from every state among them) is
// reported exactly and fails the chain; a retained instance's resources are exempt only through
// that instance's state (an emptied state reports them); the admin client and policy only by the
// recorded ids (another LZ_ADMIN_POLICY_ID reports the admin policy); a listing error or an
// unparseable listing is `fail`, never `pass`. T091: a listing that fails from the start fails the
// baseline, and the chain stops there, blocked (exit 2), before any apply (chainBlockedAtBaseline);
// one that fails only in the final reconciliation (late) fails the chain after the destroys.
func TestChainLeftovers(t *testing.T) {
	type row struct {
		name    string
		seed    func(p capturedProvenance, w syntheticWorld) syntheticSeed
		setup   func(h *laneHarness)
		faults  func(p capturedProvenance) map[string]listFault
		late    bool     // faults only from the final reconciliation's first listing on
		blocked bool     // the baseline fails: blocked before any apply (T091)
		expect  []string // leftover keys; nil with errors=true: errors only
		errors  bool
	}
	var rows []row
	p0, w0 := chainWorld(t, "x")
	for name := range capturedSeeds(p0, w0) {
		rows = append(rows, row{name: "seed-" + name, seed: func(p capturedProvenance, w syntheticWorld) syntheticSeed {
			return chainSeed(p, w, name)
		}, setup: func(h *laneHarness) {
			s := capturedSeeds(p0, w0)[name]
			// A seed known through the inventory is created by the stack of its kind.
			for _, e := range s.Inventory {
				owner, mod := "demo-state", "module.tenant_state.module.state_backend."
				if e.Type == tSubnet {
					owner, mod = "demo-dev-gra11-network", "module.project_network.module.network."
				}
				h.setStack(owner, func(st *laneStack) {
					st.Creates = append(st.Creates, laneCreate{Addr: mod + e.Address, Type: e.Type, ID: e.ID})
				})
			}
		}, expect: leftoverKeys(chainSeed(p0, w0, name).Expect)})
	}
	res := chainResources()
	keys := func(id string) []string {
		var out []Leftover
		for _, r := range res[id] {
			if r.typ != tTags {
				out = append(out, Leftover{Type: r.typ, ID: r.id})
				continue
			}
			// A tag is listed by its key; only the slice-prefixed key is a candidate (review r1:
			// the state's tags exempt it).
			out = append(out, Leftover{Type: tTags, ID: "lz-retained-tag"})
		}
		return leftoverKeys(out)
	}
	for _, id := range chainRetained {
		rows = append(rows, row{name: "state-emptied-" + id, setup: func(h *laneHarness) {
			h.setStack(id, func(s *laneStack) { s.StateDoc = "" })
		}, expect: keys(id)})
	}
	rows = append(rows,
		row{name: "admin-policy-other-id", setup: func(h *laneHarness) {
			h.writeConfig("accounts/"+laneAccount+"/account.env", map[string]string{"LZ_ACCOUNT_ID": laneAccount, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz",
				"LZ_PROJECT_ID_STATE": laneProject, "LZ_PROJECT_ID_DEMO_DEV": laneProject, "LZ_ADMIN_CLIENT_ID": laneAdminID, "LZ_ADMIN_POLICY_ID": "00000000-0000-4000-8000-0000000000ff"})
		}, expect: []string{tPolicy + " " + chainAdminPolicy}},
		// The admin client is exempt by its recorded id only: a client named like it with another
		// id is a leftover (review r1).
		row{name: "admin-client-name-other-id", seed: func(p capturedProvenance, w syntheticWorld) syntheticSeed {
			return syntheticSeed{Replace: map[string][]json.RawMessage{
				"/me/api/oauth2/client":                     {json.RawMessage(`["` + laneAdminID + `","EU.00000000000000c1"]`)},
				"/me/api/oauth2/client/EU.00000000000000c1": {json.RawMessage(`{"clientId":"EU.00000000000000c1","name":"lz-sandbox-admin"}`)},
			}}
		}, expect: []string{tClient + " EU.00000000000000c1"}},
		row{name: "listing-error", faults: func(p capturedProvenance) map[string]listFault {
			return map[string]listFault{"/cloud/project/" + laneProject + "/region/GRA/storage": {Kind: "status", Status: 503}}
		}, blocked: true},
		row{name: "listing-unparseable", faults: func(p capturedProvenance) map[string]listFault {
			return map[string]listFault{"/cloud/project/" + laneProject + "/network/private": {Kind: "malformed"}}
		}, blocked: true},
		row{name: "final-listing-error", faults: func(p capturedProvenance) map[string]listFault {
			return map[string]listFault{"/cloud/project/" + laneProject + "/region/GRA/storage": {Kind: "status", Status: 503}}
		}, late: true, errors: true},
		row{name: "final-listing-unparseable", faults: func(p capturedProvenance) map[string]listFault {
			return map[string]listFault{"/cloud/project/" + laneProject + "/network/private": {Kind: "malformed"}}
		}, late: true, errors: true},
	)
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			h := newChainHarness(t)
			if tc.setup != nil {
				tc.setup(h)
			}
			c := chainExec{}
			p, w := chainWorld(t, h.nextRunID())
			if tc.seed != nil {
				c.seed = tc.seed(p, w)
			}
			if tc.faults != nil && tc.late {
				faults := tc.faults(p)
				c.atFinal = func(_ string, f *fakeListAPI) {
					f.mu.Lock()
					defer f.mu.Unlock()
					maps.Copy(f.faults, faults)
				}
			} else if tc.faults != nil {
				c.faults = tc.faults(p)
			}
			r := h.chain(c)
			if tc.blocked {
				chainBlockedAtBaseline(t, h, r)
				return
			}
			laneExit(t, r.laneRun, 1)
			chainDestroyedAfter(t, h, r, chainDestroyOrder, "fail")
			rep := chainReport(t, r.runDir)
			if rep.Outcome != "fail" {
				t.Errorf("leftover report %q, want fail", rep.Outcome)
			}
			if got := leftoverKeys(rep.Leftovers); !tc.errors && !slices.Equal(got, tc.expect) {
				t.Errorf("leftovers %v, want exactly %v", got, tc.expect)
			}
			if tc.errors && len(rep.Errors) == 0 {
				t.Errorf("no listing error reported (leftovers %v)", leftoverKeys(rep.Leftovers))
			}
			if !tc.errors && len(rep.Errors) != 0 {
				t.Errorf("listing errors %v, want none", rep.Errors)
			}
		})
	}
}

// `destroy -- <ephemeral instance>` (G7, FR-011): a saved `plan -destroy` of that instance only,
// judged by protect.go and applied as that file, under its tenant lock and its authority; its
// record is gone, so the next `plan -- all` selects it again and nothing else. A destroy plan the
// guard refuses is not applied (exit 3) and the record stays. (`destroy` of a retained instance or
// `all` is refused: TestApplyDestroyRetainedRefused.)
func TestChainDestroyVerb(t *testing.T) {
	for _, id := range chainDestroyOrder {
		for _, refused := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/refused=%t", id, refused), func(t *testing.T) {
				h := newChainHarness(t)
				h.steady()
				if refused {
					h.setStack(id, func(s *laneStack) { s.DestroyPlan = h.destroyDoc(h.row(id), true) })
				}
				r := h.run(VerbDestroy, id)
				for _, c := range r.tofu() {
					if c.Stack != id {
						t.Errorf("tofu %s on %s: destroy -- %s acts on that instance only", c.Cmd, c.Stack, id)
					}
				}
				if got := r.events("lock"); !slices.Equal(got, []string{"tenant-demo"}) {
					t.Errorf("locks taken %v, want tenant-demo", got)
				}
				if r.index(false, func(c laneCall) bool { return c.Cmd == "show-json" && c.Destroy && c.Stack == id }) < 0 {
					t.Errorf("no judged destroy plan of %s", id)
				}
				next := h.run(VerbPlan, TargetAll)
				laneExit(t, next, 0)
				if refused {
					laneCondition(t, r, CondRetained)
					if got := r.applies(true); len(got) != 0 {
						t.Errorf("destroyed %v after the guard refused the plan", got)
					}
					if len(h.record(id)) == 0 {
						t.Errorf("record of %s removed although nothing was destroyed", id)
					}
					if got := next.stacksOf("plan"); len(got) != 0 {
						t.Errorf("plan -- all after a refused destroy planned %v, want nothing", got)
					}
					return
				}
				laneExit(t, r, 0)
				if got := r.applies(true); !slices.Equal(got, []string{id}) {
					t.Errorf("destroyed %v, want [%s]", got, id)
				}
				if s, ok := h.stateOf(id); !ok || !s.Empty {
					t.Errorf("state of %s not destroyed", id)
				}
				if got := next.stacksOf("plan"); !slices.Equal(got, []string{id}) {
					t.Errorf("plan -- all after destroy -- %s planned %v, want [%s] (its record is gone)", id, got, id)
				}
			})
		}
	}
}

// TestLiveToolchainPin (T047, research R19): the ovhcloud CLI 0.15.0 is pinned in mise.live.toml
// only (loaded with MISE_ENV=live, on the host), as its one tool; mise.toml, which the offline
// toolchain identity reads, names no ovhcloud tool. The offline entry admits both files (lz-offline's
// candidate allow-list), so both halves run there, in CI and on the host.
func TestLiveToolchainPin(t *testing.T) {
	t.Parallel()
	if got := miseTools(t, "mise.live.toml"); !slices.Equal(got, []string{`"github:ovh/ovhcloud-cli" = "0.15.0"`}) {
		t.Errorf("mise.live.toml tools %q, want exactly the ovhcloud CLI 0.15.0", got)
	}
	for _, line := range miseTools(t, "mise.toml") {
		if strings.Contains(line, "ovhcloud") {
			t.Errorf("mise.toml pins %s: the live tool belongs in mise.live.toml only", line)
		}
	}
}

// miseTools returns the entries of a mise file's [tools] table at the repository root.
func miseTools(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(laneRepoRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section == "[tools]" && line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// ---------------------------------------------------------------- T047: coordinator decisions 1, 3, 4

// Decision 1 (production lister): without the Lister seam the chain lists through lz-live's own
// read-only API client (ApplyOptions.API) with the sandbox admin credential (sandbox.env), bound to
// the account through GET /auth/details before any listing. An admin credential of another
// account is refused (exit 3) before any listing and before any tofu call.
func TestChainAdminLister(t *testing.T) {
	t.Run("bound-before-listing", func(t *testing.T) {
		h := newChainHarness(t)
		r := h.chain(chainExec{production: true})
		laneExit(t, r.laneRun, 0)
		chainDestroyedAfter(t, h, r, chainDestroyOrder, "pass")
		bound := r.index(false, func(c laneCall) bool {
			return c.Cmd == "bind" && c.Stack == laneAPINames[AuthorityBootstrap] && c.Exit == http.StatusOK
		})
		firstList := r.index(false, func(c laneCall) bool { return c.Cmd == "list" })
		if bound < 0 || firstList < 0 || bound > firstList {
			t.Errorf("the admin credential was not bound (GET /auth/details) before the first listing (events %v)", laneEvents(r.calls))
		}
		for _, c := range r.calls {
			if c.Cmd == "list" && c.Exit != http.StatusOK {
				t.Errorf("listing %s answered %d: not listed with the sandbox admin credential", c.Stack, c.Exit)
			}
		}
	})
	t.Run("admin-of-another-account", func(t *testing.T) {
		h := newChainHarness(t)
		h.writeConfig("sandbox.env", map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": laneForeignID, "OVH_CLIENT_SECRET": laneForeignSecret})
		r := h.chain(chainExec{production: true})
		laneExit(t, r.laneRun, 3)
		laneCondition(t, r.laneRun, CondAccount)
		if got := r.events("list"); len(got) != 0 {
			t.Errorf("listed %v with an admin credential of another account", got)
		}
		if got := r.tofu(); len(got) != 0 {
			t.Errorf("tofu calls %v after the admin credential was refused", laneEvents(got))
		}
		// Every way out of a chain ends with summary.json and the summary line (review r1).
		var sum map[string]any
		if raw, err := os.ReadFile(filepath.Join(r.runDir, "summary.json")); err != nil || json.Unmarshal(raw, &sum) != nil || sum["outcome"] != "fail" {
			t.Errorf("summary.json %v (%v), want outcome fail after the refusal", sum, err)
		}
		if !strings.Contains(h.term.String(), "LZ-LIVE summary "+r.runID+" fail ") {
			t.Errorf("no fail summary line after the refusal:\n%s", h.term.String())
		}
	})
}

// chainBaselineFile is the run's baseline record (T047, coordinator decision 3): the listings taken
// before the first apply, persisted redacted in the run directory, by provider type the listed ids.
const chainBaselineFile = "baseline.json"

type chainBaselineRecord struct {
	RunID  string              `json:"run_id"`
	Listed map[string][]string `json:"listed"`
	Errors []string            `json:"errors"`
}

func readChainBaseline(t *testing.T, runDir string) (chainBaselineRecord, bool) {
	t.Helper()
	var b chainBaselineRecord
	raw, err := os.ReadFile(filepath.Join(runDir, chainBaselineFile))
	if err != nil {
		return b, false
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Errorf("%s: %v", chainBaselineFile, err)
		return b, false
	}
	return b, true
}

// Decision 3 (baseline): the listings before the first apply are persisted as a redacted record in
// the run directory before that apply; the final reconciliation reads that record back and marks
// each leftover listed in it as there before the run (`before_run`); a missing record or a
// baseline listing that failed is an error of the final report (fail, never pass).
func TestChainBaseline(t *testing.T) {
	t.Run("persisted-before-first-apply", func(t *testing.T) {
		h := newChainHarness(t)
		h.setStack("account-governance", func(s *laneStack) { s.Block = "apply+events" })
		var at chainBaselineRecord
		seen := false
		r := h.chain(chainExec{signal: syscall.SIGINT, atBlock: func(runDir string) { at, seen = readChainBaseline(t, runDir) }})
		if r.err == nil {
			t.Errorf("exit 0 after SIGINT")
		}
		if !seen {
			t.Fatalf("no %s in the run directory while the first apply ran", chainBaselineFile)
		}
		if at.RunID != r.runID || !slices.Contains(at.Listed[tStorage], "lz-bkt-state") || !slices.Contains(at.Listed[tClient], laneAdminID) || len(at.Errors) != 0 {
			t.Errorf("baseline record %+v: want run %s, the listed state bucket and admin client, no error", at, r.runID)
		}
		if raws, _ := filepath.Glob(filepath.Join(r.runDir, "listings-before", "*.json")); len(raws) == 0 {
			t.Errorf("no raw baseline listings under listings-before/")
		}
	})
	storage := func(p capturedProvenance, w syntheticWorld) syntheticSeed { return chainSeed(p, w, "storage") }
	before := func(t *testing.T, r chainRun) map[string]bool {
		t.Helper()
		out := map[string]bool{}
		for _, l := range chainReport(t, r.runDir).Leftovers {
			out[l.Type+" "+l.ID] = l.Before
		}
		return out
	}
	seedKey := tStorage + " lz-seed-bkt-seed"
	for _, tc := range []struct {
		name    string
		early   bool
		atFinal func(t *testing.T) func(string, *fakeListAPI)
		faults  map[string]listFault
		want    map[string]bool // leftover key -> before_run
		errWord string          // in a report error
		blocked bool            // blocked at the baseline (T091)
	}{
		{name: "pre-existing-leftover", early: true, want: map[string]bool{seedKey: true}},
		{name: "new-leftover", want: map[string]bool{seedKey: false}},
		{name: "record-is-compared", early: true, atFinal: func(t *testing.T) func(string, *fakeListAPI) {
			return func(runDir string, _ *fakeListAPI) {
				p := filepath.Join(runDir, chainBaselineFile)
				var doc map[string]any
				raw, err := os.ReadFile(p)
				if err != nil || json.Unmarshal(raw, &doc) != nil {
					t.Errorf("no baseline record to edit: %v", err)
					return
				}
				listed, _ := doc["listed"].(map[string]any)
				delete(listed, tStorage)
				raw, _ = json.Marshal(doc)
				if err := os.WriteFile(p, raw, 0o600); err != nil {
					t.Error(err)
				}
			}
		}, want: map[string]bool{seedKey: false}},
		{name: "record-missing", atFinal: func(t *testing.T) func(string, *fakeListAPI) {
			return func(runDir string, _ *fakeListAPI) { _ = os.Remove(filepath.Join(runDir, chainBaselineFile)) }
		}, errWord: "baseline"},
		// T091: a baseline listing that failed stops the chain before its first apply.
		{name: "baseline-listing-failed", faults: map[string]listFault{"/cloud/project/" + laneProject + "/network/private": {Kind: "malformed"}},
			blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newChainHarness(t)
			c := chainExec{early: tc.early, faults: tc.faults}
			if tc.want != nil {
				p, w := chainWorld(t, h.nextRunID())
				c.seed = storage(p, w)
			}
			if tc.atFinal != nil {
				c.atFinal = tc.atFinal(t)
			}
			r := h.chain(c)
			if tc.blocked {
				chainBlockedAtBaseline(t, h, r)
				return
			}
			laneExit(t, r.laneRun, 1)
			chainDestroyedAfter(t, h, r, chainDestroyOrder, "fail")
			rep := chainReport(t, r.runDir)
			if tc.want != nil {
				if got := before(t, r); !maps.Equal(got, tc.want) {
					t.Errorf("leftovers (key -> before_run) %v, want %v", got, tc.want)
				}
			}
			if tc.errWord != "" {
				if len(rep.Leftovers) != 0 || !slices.ContainsFunc(rep.Errors, func(e string) bool { return strings.Contains(e, tc.errWord) }) {
					t.Errorf("report leftovers %v, errors %v; want no leftover and an error naming the %s", leftoverKeys(rep.Leftovers), rep.Errors, tc.errWord)
				}
			}
		})
	}
}

// chainAnswerMark is in every failing answer of TestChainStateUnreadable's fake reads: a retry line
// carrying it would carry tofu's output, not only the redacted error.
const chainAnswerMark = "fake-answer-t110"

// G9 fail closed (T047, mutant g9-state-error-ignored): a retained instance's state that cannot be
// read leaves its resources unexempted and unknowable; the report names it as an error and the
// chain fails, after the destroys. T110 (evidence/T049.md run 3, gap 2): a read whose `init` or
// `show -json` failed with a transient answer (5xx, connection reset, connection timed out) is
// retried at most twice, each after its wait, unless the wait would end after the deadline; a
// read that then succeeds exempts the instance's objects; a 4xx or any other failure is not
// retried; each retry prints one `LZ-LIVE retry` line naming the instance, without tofu's output.
// Spec 006 FR-008 (T019): a wait the deadline does not leave time for is refused and the check is
// blocked waiting for the instance (it was a plain fail naming "no retry").
func TestChainStateUnreadable(t *testing.T) {
	answer := func(s string) string { return s + " (" + chainAnswerMark + ")" }
	errReset := answer("Error: reading the state: read tcp 10.0.0.1:443: connection reset by peer")
	for _, c := range []struct {
		name     string
		stack    string
		fail     *laneReadFail
		undecode bool            // the state document does not decode (non-transient, T047)
		backoff  []time.Duration // nil: the harness's
		reads    int             // calls of fail.Call (or show-state) the run makes
		retries  int             // LZ-LIVE retry lines
		ok       bool            // the read finally succeeded: no error, no leftover, pass
		noRetry  bool            // the wait is refused: blocked waiting for the instance (spec 006 FR-008)
	}{
		{name: "transient-then-ok-show", stack: "account-governance",
			fail:  &laneReadFail{Call: "show-state", Answers: []string{answer("GET /v1/iam/policy: 503 Service Unavailable")}},
			reads: 2, retries: 1, ok: true},
		{name: "transient-then-ok-init", stack: "account-bootstrap",
			fail: &laneReadFail{Call: "init", Answers: []string{
				answer("Failed to get existing workspaces: dial tcp 10.0.0.1:443: connect: connection timed out"),
				answer("Failed to get existing workspaces: status code 502")}},
			reads: 3, retries: 2, ok: true},
		{name: "transient-on-every-attempt", stack: "demo-state",
			fail:    &laneReadFail{Call: "show-state", Answers: []string{errReset, errReset, errReset, errReset, errReset}},
			backoff: []time.Duration{200 * time.Millisecond, 400 * time.Millisecond},
			reads:   3, retries: 2},
		{name: "403-not-retried", stack: "demo-dev-project",
			fail:  &laneReadFail{Call: "show-state", Answers: []string{answer("GET /v1/iam/resource: 403 Forbidden"), answer("x")}},
			reads: 1},
		{name: "deadline-too-close", stack: "account-governance",
			fail: &laneReadFail{Call: "show-state", Answers: []string{errReset, errReset}},
			// Longer than the destroy-on-exit's budget, whichever it is: the run's 30 s deadline
			// today, at least DefaultDeadline under FR-002 (spec 006, T018).
			backoff: []time.Duration{DefaultDeadline + time.Minute, DefaultDeadline + time.Minute},
			reads:   1, noRetry: true},
		{name: "undecodable", stack: "account-bootstrap", undecode: true, reads: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newChainHarness(t)
			h.setStack(c.stack, func(s *laneStack) {
				s.ReadFail = c.fail
				if c.undecode {
					s.StateDoc = h.writeFake("state-unreadable.json", "not a state document")
				}
			})
			start := time.Now()
			r := h.chain(chainExec{stateBackoff: c.backoff})
			took := time.Since(start)
			rep := chainReport(t, r.runDir)
			named := slices.ContainsFunc(rep.Errors, func(e string) bool { return strings.Contains(e, c.stack) })
			if c.ok {
				laneExit(t, r.laneRun, 0)
				chainDestroyedAfter(t, h, r, chainDestroyOrder, "pass")
				if len(rep.Leftovers) != 0 || len(rep.Errors) != 0 {
					t.Errorf("leftovers %v, errors %v; want none once the read of %s succeeded", leftoverKeys(rep.Leftovers), rep.Errors, c.stack)
				}
			} else if c.noRetry {
				// Spec 006 FR-008 (T019): a wait the retry budget refuses is reported blocked,
				// waiting for the instance, never a pass; the destroys ran before it.
				if got := r.applies(true); !slices.Equal(got, chainDestroyOrder) {
					t.Errorf("destroy applies %v, want %v", got, chainDestroyOrder)
				}
				retryBlocked(t, h, r, rep, c.stack)
			} else {
				laneExit(t, r.laneRun, 1)
				chainDestroyedAfter(t, h, r, chainDestroyOrder, "fail")
				if !named || rep.Outcome != "fail" {
					t.Errorf("report outcome %q, errors %v; want fail with an error naming the unreadable state of %s", rep.Outcome, rep.Errors, c.stack)
				}
			}
			if c.noRetry {
				if took > 15*time.Second {
					t.Errorf("the run took %v: it waited for a retry the deadline did not leave time for", took)
				}
			}
			// The reads of the instance: fail.Call's calls (show-state for the undecodable row)
			// after the last destroy, where the leftover check runs (an applied stack's own init
			// comes before).
			call := "show-state"
			if c.fail != nil {
				call = c.fail.Call
			}
			var at []time.Time
			for i := r.index(true, func(x laneCall) bool { return x.Destroy }) + 1; i < len(r.calls); i++ {
				if x := r.calls[i]; x.Stack == c.stack && x.Cmd == call {
					ts, err := time.Parse(time.RFC3339Nano, x.At)
					if err != nil {
						t.Fatalf("call %+v without its time: %v", x, err)
					}
					at = append(at, ts)
				}
			}
			if reads := len(at); reads != c.reads {
				t.Errorf("%d %s calls of %s, want %d (at most two retries, only after a transient answer)", len(at), call, c.stack, c.reads)
			}
			backoff := chainExec{stateBackoff: c.backoff}.stateReadBackoff()
			for i := 1; i < len(at) && i <= len(backoff); i++ {
				if gap := at[i].Sub(at[i-1]); gap < backoff[i-1] {
					t.Errorf("retry %d came %v after the attempt before, want at least %v", i, gap, backoff[i-1])
				}
			}
			var retries []string
			for _, l := range strings.Split(h.term.String(), "\n") {
				if strings.HasPrefix(l, "LZ-LIVE retry") {
					retries = append(retries, l)
				}
			}
			if len(retries) != c.retries {
				t.Errorf("%d LZ-LIVE retry lines, want %d:\n%s", len(retries), c.retries, strings.Join(retries, "\n"))
			}
			// The line names the instance and carries the call's error, nothing of tofu's output.
			shape := regexp.MustCompile(`^LZ-LIVE retry state read ` + regexp.QuoteMeta(c.stack) +
				` in [0-9.]+m?s: transient failure \(attempt [23] of 3\): tofu (init|show) ` + regexp.QuoteMeta(c.stack) + `: exit status 1$`)
			for _, l := range retries {
				if !shape.MatchString(l) || strings.Contains(l, chainAnswerMark) {
					t.Errorf("retry line %q: want `LZ-LIVE retry state read %s in <wait>: transient failure (attempt n of 3): tofu <call> %s: exit status 1`, nothing of tofu's output", l, c.stack, c.stack)
				}
			}
		})
	}
}

// Decision 4 (failed destroy apply): a destroy whose saved plan passed the guard but whose apply
// fails does not stop the destroy-on-exit: the other ephemeral stack is still destroyed, the
// failure is reported, the leftover check still runs after the last destroy, and the run exits
// non-zero with a fail summary.
func TestChainDestroyApplyFails(t *testing.T) {
	for _, failing := range chainDestroyOrder {
		t.Run(failing, func(t *testing.T) {
			h := newChainHarness(t)
			h.setStack(failing, func(s *laneStack) { s.DestroyExit = 1 })
			r := h.chain(chainExec{})
			laneExit(t, r.laneRun, 1)
			if r.err == nil || !strings.Contains(r.err.Error(), failing) {
				t.Errorf("err %v, want it to name %s", r.err, failing)
			}
			if got := r.applies(true); !slices.Equal(got, chainDestroyOrder) {
				t.Errorf("destroy applies %v, want %v (the other still destroyed after the failure)", got, chainDestroyOrder)
			}
			for _, id := range chainDestroyOrder {
				s, ok := h.stateOf(id)
				if id == failing && (!ok || s.Empty) {
					t.Errorf("state of %s emptied although its destroy failed", id)
				}
				if id != failing && (!ok || !s.Empty) {
					t.Errorf("state of %s not destroyed", id)
				}
			}
			lastDestroy := r.index(true, func(c laneCall) bool { return c.Destroy })
			lastList := r.index(true, func(c laneCall) bool { return c.Cmd == "list" })
			if lastList < lastDestroy {
				t.Errorf("no leftover listing after the last destroy (events %v)", laneEvents(r.calls))
			}
			if _, err := os.Stat(filepath.Join(r.runDir, "leftovers.json")); err != nil {
				t.Errorf("no leftover report: %v", err)
			}
			lines := strings.Split(strings.TrimSpace(h.term.String()), "\n")
			if len(lines) < 2 || !strings.HasPrefix(lines[len(lines)-2], "LZ-LIVE summary "+r.runID+" fail ") {
				t.Errorf("terminal does not end with a fail summary:\n%s", strings.Join(lines[max(0, len(lines)-3):], "\n"))
			}
			if len(h.record(failing)) == 0 {
				t.Errorf("record of %s removed although its destroy failed", failing)
			}
		})
	}
}

// ---------------------------------------------------------------- T091

// chainBlockedAtBaseline checks a chain whose baseline listing failed (T091, coordinator
// 2026-10-08: a run that can only fail must not spend credit): blocked (exit 2) naming the
// baseline, after the baseline record was written with its errors; no tofu call at all (no apply,
// no destroy, no state touched), no object-store put, and no request to OVHcloud but reads (the
// OAuth2 token request aside); the run still ends with the fail summary and the cost reminder.
func chainBlockedAtBaseline(t *testing.T, h *laneHarness, r chainRun) {
	t.Helper()
	laneExit(t, r.laneRun, 2)
	var b *Blocked
	if !errors.As(r.err, &b) || !strings.Contains(b.Error(), "baseline") {
		t.Errorf("err %v, want *Blocked naming the baseline", r.err)
	}
	if got := r.tofu(); len(got) != 0 {
		t.Errorf("tofu calls %v after the baseline listing failed: a chain that can only fail applies nothing", laneEvents(got))
	}
	if got := r.puts(); len(got) != 0 {
		t.Errorf("object-store puts %v after the baseline listing failed", slices.Sorted(maps.Keys(got)))
	}
	for _, c := range h.api.Calls() {
		if c.Method != http.MethodGet && !strings.HasSuffix(c.Path, "/oauth2/token") {
			t.Errorf("API %s %s: a blocked chain only reads", c.Method, c.Path)
		}
	}
	for _, c := range r.list {
		if c.Method != http.MethodGet && c.Route != "token" {
			t.Errorf("listing API %s %s: a blocked chain only reads", c.Method, c.Route)
		}
	}
	if rec, ok := readChainBaseline(t, r.runDir); !ok || len(rec.Errors) == 0 {
		t.Errorf("baseline record %+v (present %t), want it written with the listing errors", rec, ok)
	}
	// T048 (coordinator 2026-10-08): a blocked chain reports `blocked`, not `fail`, in summary.json
	// and the summary line (contracts/checks.md outcome words; exit 2).
	lines := strings.Split(strings.TrimSpace(h.term.String()), "\n")
	tail := lines[max(0, len(lines)-2):]
	if len(tail) != 2 || tail[0] != "LZ-LIVE summary "+r.runID+" blocked known-deviations=none" || tail[1] != "record approximate cost for run "+r.runID+" in the PR" {
		t.Errorf("terminal does not end with the blocked summary and the cost reminder; it ends:\n%s", strings.Join(tail, "\n"))
	}
	if s := readChainSummary(t, r.runDir); s.Outcome != "blocked" || s.RunID != r.runID || len(s.KnownDeviations) != 0 {
		t.Errorf("summary.json %+v, want outcome blocked for run %s without deviations", s, r.runID)
	}
}

// A baseline listing cut short by SIGINT is the run's interruption, not a blocked run (T091): no
// apply, exit 1, not *Blocked, the fail summary.
func TestChainBaselineInterrupted(t *testing.T) {
	h := newChainHarness(t)
	r := h.chain(chainExec{signalAtBaseline: syscall.SIGINT})
	laneExit(t, r.laneRun, 1)
	if r.err == nil || !strings.Contains(r.err.Error(), "interrupted") {
		t.Errorf("err %v, want the interruption", r.err)
	}
	if got := r.applies(false); len(got) != 0 {
		t.Errorf("applied %v after SIGINT during the baseline", got)
	}
	if !strings.Contains(h.term.String(), "LZ-LIVE summary "+r.runID+" fail ") {
		t.Errorf("no fail summary line:\n%s", h.term.String())
	}
}

// chainConsumes gives consumer an edge of kind to producer in the harness manifest: no ephemeral
// stage produces for another in the stage table (stacks/stages.go), so the T091 rows add one.
func chainConsumes(h *laneHarness, consumer, producer, kind string) {
	for i := range h.m.Instances {
		if h.m.Instances[i].ID == consumer {
			h.m.Instances[i].Edges = append(h.m.Instances[i].Edges, stacks.Edge{Producer: producer, Kind: kind})
		}
	}
}

// T091 (FR-009, FR-011): `destroy -- <producer>` while a stack consuming it (a data or an
// authority edge) has a record is refused (exit 3, consumer-applied, naming the consumer) before
// any tofu call, and the producer's state and record stay; a consumer without a record does not
// block it. `destroy -- all` is still refused by its first retained instance, and the chain's
// reverse-order destroy-on-exit is unchanged: it destroys the producer after its consumer, also
// when the consumer's destroy failed and its record stays.
func TestChainDestroyProducer(t *testing.T) {
	producer, consumer := "demo-dev-gra11-network", "demo-dev-gra11-runtime"
	for _, kind := range []string{stacks.EdgeData, stacks.EdgeAuthority} {
		t.Run("consumer-applied-"+kind, func(t *testing.T) {
			h := newChainHarness(t)
			h.steady()
			chainConsumes(h, consumer, producer, kind)
			r := h.run(VerbDestroy, producer)
			laneExit(t, r, 3)
			laneCondition(t, r, CondConsumerApplied)
			if r.err == nil || !strings.Contains(r.err.Error(), consumer) {
				t.Errorf("err %v, want it to name the applied consumer %s", r.err, consumer)
			}
			if got := r.tofu(); len(got) != 0 {
				t.Errorf("tofu calls %v: the refusal comes before any tofu call", laneEvents(got))
			}
			if s, ok := h.stateOf(producer); !ok || s.Empty {
				t.Errorf("state of %s destroyed under its applied consumer", producer)
			}
			if len(h.record(producer)) == 0 {
				t.Errorf("record of %s removed although its destroy was refused", producer)
			}
		})
	}
	t.Run("consumer-not-applied", func(t *testing.T) {
		h := newChainHarness(t)
		h.steady()
		chainConsumes(h, consumer, producer, stacks.EdgeData)
		if err := os.Remove(filepath.Join(h.recordsDir(), consumer+".json")); err != nil {
			t.Fatal(err)
		}
		r := h.run(VerbDestroy, producer)
		laneExit(t, r, 0)
		if got := r.applies(true); !slices.Equal(got, []string{producer}) {
			t.Errorf("destroyed %v, want [%s] (its consumer has no record)", got, producer)
		}
		if got := r.events("lock"); !slices.Equal(got, []string{"tenant-demo"}) {
			t.Errorf("locks taken %v, want tenant-demo", got)
		}
	})
	// Review r1: only the consumers' records count. Another stack's undecodable record does not
	// stop the cleanup of a producer nobody consumes; a consumer's record that does not decode is
	// still a record (applied: refused, fail closed).
	t.Run("unrelated-record-undecodable", func(t *testing.T) {
		h := newChainHarness(t)
		h.steady()
		if err := os.WriteFile(filepath.Join(h.recordsDir(), "demo-state.json"), []byte("not a record"), 0o600); err != nil {
			t.Fatal(err)
		}
		r := h.run(VerbDestroy, producer)
		laneExit(t, r, 0)
		if got := r.applies(true); !slices.Equal(got, []string{producer}) {
			t.Errorf("destroyed %v, want [%s] (another stack's record has nothing to do with it)", got, producer)
		}
	})
	t.Run("consumer-record-undecodable", func(t *testing.T) {
		h := newChainHarness(t)
		h.steady()
		chainConsumes(h, consumer, producer, stacks.EdgeData)
		if err := os.WriteFile(filepath.Join(h.recordsDir(), consumer+".json"), []byte("not a record"), 0o600); err != nil {
			t.Fatal(err)
		}
		r := h.run(VerbDestroy, producer)
		laneExit(t, r, 3)
		laneCondition(t, r, CondConsumerApplied)
		if got := r.tofu(); len(got) != 0 {
			t.Errorf("tofu calls %v: the refusal comes before any tofu call", laneEvents(got))
		}
	})
	// The consumers' run locks are held too (a consumer applied by another run between the check
	// and the destroy would be orphaned): an account-scope consumer adds the account lock.
	t.Run("consumer-locks", func(t *testing.T) {
		h := newChainHarness(t)
		h.steady()
		chainConsumes(h, "account-governance", producer, stacks.EdgeAuthority)
		if err := os.Remove(filepath.Join(h.recordsDir(), "account-governance.json")); err != nil {
			t.Fatal(err)
		}
		r := h.run(VerbDestroy, producer)
		laneExit(t, r, 0)
		if got := r.events("lock"); !slices.Equal(got, []string{"account", "tenant-demo"}) {
			t.Errorf("locks taken %v, want account and tenant-demo (the producer's and its consumer's)", got)
		}
	})
	t.Run("destroy-all", func(t *testing.T) {
		h := newChainHarness(t)
		h.steady()
		chainConsumes(h, consumer, producer, stacks.EdgeData)
		r := h.run(VerbDestroy, TargetAll)
		laneCondition(t, r, CondRetained)
		if got := r.tofu(); len(got) != 0 {
			t.Errorf("tofu calls %v after destroy -- all was refused", laneEvents(got))
		}
	})
	t.Run("chain-consumer-destroy-fails", func(t *testing.T) {
		h := newChainHarness(t)
		// An authority edge: it orders the consumer after the producer and selects nothing, so the
		// chain applies as before.
		chainConsumes(h, consumer, producer, stacks.EdgeAuthority)
		h.setStack(consumer, func(s *laneStack) { s.DestroyExit = 1 })
		r := h.chain(chainExec{})
		laneExit(t, r.laneRun, 1)
		var ref *Refusal
		if errors.As(r.err, &ref) {
			t.Errorf("err %v: the chain's destroy-on-exit refuses nothing for a consumer", r.err)
		}
		if got := r.applies(true); !slices.Equal(got, chainDestroyOrder) {
			t.Errorf("destroy applies %v, want %v (reverse run order, the producer after its failed consumer)", got, chainDestroyOrder)
		}
		if s, ok := h.stateOf(producer); !ok || !s.Empty {
			t.Errorf("state of %s not destroyed by the chain", producer)
		}
		if len(h.record(consumer)) == 0 {
			t.Errorf("record of %s removed although its destroy failed", consumer)
		}
	})
}

// ---------------------------------------------------------------- spec 006 T006 (FR-004)

// chainTokenPolicy is w's IAM policy listing with one more policy, in the shape chainWorld lists
// them, as a seed.
func chainTokenPolicy(t *testing.T, w syntheticWorld, id, name string) syntheticSeed {
	t.Helper()
	elem := `{"id":"` + id + `","owner":"` + laneAccount + `","name":"` + name + `","readOnly":false,"identities":[],"resources":[],"permissions":{},"permissionsGroups":[],"createdAt":"2026-10-08T12:00:00Z"}`
	return syntheticSeed{Replace: map[string][]json.RawMessage{"/iam/policy": chainAppend(t, w, "/iam/policy", elem)}}
}

// chainListingTokens are the bearer tokens the run's listing requests carried: the leftover
// checks' tokens. It fails when there is none, or when one is the binding's (the run's first
// admin token), so the run cannot pass on the binding's token alone.
func chainListingTokens(t *testing.T, r chainRun) []string {
	t.Helper()
	b := r.bearers
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.carried) == 0 || len(b.issued) < 2 || slices.Contains(b.carried, b.issued[0]) {
		t.Fatalf("listing tokens %v of issued %v: want the leftover checks' own tokens, none the binding's", b.carried, b.issued)
	}
	return slices.Clone(b.carried)
}

// chainNoTokenInRecord fails when any file below the run directory, the terminal or the process
// streams hold one of toks.
func chainNoTokenInRecord(t *testing.T, h *laneHarness, r chainRun, toks []string) {
	t.Helper()
	err := filepath.WalkDir(r.runDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, tok := range toks {
			if strings.Contains(string(raw), tok) {
				rel, _ := filepath.Rel(r.runDir, p)
				t.Errorf("the run record's %s holds the leftover check's bearer token %s", rel, tok)
			}
		}
		return nil
	})
	if err != nil {
		t.Errorf("walking the run directory: %v", err)
	}
	for _, tok := range toks {
		if strings.Contains(h.term.String(), tok) {
			t.Errorf("the terminal holds the leftover check's bearer token %s", tok)
		}
		if strings.Contains(r.process, tok) {
			t.Errorf("the process streams hold the leftover check's bearer token %s", tok)
		}
	}
}

// chainListedPolicy is the element named name of the run's final listing record of IAM policies
// (listings/ovh_iam_policy.json), and whether it is there.
func chainListedPolicy(t *testing.T, runDir, name string) (map[string]any, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(runDir, "listings", tPolicy+".json"))
	if err != nil {
		t.Errorf("no listing record of %s: %v", tPolicy, err)
		return nil, false
	}
	var pols []map[string]any
	if err := json.Unmarshal(raw, &pols); err != nil {
		t.Errorf("listing record of %s: %v", tPolicy, err)
		return nil, false
	}
	for _, p := range pols {
		if p["name"] == name {
			return p, true
		}
	}
	t.Errorf("the listing record of %s has no policy named %s: the seeded listing did not run", tPolicy, name)
	return nil, false
}

// FR-004 (spec 006, A3): the bearer token the chain's leftover check obtains is registered with
// the run's redactor before its first listing, so no file of the run record and no stream holds
// it: not where a listing echoes it as an id, not inside a longer id, not split across two writes
// of a stream. The chain lists on the production path (ApplyOptions.API with the sandbox admin
// credential), where the check obtains the token itself; every token request gets its own token
// (chainBearers), so registering the binding's token does not pass.
func TestChainLeftoverTokenRedacted(t *testing.T) {
	t.Run("token-as-id", func(t *testing.T) {
		h := newChainHarness(t)
		_, w := chainWorld(t, h.nextRunID())
		const name = "audit-t006-token-as-id"
		r := h.chain(chainExec{production: true, bearers: true, early: true, seed: chainTokenPolicy(t, w, chainEchoBearer, name)})
		chainNoTokenInRecord(t, h, r, chainListingTokens(t, r))
		if p, ok := chainListedPolicy(t, r.runDir, name); ok && p["id"] != redacted {
			t.Errorf("listed policy %s recorded with id %q, want %q", name, p["id"], redacted)
		}
	})
	t.Run("token-inside-longer-id", func(t *testing.T) {
		h := newChainHarness(t)
		_, w := chainWorld(t, h.nextRunID())
		const name = "lz-t006-token-inside"
		r := h.chain(chainExec{production: true, bearers: true, early: true, seed: chainTokenPolicy(t, w, "lz-"+chainEchoBearer+"-t006", name)})
		chainNoTokenInRecord(t, h, r, chainListingTokens(t, r))
		want := "lz-" + redacted + "-t006"
		if p, ok := chainListedPolicy(t, r.runDir, name); ok && p["id"] != want {
			t.Errorf("listed policy %s recorded with id %q, want %q", name, p["id"], want)
		}
		// The slice prefix makes it a leftover: leftovers.json names it, the token part redacted.
		rep := chainReport(t, r.runDir)
		if !slices.ContainsFunc(rep.Leftovers, func(l Leftover) bool { return l.Type == tPolicy && l.ID == want }) {
			t.Errorf("leftovers %v, want %s %s", leftoverKeys(rep.Leftovers), tPolicy, want)
		}
	})
	t.Run("token-split-across-writes", func(t *testing.T) {
		h := newChainHarness(t)
		const before, after = "T006-split-before:", ":T006-split-after"
		// The plan runs after the baseline listed: it prints the baseline check's token in two
		// writes, split in the middle (laneStack.PlanSays).
		h.setStack("demo-dev-gra11-network", func(st *laneStack) {
			st.PlanSays = []string{before + laneBearerHead, laneBearerTail + after + "\n"}
		})
		r := h.chain(chainExec{production: true, bearers: true})
		toks := chainListingTokens(t, r)
		chainNoTokenInRecord(t, h, r, toks)
		term := h.term.String()
		for _, tok := range toks {
			k := len(tok) / 2
			if strings.Contains(term, before+tok[:k]) || strings.Contains(term, tok[k:]+after) {
				t.Errorf("a half of the split token (%s) reached the terminal unredacted", tok)
			}
		}
		if !strings.Contains(term, before+redacted+after) {
			t.Errorf("the terminal does not show %q: the plan's two writes did not pass the redactor's hold-back", before+redacted+after)
		}
	})
	t.Run("registered-before-first-listing", func(t *testing.T) {
		w, _ := loadSynthetic(t)
		f := newFakeListAPI(t, w.Responses)
		c, inv := apiCheck(t, w, syntheticSeed{}, f)
		c.Red = NewRedactor()
		var once sync.Once
		listed, held := false, false
		inner := f.Client().Transport
		c.API.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/auth/oauth2/token" && !strings.HasSuffix(r.URL.Path, "/auth/details") {
				once.Do(func() { listed, held = true, c.Red.Redact(listToken) == redacted })
			}
			return inner.RoundTrip(r)
		})}
		rep := c.Check(context.Background(), inv)
		if !listed {
			t.Fatalf("the check sent no listing request (outcome %q, errors %v)", rep.Outcome, rep.Errors)
		}
		if !held {
			t.Errorf("LeftoverCheck.Red did not hold the bearer token when the first listing was requested")
		}
	})
}

// ---------------------------------------------------------------- T018 (spec 006 FR-002)

// TestDestroyBudget: the destroy-on-exit's budget is max(DefaultDeadline, run deadline), and
// DefaultDeadline for an unset one (data-model *Fixed signatures*, FR-002 row): a run deadline
// shorter than DefaultDeadline never shortens the destroy (chain.go and runner.go use the run's
// deadline today).
func TestDestroyBudget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  time.Duration
		want time.Duration
	}{
		{"unset", 0, DefaultDeadline},
		{"negative", -time.Second, DefaultDeadline},
		{"below", 5 * time.Minute, DefaultDeadline},
		{"just-below", DefaultDeadline - time.Second, DefaultDeadline},
		{"equal", DefaultDeadline, DefaultDeadline},
		{"above", 90 * time.Minute, 90 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := destroyBudget(tc.run); got != tc.want {
				t.Errorf("destroyBudget(%v) = %v, want %v", tc.run, got, tc.want)
			}
		})
	}
}

// budgetLister passes listings through and records, for each, what its context has left (-1: no
// deadline). The leftover check after the destroys lists under the destroy-on-exit's context, so
// its last listing shows the destroy's budget without waiting for it (T018: the budget's clock is
// read, not run).
type budgetLister struct {
	inner Lister
	mu    sync.Mutex
	left  []time.Duration
}

func (b *budgetLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	left := time.Duration(-1)
	if dl, ok := ctx.Deadline(); ok {
		left = time.Until(dl)
	}
	b.mu.Lock()
	b.left = append(b.left, left)
	b.mu.Unlock()
	return b.inner.Get(ctx, path, cursor)
}

// The destroy-budget cases' times (T018). budgetForward: how long the failing forward apply takes,
// so a budget counted from the run's start shows; budgetSlack: the most the destroy phase's own
// calls (destroys, trap, state reads, listings) may take before the last listing; budgetShortRun
// and budgetSlowDestroy: a run deadline, and a destroy longer than it, so a destroy bounded by the
// run's deadline is stopped; budgetLongRun: a run deadline above DefaultDeadline, which the
// destroy keeps. The margins are wide for a loaded machine: a fake chain's forward part and
// destroy phase each take under 1 s unloaded.
const (
	budgetForward     = 15 * time.Second
	budgetSlack       = 8 * time.Second
	budgetShortRun    = 10 * time.Second
	budgetSlowDestroy = 12 * time.Second
	budgetLongRun     = 90 * time.Minute
)

// destroyBudgetJudge checks that the last listing (the leftover check after the destroys, under the
// destroy-on-exit's context) ran with what is left of the destroy budget FR-002 gives a run with
// deadline run (DefaultDeadline, or run when longer), counted from the destroy's start: at most
// that budget (bounded, never longer), and more than it less spent (the destroys' own delay) less
// budgetSlack.
func destroyBudgetJudge(t *testing.T, b *budgetLister, run, spent time.Duration) {
	t.Helper()
	if run <= 0 {
		t.Fatalf("run deadline %v, want a set one", run)
	}
	want := DefaultDeadline
	if run > want {
		want = run
	}
	b.mu.Lock()
	left := slices.Clone(b.left)
	b.mu.Unlock()
	if len(left) == 0 {
		t.Fatal("no listing ran: the leftover check after the destroys was never reached")
	}
	last, floor := left[len(left)-1], want-spent-budgetSlack
	if last <= floor || last > want {
		t.Errorf("the leftover check after the destroys had %v left (-1: unbounded); want the destroy-on-exit's own budget, "+
			"%v counted from the destroy's start (> %v) for --deadline %v: not a run deadline shorter than DefaultDeadline, "+
			"nor DefaultDeadline for a longer one, nor a budget counted from the run's start (FR-002)", last, want, floor, run)
	}
}

// TestChainDestroyBudget: chain runs with a --deadline shorter than DefaultDeadline whose runtime
// apply fails. from-destroy-start: the failing apply takes budgetForward; the destroy-on-exit's
// context is bounded by DefaultDeadline from the destroy's start (not from the run's start, nor
// by the run's deadline). outlives-run-deadline: run deadline budgetShortRun, runtime's destroy
// takes budgetSlowDestroy; it is not stopped and completes, then network's, under the same budget.
// above-default (a pin today): run deadline budgetLongRun; the destroy's budget is that, not
// DefaultDeadline.
func TestChainDestroyBudget(t *testing.T) {
	t.Run("above-default", func(t *testing.T) {
		h := newChainHarness(t)
		h.setApplyExit("demo-dev-gra11-runtime", 1)
		b := &budgetLister{}
		r := h.chain(chainExec{budget: b, deadline: budgetLongRun})
		laneExit(t, r.laneRun, 1)
		chainDestroyedAfter(t, h, r, chainDestroyOrder, "fail")
		destroyBudgetJudge(t, b, budgetLongRun, 0)
	})
	t.Run("from-destroy-start", func(t *testing.T) {
		h := newChainHarness(t)
		h.setStack("demo-dev-gra11-runtime", func(s *laneStack) { s.ApplyExit, s.ApplyDelay = 1, budgetForward })
		b := &budgetLister{}
		r := h.chain(chainExec{budget: b})
		laneExit(t, r.laneRun, 1)
		chainDestroyedAfter(t, h, r, chainDestroyOrder, "fail")
		destroyBudgetJudge(t, b, (chainExec{}).chainDeadline(), 0) // the forward delay is before the destroy
	})
	t.Run("outlives-run-deadline", func(t *testing.T) {
		h := newChainHarness(t)
		h.setApplyExit("demo-dev-gra11-runtime", 1)
		h.setStack("demo-dev-gra11-runtime", func(s *laneStack) { s.DestroyDelay = budgetSlowDestroy })
		b := &budgetLister{}
		r := h.chain(chainExec{budget: b, deadline: budgetShortRun})
		laneExit(t, r.laneRun, 1)
		for _, c := range r.tofu() {
			if c.Destroy && c.Signal != "" {
				t.Errorf("the destroy of %s (taking %v) got %s: the destroy-on-exit was bounded by the run's --deadline %v",
					c.Stack, budgetSlowDestroy, c.Signal, budgetShortRun)
			}
		}
		chainDestroyedAfter(t, h, r, chainDestroyOrder, "fail")
		destroyBudgetJudge(t, b, budgetShortRun, budgetSlowDestroy)
	})
}

// TestChainDestroyBudgetExceeded: the chain as TestProbeDestroyBudgetExceeded: a complete chain
// whose first destroy (runtime's) hangs, and the destroy-on-exit's budget runs out there
// (chainExec destroyAtBlock, as 45 minutes would). The hung destroy is interrupted once; network's
// destroy gets no time; the run fails with the fail summary; the inventory, both states and both
// records (what `--cleanup` reads) are kept.
func TestChainDestroyBudgetExceeded(t *testing.T) {
	h := newChainHarness(t)
	h.setStack("demo-dev-gra11-runtime", func(s *laneStack) { s.Block = "destroy" })
	start := time.Now()
	r := h.chain(chainExec{destroyAtBlock: true})
	if took := time.Since(start); took > testWaitCap {
		t.Errorf("the chain took %v: the exhausted budget did not stop the hung destroy", took)
	}
	if ExitCode(r.err) == 0 {
		t.Errorf("a destroy past its budget exited 0 (%v)", r.err)
	}
	blocked := r.index(false, func(c laneCall) bool { return c.Destroy && c.Stack == "demo-dev-gra11-runtime" && c.Cmd == "apply" })
	if blocked < 0 || r.calls[blocked].Signal != "interrupt" {
		t.Errorf("the hung destroy of demo-dev-gra11-runtime was not interrupted when its budget ran out (calls %v)", laneEvents(r.calls))
	}
	if got := r.applies(true); slices.Contains(got, "demo-dev-gra11-network") {
		t.Errorf("destroy applies %v: network destroyed after the budget ran out", got)
	}
	lines := strings.Split(strings.TrimSpace(h.term.String()), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[len(lines)-2], "LZ-LIVE summary "+r.runID+" fail ") {
		t.Errorf("terminal does not end with a fail summary:\n%s", strings.Join(lines[max(0, len(lines)-3):], "\n"))
	}
	inv, _ := ReadInventory(r.runDir)
	if all := chainExpectedInventory(laneOrder); !slices.Equal(inv, all) {
		t.Errorf("inventory %v, want %v kept for --cleanup", inv, all)
	}
	for _, id := range chainDestroyOrder {
		if s, ok := h.stateOf(id); !ok || s.Empty {
			t.Errorf("state of %s emptied or gone although the budget ran out before its destroy completed", id)
		}
		if len(h.record(id)) == 0 {
			t.Errorf("record of %s removed although its destroy did not complete", id)
		}
	}
}

// ---------------------------------------------------------------- T019 (spec 006 FR-008)

// TestRetryBudgetAdmit (FR-008, data-model *Retry budget*): a wait is admitted only if the reserve
// (the read and the remaining listings) is still left after it: end - now - wait >= reserve. Each
// row asks for one wait that leaves at least the reserve (admitted) and one that leaves less
// (refused), so a budget that admits everything or nothing fails every row.
func TestRetryBudgetAdmit(t *testing.T) {
	t0 := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	end := t0.Add(10 * time.Minute)
	for _, c := range []struct {
		name    string
		reserve time.Duration
		now     time.Time     // when the refused wait would start
		admitAt time.Time     // when the admitted wait would start
		admit   time.Duration // leaves at least the reserve
		refuse  time.Duration // leaves less
	}{
		// Exactly the reserve left is admitted; one nanosecond less is not.
		{name: "exactly-the-reserve", reserve: 30 * time.Second, now: t0, admitAt: t0,
			admit: 9*time.Minute + 30*time.Second, refuse: 9*time.Minute + 30*time.Second + time.Nanosecond},
		// A wait that fits the time left but eats the reserve is refused (chain.go readRetained
		// takes it today: it compares the wait with the time left only).
		{name: "fits-but-eats-the-reserve", reserve: 30 * time.Second, now: t0, admitAt: t0,
			admit: 5 * time.Minute, refuse: 9*time.Minute + 45*time.Second},
		// The time left counts from now, not from the budget's start.
		{name: "counts-from-now", reserve: 30 * time.Second, now: t0.Add(5 * time.Minute), admitAt: t0.Add(5 * time.Minute),
			admit: 4*time.Minute + 30*time.Second, refuse: 4*time.Minute + 31*time.Second},
		// A larger reserve (a slow listing so far) is kept as well.
		{name: "larger-reserve", reserve: 2 * time.Minute, now: t0, admitAt: t0,
			admit: 8 * time.Minute, refuse: 8*time.Minute + time.Second},
		// Less than the reserve left: even a zero wait is refused, where a minute earlier the same
		// budget admits a 30 s wait.
		{name: "reserve-already-gone", reserve: 30 * time.Second, now: t0.Add(9*time.Minute + 40*time.Second), admitAt: t0.Add(8*time.Minute + 40*time.Second),
			admit: 30 * time.Second, refuse: 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newRetryBudget(end, c.reserve)
			if !b.admit(c.admitAt, c.admit) {
				t.Errorf("admit(now=end-%v, wait %v) with reserve %v = false, want true: %v is left after the wait",
					end.Sub(c.admitAt), c.admit, c.reserve, end.Sub(c.admitAt)-c.admit)
			}
			if b.admit(c.now, c.refuse) {
				t.Errorf("admit(now=end-%v, wait %v) with reserve %v = true, want false: only %v is left after the wait, "+
					"no time for the read and the listings", end.Sub(c.now), c.refuse, c.reserve, end.Sub(c.now)-c.refuse)
			}
			if b.admit(end.Add(time.Second), 0) {
				t.Errorf("admit(now=end+1s, wait 0) = true, want false: the budget has ended")
			}
		})
	}
}

// retryClock is the waitClock of the retry-budget cases (T019): the wall clock moved on by spent
// (how much of the destroy-on-exit's budget is gone when the leftover check starts) plus every wait
// taken; After takes no real time. A budget of tens of minutes then runs out without waiting for it.
type retryClock struct {
	mu     sync.Mutex
	spent  time.Duration
	waited []time.Duration
}

func (c *retryClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nowLocked()
}

func (c *retryClock) nowLocked() time.Time {
	at := time.Now().Add(c.spent)
	for _, w := range c.waited {
		at = at.Add(w)
	}
	return at
}

func (c *retryClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waited = append(c.waited, d)
	ch := make(chan time.Time, 1)
	ch <- c.nowLocked()
	return ch
}

// total is the sum of the waits taken, and how many.
func (c *retryClock) total() (time.Duration, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sum time.Duration
	for _, w := range c.waited {
		sum += w
	}
	return sum, len(c.waited)
}

// The retry-budget cases' times (T019). The run deadline is budgetLongRun, so the destroy-on-exit
// has budgetLongRun from the destroy's start both today and under FR-002 (T018); retryClock starts
// budgetLongRun - left later, so the leftover check has about left of it. retryWait is every
// retained read's first wait; retryReserveFloor is the least reserve data-model sets
// (`reserve_seconds`, floor 30 s); retryMargin covers the real time the destroy phase takes before
// the check (under 1 s unloaded) on either side of a decision.
const (
	retryWait         = 4 * time.Minute
	retryReserveFloor = 30 * time.Second
	retryMargin       = 20 * time.Second
)

// retryChain runs a complete chain whose retained stacks in failing each answer their first
// `show -json` with a transient failure (then succeed), with about left of the leftover check's
// budget; it returns the harness, the run, the clock and the report. The fake listings take
// milliseconds, so a reserve estimated from the slowest listing stays at its 30 s floor.
func retryChain(t *testing.T, left time.Duration, failing ...string) (*laneHarness, chainRun, *retryClock, Report) {
	t.Helper()
	h := newChainHarness(t)
	for _, id := range failing {
		h.setStack(id, func(s *laneStack) {
			s.ReadFail = &laneReadFail{Call: "show-state", Answers: []string{
				"Error: reading the state: GET /v1/iam/policy: 503 Service Unavailable (" + chainAnswerMark + ")"}}
		})
	}
	clk := &retryClock{spent: budgetLongRun - left}
	r := h.chain(chainExec{deadline: budgetLongRun, clock: clk, stateBackoff: []time.Duration{retryWait, retryWait}})
	return h, r, clk, chainReport(t, r.runDir)
}

// retryReads counts the leftover check's `show -json` reads of a retained stack (after the last
// destroy).
func retryReads(r chainRun, stack string) int {
	n := 0
	for i := r.index(true, func(x laneCall) bool { return x.Destroy }) + 1; i < len(r.calls); i++ {
		if x := r.calls[i]; x.Stack == stack && x.Cmd == "show-state" {
			n++
		}
	}
	return n
}

// retryLines are the terminal's `LZ-LIVE retry` lines.
func retryLines(h *laneHarness) []string {
	var out []string
	for _, l := range strings.Split(h.term.String(), "\n") {
		if strings.HasPrefix(l, "LZ-LIVE retry") {
			out = append(out, l)
		}
	}
	return out
}

// retryBlocked checks a leftover check whose wait for stack was refused (FR-008): the report is
// `blocked` with an error carrying `blocked waiting-for=<stack>`, the run neither exits 0 nor ends
// with a pass summary, and the check still listed after its last state read (the reserve kept the
// time for it).
func retryBlocked(t *testing.T, h *laneHarness, r chainRun, rep Report, stack string) {
	t.Helper()
	want := "blocked waiting-for=" + stack
	if rep.Outcome != "blocked" || !slices.ContainsFunc(rep.Errors, func(e string) bool { return strings.Contains(e, want) }) {
		t.Errorf("leftover report outcome %q, errors %v; want blocked with an error carrying %q (FR-008: a refused wait is never a late pass)",
			rep.Outcome, rep.Errors, want)
	}
	if ExitCode(r.err) == 0 {
		t.Errorf("the run exited 0 (%v): a leftover check that could not wait for %s passed", r.err, stack)
	}
	for _, l := range strings.Split(h.term.String(), "\n") {
		if strings.HasPrefix(l, "LZ-LIVE summary "+r.runID+" pass ") {
			t.Errorf("summary %q: a pass although the wait for %s was refused", l, stack)
		}
	}
	lastRead := r.index(true, func(x laneCall) bool { return x.Cmd == "show-state" && slices.Contains(chainRetained, x.Stack) })
	if lastList := r.index(true, func(x laneCall) bool { return x.Cmd == "list" }); lastRead < 0 || lastList < lastRead {
		t.Errorf("no leftover listing after the last state read (events %v): the refusal keeps the time for the listings", laneEvents(r.calls))
	}
}

// TestChainRetryBudgetShared (FR-008): three retained instances each need one retry, and the
// leftover check reads them under one retry budget: the waits it takes, across the instances, all
// finish within that budget with the reserve left. all-fit (a pin today): with 30 min left, the
// three waits fit; every read is retried once and the check passes. third-does-not-fit: with
// 3*retryWait + retryMargin left, the third instance's wait would eat the reserve; it is refused
// and the check is blocked waiting for that instance. Today's code judges each wait against the
// time left only (chain.go readRetained, called afresh per instance by leftovers): it takes the
// third wait and passes with retryMargin left for the read and the listings.
func TestChainRetryBudgetShared(t *testing.T) {
	three := []string{"account-bootstrap", "account-governance", "demo-state"}
	t.Run("all-fit", func(t *testing.T) {
		left := 30 * time.Minute
		h, r, clk, rep := retryChain(t, left, three...)
		laneExit(t, r.laneRun, 0)
		if rep.Outcome != "pass" || len(rep.Errors) != 0 || len(rep.Leftovers) != 0 {
			t.Errorf("report outcome %q, errors %v, leftovers %v; want pass: the three waits fit the budget",
				rep.Outcome, rep.Errors, leftoverKeys(rep.Leftovers))
		}
		for _, id := range three {
			if n := retryReads(r, id); n != 2 {
				t.Errorf("%d reads of %s, want 2 (one retry)", n, id)
			}
		}
		if got := len(retryLines(h)); got != 3 {
			t.Errorf("%d LZ-LIVE retry lines, want 3", got)
		}
		if got, n := clk.total(); n != 3 || got > left-retryReserveFloor {
			t.Errorf("the check waited %v (%d waits) of the %v left; want 3 waits, at most %v (the reserve kept)",
				got, n, left, left-retryReserveFloor)
		}
	})
	t.Run("third-does-not-fit", func(t *testing.T) {
		left := 3*retryWait + retryMargin
		h, r, clk, rep := retryChain(t, left, three...)
		if got, n := clk.total(); n != 2 || got > left-retryReserveFloor {
			t.Errorf("the check waited %v (%d waits) of the %v left; want 2 waits, at most %v: one budget for every instance, "+
				"the reserve kept for the read and the listings", got, n, left, left-retryReserveFloor)
		}
		for _, c := range []struct {
			id    string
			reads int
		}{{"account-bootstrap", 2}, {"account-governance", 2}, {"demo-state", 1}} {
			if n := retryReads(r, c.id); n != c.reads {
				t.Errorf("%d reads of %s, want %d", n, c.id, c.reads)
			}
		}
		if got := retryLines(h); len(got) != 2 {
			t.Errorf("%d LZ-LIVE retry lines, want 2 (account-bootstrap, account-governance):\n%s", len(got), strings.Join(got, "\n"))
		}
		retryBlocked(t, h, r, rep, "demo-state")
	})
}

// TestChainRetryBudgetRefused (FR-008): a retained instance needs a retry whose wait the budget
// refuses; the check reports `blocked waiting-for=<instance>`, never pass, and takes no wait.
// reserve-not-left: the wait fits the time left but leaves retryMargin, less than the reserve
// (today: taken, then a late pass). beyond-the-budget: the wait is longer than the time left
// (today: refused, but reported as a plain fail naming no instance waited for).
func TestChainRetryBudgetRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		left time.Duration
	}{
		{name: "reserve-not-left", left: retryWait + retryMargin},
		{name: "beyond-the-budget", left: retryWait / 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, r, clk, rep := retryChain(t, c.left, "account-governance")
			if got, n := clk.total(); n != 0 {
				t.Errorf("the check took %d waits (%v) with %v left: a wait of %v leaves less than the reserve", n, got, c.left, retryWait)
			}
			if n := retryReads(r, "account-governance"); n != 1 {
				t.Errorf("%d reads of account-governance, want 1 (the retry refused)", n)
			}
			if got := retryLines(h); len(got) != 0 {
				t.Errorf("LZ-LIVE retry lines %v, want none", got)
			}
			retryBlocked(t, h, r, rep, "account-governance")
		})
	}
}
