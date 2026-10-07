// This file is the deployment manifest, stacks/deployments.yaml (FR-006, data-model *Deployment
// manifest*): decoding, the rules a manifest must satisfy, and the fields every instance derives
// from its row and the stage table (path, state bucket and key, Terramate tags, data and authority
// edges, naming input).
//
// The manifest is YAML written in its JSON-compatible flow form, as harness/checks.yaml is, so it
// is decoded strictly as JSON (exact keys, duplicate keys refused, no third-party YAML library).
// Whole-line `#` comments are allowed: a JSON string cannot span lines, so a line whose first
// non-blank character is `#` is never inside a value. Block-style YAML would need a vendored YAML
// library (a dependency decision); see evidence/T034.md.
//
// The manifest is the trust boundary of every stack: its names become directories, state keys,
// bucket names and tags, so every string in it has a pattern or an enum (manifestRules, mirrored
// by schemas/deployments.schema.json) and nothing is derived from a document that breaks one.
package stacks

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
)

// The manifest's fixed header values (FR-006).
const (
	ManifestAPIVersion = "lz.platformrelay.dev/v1alpha1"
	ManifestKind       = "Deployments"
)

// Manifest scopes (`spec.scope`; platform when absent).
const (
	ManifestScopePlatform = "platform"
	ManifestScopeTenant   = "tenant"
)

// Instance scopes, fixed per stage by the stage table.
const (
	ScopeAccount       = "account"
	ScopeAccountTenant = "account-tenant"
	ScopeEnvironment   = "environment"
	ScopeRegion        = "region"
)

// Edge kinds (data-model *Stage table*): a data edge carries the producer's outputs.json, an
// authority edge only a principal or state access the producer creates.
const (
	EdgeData      = "data"
	EdgeAuthority = "authority"
)

// Refusal codes of DecodeManifest. When a document breaks several rules, the code of the first
// group in this order is reported: decoding (syntax, duplicate key, unknown or missing field,
// then a JSON type mismatch as INVALID_VALUE; keys in sorted order, so the code is stable),
// header, values (value rules, then duplicate names), per-row rules (stage not implemented, slot,
// scope), duplicate id, duplicate scope, composition (platform rows, tenant-state, tenant scope),
// producers, shared state project.
const (
	CodeManifestSyntax      = "MANIFEST_SYNTAX"          // not one JSON-compatible document
	CodeDuplicateKey        = "DUPLICATE_KEY"            // a key twice in one object
	CodeUnknownField        = "UNKNOWN_FIELD"            // a key the schema does not have, or a case variant
	CodeMissingField        = "MISSING_FIELD"            // a required key absent or null
	CodeUnsupportedVersion  = "UNSUPPORTED_VERSION"      // apiVersion or kind other than the constants
	CodeInvalidValue        = "INVALID_VALUE"            // pattern or enum violated; a JSON type mismatch, found while decoding
	CodeStageNotImplemented = "STAGE_NOT_IMPLEMENTED"    // account-admin, account-fabric, observability
	CodeSlotNotAllowed      = "SLOT_NOT_ALLOWED"         // slot on a stage other than runtime
	CodeScopeViolation      = "SCOPE_VIOLATION"          // dimension required/forbidden by the stage, or not under spec.tenants
	CodeDuplicateID         = "DUPLICATE_ID"             // an id twice among instances and external entries
	CodeDuplicateScope      = "DUPLICATE_SCOPE"          // two rows for one (stage, tenant, environment, region, slot)
	CodePlatformRows        = "PLATFORM_ROWS"            // platform scope without exactly one bootstrap and account-governance, or with external entries
	CodeMissingTenantState  = "MISSING_TENANT_STATE"     // platform scope: a tenant with rows but no tenant-state row
	CodeTenantScopeAccount  = "TENANT_SCOPE_ACCOUNT_ROW" // tenant scope: an account or account-tenant row
	CodeUnresolvedProducer  = "UNRESOLVED_PRODUCER"      // a producer a row consumes is neither a row nor external
	CodeSharedStateProject  = "SHARED_STATE_PROJECT"     // state project = a tenant project without sandbox.shared_state_project
)

