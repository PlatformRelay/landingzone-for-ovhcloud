package stacks

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// Project alias refusal of 007 T008 (FR-003, SC-004, V002; ADR-0009, ADR-0021; research R2).
// In a bound account, two project refs the manifest uses must not resolve to one project id; the
// only alias admitted is the state ref on a tenant environment's id under
// spec.sandbox.shared_state_project (KD-1). Refs the manifest does not use are not judged. The
// refusal names both refs and never the id. Ids are built here, never read from disk.

// aliasIDs are distinct project ids for the growth fixture's refs; a case overrides some of them.
func aliasIDs() map[string]string {
	return map[string]string{
		"STATE":     "a7000000000000000000000000000001",
		"DEMO_DEV":  "a7000000000000000000000000000002",
		"DEMO_PROD": "a7000000000000000000000000000003",
		"SHOP_DEV":  "a7000000000000000000000000000004",
		"SHOP_PROD": "a7000000000000000000000000000005",
	}
}

// aliasAccount is the bound account of ids with some refs set to another ref's id.
func aliasAccount(same map[string]string) BoundAccount {
	ids := aliasIDs()
	for ref, to := range same {
		ids[ref] = ids[to]
	}
	return BoundAccount{Endpoint: "ovh-eu", ProjectIDs: ids}
}

// growthShared decodes the growth fixture with spec.sandbox.shared_state_project true, after
// replacing each old text (which must occur once) with its new text.
func growthShared(t *testing.T, replace ...string) *Manifest {
	t.Helper()
	doc := string(readManifestFixture(t, "growth/deployments.yaml"))
	replace = append(replace, `"state": {`, `"sandbox": {"shared_state_project": true},
    "state": {`)
	for i := 0; i < len(replace); i += 2 {
		if n := strings.Count(doc, replace[i]); n != 1 {
			t.Fatalf("growth fixture holds %q %d times, want once", replace[i], n)
		}
		doc = strings.Replace(doc, replace[i], replace[i+1], 1)
	}
	m, err := DecodeManifest([]byte(doc))
	if err != nil {
		t.Fatalf("growth with the KD-1 flag: DecodeManifest: %v", err)
	}
	if !m.SharedStateProject {
		t.Fatal("growth with the KD-1 flag decoded without it")
	}
	return m
}

// aliasHexID matches a 32-hex id standing alone (not inside a longer hex run), in either case.
var aliasHexID = regexp.MustCompile(`(?i)(^|[^0-9a-f])[0-9a-f]{32}([^0-9a-f]|$)`)

// wantAlias: err is the project-alias refusal naming every ref of refs and no project id.
func wantAlias(t *testing.T, err error, a BoundAccount, refs ...string) {
	t.Helper()
	var ref *RefusalError
	if !errors.As(err, &ref) || ref.Reason != ReasonProjectAlias {
		t.Fatalf("err %v, want a *RefusalError with reason %s", err, ReasonProjectAlias)
	}
	for _, r := range refs {
		if !regexp.MustCompile(`\b` + r + `\b`).MatchString(ref.Detail) {
			t.Errorf("detail %q does not name %s", ref.Detail, r)
		}
	}
	for r, id := range a.ProjectIDs {
		if strings.Contains(ref.Detail, id) {
			t.Errorf("detail %q prints the project id of %s", ref.Detail, r)
		}
	}
	if aliasHexID.MatchString(ref.Detail) {
		t.Errorf("detail %q holds a 32-hex id", ref.Detail)
	}
}

// TestProjectAliasTenants: two tenants' refs on one project are refused (T098's hole: they pass
// DUPLICATE_NAME and DUPLICATE_VLAN today).
func TestProjectAliasTenants(t *testing.T) {
	a := aliasAccount(map[string]string{"SHOP_DEV": "DEMO_DEV"})
	wantAlias(t, CheckProjectAliases(decodeFixture(t, "growth"), a), a, "DEMO_DEV", "SHOP_DEV")
}

