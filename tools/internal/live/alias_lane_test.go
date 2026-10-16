package live

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Project alias refusal in the lane's setup, 007 T008 (FR-003, SC-004, V002; ADR-0009, ADR-0021;
// research R2): plan, apply, destroy and chain on an account whose two tenant refs resolve to one
// project stop with project-alias before any child process, naming both refs and no id; the
// sandbox's KD-1 binding (STATE = DEMO_DEV under the flag) still runs. The aliased manifest is the
// checkout's sandbox manifest plus a second tenant, shop, with one environment on its own ref
// SHOP_DEV (B5's shape), its roots copied from demo's: a run that is not refused at setup goes on
// to start children. Ids are built here, never read from disk.

const (
	// laneAliasTarget is an ephemeral row of the sandbox manifest.
	laneAliasTarget = "demo-dev-gra11-network"
	// laneShopTenant is the second tenant: one environment, one region, its own ref SHOP_DEV.
	laneShopTenant = `{"name": "shop", "environments": [{"name": "dev", "project": {"mode": "reference", "ref": "SHOP_DEV"},
      "regions": [{"name": "GRA11", "network": {"cidr": "10.30.0.0/24", "vlan_id": 0}}],
      "budget_alert": {"enabled": false}, "quota_guard": {"enabled": false}}]},
      `
	laneShopRows = `{"id": "shop-state", "stage": "tenant-state", "tenant": "shop"},
      {"id": "shop-dev-project", "stage": "project", "tenant": "shop", "environment": "dev"},
      `
)

// aliasHexID matches a 32-hex project id standing alone (not inside a longer hex run), in either
// case.
var aliasHexID = regexp.MustCompile(`(?i)(^|[^0-9a-f])[0-9a-f]{32}([^0-9a-f]|$)`)

// sandboxTwoTenants decodes the checkout's sandbox manifest with the tenant shop added.
func sandboxTwoTenants(t *testing.T, checkout string) *stacks.Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(checkout, stacks.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, r := range [][2]string{{`"tenants": [`, laneShopTenant}, {`"instances": [`, laneShopRows}} {
		if strings.Count(doc, r[0]) != 1 {
			t.Fatalf("sandbox manifest: want one %s", r[0])
		}
		doc = strings.Replace(doc, r[0], r[0]+"\n      "+r[1], 1)
	}
	m, err := stacks.DecodeManifest([]byte(doc))
	if err != nil {
		t.Fatalf("sandbox manifest with a second tenant: %v", err)
	}
	if !m.SharedStateProject || len(m.Tenants) != 2 {
		t.Fatalf("sandbox manifest with a second tenant: flag %t, %d tenants", m.SharedStateProject, len(m.Tenants))
	}
	return m
}

// aliasTenants binds the two-tenant sandbox manifest to an account.env in which STATE, DEMO_DEV
// and SHOP_DEV hold the sandbox's one project: two tenants on one project.
func (h *laneHarness) aliasTenants() {
	h.t.Helper()
	h.m = sandboxTwoTenants(h.t, h.checkout)
	// shop's roots as copies of demo's, so a run the setup lets through reaches its children.
	for shop, demo := range map[string]string{"shop-state": "demo-state", "shop-dev-project": "demo-dev-project"} {
		laneCopyTree(h.t, filepath.Join(h.checkout, filepath.FromSlash(h.row(demo).Path)), filepath.Join(h.checkout, filepath.FromSlash(h.row(shop).Path)))
	}
	h.writeConfig("accounts/"+laneAccount+"/account.env", map[string]string{"LZ_ACCOUNT_ID": laneAccount, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz",
		"LZ_PROJECT_ID_STATE": laneProject, "LZ_PROJECT_ID_DEMO_DEV": laneProject, "LZ_PROJECT_ID_SHOP_DEV": laneProject,
		"LZ_ADMIN_CLIENT_ID": laneAdminID, "LZ_ADMIN_POLICY_ID": chainAdminPolicy})
}

