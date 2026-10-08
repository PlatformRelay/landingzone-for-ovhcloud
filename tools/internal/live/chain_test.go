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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
}

func (l chainLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	if !*l.done {
		if raw, err := os.ReadFile(l.log); err == nil && strings.Contains(string(raw), `"cmd":"apply"`) {
			l.late()
			*l.done = true
		}
	}
	laneAppend(l.log, laneCall{Cmd: "list", Stack: path})
	return l.inner.Get(ctx, path, cursor)
}

// chainExec is one chain run's setup beyond the fake world.
type chainExec struct {
	target   string
	deadline time.Duration
	signal   os.Signal // sent once a call blocks (laneStack.Block); nil: nothing is sent
	seed     syntheticSeed
	faults   map[string]listFault
}

// chainRun is a chain run and what the harness saw while it ran.
type chainRun struct {
	laneRun
	runID string
	// invAtBlock is the inventory as it was while the blocked call still ran.
	invAtBlock []InventoryEntry
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
	f := newFakeListAPI(t, seededResponses(w, syntheticSeed{}))
	seeded := seededResponses(w, c.seed)
	late := func() { f.mu.Lock(); f.responses = seeded; f.mu.Unlock() }
	lateDone := false
	for path, fault := range c.faults {
		f.faults[path] = fault
	}
	h.runs++
	runDir := filepath.Join(h.checkout, ".local", "live", runID)
	from := len(h.logCalls())
	before := laneTree(t, h.checkout, filepath.Join(h.checkout, ".local"))
	h.term.Reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sigs := make(chan os.Signal, 1)
	var invAtBlock []InventoryEntry
	stop, watched := make(chan struct{}), make(chan struct{})
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
			// The blocked call printed its apply_complete events first: the inventory holds
			// them while the child still runs, or it is not appended as they arrive.
			for i := 0; i < 150; i++ {
				invAtBlock, _ = ReadInventory(runDir)
				if len(invAtBlock) > 0 && invAtBlock[len(invAtBlock)-1].Stack == "demo-dev-gra11-runtime" {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if c.signal != nil {
				sigs <- c.signal
			}
			return
		}
	}()
	target := c.target
	if target == "" {
		target = TargetAll
	}
	streams := laneCaptureStd(t)
	err := Chain(ctx, ChainOptions{ApplyOptions: ApplyOptions{Target: target, Checkout: h.checkout, Manifest: h.m, ConfigRoot: h.root, Account: laneAccount,
		RunID: runID, RunDir: runDir, Records: h.recordsDir(), Revision: laneRevision, Tofu: filepath.Join(h.bin, "tofu"), Schemas: laneSchemas(),
		Locks: laneLocks{inner: stacks.DirLocks(h.world.LockDir), log: h.world.Log, after: h.afterLock}, Store: h.store.open, Terminal: &h.term, API: h.laneAPI()},
		Deadline: c.deadline, Signals: sigs, Lister: chainLister{inner: newAPILister(f.API(), listCred), log: h.world.Log, late: late, done: &lateDone}})
	process := streams()
	close(stop)
	<-watched
	_ = os.Remove(h.world.Log + ".blocked")
	r := chainRun{laneRun: laneRun{err: err, calls: h.logCalls()[from:], runDir: runDir, process: process}, runID: runID, invAtBlock: invAtBlock}
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
		deadline time.Duration
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
			deadline: 6 * time.Second, applied: laneOrder, destroy: chainDestroyOrder, stopped: "interrupt", reason: "deadline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newChainHarness(t)
			tc.setup(h)
			r := h.chain(chainExec{signal: tc.signal, deadline: tc.deadline})
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
// unparseable listing is `fail`, never `pass`.
func TestChainLeftovers(t *testing.T) {
	type row struct {
		name   string
		seed   func(p capturedProvenance, w syntheticWorld) syntheticSeed
		setup  func(h *laneHarness)
		faults func(p capturedProvenance) map[string]listFault
		expect []string // leftover keys; nil with errors=true: errors only
		errors bool
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
		}, errors: true},
		row{name: "listing-unparseable", faults: func(p capturedProvenance) map[string]listFault {
			return map[string]listFault{"/cloud/project/" + laneProject + "/network/private": {Kind: "malformed"}}
		}, errors: true},
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
			if tc.faults != nil {
				c.faults = tc.faults(p)
			}
			r := h.chain(c)
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
