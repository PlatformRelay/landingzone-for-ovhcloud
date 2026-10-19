package stacks

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"testing"
)

// Second tenant offline of 007 T017 (FR-009, US4; ADR-0004, ADR-0005; data-model *Two-tenant
// sandbox fixture*). The sandbox manifest plus tenant lab (own ref LAB_DEV, KD-1 flag set)
// decodes, reconciles, generates and orders, each tenant's stacks after its own tenant-state; with
// distinct project ids per ref it binds, and with both tenant refs on one id FR-003 refuses it (B5
// stays offline). Expectations are written out here, never derived by the code under test.

const twoTenantFixture = "sandbox-two-tenants"

// twoTenantStack is what one tenant stack of the fixture must be after reconcile and generate.
type twoTenantStack struct {
	id, path, bucket, key, tenant string
	authority                     []string // authority producers: its own tenant-state (and account-governance)
	after                         []string // the stack's Terramate after filters
}

func twoTenantStacks() []twoTenantStack {
	var out []twoTenantStack
	for _, tn := range []string{"demo", "lab"} {
		state := "tag:lz-stage-tenant-state:lz-tenant-" + tn
		env := []string{"tag:lz-stage-account-governance", state}
		regional := append(slices.Clone(env), "tag:lz-stage-project:lz-tenant-"+tn+":lz-env-dev")
		bucket := "lz-" + tn + "-bkt-state"
		out = append(out,
			twoTenantStack{tn + "-state", "stacks/account/tenant-state/" + tn, "lz-bkt-state",
				"account/tenant-state/" + tn + "/terraform.tfstate", tn,
				[]string{"account-bootstrap"}, []string{"tag:lz-stage-bootstrap"}},
			twoTenantStack{tn + "-dev-project", "stacks/tenants/" + tn + "/dev/project", bucket,
				"tenants/" + tn + "/dev/project/terraform.tfstate", tn,
				[]string{"account-governance", tn + "-state"}, env},
			twoTenantStack{tn + "-dev-gra11-network", "stacks/tenants/" + tn + "/dev/gra11/project-network", bucket,
				"tenants/" + tn + "/dev/gra11/project-network/terraform.tfstate", tn,
				[]string{"account-governance", tn + "-state"}, regional},
			twoTenantStack{tn + "-dev-gra11-runtime", "stacks/tenants/" + tn + "/dev/gra11/runtime", bucket,
				"tenants/" + tn + "/dev/gra11/runtime/terraform.tfstate", tn,
				[]string{"account-governance", tn + "-state"}, regional},
		)
	}
	return out
}

func sandboxTwoTenantsManifest(t *testing.T) *Manifest {
	t.Helper()
	m, err := DecodeManifest(manifestOf(t, twoTenantFixture))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !m.SharedStateProject || m.StateProjectRef != "STATE" {
		t.Fatalf("fixture: shared_state_project %v, state ref %q; want the KD-1 flag and STATE", m.SharedStateProject, m.StateProjectRef)
	}
	refs := map[string]string{}
	for _, tn := range m.Tenants {
		for _, e := range tn.Environments {
			refs[tn.Name+"/"+e.Name] = e.ProjectRef
		}
	}
	if want := map[string]string{"demo/dev": "DEMO_DEV", "lab/dev": "LAB_DEV"}; !maps.Equal(refs, want) {
		t.Fatalf("fixture tenant environments %v, want %v", refs, want)
	}
	return m
}

// authorityOf is the sorted producer ids of in's authority edges.
func authorityOf(in Instance) []string {
	var out []string
	for _, e := range in.Edges {
		if e.Kind == EdgeAuthority {
			out = append(out, e.Producer)
		}
	}
	slices.Sort(out)
	return out
}

