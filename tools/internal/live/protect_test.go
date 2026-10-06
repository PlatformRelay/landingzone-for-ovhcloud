package live

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Retained-resource guard G7, plan part (FR-004, FR-011, research R12). The plans in
// tests/fixtures/tofu-probes/protect/ are `tofu show -json` of saved plans, captured by its
// capture.sh through the offline entry's capture admission (`task capture:protect-<case>`, which
// runs roots.sh) with the pinned OpenTofu on provider-free roots: terraform_data stands in for the
// state bucket, the platform S3 user (create_before_destroy), the tenant buckets (for_each over
// the tenants), the adopted project, a governance module and one non-retained resource
// (project_scratch). truncated.json is org-change.json cut in half. The moved-* plans (T064) move
// the state bucket with a `moved` block: alone, with a replacement, and to an address with no block;
// state-not-plan.json is `tofu show -json` without a plan file (the state).

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
		{name: "refuse-unparseable-wrong-type", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":"none"}`, retained: both, refused: []string{}},
		// A plan OpenTofu marks as errored is incomplete: what it would delete is unknown.
		{name: "refuse-errored", raw: `{"format_version":"1.2","planned_values":{},"errored":true,"resource_changes":[]}`, retained: both, refused: []string{}},
		// Fail closed (T064): an action the guard does not know as harmless is refused on a covered
		// address, admitted on any other. Synthetic shape: no OpenTofu 1.13 plan emits such an action.
		{name: "refuse-unknown-action", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"terraform_data.state_bucket","change":{"actions":["purge"]}}]}`,
			retained: both, refused: []string{"terraform_data.state_bucket"}},
		{name: "admit-unknown-action-unretained", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"terraform_data.project_scratch","change":{"actions":["purge"]}}]}`,
			retained: both},
		// An entry with no actions on a covered address is not a shape the guard can judge. Synthetic.
		{name: "refuse-empty-actions", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"terraform_data.state_bucket","change":{"actions":[]}}]}`,
			retained: both, refused: []string{"terraform_data.state_bucket"}},
		{name: "refuse-no-change", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"terraform_data.state_bucket"}]}`,
			retained: both, refused: []string{"terraform_data.state_bucket"}},
		{name: "admit-empty-actions-unretained", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"terraform_data.project_scratch","change":{"actions":[]}}]}`,
			retained: both},
		// A data source read under a retained module (OpenTofu plans `read` for deferred data reads). Synthetic.
		{name: "admit-read-under-retained-module", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"module.governance.data.terraform_data.x","change":{"actions":["read"]}}]}`,
			retained: both},
		// T064 review: a `moved` block takes the retained address away in the same plan. The change's
		// address is the new one, `previous_address` the retained one.
		{name: "refuse-moved-replace", fixture: "moved-replace", retained: both,
			shape:   map[string][]string{"terraform_data.bucket": {"delete", "create"}},
			refused: []string{"terraform_data.bucket", "terraform_data.state_bucket"}},
		{name: "refuse-moved-out", fixture: "moved-out", retained: both,
			shape:   map[string][]string{"terraform_data.other": {"delete"}},
			refused: []string{"terraform_data.other", "terraform_data.state_bucket"}},
		// T064 review r2: a move alone out of the retained set is refused, or the next plan could
		// delete the resource under an address no entry covers; a move within the set is admitted.
		{name: "refuse-moved-out-of-set", fixture: "moved-only", retained: both,
			shape:   map[string][]string{"terraform_data.bucket": {"no-op"}},
			refused: []string{"terraform_data.bucket", "terraform_data.state_bucket"}},
		{name: "admit-moved-within-set", fixture: "moved-only",
			retained: []Retained{{Instance: "account-bootstrap", Addresses: []string{"terraform_data.state_bucket", "terraform_data.bucket"}}}},
		// T064 review r2: `tofu show -json` without the plan file prints the state; it is not a plan.
		{name: "refuse-state-not-plan", fixture: "state-not-plan", retained: both, refused: []string{}},
		{name: "refuse-unparseable-no-planned-values", raw: `{"format_version":"1.2","resource_changes":[]}`, retained: both, refused: []string{}},
		{name: "refuse-unparseable-null-planned-values", raw: `{"format_version":"1.2","planned_values":null,"resource_changes":[]}`, retained: both, refused: []string{}},
		{name: "refuse-unparseable-planned-values-only", raw: `{"planned_values":{},"resource_changes":[]}`, retained: both, refused: []string{}},
		{name: "admit-minimal-plan", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[]}`, retained: both},
		// T055 (T064 gap): a deposed object of a covered address (left by a failed
		// create_before_destroy replacement) is refused whatever its actions; on any other address
		// it is judged as usual. Synthetic: no capture of a deposed object yet.
		{name: "refuse-deposed-delete", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"terraform_data.state_bucket","deposed":"0a1b2c3d","change":{"actions":["delete"]}}]}`,
			retained: both, refused: []string{"terraform_data.state_bucket", "deposed"}},
		{name: "refuse-deposed-no-op", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"terraform_data.state_bucket","deposed":"0a1b2c3d","change":{"actions":["no-op"]}}]}`,
			retained: both, refused: []string{"terraform_data.state_bucket", "deposed"}},
		{name: "admit-deposed-unretained", raw: `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"terraform_data.project_scratch","deposed":"0a1b2c3d","change":{"actions":["delete"]}}]}`,
			retained: both},
		// T055 (T064 gap): only the JSON format major version 1 (the captured 1.2) is read; another
		// major version may move fields the guard relies on. Synthetic.
		{name: "refuse-format-version-2", raw: `{"format_version":"2.0","planned_values":{},"resource_changes":[]}`, retained: both, refused: []string{}},
		{name: "refuse-format-version-10", raw: `{"format_version":"10.1","planned_values":{},"resource_changes":[]}`, retained: both, refused: []string{}},
		{name: "refuse-format-version-bare", raw: `{"format_version":"1","planned_values":{},"resource_changes":[]}`, retained: both, refused: []string{}},
		{name: "admit-format-version-1-minor", raw: `{"format_version":"1.0","planned_values":{},"resource_changes":[]}`, retained: both},
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

// protectInput is the inner capture script every captured plan's sidecar names as its input: it
// holds the case table and the provider-free root, so its digest binds what was captured.
const protectInput = "tests/fixtures/tofu-probes/protect/roots.sh"

// protectSidecarFaults judges one sidecar of tests/fixtures/tofu-probes/protect/ (T070): the
// fixture <name>.json was produced by `task capture:protect-<name>` through the offline entry's
// capture admission with the pinned OpenTofu, and is unedited since. A captured plan's sidecar
// must name the entry command for its own target, entry exit 0, the entry's qualification line
// (pinned tofu, network none), the fixture's file and digest, and the input script and its
// digest; Taskfile.yml must define the target. A derived fixture (truncated.json) names the
// captured plan it is cut from and is no plan itself. A sidecar of a host capture (T063's shape:
// a bare `tofu …` command, no entry, no toolchain line) has faults.
func protectSidecarFaults(name string, raw, plan, src []byte, taskfile, inputSHA string) []string {
	var meta struct {
		Case        string `json:"case"`
		File        string `json:"file"`
		Command     string `json:"command"`
		Toolchain   string `json:"toolchain"`
		Tool        string `json:"tool"`
		ToolVersion string `json:"tool_version"`
		EntryExit   *int   `json:"entry_exit"`
		SHA         string `json:"sha256"`
		Input       string `json:"input"`
		InputSHA    string `json:"input_sha256"`
		Inner       string `json:"inner_command"`
		DerivedFrom string `json:"derived_from"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return []string{"sidecar does not parse: " + err.Error()}
	}
	var faults []string
	if meta.Case != name || meta.File != name+".json" {
		faults = append(faults, fmt.Sprintf("case %q, file %q: the sidecar names another fixture", meta.Case, meta.File))
	}
	sum := sha256.Sum256(plan)
	if hex.EncodeToString(sum[:]) != meta.SHA {
		faults = append(faults, "digest differs from the fixture: edited after capture")
	}
	if meta.DerivedFrom != "" {
		if meta.Command != "" || !strings.HasPrefix(string(src), string(plan)) || len(plan) >= len(src) || json.Valid(plan) {
			faults = append(faults, fmt.Sprintf("not a cut of %s (derived fixtures are no plan and no capture)", meta.DerivedFrom))
		}
		return faults
	}
	if want := "lz-offline --candidate <checkout> -- task capture:protect-" + name; meta.Command != want {
		faults = append(faults, fmt.Sprintf("command %q, want %q: not captured through the offline entry", meta.Command, want))
	}
	if !strings.Contains(taskfile, "\n  capture:protect-"+name+":\n    cmds: [{task: capture-protect, vars: {CASE: "+name+"}}]\n") {
		faults = append(faults, "Taskfile.yml defines no target capture:protect-"+name+" running case "+name)
	}
	if meta.Inner != "sh roots.sh /tcb/tofu "+name {
		faults = append(faults, fmt.Sprintf("inner command %q does not run case %s with the entry's tofu", meta.Inner, name))
	}
	if meta.EntryExit == nil || *meta.EntryExit != 0 {
		faults = append(faults, "entry exit missing or not 0")
	}
	if !strings.HasPrefix(meta.Toolchain, "TOOLCHAIN_QUALIFIED ") || !strings.Contains(meta.Toolchain, " tofu="+pinnedTofu+" ") || !strings.HasSuffix(meta.Toolchain, " network=none") {
		faults = append(faults, fmt.Sprintf("toolchain %q, want the entry's qualification line with tofu %s and network=none", meta.Toolchain, pinnedTofu))
	}
	var p struct {
		Version string `json:"terraform_version"`
	}
	if meta.Tool != "tofu" || meta.ToolVersion != pinnedTofu || json.Unmarshal(plan, &p) != nil || p.Version != pinnedTofu {
		faults = append(faults, fmt.Sprintf("tool %q %q, plan version %q, want tofu %s", meta.Tool, meta.ToolVersion, p.Version, pinnedTofu))
	}
	if meta.Input != protectInput || meta.InputSHA != inputSHA {
		faults = append(faults, fmt.Sprintf("input %q: not the committed %s", meta.Input, protectInput))
	}
	return faults
}

