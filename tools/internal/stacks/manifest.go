// This file is the deployment manifest, stacks/deployments.yaml (FR-006, data-model *Deployment
// manifest*): decoding, the rules a manifest must satisfy, and the fields every instance derives
// from its row and the stage table (path, state bucket and key, Terramate tags, data and authority
// edges, naming input).
//
// The manifest is YAML written in its JSON-compatible flow form, as harness/checks.yaml is, so it
// is decoded strictly as JSON (exact keys, duplicate keys refused, no third-party YAML library).
// Whole-line `#` comments are allowed: a JSON string cannot span lines, so a line whose first
// non-blank character is `#` is never inside a value.
//
// STUB (005 T033): DecodeManifest accepts every document and derives nothing. T034 implements it;
// TestManifest* are the controls.
package stacks

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
// group in this order is reported: decoding (syntax, duplicate key, unknown or missing field),
// header, values, per-row rules (stage not implemented, slot, scope), duplicate id, duplicate
// scope, composition (platform rows, tenant-state, tenant scope), producers, shared state project.
const (
	CodeManifestSyntax      = "MANIFEST_SYNTAX"          // not one JSON-compatible document
	CodeDuplicateKey        = "DUPLICATE_KEY"            // a key twice in one object
	CodeUnknownField        = "UNKNOWN_FIELD"            // a key the schema does not have, or a case variant
	CodeMissingField        = "MISSING_FIELD"            // a required key absent or null
	CodeUnsupportedVersion  = "UNSUPPORTED_VERSION"      // apiVersion or kind other than the constants
	CodeInvalidValue        = "INVALID_VALUE"            // pattern or enum violated (id, stage, region, slot, scope, mode)
	CodeStageNotImplemented = "STAGE_NOT_IMPLEMENTED"    // account-admin, account-fabric, observability
	CodeSlotNotAllowed      = "SLOT_NOT_ALLOWED"         // slot on a stage other than runtime
	CodeScopeViolation      = "SCOPE_VIOLATION"          // dimension required/forbidden by the stage, or not under spec.tenants
	CodeDuplicateID         = "DUPLICATE_ID"             // an id twice among instances and external entries
	CodeDuplicateScope      = "DUPLICATE_SCOPE"          // two rows for one (stage, tenant, environment, region, slot)
	CodePlatformRows        = "PLATFORM_ROWS"            // platform scope without exactly one bootstrap and account-governance
	CodeMissingTenantState  = "MISSING_TENANT_STATE"     // platform scope: a tenant with rows but no tenant-state row
	CodeTenantScopeAccount  = "TENANT_SCOPE_ACCOUNT_ROW" // tenant scope: an account or account-tenant row
	CodeUnresolvedProducer  = "UNRESOLVED_PRODUCER"      // a producer a row consumes is neither a row nor external
	CodeSharedStateProject  = "SHARED_STATE_PROJECT"     // state project = a tenant project without sandbox.shared_state_project
)

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

// DecodeManifest decodes deployments.yaml strictly, checks it and derives every instance.
func DecodeManifest(data []byte) (*Manifest, error) {
	return &Manifest{}, nil
}