// Case 1: given the fixture, when decoded, reconciled and generated into a temporary tree, then
// lab's four stacks exist beside demo's, each with its own state bucket and its own tenant-state
// as deployer authority (a generator that assumes one tenant fails here).
func TestTwoTenantGenerates(t *testing.T) {
	data := manifestOf(t, twoTenantFixture)
	m := sandboxTwoTenantsManifest(t)
	root := stageLockedRoot(t, data)
	if err := generateIn(t, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	all := generatedFiles(t, root)
	existing, _, err := discoverStacks(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(existing) != len(m.Instances) || len(m.Instances) != 10 {
		t.Errorf("%d stacks for %d rows, want 10 of each", len(existing), len(m.Instances))
	}
	for _, want := range twoTenantStacks() {
		t.Run(want.id, func(t *testing.T) {
			checkTwoTenantStack(t, m, want, existing[want.path], parseStack(t, all, want.path))
		})
	}
	var raw rawSpec
	if err := json.Unmarshal(stripComments(data), &raw); err != nil {
		t.Fatal(err)
	}
	checkStackInvariants(t, twoTenantFixture, m, raw, all)
}

func checkTwoTenantStack(t *testing.T, m *Manifest, want twoTenantStack, got existingStack, ps parsedStack) {
	t.Helper()
	in, err := m.Row(want.id)
	if err != nil {
		t.Fatal(err)
	}
	if in.Path != want.path || got.id != want.id {
		t.Errorf("row path %s, stack id %q at %s; want stack %s at %s", in.Path, got.id, want.path, want.id, want.path)
	}
	if a := authorityOf(in); !slices.Equal(a, want.authority) {
		t.Errorf("authority %q, want %q", a, want.authority)
	}
	if !sameSet(got.after, want.after) {
		t.Errorf("after %q, want %q", got.after, want.after)
	}
	terraform, _ := ps.blocks("terraform")
	var backends []string
	for _, tb := range terraform {
		for _, be := range childBlocks(tb.Body, "backend") {
			backends = append(backends, attr(be.Body, "bucket"), attr(be.Body, "key"))
		}
	}
	if wantBE := []string{strconv.Quote(want.bucket), strconv.Quote(want.key)}; !slices.Equal(backends, wantBE) {
		t.Errorf("generated backend bucket, key %q, want %q", backends, wantBE)
	}
	if in.Stage != "tenant-state" && in.Stage != "project" {
		return // the regional stages take the tenant from their project input, not an argument
	}
	mods, _ := ps.blocks("module")
	if len(mods) != 1 || attr(mods[0].Body, "tenant") != strconv.Quote(want.tenant) {
		t.Errorf("generated module does not name tenant %q", want.tenant)
	}
}

// Case 2: given the fixture, when Order runs, then each tenant's stacks follow its own
// tenant-state and account-governance (cross-tenant ordering fails here).
func TestTwoTenantOrder(t *testing.T) {
	m := sandboxTwoTenantsManifest(t)
	ids, err := Order(m)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	after := orderedAfter(t, m, ids)
	for _, want := range twoTenantStacks() {
		in, err := m.Row(want.id)
		if err != nil {
			t.Fatal(err)
		}
		if a := authorityOf(in); !slices.Equal(a, want.authority) {
			t.Errorf("%s runs under %q, want %q", want.id, a, want.authority)
		}
		after(want.id, want.authority...)
	}
	for _, tn := range []string{"demo", "lab"} {
		after(tn+"-dev-project", tn+"-state", "account-governance")
		after(tn+"-dev-gra11-network", tn+"-state", "account-governance", tn+"-dev-project")
		after(tn+"-dev-gra11-runtime", tn+"-state", "account-governance", tn+"-dev-project")
	}
	for _, in := range m.Instances {
		for _, e := range in.Edges {
			after(in.ID, e.Producer)
		}
	}
}

// orderedAfter checks that ids holds every row of m once and returns a check that id comes after
// each of producers in ids.
func orderedAfter(t *testing.T, m *Manifest, ids []string) func(id string, producers ...string) {
	t.Helper()
	at := map[string]int{}
	for i, id := range ids {
		at[id] = i
	}
	missing := slices.IndexFunc(m.Instances, func(in Instance) bool { _, ok := at[in.ID]; return !ok })
	if len(ids) != len(m.Instances) || len(at) != len(m.Instances) || missing >= 0 {
		t.Fatalf("order %q, want every one of the %d rows once", ids, len(m.Instances))
	}
	return func(id string, producers ...string) {
		t.Helper()
		for _, p := range producers {
			if at[p] >= at[id] {
				t.Errorf("order %q: %s runs before %s", ids, id, p)
			}
		}
	}
}

// twoTenantAccount is a bound account with distinct ids for STATE, DEMO_DEV and LAB_DEV, after
// setting each ref of same to the id of the ref it maps to. Ids are built here, never read from
// ~/.config.
func twoTenantAccount(same map[string]string) BoundAccount {
	ids := map[string]string{
		"STATE":    "a7100000000000000000000000000001",
		"DEMO_DEV": "a7100000000000000000000000000002",
		"LAB_DEV":  "a7100000000000000000000000000003",
	}
	for ref, to := range same {
		ids[ref] = ids[to]
	}
	return BoundAccount{Endpoint: "ovh-eu", ProjectIDs: ids}
}

// Case 3: given distinct ids for DEMO_DEV and LAB_DEV (STATE = DEMO_DEV under KD-1), when
// CheckProjectAliases runs, then nil and each tenant's project stack binds its own id; given
// LAB_DEV = DEMO_DEV (the one sandbox project), then project-alias naming both refs (pin after
// T009: B5 on one project is refused).
func TestTwoTenantAliases(t *testing.T) {
	m := sandboxTwoTenantsManifest(t)
	t.Run("distinct", func(t *testing.T) {
		a := twoTenantAccount(map[string]string{"STATE": "DEMO_DEV"})
		if err := CheckProjectAliases(m, a); err != nil {
			t.Fatalf("STATE = DEMO_DEV, LAB_DEV distinct: %v, want nil", err)
		}
		for id, ref := range map[string]string{"demo-dev-project": "DEMO_DEV", "lab-dev-project": "LAB_DEV"} {
			b, err := InstanceBinding(m, id, a)
			if err != nil || b.ProjectID != a.ProjectIDs[ref] {
				t.Errorf("%s binds %+v, %v; want the id of %s", id, b, err, ref)
			}
		}
	})
	t.Run("one-project", func(t *testing.T) {
		a := twoTenantAccount(map[string]string{"STATE": "DEMO_DEV", "LAB_DEV": "DEMO_DEV"})
		wantAlias(t, CheckProjectAliases(m, a), a, "DEMO_DEV", "LAB_DEV")
	})
}
