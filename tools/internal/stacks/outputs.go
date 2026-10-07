// Package stacks holds the stack model of 005: the stage table, the manifest, generation and the
// output exchange between stacks (ADR-0004, data-model *Output envelope*).
//
// This file is the compiling stub of 005 T017: the signatures the output-contract tests pin, with
// pass-through bodies. T018 implements the envelope builder and validator against
// schemas/outputs/*.schema.json.
package stacks

import (
	"encoding/json"
	"io/fs"
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
	ReasonPlaceholder    = "placeholder"     // null or "" where a capability or optional value is absent
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

// BuildEnvelope turns the pinned `tofu output -json` document of an applied root into the
// envelope for producer p, dropping every entry tofu marked sensitive.
func BuildEnvelope(tofuOutput []byte, p Producer) (Envelope, error) {
	var raw map[string]struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(tofuOutput, &raw); err != nil {
		return Envelope{}, err
	}
	values := map[string]json.RawMessage{}
	for name, entry := range raw {
		values[name] = entry.Value
	}
	return Envelope{
		APIVersion: OutputsAPIVersion, Kind: OutputsKind,
		InstanceID: p.InstanceID, Stage: p.Stage, SourceRevision: p.SourceRevision,
		Values: values,
	}, nil
}

// ValidateEnvelope checks a published outputs.json against the envelope rules, the stage schema
// `<stage>.schema.json` in schemas and the expectation, and returns the decoded envelope.
func ValidateEnvelope(schemas fs.FS, data []byte, want Expectation) (Envelope, error) {
	var env Envelope
	err := json.Unmarshal(data, &env)
	return env, err
}
