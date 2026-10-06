package live

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Retained-resource guard G7, plan part (FR-004, FR-011, research R12). The plans in
// tests/fixtures/tofu-probes/protect/ are `tofu show -json` of saved plans, captured by its
// capture.sh with the pinned OpenTofu on provider-free roots: terraform_data stands in for the
// state bucket, the platform S3 user (create_before_destroy), the tenant buckets (for_each over
// the tenants), the adopted project, a governance module and one non-retained resource
// (project_scratch). truncated.json is org-change.json cut in half.

var protectDir = filepath.Join("..", "..", "..", "tests", "fixtures", "tofu-probes", "protect")

// pinnedTofu is the OpenTofu version of mise.toml and the offline entry.
const pinnedTofu = "1.13.0"

// bootstrapRetained is the planned root's retained set as a caller declares it: unkeyed resource
// addresses and module addresses (one of them keyed by for_each). The project is a retained
// instance of its own.
var bootstrapRetained = Retained{Instance: "account-bootstrap", Addresses: []string{
	"terraform_data.state_bucket", "terraform_data.platform_user", "terraform_data.tenant_bucket", "module.governance", "module.region",
}}
var projectRetained = Retained{Instance: "demo-dev-project", Addresses: []string{"terraform_data.project"}}

// planValues are values the captured plans carry; a refusal never repeats one (no raw plan JSON in
// output, contracts/checks.md). Values that are substrings of addresses (`platform`, `r2`) cannot
// be checked this way.
var planValues = []string{"acme-lz-state", "acme2-lz-state", "p-1111", "p-2222", "lzseed-7f3a9c", "alpha-state", "beta-state",
	"platform-deployer", "platform-renamed"}

func readPlan(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(protectDir, name+".json"))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// planActions returns the actions per address of a captured plan (fixture shape check: a
// recapture that changes the shape fails here, not in the guard's verdict).
func planActions(t *testing.T, plan []byte) map[string][]string {
	t.Helper()
	var p struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(plan, &p); err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	out := map[string][]string{}
	for _, rc := range p.ResourceChanges {
		out[rc.Address] = rc.Change.Actions
	}
	return out
}

