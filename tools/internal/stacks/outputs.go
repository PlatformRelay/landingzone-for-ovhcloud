// Package stacks holds the stack model of 005: the stage table, the manifest, generation and the
// output exchange between stacks (ADR-0004, data-model *Output envelope*).
//
// This file is the output contract: the envelope builder, which turns `tofu output -json` into an
// outputs.json without sensitive entries, and the validator a consumer runs on an artefact before
// it reads anything from it. The validator is strict, typed decoding: schemas/outputs/*.schema.json
// is the published contract, the Go types below are what is enforced, and
// TestOutputsSchemaMatchesTypes keeps the two from drifting. Decoding uses encoding/json/v2 because
// encoding/json matches object keys case-insensitively, so a case variant of a known key would pass
// DisallowUnknownFields and could override the real one; v2 matches exactly and refuses duplicate
// keys and trailing data.
package stacks

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io/fs"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// The envelope's fixed header values (FR-005).
const (
	OutputsAPIVersion = "lz.platformrelay.dev/v1alpha1"
	OutputsKind       = "StageOutputs"
)

// Envelope is a published outputs.json (data-model *Output envelope*).
type Envelope struct {
	APIVersion     string                     `json:"apiVersion"`
	Kind           string                     `json:"kind"`
	InstanceID     string                     `json:"instance_id"`
	Stage          string                     `json:"stage"`
	SourceRevision string                     `json:"source_revision"`
	Values         map[string]json.RawMessage `json:"values"`
}

// Producer names the instance an envelope is built for and the revision it was applied from.
type Producer struct {
	InstanceID     string
	Stage          string
	SourceRevision string
}

// ProjectBinding is the bound account's resolved reference for one tenant and environment
// (`LZ_PROJECT_ID_<REF>`); a `project` artefact must carry exactly these ids (KD-3).
type ProjectBinding struct {
	ProjectID  string
	ProjectURN string
}

// Expectation is what a consumer checks an artefact against: the producer instance and stage of
// the derived edge and, for a `project` producer, the bound project reference. It carries no
// revision: staleness is not judged per artefact (ADR-0004, artefact transaction postponed).
type Expectation struct {
	InstanceID string
	Stage      string
	Project    *ProjectBinding
}

// Refusal reasons a validator or builder reports (FR-005, V004, V012).
const (
	ReasonMalformed      = "malformed"       // not one JSON object, or a broken tofu output document
	ReasonEnvelope       = "envelope"        // header field missing, wrong or unknown
	ReasonWrongProducer  = "wrong-producer"  // instance_id or stage differs from the derived edge
	ReasonUnknownStage   = "unknown-stage"   // no outputs schema for the stage
	ReasonSchema         = "schema"          // values do not match the stage's schema
	ReasonSecretKey      = "secret-key"      // a key in values matches the secret pattern
	ReasonPlaceholder    = "placeholder"     // null or "" anywhere in values
	ReasonUnboundProject = "unbound-project" // project id or URN differs from the bound reference
	ReasonSensitive      = "sensitive"       // a tofu output entry without a sensitivity marker
)

// RefusalError is the error of a refused build or artefact; Reason is one of the Reason
// constants. Detail never carries a value from the document.
type RefusalError struct {
	Reason string
	Detail string
}

func (e *RefusalError) Error() string { return e.Reason + ": " + e.Detail }

func refuse(reason, format string, args ...any) error {
	return &RefusalError{Reason: reason, Detail: fmt.Sprintf(format, args...)}
}

const (
	// RuntimeKindManagedOnly is the only runtime kind of this slice (ADR-0017).
	RuntimeKindManagedOnly = "managed-only"
	// revisionPattern is a git object id, SHA-1 or SHA-256.
	revisionPattern = "^[0-9a-f]{40}([0-9a-f]{24})?$"
	// slotPattern is a manifest slot name (data-model *Manifest*).
	slotPattern = "^[a-z][a-z0-9]{0,15}$"
)

var (
	secretKey = regexp.MustCompile(`(?i)secret|password|token|private_key|access_key`)
	revision  = regexp.MustCompile(revisionPattern)
	slotName  = regexp.MustCompile(slotPattern)
	strict    = jsonv2.RejectUnknownMembers(true)
)

