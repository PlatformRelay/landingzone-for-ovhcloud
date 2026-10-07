package stacks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"maps"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Producer→consumer pins of 005 T022 and T024 (T018 review gap; bootstrap added by T038): a stage's real plan, made by its
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

// stagePlans are the directories each captured case's plan is made from, by capture case (a stage,
// or a stage and one of its other runs, planCases); test configuration counts only for the stage
// itself (its mocks give the values). Keep in step with capture-stage-plan.sh.
var stagePlans = map[string][]string{
	"bootstrap":          {"stages/bootstrap", "components/state-backend", "modules/naming", "modules/object-storage-protected", "modules/object-storage-user"},
	"tenant-state":       {"stages/tenant-state", "components/state-backend", "modules/naming", "modules/object-storage-protected", "modules/object-storage-user"},
	"account-governance": {"stages/account-governance", "components/identity/ovh-native", "modules/naming", "modules/iam-service-account", "modules/iam-policy", "modules/identity-group"},
	"project":            {"stages/project", "components/project-factory", "modules/naming", "modules/cloud-project", "modules/cloud-quota"},
	"project-reference":  {"stages/project", "components/project-factory", "modules/naming", "modules/cloud-project", "modules/cloud-quota"},
	"project-network":    {"stages/project-network", "components/network/island", "modules/naming", "modules/private-network"},
	"runtime":            {"stages/runtime", "components/runtime/managed-only", "modules/naming", "modules/object-storage"},
	"runtime-slot":       {"stages/runtime", "components/runtime/managed-only", "modules/naming", "modules/object-storage"},
}

// planCases are the capture cases that are not a stage's default run: their stage and run.
var planCases = map[string]struct{ stage, run string }{
	"project-reference": {"project", "reference_published_outputs_with_both_toggles"},
	"runtime-slot":      {"runtime", "slot_blue_names_the_bucket"},
}

// planCase returns a capture case's stage and run.
func planCase(c string) (stage, run string) {
	if pc, ok := planCases[c]; ok {
		return pc.stage, pc.run
	}
	return c, "published_outputs_match_the_schema"
}