// CodeDuplicateName belongs to the values group: a tenant, an environment of one tenant or a
// region of one environment named twice in spec.tenants, or one project ref on two environments
// (two project stacks would own one project).
const CodeDuplicateName = "DUPLICATE_NAME"

// ManifestError is the error of a refused manifest; Code is one of the Code constants.
type ManifestError struct {
	Code   string
	Detail string
}

func (e *ManifestError) Error() string { return e.Code + ": " + e.Detail }

// Manifest is a decoded, checked deployments.yaml.
type Manifest struct {
	Name               string        // metadata.name
	Scope              string        // spec.scope, ManifestScopePlatform when absent
	Org                string        // spec.org
	Forge              string        // spec.forge
	StageSource        string        // spec.stage_source.kind; generation refuses the git seam
	SharedStateProject bool          // spec.sandbox.shared_state_project
	StateProjectRef    string        // spec.state.project.ref
	Tenants            []Tenant      // spec.tenants, in order
	Instances          []Instance    // spec.instances, in order, with derived fields
	External           []ExternalRef // spec.external, in order (tenant scope)
}

// Tenant is one spec.tenants entry.
type Tenant struct {
	Name         string
	Environments []Environment
}

// Environment is one tenant environment: its project and regions.
type Environment struct {
	Name        string
	ProjectMode string   // adopt | reference
	ProjectRef  string   // resolved from account.env, never an id
	Regions     []string // region names, as written
}

// ExternalRef is a platform instance a tenant-only manifest consumes but never plans.
type ExternalRef struct {
	ID          string
	Stage       string
	Tenant      string
	Environment string
	Region      string
	Slot        string
}

// Instance is one manifest row and the fields derived from it.
type Instance struct {
	ID          string
	Stage       string
	Tenant      string
	Environment string
	Region      string // as written (GRA11)
	Slot        string

	Scope       string      // one of the Scope constants
	Path        string      // stacks/…, region lower case, -<slot> suffix on the stage directory
	Backend     string      // "local" (bootstrap) or "s3"
	StateBucket string      // empty for the local backend
	StateKey    string      // <Path minus "stacks/">/terraform.tfstate; empty for the local backend
	Tags        []string    // Terramate tags lz-stage-*, lz-scope-*, lz-tenant-*, lz-env-*, lz-region-*, lz-slot-*
	Edges       []Edge      // producers this instance consumes (data) or runs under (authority)
	Naming      NamingInput // the naming segments of the instance's resources
}

// Edge is one derived dependency on a producer instance (a row or an external entry).
type Edge struct {
	Producer string
	Kind     string // EdgeData or EdgeAuthority
}

// NamingInput is the per-instance part of modules/naming's input; empty segments are unset.
type NamingInput struct {
	Org         string
	Tenant      string
	Environment string
	Region      string
	Slot        string
}

// The document types: deployments.yaml exactly as written. schemas/deployments.schema.json
// publishes the same shape (TestManifestSchemaMatchesTypes): a field without omitempty is
// required, every object is closed, and an optional block is a value, never a pointer, because
// checks.DecodeStrict does not descend through pointers.
type manifestDoc struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   docMetadata `json:"metadata"`
	Spec       docSpec     `json:"spec"`
}

type docMetadata struct {
	Name string `json:"name"`
}

type docSpec struct {
	Scope       string         `json:"scope,omitempty"`
	Org         string         `json:"org"`
	Forge       string         `json:"forge"`
	StageSource docStageSource `json:"stage_source"`
	Sandbox     docSandbox     `json:"sandbox,omitempty"`
	State       docState       `json:"state"`
	Tenants     []docTenant    `json:"tenants"`
	External    []docRow       `json:"external,omitempty"`
	Instances   []docRow       `json:"instances"`
}

type docStageSource struct {
	Kind string `json:"kind"`
}

type docSandbox struct {
	SharedStateProject bool `json:"shared_state_project"`
}

type docState struct {
	Project  docProjectRef `json:"project"`
	Region   string        `json:"region"`
	Endpoint string        `json:"endpoint"`
}

type docProjectRef struct {
	Ref string `json:"ref"`
}