// laneAliasVerbs runs each lane verb once; children counts the chain's child processes too.
var laneAliasVerbs = []struct {
	name string
	run  func(h *laneHarness, children *childDeadlines) laneRun
}{
	{VerbPlan, func(h *laneHarness, _ *childDeadlines) laneRun { r, _ := h.exec(VerbPlan, TargetAll); return r }},
	{VerbApply, func(h *laneHarness, _ *childDeadlines) laneRun { r, _ := h.exec(VerbApply, TargetAll); return r }},
	{VerbDestroy, func(h *laneHarness, _ *childDeadlines) laneRun { r, _ := h.exec(VerbDestroy, laneAliasTarget); return r }},
	{"chain", func(h *laneHarness, children *childDeadlines) laneRun {
		return h.chain(chainExec{children: children}).laneRun
	}},
}

// TestLaneProjectAlias: two tenant refs on one project end every verb with project-alias, with
// zero child processes and no project id in the error, the terminal or the process's own
// streams. The harness is steady (every sandbox stack applied, the deployer files written), so a
// run the setup lets through starts tofu. Exposes a check placed after the first child (init) or
// a refusal printing the id.
func TestLaneProjectAlias(t *testing.T) {
	for _, v := range laneAliasVerbs {
		t.Run(v.name, func(t *testing.T) {
			h := newChainHarness(t)
			h.steady()
			h.aliasTenants()
			children := &childDeadlines{}
			r := v.run(h, children)
			laneAliasRefused(t, r)
			if calls := r.tofu(); len(calls) > 0 {
				t.Errorf("%d child processes (first: tofu %s on %s) before the alias refusal, want none", len(calls), calls[0].Cmd, calls[0].Stack)
			}
			if n := len(children.calls); n > 0 {
				t.Errorf("the chain started %d children before the alias refusal, want none", n)
			}
			laneAliasNoID(t, h, r)
		})
	}
}

// laneAliasRefused: the run ended with the project-alias refusal naming both tenant refs.
func laneAliasRefused(t *testing.T, r laneRun) {
	t.Helper()
	laneCondition(t, r, CondProjectAlias)
	var ref *Refusal
	if !errors.As(r.err, &ref) {
		return
	}
	for _, name := range []string{"DEMO_DEV", "SHOP_DEV"} {
		if !strings.Contains(ref.Detail, name) {
			t.Errorf("refusal %q does not name %s", ref.Detail, name)
		}
	}
}

// laneAliasNoID: neither the error, the terminal nor the process's own streams hold a project id.
func laneAliasNoID(t *testing.T, h *laneHarness, r laneRun) {
	t.Helper()
	out := map[string]string{"terminal": h.term.String(), "process streams": r.process}
	if r.err != nil {
		out["error"] = r.err.Error()
	}
	for where, s := range out {
		if strings.Contains(s, laneProject) || aliasHexID.MatchString(s) {
			t.Errorf("the %s holds a project id: %q", where, s)
		}
	}
}

// TestLaneProjectAliasSandboxPin (pin): the sandbox's KD-1 account (STATE = DEMO_DEV under the
// flag) is not refused as an alias on any verb: each run passes (plan and destroy on the steady
// account, apply and chain as a first run).
func TestLaneProjectAliasSandboxPin(t *testing.T) {
	for _, v := range laneAliasVerbs {
		t.Run(v.name, func(t *testing.T) {
			h := newChainHarness(t)
			if v.name == VerbPlan || v.name == VerbDestroy {
				h.steady()
			}
			r := v.run(h, &childDeadlines{})
			var ref *Refusal
			if errors.As(r.err, &ref) && ref.Condition == CondProjectAlias {
				t.Fatalf("the sandbox's KD-1 binding refused: %v", r.err)
			}
			laneExit(t, r, 0)
		})
	}
}
