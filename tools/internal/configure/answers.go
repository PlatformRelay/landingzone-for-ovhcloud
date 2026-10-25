// Package configure turns an answers file into a deployments.yaml (spec 004, D127).
//
// This file is the answers file (FR-001, FR-008, data-model.md *Answers file*): its Go types,
// which together with that table are its definition (no JSON Schema is published), its strict
// decoding and the reader that admits only a regular file.
//
// The answers file is YAML written in its JSON-compatible flow form, as stacks/deployments.yaml
// is, decoded strictly through checks.DecodeStrict; whole-line `#` comments are allowed. A shared
// field keeps its own copy of the pattern the manifest gives the field it maps to
// (answerRules); TestAnswersPatternsMatchManifestSchema holds each copy equal to
// schemas/deployments.schema.json, so a drifted copy cannot let a value through that the
// manifest decoder would then refuse under the manifest's path.
//
// The package opens no network connection, runs nothing and reads no environment variable
// (FR-008; TestAnswersSourceHasNoAuthority).
package configure

import (
	"errors"
	"fmt"
	"regexp"
)

// The answers file's fixed header values and its size bound (FR-001).
const (
	AnswersAPIVersion = "lz.platformrelay.dev/v1alpha1"
	AnswersKind       = "DeploymentAnswers"
	MaxAnswersBytes   = 64 << 10
)

// Refusal codes of DecodeAnswers and ReadAnswers (data-model.md *Refusal message*). When a
// document breaks several rules, the first in this order is reported: size, syntax and duplicate
// keys (in document order), unknown, missing or null fields (keys in sorted order), a JSON type
// mismatch, the header, value rules (keys in sorted order), then a VLAN id without a CIDR.
const (
	CodeAnswersSyntax      = "ANSWERS_SYNTAX"      // not one JSON-compatible document
	CodeAnswersTooLarge    = "ANSWERS_TOO_LARGE"   // more than MaxAnswersBytes
	CodeAnswersNotRegular  = "ANSWERS_NOT_REGULAR" // a symlink, a directory, another non-regular file or missing
	CodeDuplicateKey       = "DUPLICATE_KEY"       // a key twice in one object
	CodeUnknownField       = "UNKNOWN_FIELD"       // a key the answers file does not have, or a case variant
	CodeMissingField       = "MISSING_FIELD"       // a required key absent or null
	CodeUnsupportedVersion = "UNSUPPORTED_VERSION" // apiVersion or kind other than the constants
	CodeInvalidValue       = "INVALID_VALUE"       // pattern, enum or range violated; a JSON type mismatch; vlan_id without cidr
)

// AnswersError is the error of a refused answers file; Code is one of the Code constants.
// Detail is the field path of a field error (tenants[0].environments[0].regions[0].cidr), the
// line of a syntax error (line 9), the size of an oversized document, or the file path of an
// error of the file itself.
type AnswersError struct {
	Code   string
	Detail string
}

func (e *AnswersError) Error() string { return e.Code + ": " + e.Detail }

// Answers is a decoded answers file. Defaults are not applied here: an omitted optional string
// is "", an omitted vlan_id nil, an omitted boolean false (data-model.md gives the defaults the
// derivation applies). Every object is closed and a field without omitempty is required, as
// checks.DecodeStrict reads the tags.
type Answers struct {
	APIVersion         string   `json:"apiVersion"`
	Kind               string   `json:"kind"`
	Name               string   `json:"name"`
	Org                string   `json:"org"`
	Forge              string   `json:"forge"`
	StateRegion        string   `json:"state_region"`
	StateEndpoint      string   `json:"state_endpoint,omitempty"`
	StateRef           string   `json:"state_ref,omitempty"`
	SharedStateProject bool     `json:"shared_state_project,omitempty"`
	Tenants            []Tenant `json:"tenants"`
}

// Tenant is one tenants entry.
type Tenant struct {
	Name         string        `json:"name"`
	Environments []Environment `json:"environments"`
}

// Environment is one environment of a tenant: its project and regions.
type Environment struct {
	Name        string   `json:"name"`
	Regions     []Region `json:"regions"`
	ProjectRef  string   `json:"project_ref,omitempty"`
	ProjectMode string   `json:"project_mode,omitempty"`
	BudgetAlert bool     `json:"budget_alert,omitempty"`
	QuotaGuard  bool     `json:"quota_guard,omitempty"`
}