// The typed values of each in-scope stage (data-model *Per-stage values*). A field without
// omitempty is required; every object is closed. schemas/outputs/<stage>.schema.json mirrors each.

type bootstrapValues struct {
	StateBucket      string   `json:"state_bucket"`
	StateProjectID   string   `json:"state_project_id"`
	StateRegion      string   `json:"state_region"`
	StateEndpoint    string   `json:"state_endpoint"`
	PlatformS3UserID string   `json:"platform_s3_user_id"`
	Unlabelled       []string `json:"unlabelled"`
}

type tenantStateValues struct {
	Tenant           string   `json:"tenant"`
	StateBucket      string   `json:"state_bucket"`
	TenantS3UserID   string   `json:"tenant_s3_user_id"`
	PlatformS3UserID string   `json:"platform_s3_user_id"`
	Unlabelled       []string `json:"unlabelled"`
}

type platformDeployer struct {
	ClientID    string `json:"client_id"`
	IdentityURN string `json:"identity_urn"`
}

type tenantIdentity struct {
	DeployerClientID    string `json:"deployer_client_id"`
	DeployerIdentityURN string `json:"deployer_identity_urn"`
	GroupURN            string `json:"group_urn"`
}

type accountGovernanceValues struct {
	PlatformDeployer platformDeployer          `json:"platform_deployer"`
	Tenants          map[string]tenantIdentity `json:"tenants"`
	Unlabelled       []string                  `json:"unlabelled"`
}

type projectValues struct {
	Tenant        string   `json:"tenant"`
	Environment   string   `json:"environment"`
	ProjectID     string   `json:"project_id"`
	ProjectURN    string   `json:"project_urn"`
	Regions       []string `json:"regions"`
	BudgetAlertID *string  `json:"budget_alert_id,omitempty"`
	Unlabelled    []string `json:"unlabelled"`
}

type projectNetworkValues struct {
	NetworkID           string            `json:"network_id"`
	RegionsOpenstackIDs map[string]string `json:"regions_openstack_ids"`
	SubnetID            string            `json:"subnet_id"`
	CIDR                string            `json:"cidr"`
	Unlabelled          []string          `json:"unlabelled"`
}

type runtimeScope struct {
	Instance  string `json:"instance"`
	ProjectID string `json:"project_id"`
	Region    string `json:"region"`
}

type objectStorageCapability struct {
	Bucket   string `json:"bucket"`
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
}

type runtimeCapabilities struct {
	ObjectStorage objectStorageCapability `json:"object-storage"`
}

type runtimeValues struct {
	Kind           string              `json:"kind"`
	Slot           *string             `json:"slot,omitempty"`
	Scope          runtimeScope        `json:"scope"`
	Readiness      string              `json:"readiness"`
	PendingActions []string            `json:"pending_actions"`
	Capabilities   runtimeCapabilities `json:"capabilities"`
	Unlabelled     []string            `json:"unlabelled"`
}

// stageValues maps each in-scope stage to its values type; a stage not listed has no schema.
var stageValues = map[string]reflect.Type{
	"bootstrap":          reflect.TypeOf(bootstrapValues{}),
	"tenant-state":       reflect.TypeOf(tenantStateValues{}),
	"account-governance": reflect.TypeOf(accountGovernanceValues{}),
	"project":            reflect.TypeOf(projectValues{}),
	"project-network":    reflect.TypeOf(projectNetworkValues{}),
	"runtime":            reflect.TypeOf(runtimeValues{}),
}

// declaredNames are the object keys the contract declares; refusal paths print only these, so a
// detail never carries a key taken from the document (a tenant name, a map key).
var declaredNames = func() map[string]bool {
	names := map[string]bool{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map:
			walk(t.Elem())
		case reflect.Struct:
			for i := 0; i < t.NumField(); i++ {
				name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
				names[name] = true
				walk(t.Field(i).Type)
			}
		}
	}
	for _, t := range stageValues {
		walk(t)
	}
	return names
}()

func child(path, key string) string {
	if declaredNames[key] {
		return path + "." + key
	}
	return path + ".*"
}

// object decodes data as exactly one JSON object: no trailing data, no duplicate key at any depth,
// valid UTF-8.
func object(data []byte) (map[string]any, bool) {
	var doc any
	if err := jsonv2.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	m, ok := doc.(map[string]any)
	return m, ok
}