// stagePlanInputsDigest is the sha256 of `<sha256>  <path>` lines, in byte order of path, over
// every file of the case's plan inputs a plan can read: configuration, data files such as
// naming's kinds.yaml and the dependency lock files; not Markdown, state files or hidden
// directories (sha256sum's format, so the capture script computes the same digest).
func stagePlanInputsDigest(t *testing.T, planCaseName string) string {
	t.Helper()
	stage, _ := planCase(planCaseName)
	var paths []string
	for _, dir := range stagePlans[planCaseName] {
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

// stagePlan returns a capture case's plan after checking that its sidecar vouches for exactly
// this file, made from the current configuration by the pinned OpenTofu through the isolated entry.
func stagePlan(t *testing.T, stage string) []byte {
	t.Helper()
	data := stagePlanFile(t, stage)
	if digest := stagePlanInputsDigest(t, stage); stagePlanMeta(t, stage).InputsSHA256 != digest {
		t.Fatalf("capture is stale: the %s configuration changed since it was captured; rerun tests/fixtures/outputs/capture-stage-plan.sh <entry> <checkout> %s on a clean tree", stage, stage)
	}
	return data
}

// stagePlanMeta is the part of a capture's .meta.json sidecar the checks read.
type stagePlanMetaDoc struct {
	File         string `json:"file"`
	Command      string `json:"command"`
	Inner        string `json:"inner_command"`
	Toolchain    string `json:"toolchain"`
	Tool         string `json:"tool"`
	ToolVersion  string `json:"tool_version"`
	EntryExit    int    `json:"entry_exit"`
	SHA256       string `json:"sha256"`
	InputsSHA256 string `json:"inputs_sha256"`
}

// stagePlanMeta decodes a capture case's sidecar.
func stagePlanMeta(t *testing.T, stage string) stagePlanMetaDoc {
	t.Helper()
	var meta stagePlanMetaDoc
	if err := json.Unmarshal(readFile(t, fixtureDir+"/captures/"+stage+"-plan.json.meta.json"), &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

// stagePlanFile returns a capture case's plan after checking that its sidecar vouches for exactly
// this file, made by the pinned OpenTofu through the isolated entry, whether or not the
// configuration changed since (the pin controls of T085 mutate the plan, so its freshness is not
// theirs to judge; stagePlan judges it for the pins themselves).
func stagePlanFile(t *testing.T, stage string) []byte {
	t.Helper()
	stageDir, run := planCase(stage)
	file := stage + "-plan.json"
	data := readFile(t, fixtureDir+"/captures/"+file)
	meta := stagePlanMeta(t, stage)
	if meta.File != file || meta.SHA256 != digest(data) || meta.EntryExit != 0 ||
		!strings.Contains(meta.Command, "capture:"+stage+"-plan") || !strings.HasSuffix(meta.Inner, "(stages/"+stageDir+"); run "+run) || meta.Tool != "tofu" || meta.ToolVersion != "1.13.0" ||
		!strings.Contains(meta.Toolchain, " tofu=1.13.0 ") || !strings.HasSuffix(meta.Toolchain, " network=none") {
		t.Fatalf("capture sidecar does not vouch for %s from the pinned tofu: %+v", file, meta)
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
		// Variables are the root input values the plan was made with.
		Variables map[string]struct {
			Value json.RawMessage `json:"value"`
		} `json:"variables"`
		// Configuration is the configuration the plan was made from: which module each call reads.
		Configuration struct {
			RootModule planModule `json:"root_module"`
		} `json:"configuration"`
	} `json:"test_plan"`
}

// planModule is one module of the plan's configuration: its own resources and its module calls.
type planModule struct {
	Resources []struct {
		Address string `json:"address"`
	} `json:"resources"`
	ModuleCalls map[string]planModuleCall `json:"module_calls"`
}

// planModuleCall is one module call of the plan's configuration: its source as written, its
// argument expressions and the called module.
type planModuleCall struct {
	Source      string                    `json:"source"`
	Expressions map[string]planExpression `json:"expressions"`
	Module      planModule                `json:"module"`
}

// planExpression is one argument expression: a constant, or the references it reads.
type planExpression struct {
	ConstantValue any      `json:"constant_value"`
	References    []string `json:"references"`
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

// decodePlan decodes one captured test_plan message of the given run.
func decodePlan(t testing.TB, data []byte, run string) testPlan {
	t.Helper()
	var msg testPlan
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "test_plan" || msg.Run != run || msg.Plan.Errored || len(msg.Plan.Outputs) == 0 {
		t.Fatalf("capture is not the plan of run %s: type %q run %q errored %v outputs %d", run, msg.Type, msg.Run, msg.Plan.Errored, len(msg.Plan.Outputs))
	}
	return msg
}

// planOutputs turns the root output changes of one test_plan message into a `tofu output -json`
// document, and returns the string leaves of the sensitive entries and of the named credential
// outputs (data-model *Sensitive outputs*: their values are collected whatever marker the plan
// gives them, so a lost marker shows as a leak).
func planOutputs(t testing.TB, msg testPlan, credentialOutputs ...string) (doc []byte, secrets []string) {
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
// stage's schema against the consumer's expectation, and checks it publishes exactly the schema's
// values and none of the secrets.
func publishedEnvelope(t testing.TB, exp Expectation, doc []byte, secrets, want []string) {
	t.Helper()
	env, err := BuildEnvelope(doc, Producer{InstanceID: exp.InstanceID, Stage: exp.Stage, SourceRevision: testRev})
	if err != nil {
		t.Fatalf("builder refused the stage's outputs: %v", err)
	}
	published, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ValidateEnvelope(schemas(), published, exp)
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

// planOutputContract pins the plan's own root outputs (005 T086, FR-005): their names are exactly
// the stage schema's published values and the stage's credential outputs (data-model *Sensitive
// outputs*); the credentials are planned sensitive and every other output is not, whatever its type.
// BuildEnvelope drops a sensitive output before the schema sees it and the secrets scan reads only
// string leaves, so neither catches a sensitive number, list, object or nested marker.
func planOutputContract(t testing.TB, msg testPlan, stage string, credentials ...string) {
	t.Helper()
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	raw, err := fs.ReadFile(schemas(), stage+".schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	want := slices.Sorted(slices.Values(append(slices.Collect(maps.Keys(schema.Properties)), credentials...)))
	if names := slices.Sorted(maps.Keys(msg.Plan.Outputs)); !reflect.DeepEqual(names, want) {
		t.Errorf("root outputs %v, want exactly %v", names, want)
	}
	for name, o := range msg.Plan.Outputs {
		switch {
		case slices.Contains(credentials, name) && string(o.AfterSensitive) != "true":
			t.Errorf("credential output %s is not planned sensitive (%s)", name, o.AfterSensitive)
		case !slices.Contains(credentials, name) && string(o.AfterSensitive) != "false":
			t.Errorf("output %s is planned sensitive (%s); only the credential outputs may be", name, o.AfterSensitive)
		}
	}
}

// stagePlanPins is each capture case's pin over its plan; TestOutputsStagePlanPinsRefuse
// (outputs_test.go, T085) runs them on mutated copies of the captured plans.
var stagePlanPins = map[string]func(testing.TB, []byte){
	"bootstrap":          pinBootstrapPlan,
	"tenant-state":       pinTenantStatePlan,
	"account-governance": pinAccountGovernancePlan,
	"project":            func(t testing.TB, data []byte) { pinProjectPlan(t, "project", data) },
	"project-reference":  func(t testing.TB, data []byte) { pinProjectPlan(t, "project-reference", data) },
	"project-network":    pinProjectNetworkPlan,
	"runtime":            func(t testing.TB, data []byte) { pinRuntimePlan(t, "runtime", data) },
	"runtime-slot":       func(t testing.TB, data []byte) { pinRuntimePlan(t, "runtime-slot", data) },
}

func TestOutputsBootstrapStagePlan(t *testing.T) {
	pinBootstrapPlan(t, stagePlan(t, "bootstrap"))
}

// pinBootstrapPlan (005 T038, T086 gap 5): bootstrap's plan publishes exactly its schema's values,
// its one credential output `platform_s3` planned sensitive and never published.
func pinBootstrapPlan(t testing.TB, data []byte) {
	t.Helper()
	msg := decodePlan(t, data, "published_outputs_match_the_schema")
	planOutputContract(t, msg, "bootstrap", "platform_s3")
	doc, secrets := planOutputs(t, msg, "platform_s3")
	// The credential's two fields: the control that the secrets scan below can fail.
	if len(secrets) != 2 {
		t.Fatalf("want the two credential strings of platform_s3, got %d", len(secrets))
	}
	publishedEnvelope(t, expectation("bootstrap", "account-bootstrap"), doc, secrets,
		[]string{"platform_s3_user_id", "state_bucket", "state_endpoint", "state_project_id", "state_region", "unlabelled"})
}

func TestOutputsTenantStateStagePlan(t *testing.T) {
	pinTenantStatePlan(t, stagePlan(t, "tenant-state"))
}

func pinTenantStatePlan(t testing.TB, data []byte) {
	t.Helper()
	msg := decodePlan(t, data, "published_outputs_match_the_schema")
	planOutputContract(t, msg, "tenant-state", "tenant_s3", "platform_s3")
	doc, secrets := planOutputs(t, msg, "tenant_s3", "platform_s3")
	// The two credentials, two fields each: the control that the secrets scan below can fail.
	if len(secrets) != 4 {
		t.Fatalf("want the four credential strings of tenant_s3 and platform_s3, got %d", len(secrets))
	}
	publishedEnvelope(t, expectation("tenant-state", "demo-state"), doc, secrets,
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
func strs(t testing.TB, r plannedResource, attr string) []string {
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
	pinAccountGovernancePlan(t, stagePlan(t, "account-governance"))
}

func pinAccountGovernancePlan(t testing.TB, data []byte) {
	t.Helper()
	msg := decodePlan(t, data, "published_outputs_match_the_schema")
	planOutputContract(t, msg, "account-governance", "platform_deployer_secret", "tenant_deployer_secrets")
	doc, secrets := planOutputs(t, msg, "platform_deployer_secret", "tenant_deployer_secrets")
	// The platform secret and one per tenant: the control that the secrets scan can fail.
	if len(secrets) != 3 {
		t.Fatalf("want the three deployer secrets (platform, alpha, demo), got %d", len(secrets))
	}
	publishedEnvelope(t, expectation("account-governance", "account-governance"), doc, secrets,
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

// The fixtures of the project stage's captured runs (stages/project/tests): adopt mode with both
// toggles and the order arguments (unit.tftest.hcl, published_outputs_match_the_schema), and
// reference mode with both toggles (reference.tftest.hcl,
// reference_published_outputs_with_both_toggles). Both bind the same project; its URN carries the
// `ca` region part the mocks give it.
const (
	pfModule       = "module.project_factory.module."
	pfProjectID    = "0123456789abcdef0123456789abcdef"
	pfProjectURN   = "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
	pfImportTarget = pfModule + "project.ovh_cloud_project.this[0]"
)

// pfLabels is the label set of the captured scope (data-model *Label set*).
var pfLabels = map[string]any{
	"lz:managed-by": "opentofu",
	"lz:managed-in": "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/project",
	"lz:instance":   "demo-dev-project",
	"lz:tenant":     "demo",
	"lz:release":    "unreleased",
}

// managedByAddress indexes a plan's managed resources by address and refuses any change but a
// create (the stack's import turns the adopted project's create into an import, T037).
func managedByAddress(t testing.TB, msg testPlan) map[string]plannedResource {
	t.Helper()
	out := map[string]plannedResource{}
	for _, r := range msg.Plan.Resources {
		if r.Mode != "managed" {
			continue
		}
		if _, dup := out[r.Address]; dup {
			t.Errorf("%s planned twice", r.Address)
		}
		out[r.Address] = r
		if !reflect.DeepEqual(r.Change.Actions, []string{"create"}) {
			t.Errorf("%s: actions %v, want [create]", r.Address, r.Change.Actions)
		}
	}
	return out
}

// projectCommon pins what both modes plan beside the project: the label set on the project's own
// URN, the alert and the quota guard with the given values, all on the bound project.
func projectCommon(t testing.TB, byAddress map[string]plannedResource, threshold float64) {
	t.Helper()
	tags := byAddress[pfModule+"project.ovh_iam_resource_tags.this"].Change.After
	if tags["urn"] != pfProjectURN {
		t.Errorf("tags on %v, want the bound project's URN %s", tags["urn"], pfProjectURN)
	}
	if !reflect.DeepEqual(tags["tags"], pfLabels) {
		t.Errorf("project tags %v, want exactly the label set %v", tags["tags"], pfLabels)
	}
	alert := byAddress[pfModule+"project.ovh_cloud_project_alerting.this[0]"].Change.After
	for attr, want := range map[string]any{"service_name": pfProjectID, "monthly_threshold": threshold, "email": "finops@example.org", "delay": float64(3600)} {
		if alert[attr] != want {
			t.Errorf("budget alert %s %v, want %v", attr, alert[attr], want)
		}
	}
	quota := byAddress[pfModule+"quota.ovh_cloud_quota.this[0]"].Change.After
	if quota["service_name"] != pfProjectID || quota["prevent_automatic_quota_upgrade"] != true {
		t.Errorf("quota guard on %v with prevent_automatic_quota_upgrade %v, want the bound project %s and true", quota["service_name"], quota["prevent_automatic_quota_upgrade"], pfProjectID)
	}
}

// T028 (coordinator decision 2026-10-07 on T027's decision request 1): the project stage's real
// plan in both modes. tofu test reads only a child module's outputs, so the component and stage
// tests cannot see the order arguments, the alert's values or which project the alert, the quota
// guard and the tags act on (T027 gaps 1, 2); a replacement of an adopted project orders a new one
// (cloud_project.md), and T009 showed that the order arguments force one. The envelope built from
// each plan validates under project.schema.json against the bound project (KD-3).
func TestOutputsProjectStagePlan(t *testing.T) {
	t.Run("adopt", func(t *testing.T) { pinProjectPlan(t, "project", stagePlan(t, "project")) })
	// Reference mode manages no project: it is only read (its tags, alert and guard still apply).
	t.Run("reference", func(t *testing.T) { pinProjectPlan(t, "project-reference", stagePlan(t, "project-reference")) })
}

// pinProjectPlan pins one capture case of the project stage: `project` (adopt mode) or
// `project-reference`.
func pinProjectPlan(t testing.TB, c string, data []byte) {
	t.Helper()
	binding := Expectation{InstanceID: "demo-dev-project", Stage: "project", Project: &ProjectBinding{ProjectID: pfProjectID, ProjectURN: pfProjectURN}}
	values := []string{"budget_alert_id", "environment", "project_id", "project_urn", "regions", "tenant", "unlabelled"}
	want := []string{
		pfModule + "project.ovh_cloud_project_alerting.this[0]",
		pfModule + "project.ovh_iam_resource_tags.this",
		pfModule + "quota.ovh_cloud_quota.this[0]",
	}
	adopt := c == "project"
	if adopt {
		want = append([]string{pfImportTarget}, want...)
	}
	_, run := planCase(c)
	msg := decodePlan(t, data, run)
	planOutputContract(t, msg, "project")
	doc, secrets := planOutputs(t, msg)
	if len(secrets) != 0 {
		t.Errorf("the project stage publishes every output; %d sensitive strings found", len(secrets))
	}
	publishedEnvelope(t, binding, doc, secrets, values)
	byAddress := managedByAddress(t, msg)
	if got := slices.Sorted(maps.Keys(byAddress)); !reflect.DeepEqual(got, want) {
		t.Fatalf("planned resources %v, want exactly %v", got, want)
	}
	if !adopt {
		projectCommon(t, byAddress, 50)
		return
	}

	project := byAddress[pfImportTarget].Change.After
	if project["deletion_protection"] != true || project["urn"] != pfProjectURN {
		t.Errorf("adopted project: deletion_protection %v, urn %v, want true and %s", project["deletion_protection"], project["urn"], pfProjectURN)
	}
	// Order arguments as given (stages/project/tests/unit.tftest.hcl), never invented.
	if project["ovh_subsidiary"] != "FR" || project["description"] != "demo-dev" {
		t.Errorf("adopted project: ovh_subsidiary %v, description %v, want FR and demo-dev as given", project["ovh_subsidiary"], project["description"])
	}
	blocks, ok := project["plan"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("adopted project: plan %v, want the one given plan block", project["plan"])
	}
	block, _ := blocks[0].(map[string]any)
	for attr, want := range map[string]any{"duration": "P1M", "plan_code": "project.2018", "pricing_mode": "default"} {
		if block[attr] != want {
			t.Errorf("adopted project: plan %s %v, want %v as given", attr, block[attr], want)
		}
	}
	projectCommon(t, byAddress, 100)
}

// The fixture of run published_outputs_match_the_schema (stages/project-network/tests/unit.tftest.hcl):
// the demo/dev project's published values (its URN with the `eu` part the run gives), region GRA11,
// the network row 10.20.0.0/24 without a vlan_id, and the ids the run's overrides give.
const (
	pnModule     = "module.island.module.network."
	pnNetwork    = pnModule + "ovh_cloud_project_network_private.this"
	pnSubnet     = pnModule + "ovh_cloud_project_network_private_subnet.this"
	pnProjectID  = "0123456789abcdef0123456789abcdef"
	pnProjectURN = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
	pnRegion     = "GRA11"
	pnCIDR       = "10.20.0.0/24"
)

// T030 (coordinator decision 2026-10-07 on T029's decision request 1, option A): the project-network
// stage's real plan. tofu test cannot enumerate resources and reads only a child module's outputs,
// so the stage and component tests see neither a second network or subnet at another address nor
// DHCP, no_gateway and the pool below the module (T029 gap 1). The plan holds exactly the one network
// and the one subnet, nothing else; the project values it was made from pass the adapter's KD-3
// check against the bound project, and the envelope built from it validates under
// project-network.schema.json.
func TestOutputsProjectNetworkStagePlan(t *testing.T) {
	pinProjectNetworkPlan(t, stagePlan(t, "project-network"))
}

func pinProjectNetworkPlan(t testing.TB, data []byte) {
	t.Helper()
	binding := &ProjectBinding{ProjectID: pnProjectID, ProjectURN: pnProjectURN}
	msg := decodePlan(t, data, "published_outputs_match_the_schema")
	projectInputBound(t, msg, binding)

	// The plan's own root outputs are exactly the five published, none sensitive (T030 review r1).
	planOutputContract(t, msg, "project-network")
	doc, secrets := planOutputs(t, msg)
	if len(secrets) != 0 {
		t.Errorf("the project-network stage publishes every output; %d sensitive strings found", len(secrets))
	}
	publishedEnvelope(t, Expectation{InstanceID: "demo-dev-gra11-network", Stage: "project-network", Project: binding}, doc, secrets,
		[]string{"cidr", "network_id", "regions_openstack_ids", "subnet_id", "unlabelled"})

	// Exactly one network and one subnet, at the module's unkeyed addresses; nothing else, managed
	// or read.
	var all []string
	for _, r := range msg.Plan.Resources {
		all = append(all, r.Mode+" "+r.Address)
	}
	sort.Strings(all)
	if want := []string{"managed " + pnNetwork, "managed " + pnSubnet}; !reflect.DeepEqual(all, want) {
		t.Fatalf("planned resources %v, want exactly %v", all, want)
	}
	byAddress := managedByAddress(t, msg)

	network := byAddress[pnNetwork].Change.After
	if network["service_name"] != pnProjectID {
		t.Errorf("network in project %v, want the bound project %s", network["service_name"], pnProjectID)
	}
	if got := strs(t, byAddress[pnNetwork], "regions"); !reflect.DeepEqual(got, []string{pnRegion}) {
		t.Errorf("network regions %v, want exactly [%s]", got, pnRegion)
	}
	if network["name"] != "lz-demo-dev-gra11-pn-main" || network["vlan_id"] != float64(0) {
		t.Errorf("network name %v, vlan_id %v, want lz-demo-dev-gra11-pn-main and 0", network["name"], network["vlan_id"])
	}

	subnet := byAddress[pnSubnet].Change.After
	for attr, want := range map[string]any{"service_name": pnProjectID, "region": pnRegion, "network": pnCIDR, "network_id": network["id"], "dhcp": true, "no_gateway": true} {
		if subnet[attr] != want {
			t.Errorf("subnet %s %v, want %v", attr, subnet[attr], want)
		}
	}
	// The DHCP pool lies inside the CIDR, after its network and first host addresses (the first host
	// is kept free, modules/private-network) and before its broadcast address, in order.
	prefix := netip.MustParsePrefix(pnCIDR)
	start, errStart := netip.ParseAddr(fmtAny(subnet["start"]))
	end, errEnd := netip.ParseAddr(fmtAny(subnet["end"]))
	if errStart != nil || errEnd != nil {
		t.Fatalf("subnet pool %v–%v is not two addresses", subnet["start"], subnet["end"])
	}
	if !prefix.Contains(start) || !prefix.Contains(end) || start.Compare(prefix.Addr().Next()) <= 0 || end.Compare(lastAddr(prefix)) >= 0 || start.Compare(end) > 0 {
		t.Errorf("subnet pool %s–%s, want an ordered pool inside %s after its first host and before its broadcast address", start, end, pnCIDR)
	}

	// `unlabelled` lists exactly the planned resources (review r1): a renamed resource cannot drift
	// from the hand-written addresses.
	var unlabelled []string
	if err := json.Unmarshal(msg.Plan.Outputs["unlabelled"].After, &unlabelled); err != nil {
		t.Fatal(err)
	}
	sort.Strings(unlabelled)
	if want := []string{pnNetwork, pnSubnet}; !reflect.DeepEqual(unlabelled, want) {
		t.Errorf("unlabelled %v, want exactly the planned resources %v", unlabelled, want)
	}

	// The published ids are the planned resources' own.
	for name, want := range map[string]any{"network_id": network["id"], "subnet_id": subnet["id"], "cidr": pnCIDR} {
		var got any
		if err := json.Unmarshal(msg.Plan.Outputs[name].After, &got); err != nil || got != want {
			t.Errorf("output %s %v, want %v", name, got, want)
		}
	}
}

// projectInputBound is KD-3 on a plan's own `project` input: its values are a project envelope's
// values for the bound reference (the adapter refuses any other before the plan).
func projectInputBound(t testing.TB, msg testPlan, binding *ProjectBinding) {
	t.Helper()
	raw, ok := msg.Plan.Variables["project"]
	if !ok {
		t.Fatal("the plan has no `project` input")
	}
	var project map[string]any
	if err := json.Unmarshal(raw.Value, &project); err != nil {
		t.Fatal(err)
	}
	entries := map[string]any{}
	for name, v := range project {
		if v != nil { // an absent optional value (budget_alert_id) is not published
			entries[name] = map[string]any{"sensitive": false, "value": v}
		}
	}
	projectDoc, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	publishedEnvelope(t, Expectation{InstanceID: "demo-dev-project", Stage: "project", Project: binding}, projectDoc, nil, slices.Sorted(maps.Keys(entries)))
}

// The fixtures of the runtime stage's captured runs (stages/runtime/tests/unit.tftest.hcl): the
// demo/dev project's published values, region GRA11, no slot (published_outputs_match_the_schema)
// and slot `blue` (slot_blue_names_the_bucket).
const (
	rtBucket     = "module.runtime.module.bucket.ovh_cloud_project_storage.this"
	rtProjectID  = "0123456789abcdef0123456789abcdef"
	rtProjectURN = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
	rtRegion     = "GRA11"
	// rtStorageRegion is the instance region's leading letters (UNVERIFIED until T010,
	// components/runtime/managed-only/README.md).
	rtStorageRegion = "GRA"
	rtManagedIn     = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/"
)

// rtCases are the runtime capture cases: the instance, its slot ("" when none is set) and the
// bucket name naming gives it.
var rtCases = map[string]struct{ instance, slot, bucket string }{
	"runtime":      {"demo-dev-gra11-runtime", "", "lz-demo-dev-gra11-bkt-runtime"},
	"runtime-slot": {"demo-dev-gra11-runtime-blue", "blue", "lz-demo-dev-gra11-bkt-runtime-blue"},
}

// T032 (coordinator decision 2026-10-07 on T031's decision request 1, option A): the runtime stage's
// real plan, without and with a slot. tofu test cannot enumerate resources or see which module a
// call reads, so the component and stage tests see neither a second bucket at another address nor
// the protected module or a hand-written name in place of modules/naming (T031 gaps 1, 5). Each plan
// holds exactly the one bucket, nothing else, built through modules/object-storage and named and
// labelled through modules/naming; its project input passes the adapter's KD-3 check and the
// envelope built from it validates under runtime.schema.json.
func TestOutputsRuntimeStagePlan(t *testing.T) {
	t.Run("no slot", func(t *testing.T) { pinRuntimePlan(t, "runtime", stagePlan(t, "runtime")) })
	t.Run("slot", func(t *testing.T) { pinRuntimePlan(t, "runtime-slot", stagePlan(t, "runtime-slot")) })
}

// pinRuntimePlan pins one capture case of the runtime stage: `runtime` (no slot) or `runtime-slot`.
func pinRuntimePlan(t testing.TB, c string, data []byte) {
	t.Helper()
	rc := rtCases[c]
	binding := &ProjectBinding{ProjectID: rtProjectID, ProjectURN: rtProjectURN}
	_, run := planCase(c)
	msg := decodePlan(t, data, run)
	projectInputBound(t, msg, binding)

	// The slot the plan was made with: the case's, or none.
	var slotInput any
	if err := json.Unmarshal(msg.Plan.Variables["slot"].Value, &slotInput); err != nil {
		t.Fatalf("the plan has no `slot` input: %v", err)
	}
	var wantSlot any
	if rc.slot != "" {
		wantSlot = rc.slot
	}
	if slotInput != wantSlot {
		t.Fatalf("plan made with slot %v, want %v", slotInput, wantSlot)
	}

	// Root outputs: exactly the schema's, none sensitive. An unset slot is planned null; `tofu
	// output -json` does not list a null root output, so it is absent from the published values.
	planOutputContract(t, msg, "runtime")
	doc, secrets := planOutputs(t, msg)
	if len(secrets) != 0 {
		t.Errorf("the runtime stage publishes every output; %d sensitive strings found", len(secrets))
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(doc, &entries); err != nil {
		t.Fatal(err)
	}
	values := []string{"capabilities", "kind", "pending_actions", "readiness", "scope", "unlabelled"}
	if rc.slot == "" {
		if string(msg.Plan.Outputs["slot"].After) != "null" {
			t.Errorf("slot planned %s, want null when none is set", msg.Plan.Outputs["slot"].After)
		}
		delete(entries, "slot")
	} else {
		values = append(values, "slot")
		if string(msg.Plan.Outputs["slot"].After) != `"`+rc.slot+`"` {
			t.Errorf("slot planned %s, want %q", msg.Plan.Outputs["slot"].After, rc.slot)
		}
	}
	slices.Sort(values)
	doc, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	publishedEnvelope(t, Expectation{InstanceID: rc.instance, Stage: "runtime", Project: binding}, doc, secrets, values)

	// Exactly one bucket, at the plain module's unkeyed address in the one runtime call; nothing
	// else, managed or read.
	var all []string
	for _, r := range msg.Plan.Resources {
		all = append(all, r.Mode+" "+r.Address)
	}
	sort.Strings(all)
	if want := []string{"managed " + rtBucket}; !reflect.DeepEqual(all, want) {
		t.Fatalf("planned resources %v, want exactly %v", all, want)
	}
	bucket := managedByAddress(t, msg)[rtBucket].Change.After
	for attr, want := range map[string]any{"service_name": rtProjectID, "region_name": rtStorageRegion, "name": rc.bucket} {
		if bucket[attr] != want {
			t.Errorf("bucket %s %v, want %v", attr, bucket[attr], want)
		}
	}
	labels := map[string]any{
		"lz:managed-by": "opentofu",
		"lz:managed-in": rtManagedIn + strings.TrimPrefix(rc.instance, "demo-dev-gra11-"),
		"lz:instance":   rc.instance,
		"lz:tenant":     "demo",
		"lz:release":    "unreleased",
	}
	if !reflect.DeepEqual(bucket["tags"], labels) {
		t.Errorf("bucket tags %v, want exactly the label set %v", bucket["tags"], labels)
	}

	// The published values are the planned bucket's and the bound project's.
	var published struct {
		Scope        map[string]any `json:"scope"`
		Capabilities map[string]any `json:"capabilities"`
		Unlabelled   []any          `json:"unlabelled"`
	}
	for name, dst := range map[string]any{"scope": &published.Scope, "capabilities": &published.Capabilities, "unlabelled": &published.Unlabelled} {
		if err := json.Unmarshal(msg.Plan.Outputs[name].After, dst); err != nil {
			t.Fatalf("output %s: %v", name, err)
		}
	}
	if want := map[string]any{"instance": rc.instance, "project_id": rtProjectID, "region": rtRegion}; !reflect.DeepEqual(published.Scope, want) {
		t.Errorf("scope %v, want %v", published.Scope, want)
	}
	storage := map[string]any{"bucket": bucket["name"], "endpoint": "https://s3.gra.io.cloud.ovh.net", "region": bucket["region_name"]}
	if want := map[string]any{"object-storage": storage}; !reflect.DeepEqual(published.Capabilities, want) {
		t.Errorf("capabilities %v, want exactly %v", published.Capabilities, want)
	}
	if len(published.Unlabelled) != 0 {
		t.Errorf("unlabelled %v, want none: the bucket carries tags", published.Unlabelled)
	}
	// A managed-only runtime is ready once its bucket exists; nothing is left to the operator.
	for name, want := range map[string]string{"kind": `"managed-only"`, "readiness": `"ready"`, "pending_actions": `[]`} {
		if got := string(msg.Plan.Outputs[name].After); got != want {
			t.Errorf("output %s %s, want %s", name, got, want)
		}
	}

	// Built through the plain modules/object-storage (never the protected state-bucket module) and
	// named and labelled through modules/naming, in the stage's one runtime call; the component
	// manages no resource of its own.
	root := msg.Plan.Configuration.RootModule
	if got := slices.Sorted(maps.Keys(root.ModuleCalls)); !reflect.DeepEqual(got, []string{"runtime"}) || root.ModuleCalls["runtime"].Source != "../../components/runtime/managed-only" || len(root.Resources) != 0 {
		t.Fatalf("stage calls %v (runtime from %q) and holds %d resources, want only module.runtime from ../../components/runtime/managed-only", got, root.ModuleCalls["runtime"].Source, len(root.Resources))
	}
	component := root.ModuleCalls["runtime"].Module
	sources := map[string]string{}
	for name, call := range component.ModuleCalls {
		sources[name] = call.Source
	}
	if want := map[string]string{"bucket": "../../../modules/object-storage", "bucket_name": "../../../modules/naming"}; !reflect.DeepEqual(sources, want) || len(component.Resources) != 0 {
		t.Fatalf("component calls %v and holds %d resources, want exactly %v and none", sources, len(component.Resources), want)
	}
	call := component.ModuleCalls["bucket"]
	// Each reads only the naming output (review r1): a condition beside it adds a reference. A
	// literal fallback inside the same expression (try, a conditional on the output itself) adds
	// none, so the references show the source, not that the value passes unchanged.
	for arg, ref := range map[string]string{"name": "module.bucket_name.name", "tags": "module.bucket_name.labels"} {
		if got := call.Expressions[arg].References; !reflect.DeepEqual(got, []string{ref, "module.bucket_name"}) {
			t.Errorf("bucket %s reads %v, want only %s", arg, got, ref)
		}
	}
	naming := component.ModuleCalls["bucket_name"].Expressions
	if naming["kind"].ConstantValue != "bucket" || naming["role"].ConstantValue != "runtime" || !slices.Contains(naming["slot"].References, "var.slot") {
		t.Errorf("naming call kind %v, role %v, slot %v, want bucket, runtime and var.slot", naming["kind"].ConstantValue, naming["role"].ConstantValue, naming["slot"].References)
	}
}

// fmtAny returns a planned string attribute, or "" for anything else.
func fmtAny(v any) string {
	s, _ := v.(string)
	return s
}

// lastAddr is the broadcast address of an IPv4 prefix.
func lastAddr(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr().As4()
	for i := p.Bits(); i < 32; i++ {
		a[i/8] |= 0x80 >> (i % 8)
	}
	return netip.AddrFrom4(a)
}
