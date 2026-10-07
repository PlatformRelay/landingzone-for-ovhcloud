package stacks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Producer→consumer pins of 005 T022 and T024 (T018 review gap): a stage's real plan, made by its
// own unit tests on the pinned OpenTofu (mocked provider), goes through BuildEnvelope and the result
// validates under schemas/outputs/<stage>.schema.json with exactly the schema's published values and
// no sensitive value. For account-governance the same plan also pins guard G5 on the planned
// resources themselves (T023 decision request 1): the component's tests can only read outputs, and
// `override_resource` cannot set configured attributes, so a component publishing the allowlist
// while planning something wider would pass them.
//
// Fixture: tests/fixtures/outputs/captures/<stage>-plan.json is one `tofu test -json -verbose`
// test_plan message (run `published_outputs_match_the_schema`), captured through the offline entry
// by tests/fixtures/outputs/capture-stage-plan.sh; its .meta.json sidecar vouches for the tool and
// for the configuration it was made from. The plan's root `output_changes` are read as the
// `tofu output -json` entries an apply would print: `after` is the value, `after_sensitive` the
// marker (anything but false counts as sensitive), and an unknown value is refused.

const repoRoot = "../../.."

// stagePlans are the directories each captured stage plan is made from; test configuration counts
// only for the stage itself (its mocks give the values). Keep in step with capture-stage-plan.sh.
var stagePlans = map[string][]string{
	"tenant-state":       {"stages/tenant-state", "components/state-backend", "modules/naming", "modules/object-storage-protected", "modules/object-storage-user"},
	"account-governance": {"stages/account-governance", "components/identity/ovh-native", "modules/naming", "modules/iam-service-account", "modules/iam-policy", "modules/identity-group"},
}