// BuildEnvelope turns the pinned `tofu output -json` document of an applied root into the
// envelope for producer p, dropping every entry tofu marked sensitive (guard G2, envelope part).
// An entry without a boolean marker is refused, never assumed plain; a plain output whose name or
// any nested key matches the secret pattern is refused.
func BuildEnvelope(tofuOutput []byte, p Producer) (Envelope, error) {
	if _, ok := object(tofuOutput); !ok {
		return Envelope{}, refuse(ReasonMalformed, "tofu output document is not one JSON object")
	}
	var entries map[string]struct {
		Sensitive *bool          `json:"sensitive"`
		Value     jsontext.Value `json:"value"`
	}
	if err := jsonv2.Unmarshal(tofuOutput, &entries); err != nil {
		return Envelope{}, refuse(ReasonMalformed, "tofu output entries do not decode")
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	values := map[string]json.RawMessage{}
	for _, name := range names {
		entry := entries[name]
		if entry.Sensitive == nil {
			return Envelope{}, refuse(ReasonSensitive, "an output entry carries no sensitive marker")
		}
		if *entry.Sensitive {
			continue
		}
		if secretKey.MatchString(name) {
			return Envelope{}, refuse(ReasonSecretKey, "a plain output name matches the secret pattern")
		}
		var v any
		if err := jsonv2.Unmarshal(entry.Value, &v); err != nil { // an absent value is empty and fails here
			return Envelope{}, refuse(ReasonMalformed, "a plain output entry carries no value")
		}
		if hasSecretKey(v) {
			return Envelope{}, refuse(ReasonSecretKey, "a key in a plain output matches the secret pattern")
		}
		values[name] = json.RawMessage(entry.Value)
	}
	return Envelope{
		APIVersion: OutputsAPIVersion, Kind: OutputsKind,
		InstanceID: p.InstanceID, Stage: p.Stage, SourceRevision: p.SourceRevision,
		Values: values,
	}, nil
}

func hasSecretKey(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if secretKey.MatchString(k) || hasSecretKey(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if hasSecretKey(e) {
				return true
			}
		}
	}
	return false
}

// schemaPresent reads name from schemas and requires one JSON object there.
func schemaPresent(schemas fs.FS, name string) bool {
	data, err := fs.ReadFile(schemas, name)
	if err != nil {
		return false
	}
	_, ok := object(data)
	return ok
}

// ValidateEnvelope checks a published outputs.json against the envelope rules, the stage schema
// `<stage>.schema.json` in schemas and the expectation, and returns the decoded envelope. Checks
// run in this order, so overlapping faults report the earlier reason: one JSON object, envelope
// header, producer, deny-list (secret-pattern key, null or ""), stage schema, KD-3 project binding.
func ValidateEnvelope(schemas fs.FS, data []byte, want Expectation) (Envelope, error) {
	doc, ok := object(data)
	if !ok {
		return Envelope{}, refuse(ReasonMalformed, "artefact is not one JSON object")
	}
	if !schemaPresent(schemas, "envelope.schema.json") {
		return Envelope{}, refuse(ReasonEnvelope, "no envelope schema")
	}
	env, err := header(data, doc)
	if err != nil {
		return Envelope{}, err
	}
	if env.InstanceID != want.InstanceID || env.Stage != want.Stage {
		return Envelope{}, refuse(ReasonWrongProducer, "artefact is not the derived edge's producer %s/%s", want.Stage, want.InstanceID)
	}
	if err := denyList(doc["values"], "values"); err != nil {
		return Envelope{}, err
	}
	typ, known := stageValues[env.Stage]
	if !known || !schemaPresent(schemas, env.Stage+".schema.json") {
		return Envelope{}, refuse(ReasonUnknownStage, "no outputs schema for stage %s", want.Stage)
	}
	if err := required(doc["values"], typ, "values"); err != nil {
		return Envelope{}, err
	}
	var raw struct {
		Values jsontext.Value `json:"values"`
	}
	if err := jsonv2.Unmarshal(data, &raw); err != nil {
		return Envelope{}, refuse(ReasonSchema, "values do not decode")
	}
	typed := reflect.New(typ)
	if err := jsonv2.Unmarshal(raw.Values, typed.Interface(), strict); err != nil {
		return Envelope{}, refuse(ReasonSchema, "values are not %s outputs: unknown field, case variant or wrong type", env.Stage)
	}
	switch v := typed.Interface().(type) {
	case *runtimeValues:
		if v.Kind != RuntimeKindManagedOnly {
			return Envelope{}, refuse(ReasonSchema, "values.kind is not %s", RuntimeKindManagedOnly)
		}
		if v.Slot != nil && !slotName.MatchString(*v.Slot) {
			return Envelope{}, refuse(ReasonSchema, "values.slot is not a slot name")
		}
	case *projectValues:
		if want.Project == nil {
			return Envelope{}, refuse(ReasonUnboundProject, "no bound project reference for %s", env.InstanceID)
		}
		if v.ProjectID != want.Project.ProjectID || v.ProjectURN != want.Project.ProjectURN {
			return Envelope{}, refuse(ReasonUnboundProject, "project of %s differs from the bound reference", env.InstanceID)
		}
	}
	return env, nil
}