// protectFixtureInputs returns Taskfile.yml and the digest of the committed inner capture script.
func protectFixtureInputs(t *testing.T) (string, string) {
	t.Helper()
	taskfile, err := os.ReadFile(filepath.Join("..", "..", "..", "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "..", protectInput))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(script)
	return string(taskfile), hex.EncodeToString(sum[:])
}

// TestProtectFixtures: every plan was captured through the offline entry's capture admission by
// the pinned OpenTofu and is unedited since, and truncated.json is a cut of org-change.json. The
// plan a sidecar vouches for is named by the sidecar's own file name.
func TestProtectFixtures(t *testing.T) {
	metas, err := filepath.Glob(filepath.Join(protectDir, "*.meta.json"))
	if err != nil || len(metas) != 19 {
		t.Fatalf("%d meta sidecars (%v), want 19 (18 captured, 1 derived)", len(metas), err)
	}
	all, _ := filepath.Glob(filepath.Join(protectDir, "*.json"))
	if len(all) != 2*len(metas) {
		t.Errorf("%d json files for %d sidecars: a plan without a sidecar", len(all), len(metas))
	}
	taskfile, inputSHA := protectFixtureInputs(t)
	derived := 0
	for _, m := range metas {
		name := strings.TrimSuffix(filepath.Base(m), ".meta.json")
		raw, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		var d struct {
			DerivedFrom string `json:"derived_from"`
		}
		_ = json.Unmarshal(raw, &d)
		var src []byte
		if d.DerivedFrom != "" {
			derived++
			src = readPlan(t, strings.TrimSuffix(d.DerivedFrom, ".json"))
		}
		for _, f := range protectSidecarFaults(name, raw, readPlan(t, name), src, taskfile, inputSHA) {
			t.Errorf("%s: %s", name, f)
		}
	}
	if derived != 1 {
		t.Errorf("%d derived fixtures, want 1 (truncated.json)", derived)
	}
}

// TestProtectFixturesRefuseHostCapture: a sidecar that names a host capture, or that the entry
// did not qualify, is red; the committed create.meta.json is the positive control.
func TestProtectFixturesRefuseHostCapture(t *testing.T) {
	taskfile, inputSHA := protectFixtureInputs(t)
	plan := readPlan(t, "create")
	raw, err := os.ReadFile(filepath.Join(protectDir, "create.meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if f := protectSidecarFaults("create", raw, plan, nil, taskfile, inputSHA); len(f) != 0 {
		t.Fatalf("committed create sidecar has faults %v, want none", f)
	}
	sum := sha256.Sum256(plan)
	digest := hex.EncodeToString(sum[:])
	edit := func(change map[string]any, drop ...string) []byte {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		for k, v := range change {
			m[k] = v
		}
		for _, k := range drop {
			delete(m, k)
		}
		b, _ := json.Marshal(m)
		return b
	}
	cases := map[string][]byte{
		// The sidecar T063 wrote on the host, digest current.
		"t063-host-sidecar":     []byte(`{"case":"create","variant":"base","applied_base_first":"no","plan_args":"","command":"tofu plan -out=plan.bin; tofu show -json plan.bin","tool":"tofu","tool_version":"` + pinnedTofu + `","json_sha256":"` + digest + `","captured_on":"2026-10-06"}`),
		"host-command":          edit(map[string]any{"command": "tofu plan -out=plan.bin; tofu show -json plan.bin"}),
		"host-capture-script":   edit(map[string]any{"command": "tests/fixtures/tofu-probes/protect/capture.sh /usr/bin/tofu /tmp/x"}),
		"other-target":          edit(map[string]any{"command": "lz-offline --candidate <checkout> -- task capture:protect-noop"}),
		"no-toolchain":          edit(nil, "toolchain"),
		"toolchain-network":     edit(map[string]any{"toolchain": "TOOLCHAIN_QUALIFIED go=1.27.1 tofu=" + pinnedTofu + " terramate=0.17.3 task=3.53.1 tflint=0.64.0 provider=ovh/ovh@2.21.0 network=host"}),
		"toolchain-unqualified": edit(map[string]any{"toolchain": "go=1.27.1 tofu=" + pinnedTofu + " terramate=0.17.3 network=none"}),
		"toolchain-other-tofu":  edit(map[string]any{"toolchain": "TOOLCHAIN_QUALIFIED go=1.27.1 tofu=1.12.0 terramate=0.17.3 task=3.53.1 tflint=0.64.0 provider=ovh/ovh@2.21.0 network=none"}),
		"entry-failed":          edit(map[string]any{"entry_exit": 1}),
		"no-entry-exit":         edit(nil, "entry_exit"),
		"other-input":           edit(map[string]any{"input": "tests/fixtures/tofu-probes/protect/capture.sh"}),
		"stale-input":           edit(map[string]any{"input_sha256": strings.Repeat("0", 64)}),
		"other-file":            edit(map[string]any{"file": "noop.json"}),
		"stale-digest":          edit(map[string]any{"sha256": strings.Repeat("0", 64)}),
		"other-tool":            edit(map[string]any{"tool": "terraform"}),
		"other-tool-version":    edit(map[string]any{"tool_version": "1.12.0"}),
		"other-inner-case":      edit(map[string]any{"inner_command": "sh roots.sh /tcb/tofu noop"}),
		"host-inner-tofu":       edit(map[string]any{"inner_command": "sh roots.sh /usr/bin/tofu create"}),
	}
	for name, sidecar := range cases {
		if f := protectSidecarFaults("create", sidecar, plan, nil, taskfile, inputSHA); len(f) == 0 {
			t.Errorf("%s: sidecar admitted, want it red", name)
		}
	}
	if f := protectSidecarFaults("create", raw, plan, nil, strings.ReplaceAll(taskfile, "capture:protect-create:", "capture:protect-created:"), inputSHA); len(f) == 0 {
		t.Error("no-target: sidecar admitted without a capture:protect-create target, want it red")
	}
	swapped := strings.Replace(taskfile, "capture-protect, vars: {CASE: create}", "capture-protect, vars: {CASE: noop}", 1)
	if f := protectSidecarFaults("create", raw, plan, nil, swapped, inputSHA); len(f) == 0 {
		t.Error("target-runs-other-case: capture:protect-create running case noop admitted, want it red")
	}
	// A plan written by another OpenTofu, its sidecar digest current.
	other := []byte(strings.Replace(string(plan), `"terraform_version":"`+pinnedTofu+`"`, `"terraform_version":"1.12.0"`, 1))
	osum := sha256.Sum256(other)
	if f := protectSidecarFaults("create", edit(map[string]any{"sha256": hex.EncodeToString(osum[:])}), other, nil, taskfile, inputSHA); len(f) == 0 {
		t.Error("plan-version: a plan of tofu 1.12.0 admitted, want it red")
	}

	// Derived fixtures: the committed truncated sidecar is the positive control; a derived fixture
	// that is a whole plan, carries a capture command, or is no cut of its source is red. Each
	// negative differs from a passing derived sidecar in one clause only.
	cut, src := readPlan(t, "truncated"), readPlan(t, "org-change")
	tRaw, err := os.ReadFile(filepath.Join(protectDir, "truncated.meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if f := protectSidecarFaults("truncated", tRaw, cut, src, taskfile, inputSHA); len(f) != 0 {
		t.Fatalf("committed truncated sidecar has faults %v, want none", f)
	}
	whole := []byte(`{"case":"create","file":"create.json","derived_from":"padded.json","sha256":"` + digest + `"}`)
	if f := protectSidecarFaults("create", whole, plan, append(append([]byte{}, plan...), '\n'), taskfile, inputSHA); len(f) == 0 {
		t.Error("derived-whole-plan: a plan smuggled in as derived admitted, want it red")
	}
	var tm map[string]any
	if err := json.Unmarshal(tRaw, &tm); err != nil {
		t.Fatal(err)
	}
	tm["command"] = "lz-offline --candidate <checkout> -- task capture:protect-truncated"
	withCommand, _ := json.Marshal(tm)
	if f := protectSidecarFaults("truncated", withCommand, cut, src, taskfile, inputSHA); len(f) == 0 {
		t.Error("derived-with-command: derived sidecar naming a capture admitted, want it red")
	}
	if f := protectSidecarFaults("truncated", tRaw, cut, readPlan(t, "noop"), taskfile, inputSHA); len(f) == 0 {
		t.Error("derived-not-a-cut: derived fixture that is no cut of its source admitted, want it red")
	}
}

// TestProtectOnlyPlanReader: protect.go is the one reader of a plan's resource changes (T064
// row 9, coordinator decision for T055): no other non-test Go file of the tools module names
// `resource_changes`, so no verb can judge a plan with a second, weaker reading of its own. The
// scan must see protect.go itself, or it scans nothing.
func TestProtectOnlyPlanReader(t *testing.T) {
	root := filepath.Join("..", "..")
	own := filepath.Join(root, "internal", "live", "protect.go")
	sawOwn := false
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(p) != ".go" || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !strings.Contains(string(raw), "resource_changes") {
			return nil
		}
		if p == own {
			sawOwn = true
			return nil
		}
		t.Errorf("%s reads `resource_changes`: plans are judged by live.Protect (protect.go) only", p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawOwn {
		t.Errorf("the scan never saw %s reading `resource_changes`", own)
	}
}