type docTenant struct {
	Name         string           `json:"name"`
	Environments []docEnvironment `json:"environments"`
}

type docEnvironment struct {
	Name        string      `json:"name"`
	Project     docProject  `json:"project"`
	Regions     []docRegion `json:"regions"`
	BudgetAlert docToggle   `json:"budget_alert,omitempty"`
	QuotaGuard  docToggle   `json:"quota_guard,omitempty"`
}

type docProject struct {
	Mode string `json:"mode"`
	Ref  string `json:"ref"`
}

type docRegion struct {
	Name    string     `json:"name"`
	Network docNetwork `json:"network,omitempty"`
}

type docNetwork struct {
	CIDR   string `json:"cidr"`
	VlanID int    `json:"vlan_id"`
}

type docToggle struct {
	Enabled bool `json:"enabled"`
}

// docRow is a spec.instances row or a spec.external entry.
type docRow struct {
	ID          string `json:"id"`
	Stage       string `json:"stage"`
	Tenant      string `json:"tenant,omitempty"`
	Environment string `json:"environment,omitempty"`
	Region      string `json:"region,omitempty"`
	Slot        string `json:"slot,omitempty"`
}

// valueRule constrains one string of the document: a pattern or an enum.
type valueRule struct {
	Pattern string
	Enum    []string
}

// Value patterns of the manifest (data-model *Deployment manifest* rules).
const (
	manifestIDPattern    = `^[a-z][a-z0-9-]{2,62}$`
	metadataNamePattern  = `^[a-z][a-z0-9-]{0,62}$`
	segmentPattern       = `^[a-z][a-z0-9]{0,15}$`  // org, tenant, environment: one naming segment, no separator
	regionPattern        = `^[A-Z]+[0-9]+$`         // 3-AZ region ids refused until handled (coordinator 2026-10-07)
	projectRefPattern    = `^[A-Z][A-Z0-9_]{0,62}$` // the <REF> of LZ_PROJECT_ID_<REF> in account.env
	forgePattern         = `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(/[A-Za-z0-9_-][A-Za-z0-9._-]*)+$`
	storageRegionPattern = `^[a-z]+$`
	endpointPattern      = `^https://[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`
	cidrPattern          = `^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$`
)

// manifestRules holds the value rule of every string in the document except the header constants,
// keyed by its path (object keys joined by ".", "[]" for an array element). The schema carries
// the same patterns and enums.
var manifestRules = func() map[string]valueRule {
	p := func(pattern string) valueRule { return valueRule{Pattern: pattern} }
	env := "spec.tenants[].environments[]"
	rules := map[string]valueRule{
		"metadata.name":                 p(metadataNamePattern),
		"spec.scope":                    {Enum: []string{ManifestScopePlatform, ManifestScopeTenant}},
		"spec.org":                      p(segmentPattern),
		"spec.forge":                    p(forgePattern),
		"spec.stage_source.kind":        {Enum: []string{"local", "git"}},
		"spec.state.project.ref":        p(projectRefPattern),
		"spec.state.region":             p(storageRegionPattern),
		"spec.state.endpoint":           p(endpointPattern),
		"spec.tenants[].name":           p(segmentPattern),
		env + ".name":                   p(segmentPattern),
		env + ".project.mode":           {Enum: []string{"adopt", "reference"}},
		env + ".project.ref":            p(projectRefPattern),
		env + ".regions[].name":         p(regionPattern),
		env + ".regions[].network.cidr": p(cidrPattern),
	}
	for _, rows := range []string{"spec.instances[]", "spec.external[]"} {
		rules[rows+".id"] = p(manifestIDPattern)
		rules[rows+".stage"] = valueRule{Enum: stageNames()}
		rules[rows+".tenant"] = p(segmentPattern)
		rules[rows+".environment"] = p(segmentPattern)
		rules[rows+".region"] = p(regionPattern)
		rules[rows+".slot"] = p(slotPattern)
	}
	return rules
}()

// rulePatterns are manifestRules' patterns, compiled once.
var rulePatterns = func() map[string]*regexp.Regexp {
	compiled := map[string]*regexp.Regexp{}
	for path, rule := range manifestRules {
		if rule.Pattern != "" {
			compiled[path] = regexp.MustCompile(rule.Pattern)
		}
	}
	return compiled
}()