// header decodes and checks the envelope header: exactly the six known keys, each present,
// typed and well-formed.
func header(data []byte, doc map[string]any) (Envelope, error) {
	if err := required(doc, reflect.TypeOf(Envelope{}), "envelope"); err != nil {
		return Envelope{}, refuse(ReasonEnvelope, "%s", err.(*RefusalError).Detail)
	}
	var env Envelope
	if err := jsonv2.Unmarshal(data, &env, strict); err != nil {
		return Envelope{}, refuse(ReasonEnvelope, "header has an unknown field, a case variant or a wrong type")
	}
	switch {
	case env.APIVersion != OutputsAPIVersion:
		return Envelope{}, refuse(ReasonEnvelope, "apiVersion is not %s", OutputsAPIVersion)
	case env.Kind != OutputsKind:
		return Envelope{}, refuse(ReasonEnvelope, "kind is not %s", OutputsKind)
	case env.InstanceID == "" || env.Stage == "":
		return Envelope{}, refuse(ReasonEnvelope, "instance_id or stage is empty")
	case !revision.MatchString(env.SourceRevision):
		return Envelope{}, refuse(ReasonEnvelope, "source_revision is not a git object id")
	case env.Values == nil:
		return Envelope{}, refuse(ReasonEnvelope, "values is not an object")
	}
	return env, nil
}

// required refuses a missing field without omitempty in the type t, at any depth of v. Unknown
// fields and wrong types are left to the strict typed decoding that follows.
func required(v any, t reflect.Type, path string) error {
	switch t.Kind() {
	case reflect.Pointer:
		return required(v, t.Elem(), path)
	case reflect.Slice, reflect.Map:
		if t == reflect.TypeOf(json.RawMessage{}) {
			return nil
		}
		var elems []any
		switch x := v.(type) {
		case []any:
			elems = x
		case map[string]any:
			for _, e := range x {
				elems = append(elems, e)
			}
		}
		for _, e := range elems {
			if err := required(e, t.Elem(), path+"[]"); err != nil {
				return err
			}
		}
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		for i := 0; i < t.NumField(); i++ {
			name, options, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
			e, present := m[name]
			if !present {
				if strings.Contains(options, "omitempty") {
					continue
				}
				return refuse(ReasonSchema, "%s lacks %s", path, name)
			}
			if err := required(e, t.Field(i).Type, path+"."+name); err != nil {
				return err
			}
		}
	}
	return nil
}

// denyList refuses, anywhere in v, an object key matching the secret pattern and a null or ""
// value (FR-005: a value that does not exist is absent, never a placeholder). Keys of one object
// are checked before its values are descended into.
func denyList(v any, path string) error {
	switch x := v.(type) {
	case nil:
		return refuse(ReasonPlaceholder, "null at %s", path)
	case string:
		if x == "" {
			return refuse(ReasonPlaceholder, "empty string at %s", path)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if secretKey.MatchString(k) {
				return refuse(ReasonSecretKey, "a key under %s matches the secret pattern", path)
			}
		}
		for _, k := range keys {
			if err := denyList(x[k], child(path, k)); err != nil {
				return err
			}
		}
	case []any:
		for i, e := range x {
			if err := denyList(e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