// TestProjectAliasEnvironments: one tenant's two environments on one project are refused too: a
// check limited to pairs of tenants misses it (T098's VLAN collisions across environments).
func TestProjectAliasEnvironments(t *testing.T) {
	a := aliasAccount(map[string]string{"DEMO_PROD": "DEMO_DEV"})
	wantAlias(t, CheckProjectAliases(decodeFixture(t, "growth"), a), a, "DEMO_DEV", "DEMO_PROD")
}

// TestProjectAliasSandboxPin (pin): the sandbox manifest under the KD-1 flag with STATE = DEMO_DEV
// binds; the sandbox runs on it.
func TestProjectAliasSandboxPin(t *testing.T) {
	id := "a7000000000000000000000000000002"
	a := BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"STATE": id, "DEMO_DEV": id}}
	if err := CheckProjectAliases(decodeFixture(t, "sandbox"), a); err != nil {
		t.Errorf("the sandbox's KD-1 binding: %v, want nil", err)
	}
}

// TestProjectAliasFlagTwoTenantsPin (pin): the KD-1 exemption holds on a manifest with two tenants
// too: under the flag, STATE = DEMO_DEV with every other ref on its own project binds (FR-003
// limits the exemption to the state ref, not to one-tenant manifests).
func TestProjectAliasFlagTwoTenantsPin(t *testing.T) {
	a := aliasAccount(map[string]string{"STATE": "DEMO_DEV"})
	if err := CheckProjectAliases(growthShared(t), a); err != nil {
		t.Errorf("growth under the KD-1 flag with STATE = DEMO_DEV: %v, want nil", err)
	}
}

// TestProjectAliasStateWithoutFlag: without the flag, the state ref on a tenant environment's id
// is refused naming STATE: sharing at the id, which the decoder's ref check
// (manifest.go:583-590) cannot see.
func TestProjectAliasStateWithoutFlag(t *testing.T) {
	m := decodeFixture(t, "growth")
	if m.SharedStateProject {
		t.Fatal("growth fixture carries the KD-1 flag")
	}
	a := aliasAccount(map[string]string{"STATE": "DEMO_DEV"})
	wantAlias(t, CheckProjectAliases(m, a), a, "STATE", "DEMO_DEV")
}

// TestProjectAliasFlagNotTenants: the flag admits the state ref beside a tenant, never two
// tenants: STATE = DEMO_DEV = SHOP_DEV is refused naming the two tenant refs (D119: never weaken
// A17).
func TestProjectAliasFlagNotTenants(t *testing.T) {
	a := aliasAccount(map[string]string{"STATE": "DEMO_DEV", "SHOP_DEV": "DEMO_DEV"})
	wantAlias(t, CheckProjectAliases(growthShared(t), a), a, "DEMO_DEV", "SHOP_DEV")
}

// TestProjectAliasTenantOwnsState: under the flag, a second tenant environment (shop/dev) that
// itself owns the STATE ref, with STATE = DEMO_DEV, is refused naming the two tenant
// environments' refs: an exemption keyed on the state ref's name would let a second tenant onto
// the one project (T001, research R2 step 4 and *Consequence*).
func TestProjectAliasTenantOwnsState(t *testing.T) {
	m := growthShared(t, `"project": {"mode": "adopt", "ref": "SHOP_DEV"}`, `"project": {"mode": "adopt", "ref": "STATE"}`)
	if got := m.projectRef("shop", "dev"); got != "STATE" {
		t.Fatalf("shop/dev ref %q, want STATE", got)
	}
	a := aliasAccount(map[string]string{"STATE": "DEMO_DEV"})
	delete(a.ProjectIDs, "SHOP_DEV")
	wantAlias(t, CheckProjectAliases(m, a), a, "STATE", "DEMO_DEV")
}

// TestProjectAliasUnusedRefPin (pin): a ref the manifest never uses is not judged, even when it
// holds a used ref's id.
func TestProjectAliasUnusedRefPin(t *testing.T) {
	a := aliasAccount(nil)
	a.ProjectIDs["SPARE"] = a.ProjectIDs["DEMO_DEV"]
	if err := CheckProjectAliases(decodeFixture(t, "growth"), a); err != nil {
		t.Errorf("LZ_PROJECT_ID_SPARE beside the growth manifest: %v, want nil", err)
	}
}