func refuseManifest(code, format string, a ...any) error {
	return &ManifestError{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// stripComments blanks whole-line `#` comments, keeping line numbers.
func stripComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines[i] = ""
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// decodeError maps a checks.DecodeStrict error to its refusal code.
func decodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	msg := err.Error()
	switch {
	case errors.As(err, &typeErr):
		return refuseManifest(CodeInvalidValue, "%s", msg)
	case strings.HasPrefix(msg, "DUPLICATE_KEY"):
		return refuseManifest(CodeDuplicateKey, "%s", msg)
	case strings.HasSuffix(msg, ": unknown field"):
		return refuseManifest(CodeUnknownField, "%s", msg)
	case strings.HasSuffix(msg, ": required"), strings.HasSuffix(msg, ": null"):
		return refuseManifest(CodeMissingField, "%s", msg)
	}
	return refuseManifest(CodeManifestSyntax, "%s", msg)
}

// checkValues applies manifestRules to every string of the decoded document, in a fixed order.
// A string without a rule outside the header is refused: no free-form string crosses the boundary.
func checkValues(v any, path string) error {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sub := k
			if path != "" {
				sub = path + "." + k
			}
			if err := checkValues(v[k], sub); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := checkValues(item, path+"[]"); err != nil {
				return err
			}
		}
	case string:
		if path == "apiVersion" || path == "kind" {
			return nil
		}
		rule, ok := manifestRules[path]
		switch {
		case !ok:
			return refuseManifest(CodeInvalidValue, "%s: no value rule", path)
		case rule.Enum != nil && !contains(rule.Enum, v):
			return refuseManifest(CodeInvalidValue, "%s: %q is not one of %s", path, v, strings.Join(rule.Enum, ", "))
		case rule.Pattern != "" && !rulePatterns[path].MatchString(v):
			return refuseManifest(CodeInvalidValue, "%s: %q does not match %s", path, v, rule.Pattern)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// scopeDims are the dimensions (tenant, environment, region) a scope requires; the others are forbidden.
var scopeDims = map[string][3]bool{
	ScopeAccount:       {false, false, false},
	ScopeAccountTenant: {true, false, false},
	ScopeEnvironment:   {true, true, false},
	ScopeRegion:        {true, true, true},
}

// within reports whether a row sits in the consumer's place down to the producer's scope.
func within(producer docRow, scope string, consumer docRow) bool {
	need := scopeDims[scope]
	return (!need[0] || producer.Tenant == consumer.Tenant) &&
		(!need[1] || producer.Environment == consumer.Environment) &&
		(!need[2] || producer.Region == consumer.Region)
}

// DecodeManifest decodes deployments.yaml strictly, checks it and derives every instance.
func DecodeManifest(data []byte) (*Manifest, error) {
	// Decoding.
	data = stripComments(data)
	var doc manifestDoc
	if err := checks.DecodeStrict(data, &doc); err != nil {
		return nil, decodeError(err)
	}
	// Header.
	if doc.APIVersion != ManifestAPIVersion || doc.Kind != ManifestKind {
		return nil, refuseManifest(CodeUnsupportedVersion, "apiVersion %q kind %q", doc.APIVersion, doc.Kind)
	}
	// Values.
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return nil, refuseManifest(CodeManifestSyntax, "%v", err)
	}
	if err := checkValues(generic, ""); err != nil {
		return nil, err
	}
	s := doc.Spec
	m := &Manifest{Name: doc.Metadata.Name, Scope: s.Scope, Org: s.Org, Forge: s.Forge, StageSource: s.StageSource.Kind,
		SharedStateProject: s.Sandbox.SharedStateProject, StateProjectRef: s.State.Project.Ref}
	if m.Scope == "" {
		m.Scope = ManifestScopePlatform
	}
	where, projectOwner := map[[3]string]bool{}, map[string]string{}
	for _, t := range s.Tenants {
		if where[[3]string{t.Name}] {
			return nil, refuseManifest(CodeDuplicateName, "tenant %s named twice", t.Name)
		}
		where[[3]string{t.Name}] = true
		tenant := Tenant{Name: t.Name}
		for _, e := range t.Environments {
			if where[[3]string{t.Name, e.Name}] {
				return nil, refuseManifest(CodeDuplicateName, "environment %s/%s named twice", t.Name, e.Name)
			}
			where[[3]string{t.Name, e.Name}] = true
			if other, taken := projectOwner[e.Project.Ref]; taken {
				return nil, refuseManifest(CodeDuplicateName, "project %s is owned by %s and %s/%s", e.Project.Ref, other, t.Name, e.Name)
			}
			projectOwner[e.Project.Ref] = t.Name + "/" + e.Name
			env := Environment{Name: e.Name, ProjectMode: e.Project.Mode, ProjectRef: e.Project.Ref}
			for _, r := range e.Regions {
				if where[[3]string{t.Name, e.Name, r.Name}] {
					return nil, refuseManifest(CodeDuplicateName, "region %s/%s/%s named twice", t.Name, e.Name, r.Name)
				}
				where[[3]string{t.Name, e.Name, r.Name}] = true
				env.Regions = append(env.Regions, r.Name)
			}
			tenant.Environments = append(tenant.Environments, env)
		}
		m.Tenants = append(m.Tenants, tenant)
	}
	all := append(append([]docRow{}, s.Instances...), s.External...)
	// Per row.
	for _, r := range all {
		st, _ := stageNamed(r.Stage) // the stage enum is checked with the values
		if !st.Implemented {
			return nil, refuseManifest(CodeStageNotImplemented, "%s: stage %s is reserved", r.ID, r.Stage)
		}
		if r.Slot != "" && !st.Slots {
			return nil, refuseManifest(CodeSlotNotAllowed, "%s: slot on stage %s", r.ID, r.Stage)
		}
		if have := [3]bool{r.Tenant != "", r.Environment != "", r.Region != ""}; have != scopeDims[st.Scope] {
			return nil, refuseManifest(CodeScopeViolation, "%s: stage %s (%s scope) with tenant %q environment %q region %q",
				r.ID, r.Stage, st.Scope, r.Tenant, r.Environment, r.Region)
		}
		if place := [3]string{r.Tenant, r.Environment, r.Region}; r.Tenant != "" && !where[place] {
			return nil, refuseManifest(CodeScopeViolation, "%s: %s/%s/%s is not under spec.tenants", r.ID, r.Tenant, r.Environment, r.Region)
		}
	}
	ids := map[string]bool{}
	for _, r := range all {
		if ids[r.ID] {
			return nil, refuseManifest(CodeDuplicateID, "%s", r.ID)
		}
		ids[r.ID] = true
	}
	owners := map[docRow]string{}
	for _, r := range all {
		key := docRow{Stage: r.Stage, Tenant: r.Tenant, Environment: r.Environment, Region: r.Region, Slot: r.Slot}
		if other, taken := owners[key]; taken {
			return nil, refuseManifest(CodeDuplicateScope, "%s and %s own one scope and slot", other, r.ID)
		}
		owners[key] = r.ID
	}
	// Composition.
	if m.Scope == ManifestScopePlatform {
		if len(s.External) != 0 {
			return nil, refuseManifest(CodePlatformRows, "a platform manifest owns its producers; external entries belong to a tenant manifest")
		}
		count, tenantState := map[string]int{}, map[string]bool{}
		for _, r := range s.Instances {
			count[r.Stage]++
			if r.Stage == "tenant-state" {
				tenantState[r.Tenant] = true
			}
		}
		if count["bootstrap"] != 1 || count["account-governance"] != 1 {
			return nil, refuseManifest(CodePlatformRows, "%d bootstrap and %d account-governance rows, want one each",
				count["bootstrap"], count["account-governance"])
		}
		for _, r := range s.Instances {
			if r.Tenant != "" && !tenantState[r.Tenant] {
				return nil, refuseManifest(CodeMissingTenantState, "%s: tenant %s has no tenant-state row", r.ID, r.Tenant)
			}
		}
	} else {
		for _, r := range s.Instances {
			if st, _ := stageNamed(r.Stage); st.Scope == ScopeAccount || st.Scope == ScopeAccountTenant {
				return nil, refuseManifest(CodeTenantScopeAccount, "%s: %s row in a tenant manifest", r.ID, r.Stage)
			}
		}
	}
	// Producers and derived fields.
	for _, r := range s.Instances {
		in, err := derive(r, s, all)
		if err != nil {
			return nil, err
		}
		m.Instances = append(m.Instances, in)
	}
	for _, e := range s.External {
		m.External = append(m.External, ExternalRef{ID: e.ID, Stage: e.Stage, Tenant: e.Tenant, Environment: e.Environment, Region: e.Region, Slot: e.Slot})
	}
	// Shared state project.
	if !m.SharedStateProject {
		for _, t := range m.Tenants {
			for _, e := range t.Environments {
				if e.ProjectRef == m.StateProjectRef {
					return nil, refuseManifest(CodeSharedStateProject, "%s/%s uses the state project %s without spec.sandbox.shared_state_project",
						t.Name, e.Name, e.ProjectRef)
				}
			}
		}
	}
	return m, nil
}

// derive computes one instance's fields from its row and the stage table, resolving each producer
// to the row or external entry of its stage in the consumer's place.
func derive(r docRow, s docSpec, all []docRow) (Instance, error) {
	st, _ := stageNamed(r.Stage)
	in := Instance{ID: r.ID, Stage: r.Stage, Tenant: r.Tenant, Environment: r.Environment, Region: r.Region, Slot: r.Slot,
		Scope: st.Scope, Backend: st.Backend,
		Naming: NamingInput{Org: s.Org, Tenant: r.Tenant, Environment: r.Environment, Region: r.Region, Slot: r.Slot}}
	dir := r.Stage
	if r.Slot != "" {
		dir += "-" + r.Slot
	}
	switch st.Scope {
	case ScopeAccount:
		in.Path = "stacks/account/" + dir
	case ScopeAccountTenant:
		in.Path = "stacks/account/" + dir + "/" + r.Tenant
	case ScopeEnvironment:
		in.Path = "stacks/tenants/" + r.Tenant + "/" + r.Environment + "/" + dir
	case ScopeRegion:
		in.Path = "stacks/tenants/" + r.Tenant + "/" + r.Environment + "/" + strings.ToLower(r.Region) + "/" + dir
	}
	if st.Backend == "s3" {
		in.StateKey = strings.TrimPrefix(in.Path, "stacks/") + "/terraform.tfstate"
		in.StateBucket = stateBucket(s.Org, r.Tenant, st)
	}
	in.Tags = []string{"lz-stage-" + r.Stage, "lz-scope-" + st.Scope}
	for _, tag := range [][2]string{{"lz-tenant-", r.Tenant}, {"lz-env-", r.Environment},
		{"lz-region-", strings.ToLower(r.Region)}, {"lz-slot-", r.Slot}} {
		if tag[1] != "" {
			in.Tags = append(in.Tags, tag[0]+tag[1])
		}
	}
	for _, need := range []struct {
		kind   string
		stages []string
	}{{EdgeData, st.Data}, {EdgeAuthority, st.Authority}} {
		for _, stage := range need.stages {
			producer, _ := stageNamed(stage)
			var found []string
			for _, p := range all {
				if p.Stage == stage && within(p, producer.Scope, r) {
					found = append(found, p.ID)
				}
			}
			if len(found) != 1 {
				return Instance{}, refuseManifest(CodeUnresolvedProducer, "%s consumes %s (%s): %d rows or external entries match, want one",
					r.ID, stage, need.kind, len(found))
			}
			in.Edges = append(in.Edges, Edge{Producer: found[0], Kind: need.kind})
		}
	}
	return in, nil
}

// stateBucket is the bucket a stage instance's state (and its published artefact) lives in: the
// tenant's bucket for a tenant-bucket stage, the account bucket otherwise, including the
// local-state bootstrap, which creates the account bucket and publishes there (FR-008).
func stateBucket(org, tenant string, st Stage) string {
	if st.Bucket == "tenant" {
		return org + "-" + tenant + "-bkt-state"
	}
	return org + "-bkt-state"
}