// TestProtect: admitted plans return nil; refused plans return a *Refusal with CondRetained (exit
// 3) naming every refused address and its instance, and no value from the plan.
func TestProtect(t *testing.T) {
	type tc struct {
		name     string
		fixture  string // captured plan; "" when raw is used
		raw      string
		retained []Retained
		shape    map[string][]string // fixture actions the case relies on
		refused  []string            // addresses the refusal names; nil: admitted
	}
	both := []Retained{bootstrapRetained, projectRetained}
	cases := []tc{
		{name: "admit-noop", fixture: "noop", retained: both,
			shape: map[string][]string{"terraform_data.state_bucket": {"no-op"}, "terraform_data.project": {"no-op"}}},
		{name: "admit-create", fixture: "create", retained: both,
			shape: map[string][]string{"terraform_data.state_bucket": {"create"}, `terraform_data.tenant_bucket["beta"]`: {"create"}}},
		{name: "admit-update-retained", fixture: "update", retained: both,
			shape: map[string][]string{"terraform_data.platform_user": {"update"}}},
		// project_scratch only shares a prefix with the retained terraform_data.project.
		{name: "admit-unretained-replace", fixture: "unretained-replace", retained: both,
			shape: map[string][]string{"terraform_data.project_scratch": {"delete", "create"}}},
		// The same replacement as refuse-org-change, but nothing retains the state bucket.
		{name: "admit-unretained-org-change", fixture: "org-change", retained: []Retained{projectRetained},
			shape: map[string][]string{"terraform_data.state_bucket": {"delete", "create"}}},
		{name: "refuse-removed-block", fixture: "removed-block", retained: both,
			shape:   map[string][]string{"terraform_data.state_bucket": {"delete"}},
			refused: []string{"terraform_data.state_bucket"}},
		{name: "refuse-org-change", fixture: "org-change", retained: both,
			shape:   map[string][]string{"terraform_data.state_bucket": {"delete", "create"}},
			refused: []string{"terraform_data.state_bucket"}},
		{name: "refuse-create-before-destroy", fixture: "create-before-destroy", retained: both,
			shape:   map[string][]string{"terraform_data.platform_user": {"create", "delete"}},
			refused: []string{"terraform_data.platform_user"}},
		{name: "refuse-tenant-removed-unkeyed", fixture: "tenant-removed", retained: both,
			shape:   map[string][]string{`terraform_data.tenant_bucket["beta"]`: {"delete"}, `terraform_data.tenant_bucket["alpha"]`: {"no-op"}},
			refused: []string{`terraform_data.tenant_bucket["beta"]`}},
		// The retained set as `tofu state list` gives it: keyed instances.
		{name: "refuse-tenant-removed-keyed", fixture: "tenant-removed",
			retained: []Retained{{Instance: "demo-state", Addresses: []string{`terraform_data.tenant_bucket["alpha"]`, `terraform_data.tenant_bucket["beta"]`}}},
			shape:    map[string][]string{`terraform_data.tenant_bucket["beta"]`: {"delete"}},
			refused:  []string{`terraform_data.tenant_bucket["beta"]`}},
		{name: "refuse-project-replaced", fixture: "project-replaced", retained: both,
			shape:   map[string][]string{"terraform_data.project": {"delete", "create"}},
			refused: []string{"terraform_data.project"}},
		{name: "refuse-forget", fixture: "forget", retained: both,
			shape:   map[string][]string{"terraform_data.state_bucket": {"forget"}},
			refused: []string{"terraform_data.state_bucket"}},
		{name: "refuse-module-removed", fixture: "module-removed", retained: both,
			shape:   map[string][]string{"module.governance.terraform_data.policy": {"delete"}},
			refused: []string{"module.governance.terraform_data.policy"}},
		{name: "refuse-module-removed-exact", fixture: "module-removed",
			retained: []Retained{{Instance: "account-governance", Addresses: []string{"module.governance.terraform_data.policy"}}},
			refused:  []string{"module.governance.terraform_data.policy"}},
		// for_each module instance: `module.region` covers `module.region["ca"].…`.
		{name: "refuse-region-removed", fixture: "region-removed", retained: both,
			shape:   map[string][]string{`module.region["ca"].terraform_data.policy`: {"delete"}, `module.region["eu"].terraform_data.policy`: {"no-op"}},
			refused: []string{`module.region["ca"].terraform_data.policy`}},
		// A keyed module instance as the retained entry covers the resources under it.
		{name: "refuse-region-removed-keyed-module", fixture: "region-removed",
			retained: []Retained{{Instance: "account-bootstrap", Addresses: []string{`module.region["ca"]`}}},
			refused:  []string{`module.region["ca"].terraform_data.policy`}},
		// Module-level prefix siblings are not covered: `module.gov` is not `module.governance`,
		// `module.region["c"]` is not `module.region["ca"]`.
		{name: "admit-module-prefix-sibling", fixture: "module-removed",
			retained: []Retained{{Instance: "account-governance", Addresses: []string{"module.gov"}}},
			shape:    map[string][]string{"module.governance.terraform_data.policy": {"delete"}}},
		{name: "admit-module-key-sibling", fixture: "region-removed",
			retained: []Retained{{Instance: "account-bootstrap", Addresses: []string{`module.region["c"]`}}}},
		// `tofu plan -replace=…`: the actions are a replacement, the reason is the request.
		{name: "refuse-replace-requested", fixture: "replace-requested", retained: both,
			shape:   map[string][]string{"terraform_data.state_bucket": {"delete", "create"}},
			refused: []string{"terraform_data.state_bucket"}},
		{name: "refuse-two-replacements", fixture: "two-replacements", retained: both,
			shape:   map[string][]string{"terraform_data.state_bucket": {"delete", "create"}, "terraform_data.project": {"delete", "create"}},
			refused: []string{"terraform_data.state_bucket", "terraform_data.project"}},
		{name: "refuse-truncated", fixture: "truncated", retained: both, refused: []string{}},
		{name: "refuse-truncated-nothing-retained", fixture: "truncated", retained: nil, refused: []string{}},
		{name: "refuse-unparseable-empty", raw: "", retained: both, refused: []string{}},
		{name: "refuse-unparseable-text", raw: "Error: Failed to read plan file", retained: both, refused: []string{}},
		{name: "refuse-unparseable-null", raw: "null", retained: both, refused: []string{}},
		{name: "refuse-unparseable-array", raw: "[]", retained: both, refused: []string{}},
		{name: "refuse-unparseable-no-format-version", raw: "{}", retained: both, refused: []string{}},
		// Valid JSON with a version, but not a plan's shape: a decoder that drops the type error admits it.
		{name: "refuse-unparseable-wrong-type", raw: `{"format_version":"1.2","resource_changes":"none"}`, retained: both, refused: []string{}},
		// A plan OpenTofu marks as errored is incomplete: what it would delete is unknown.
		{name: "refuse-errored", raw: `{"format_version":"1.2","errored":true,"resource_changes":[]}`, retained: both, refused: []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := []byte(c.raw)
			if c.fixture != "" {
				plan = readPlan(t, c.fixture)
				if c.shape != nil {
					got := planActions(t, plan)
					for addr, want := range c.shape {
						if !slices.Equal(got[addr], want) {
							t.Fatalf("fixture %s: %s has actions %v, want %v (recapture changed the shape)", c.fixture, addr, got[addr], want)
						}
					}
				}
			}
			err := Protect(plan, c.retained)
			if c.refused == nil {
				if err != nil {
					t.Errorf("admitted plan refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("plan admitted, want a refusal naming %v", c.refused)
			}
			var r *Refusal
			if !errors.As(err, &r) || r.Condition != CondRetained || ExitCode(err) != RefusalExit {
				t.Errorf("error %v (exit %d), want a %q refusal with exit %d", err, ExitCode(err), CondRetained, RefusalExit)
			}
			msg := err.Error()
			for _, a := range c.refused {
				if !strings.Contains(msg, a) {
					t.Errorf("refusal %q does not name %s", msg, a)
				}
			}
			for _, v := range planValues {
				if strings.Contains(msg, v) {
					t.Errorf("refusal %q carries the plan value %q", msg, v)
				}
			}
		})
	}
}

// TestProtectNamesInstance: the refusal names the retained instance the address belongs to.
func TestProtectNamesInstance(t *testing.T) {
	err := Protect(readPlan(t, "project-replaced"), []Retained{bootstrapRetained, projectRetained})
	if err == nil || !strings.Contains(err.Error(), "demo-dev-project") || strings.Contains(err.Error(), "account-bootstrap") {
		t.Errorf("refusal %v, want it to name instance demo-dev-project (and not account-bootstrap)", err)
	}
	err = Protect(readPlan(t, "org-change"), []Retained{projectRetained, bootstrapRetained})
	if err == nil || !strings.Contains(err.Error(), "account-bootstrap") || strings.Contains(err.Error(), "demo-dev-project") {
		t.Errorf("refusal %v, want it to name instance account-bootstrap (and not demo-dev-project)", err)
	}
}

// TestProtectFixtures: every plan was captured by the pinned OpenTofu and is unedited since (the
// sidecar's digest), and truncated.json is a cut of org-change.json. The plan a sidecar vouches
// for is named by the sidecar's own file name; its `case` field must agree.
func TestProtectFixtures(t *testing.T) {
	metas, err := filepath.Glob(filepath.Join(protectDir, "*.meta.json"))
	if err != nil || len(metas) != 15 {
		t.Fatalf("%d meta sidecars (%v), want 15 (14 captured, 1 derived)", len(metas), err)
	}
	all, _ := filepath.Glob(filepath.Join(protectDir, "*.json"))
	if len(all) != 2*len(metas) {
		t.Errorf("%d json files for %d sidecars: a plan without a sidecar", len(all), len(metas))
	}
	for _, m := range metas {
		name := strings.TrimSuffix(filepath.Base(m), ".meta.json")
		raw, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		var meta struct {
			Case        string `json:"case"`
			Tool        string `json:"tool"`
			ToolVersion string `json:"tool_version"`
			DerivedFrom string `json:"derived_from"`
			SHA         string `json:"json_sha256"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		if meta.Case != name {
			t.Errorf("%s: case %q differs from the file name", m, meta.Case)
		}
		plan := readPlan(t, name)
		sum := sha256.Sum256(plan)
		if hex.EncodeToString(sum[:]) != meta.SHA {
			t.Errorf("%s.json digest differs from its sidecar: edited after capture", meta.Case)
		}
		if meta.DerivedFrom != "" {
			if src := readPlan(t, strings.TrimSuffix(meta.DerivedFrom, ".json")); !strings.HasPrefix(string(src), string(plan)) || len(plan) >= len(src) {
				t.Errorf("%s.json is not a cut of %s", meta.Case, meta.DerivedFrom)
			}
			continue
		}
		var p struct {
			Version string `json:"terraform_version"`
		}
		if meta.Tool != "tofu" || meta.ToolVersion != pinnedTofu || json.Unmarshal(plan, &p) != nil || p.Version != pinnedTofu {
			t.Errorf("%s: tool %q %q, plan version %q, want tofu %s", meta.Case, meta.Tool, meta.ToolVersion, p.Version, pinnedTofu)
		}
	}
}