// stagePlanInputsDigest is the sha256 of `<sha256>  <path>` lines, in byte order of path, over
// every file of the stage's plan inputs a plan can read: configuration, data files such as
// naming's kinds.yaml and the dependency lock files; not Markdown, state files or hidden
// directories (sha256sum's format, so the capture script computes the same digest).
func stagePlanInputsDigest(t *testing.T, stage string) string {
	t.Helper()
	var paths []string
	for _, dir := range stagePlans[stage] {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repoRoot, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			switch {
			case d.IsDir() && strings.HasPrefix(d.Name(), "."):
				return fs.SkipDir
			case d.IsDir(), strings.HasSuffix(rel, ".md"), strings.Contains(d.Name(), ".tfstate"):
				return nil
			case strings.Contains(rel, "/tests/") && !strings.HasPrefix(rel, "stages/"+stage+"/"):
				return nil
			}
			paths = append(paths, rel)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(paths)
	var lines strings.Builder
	for _, p := range paths {
		lines.WriteString(digest(readFile(t, filepath.Join(repoRoot, p))) + "  " + p + "\n")
	}
	sum := sha256.Sum256([]byte(lines.String()))
	return hex.EncodeToString(sum[:])
}

// stagePlan returns the stage's captured plan after checking that its sidecar vouches for exactly
// this file, made from the current configuration by the pinned OpenTofu through the isolated entry.
func stagePlan(t *testing.T, stage string) []byte {
	t.Helper()
	file := stage + "-plan.json"
	data := readFile(t, fixtureDir+"/captures/"+file)
	var meta struct {
		File         string `json:"file"`
		Command      string `json:"command"`
		Toolchain    string `json:"toolchain"`
		Tool         string `json:"tool"`
		ToolVersion  string `json:"tool_version"`
		EntryExit    int    `json:"entry_exit"`
		SHA256       string `json:"sha256"`
		InputsSHA256 string `json:"inputs_sha256"`
	}
	if err := json.Unmarshal(readFile(t, fixtureDir+"/captures/"+file+".meta.json"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.File != file || meta.SHA256 != digest(data) || meta.EntryExit != 0 ||
		!strings.Contains(meta.Command, "capture:"+stage+"-plan") || meta.Tool != "tofu" || meta.ToolVersion != "1.13.0" ||
		!strings.Contains(meta.Toolchain, " tofu=1.13.0 ") || !strings.HasSuffix(meta.Toolchain, " network=none") {
		t.Fatalf("capture sidecar does not vouch for %s from the pinned tofu: %+v", file, meta)
	}
	if meta.InputsSHA256 != stagePlanInputsDigest(t, stage) {
		t.Fatalf("capture is stale: the %s configuration changed since it was captured; rerun tests/fixtures/outputs/capture-stage-plan.sh <entry> <checkout> %s on a clean tree", stage, stage)
	}
	return data
}

// testPlan is the part of one test_plan message the pins read.
type testPlan struct {
	Type string `json:"type"`
	Run  string `json:"@testrun"`
	Plan struct {
		Errored bool `json:"errored"`
		Outputs map[string]struct {
			After          json.RawMessage `json:"after"`
			AfterSensitive json.RawMessage `json:"after_sensitive"`
			AfterUnknown   json.RawMessage `json:"after_unknown"`
		} `json:"output_changes"`
		Resources []plannedResource `json:"resource_changes"`
	} `json:"test_plan"`
}

// plannedResource is one entry of the plan's resource_changes.
type plannedResource struct {
	Address string `json:"address"`
	Mode    string `json:"mode"`
	Type    string `json:"type"`
	Change  struct {
		Actions []string       `json:"actions"`
		After   map[string]any `json:"after"`
	} `json:"change"`
}

// decodePlan decodes one captured test_plan message of run published_outputs_match_the_schema.
func decodePlan(t *testing.T, data []byte) testPlan {
	t.Helper()
	var msg testPlan
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "test_plan" || msg.Run != "published_outputs_match_the_schema" || msg.Plan.Errored || len(msg.Plan.Outputs) == 0 {
		t.Fatalf("capture is not the plan of run published_outputs_match_the_schema: type %q run %q errored %v outputs %d", msg.Type, msg.Run, msg.Plan.Errored, len(msg.Plan.Outputs))
	}
	return msg
}

// planOutputs turns the root output changes of one test_plan message into a `tofu output -json`
// document, and returns the string leaves of the sensitive entries and of the named credential
// outputs (data-model *Sensitive outputs*: their values are collected whatever marker the plan
// gives them, so a lost marker shows as a leak).
func planOutputs(t *testing.T, msg testPlan, credentialOutputs ...string) (doc []byte, secrets []string) {
	t.Helper()
	type entry struct {
		Sensitive bool            `json:"sensitive"`
		Value     json.RawMessage `json:"value"`
	}
	entries := map[string]entry{}
	for name, o := range msg.Plan.Outputs {
		if string(o.AfterUnknown) != "false" {
			t.Fatalf("output %s is not known in the plan (after_unknown %s)", name, o.AfterUnknown)
		}
		sensitive := string(o.AfterSensitive) != "false"
		if sensitive || slices.Contains(credentialOutputs, name) {
			var v any
			if err := json.Unmarshal(o.After, &v); err != nil {
				t.Fatal(err)
			}
			secrets = append(secrets, leaves(v)...)
		}
		entries[name] = entry{Sensitive: sensitive, Value: o.After}
	}
	doc, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return doc, secrets
}

// leaves lists the non-empty string leaves of a decoded JSON value.
func leaves(v any) []string {
	switch x := v.(type) {
	case string:
		if x != "" {
			return []string{x}
		}
	case map[string]any:
		var out []string
		for _, e := range x {
			out = append(out, leaves(e)...)
		}
		return out
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, leaves(e)...)
		}
		return out
	}
	return nil
}

// publishedEnvelope builds the stage's envelope from the plan's outputs, validates it under the
// stage's schema, and checks it publishes exactly the schema's values and none of the secrets.
func publishedEnvelope(t *testing.T, stage, instance string, doc []byte, secrets, want []string) {
	t.Helper()
	env, err := BuildEnvelope(doc, Producer{InstanceID: instance, Stage: stage, SourceRevision: testRev})
	if err != nil {
		t.Fatalf("builder refused the stage's outputs: %v", err)
	}
	published, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ValidateEnvelope(schemas(), published, expectation(stage, instance))
	if err != nil {
		t.Fatalf("envelope built from the stage's plan does not validate: %v", err)
	}
	if names := slices.Sorted(maps.Keys(got.Values)); !reflect.DeepEqual(names, want) {
		t.Errorf("published values %v, want exactly the schema's %v", names, want)
	}
	for _, s := range secrets {
		if strings.Contains(string(published), s) {
			t.Errorf("envelope carries a value of a sensitive output")
		}
	}
}

func TestOutputsTenantStateStagePlan(t *testing.T) {
	doc, secrets := planOutputs(t, decodePlan(t, stagePlan(t, "tenant-state")), "tenant_s3", "platform_s3")
	// The two credentials, two fields each: the control that the secrets scan below can fail.
	if len(secrets) != 4 {
		t.Fatalf("want the four credential strings of tenant_s3 and platform_s3, got %d", len(secrets))
	}
	publishedEnvelope(t, "tenant-state", "demo-state", doc, secrets,
		[]string{"platform_s3_user_id", "state_bucket", "tenant", "tenant_s3_user_id", "unlabelled"})
}

// p9Allowlist is the tenant deployer's allowlist, research R6 (P9): exactly these actions.
var p9Allowlist = []string{
	"publicCloudProject:apiovh:network/private/create",
	"publicCloudProject:apiovh:network/private/delete",
	"publicCloudProject:apiovh:network/private/edit",
	"publicCloudProject:apiovh:network/private/get",
	"publicCloudProject:apiovh:network/private/region/create",
	"publicCloudProject:apiovh:network/private/subnet/create",
	"publicCloudProject:apiovh:network/private/subnet/delete",
	"publicCloudProject:apiovh:network/private/subnet/get",
	"publicCloudProject:apiovh:region/storage/bulkDeleteObjects",
	"publicCloudProject:apiovh:region/storage/create",
	"publicCloudProject:apiovh:region/storage/delete",
	"publicCloudProject:apiovh:region/storage/edit",
	"publicCloudProject:apiovh:region/storage/get",
}

// strs returns a planned list or set attribute as sorted strings; anything else fails the test.
func strs(t *testing.T, r plannedResource, attr string) []string {
	t.Helper()
	raw, ok := r.Change.After[attr].([]any)
	if !ok {
		t.Fatalf("%s: %s is not a planned list (%T)", r.Address, attr, r.Change.After[attr])
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s: %s holds a non-string %v", r.Address, attr, v)
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// empty reports whether a planned optional attribute is absent, null or an empty list.
func empty(v any) bool {
	l, isList := v.([]any)
	return v == nil || isList && len(l) == 0
}

// The fixture of run published_outputs_match_the_schema (stages/account-governance/tests): two
// tenants, `alpha` and `demo`, their project ids, and the identities the run's overrides give the
// OAuth2 clients.
const (
	agModule        = "module.identity."
	demoProjectURN  = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
	alphaProjectURN = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
)

func TestOutputsAccountGovernanceStagePlan(t *testing.T) {
	msg := decodePlan(t, stagePlan(t, "account-governance"))
	doc, secrets := planOutputs(t, msg, "platform_deployer_secret", "tenant_deployer_secrets")
	// The platform secret and one per tenant: the control that the secrets scan can fail.
	if len(secrets) != 3 {
		t.Fatalf("want the three deployer secrets (platform, alpha, demo), got %d", len(secrets))
	}
	publishedEnvelope(t, "account-governance", "account-governance", doc, secrets,
		[]string{"platform_deployer", "tenants", "unlabelled"})

	// Inventory: one platform client and policy, and per tenant one client, policy and group
	// (1+T, 1+T, T for T = 2), all created, no other managed resource. That also refuses any group
	// member, S3 user or group policy (decision request 4: none in this slice).
	want := map[string]string{
		"module.platform_deployer.ovh_me_api_oauth2_client.this":        "lz-sa-platform-deployer",
		"module.platform_policy.ovh_iam_policy.this":                    "lz-pol-platform-deployer",
		`module.tenant_deployer["alpha"].ovh_me_api_oauth2_client.this`: "lz-alpha-sa-deployer",
		`module.tenant_deployer["demo"].ovh_me_api_oauth2_client.this`:  "lz-demo-sa-deployer",
		`module.tenant_policy["alpha"].ovh_iam_policy.this`:             "lz-alpha-pol-deployer",
		`module.tenant_policy["demo"].ovh_iam_policy.this`:              "lz-demo-pol-deployer",
		`module.tenant_group["alpha"].ovh_me_identity_group.this`:       "lz-alpha-grp-tenant",
		`module.tenant_group["demo"].ovh_me_identity_group.this`:        "lz-demo-grp-tenant",
	}
	byAddress := map[string]plannedResource{}
	perType := map[string]int{}
	for _, r := range msg.Plan.Resources {
		if r.Mode != "managed" {
			continue
		}
		perType[r.Type]++
		byAddress[strings.TrimPrefix(r.Address, agModule)] = r
		if !strings.HasPrefix(r.Address, agModule) {
			t.Errorf("planned resource %s is outside the stage's one component call", r.Address)
		}
		if !reflect.DeepEqual(r.Change.Actions, []string{"create"}) {
			t.Errorf("%s: actions %v, want [create]", r.Address, r.Change.Actions)
		}
	}
	if wantTypes := map[string]int{"ovh_me_api_oauth2_client": 3, "ovh_iam_policy": 3, "ovh_me_identity_group": 2}; !reflect.DeepEqual(perType, wantTypes) {
		t.Errorf("planned resources per type %v, want exactly %v", perType, wantTypes)
	}
	if got := slices.Sorted(maps.Keys(byAddress)); !reflect.DeepEqual(got, slices.Sorted(maps.Keys(want))) {
		t.Fatalf("planned addresses %v, want %v", got, slices.Sorted(maps.Keys(want)))
	}
	for addr, name := range want {
		if got := byAddress[addr].Change.After["name"]; got != name {
			t.Errorf("%s: name %v, want %q", addr, got, name)
		}
	}

	identity := func(addr string) string {
		t.Helper()
		id, ok := byAddress[addr].Change.After["identity"].(string)
		if !ok || id == "" {
			t.Fatalf("%s: no planned identity", addr)
		}
		return id
	}
	for _, addr := range []string{"module.platform_deployer.ovh_me_api_oauth2_client.this", `module.tenant_deployer["alpha"].ovh_me_api_oauth2_client.this`, `module.tenant_deployer["demo"].ovh_me_api_oauth2_client.this`} {
		if flow := byAddress[addr].Change.After["flow"]; flow != "CLIENT_CREDENTIALS" {
			t.Errorf("%s: flow %v, want CLIENT_CREDENTIALS (research R6)", addr, flow)
		}
	}

	// G5 on every planned policy: no permissions group, except, deny or expiry beside the actions,
	// and no IAM, `me`, storage-policy or wildcard action outside the platform's one grant.
	for addr, r := range byAddress {
		if r.Type != "ovh_iam_policy" {
			continue
		}
		for _, attr := range []string{"permissions_groups", "except", "deny", "expired_at"} {
			if !empty(r.Change.After[attr]) {
				t.Errorf("%s: %s %v, want none", addr, attr, r.Change.After[attr])
			}
		}
		if conditions := r.Change.After["conditions"]; !empty(conditions) {
			t.Errorf("%s: conditions %v, want none", addr, conditions)
		}
		for _, a := range strs(t, r, "allow") {
			lower := strings.ToLower(a)
			if strings.HasPrefix(lower, "account:") || strings.Contains(lower, "/storage/policy") ||
				strings.Contains(a, "*") && addr != "module.platform_policy.ovh_iam_policy.this" {
				t.Errorf("%s: action %q is outside every deployer grant (G5)", addr, a)
			}
		}
	}

	// Tenant deployer policies: exactly the P9 allowlist, on the tenant's own project URN only,
	// for the tenant's own deployer only.
	for tenant, urn := range map[string]string{"alpha": alphaProjectURN, "demo": demoProjectURN} {
		p := byAddress[`module.tenant_policy["`+tenant+`"].ovh_iam_policy.this`]
		if got := strs(t, p, "allow"); !reflect.DeepEqual(got, p9Allowlist) {
			t.Errorf("tenant %s policy allows %v, want exactly the P9 allowlist %v", tenant, got, p9Allowlist)
		}
		if got := strs(t, p, "resources"); !reflect.DeepEqual(got, []string{urn}) {
			t.Errorf("tenant %s policy resources %v, want only its project %s", tenant, got, urn)
		}
		deployer := identity(`module.tenant_deployer["` + tenant + `"].ovh_me_api_oauth2_client.this`)
		if got := strs(t, p, "identities"); !reflect.DeepEqual(got, []string{deployer}) {
			t.Errorf("tenant %s policy identities %v, want only its deployer %s", tenant, got, deployer)
		}
	}

	// Platform deployer policy: publicCloudProject:apiovh:* on exactly the tenant projects, for
	// the platform deployer only.
	platform := byAddress["module.platform_policy.ovh_iam_policy.this"]
	if got := strs(t, platform, "allow"); !reflect.DeepEqual(got, []string{"publicCloudProject:apiovh:*"}) {
		t.Errorf("platform policy allows %v, want [publicCloudProject:apiovh:*]", got)
	}
	if got := strs(t, platform, "resources"); !reflect.DeepEqual(got, []string{demoProjectURN, alphaProjectURN}) {
		t.Errorf("platform policy resources %v, want exactly the tenant projects", got)
	}
	if got, want := strs(t, platform, "identities"), []string{identity("module.platform_deployer.ovh_me_api_oauth2_client.this")}; !reflect.DeepEqual(got, want) {
		t.Errorf("platform policy identities %v, want only the platform deployer %v", got, want)
	}

	// Tenant groups: role NONE, as planned.
	for _, tenant := range []string{"alpha", "demo"} {
		if role := byAddress[`module.tenant_group["`+tenant+`"].ovh_me_identity_group.this`].Change.After["role"]; role != "NONE" {
			t.Errorf("tenant %s group role %v, want NONE", tenant, role)
		}
	}
}