// Region is one region of an environment; without CIDR it has no network row.
type Region struct {
	Name   string `json:"name"`
	CIDR   string `json:"cidr,omitempty"`
	VlanID *int   `json:"vlan_id,omitempty"` // nil when omitted; a scalar pointer, so DecodeStrict still refuses null
}

// fieldRule is the value rule of one answers field and the manifest field it maps to. Paths
// join object keys with "." and mark an array element with "[]". A rule holds exactly one of a
// pattern, an enum or a range.
type fieldRule struct {
	Answers  string
	Manifest string // its path in schemas/deployments.schema.json
	Pattern  string
	Enum     []string
	Range    []int // inclusive minimum and maximum of a number
}

// Copies of the manifest's patterns (tools/internal/stacks/manifest.go), held equal to
// schemas/deployments.schema.json by TestAnswersPatternsMatchManifestSchema.
const (
	metadataNamePattern  = `^[a-z][a-z0-9-]{0,62}$`
	segmentPattern       = `^[a-z][a-z0-9]{0,15}$`
	regionPattern        = `^[A-Z]+[0-9]+$`
	projectRefPattern    = `^[A-Z][A-Z0-9_]{0,62}$`
	forgePattern         = `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(/[A-Za-z0-9_-][A-Za-z0-9._-]*)+$`
	storageRegionPattern = `^[a-z]+$`
	endpointPattern      = `^https://[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`
	cidrPattern          = `^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$`
)

// answerRules holds the rule of every value of the answers file except the header constants and
// the booleans.
var answerRules = func() []fieldRule {
	env, menv := "tenants[].environments[]", "spec.tenants[].environments[]"
	return []fieldRule{
		{Answers: "name", Manifest: "metadata.name", Pattern: metadataNamePattern},
		{Answers: "org", Manifest: "spec.org", Pattern: segmentPattern},
		{Answers: "forge", Manifest: "spec.forge", Pattern: forgePattern},
		{Answers: "state_region", Manifest: "spec.state.region", Pattern: storageRegionPattern},
		{Answers: "state_endpoint", Manifest: "spec.state.endpoint", Pattern: endpointPattern},
		{Answers: "state_ref", Manifest: "spec.state.project.ref", Pattern: projectRefPattern},
		{Answers: "tenants[].name", Manifest: "spec.tenants[].name", Pattern: segmentPattern},
		{Answers: env + ".name", Manifest: menv + ".name", Pattern: segmentPattern},
		{Answers: env + ".project_ref", Manifest: menv + ".project.ref", Pattern: projectRefPattern},
		{Answers: env + ".project_mode", Manifest: menv + ".project.mode", Enum: []string{"adopt", "reference"}},
		{Answers: env + ".regions[].name", Manifest: menv + ".regions[].name", Pattern: regionPattern},
		{Answers: env + ".regions[].cidr", Manifest: menv + ".regions[].network.cidr", Pattern: cidrPattern},
		{Answers: env + ".regions[].vlan_id", Manifest: menv + ".regions[].network.vlan_id", Range: []int{0, 4000}},
	}
}()

// rulesByPath indexes answerRules by answers path, with each pattern compiled once.
var rulesByPath = func() map[string]compiledRule {
	index := map[string]compiledRule{}
	for _, r := range answerRules {
		c := compiledRule{fieldRule: r}
		if r.Pattern != "" {
			c.re = regexp.MustCompile(r.Pattern)
		}
		index[r.Answers] = c
	}
	return index
}()

type compiledRule struct {
	fieldRule
	re *regexp.Regexp
}

func refuse(code, format string, a ...any) error {
	return &AnswersError{Code: code, Detail: fmt.Sprintf(format, a...)}
}

var errNotImplemented = errors.New("not implemented")

// DecodeAnswers decodes an answers file strictly. It applies no default.
func DecodeAnswers(data []byte) (Answers, error) {
	return Answers{}, errNotImplemented
}

// ReadAnswers reads the answers file at path: a regular file, never followed through a symlink,
// of at most MaxAnswersBytes (FR-008). It does not decode it.
func ReadAnswers(path string) ([]byte, error) {
	return nil, errNotImplemented
}
