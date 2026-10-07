package live

// This file is the publishing side of the output exchange (005 FR-005, FR-009; research R4;
// data-model *Output envelope*, *Live run record*; ADR-0004, KD-3): after an apply the lane turns
// the root's `tofu output -json` into the instance's outputs.json and writes it to the bucket the
// instance publishes to, and keeps each consumer's last-applied record of what it consumed.
//
// The publisher validates its own envelope as a consumer will (stacks.ValidateEnvelope with the
// producer's expectation, the KD-3 binding included) and, for `runtime`, checks scope.project_id
// against the bound project of its environment (T018 decision 2: nothing consumes runtime in this
// slice, so the exchange holds that binding here). A refused envelope never reaches the bucket.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// ObjectStore is the state bucket API of the exchange: S3 with the instance's state credentials
// live, a directory per bucket offline. Get of a missing object is an error matching
// fs.ErrNotExist.
type ObjectStore interface {
	Put(bucket, key string, data []byte) error
	Get(bucket, key string) ([]byte, error)
}

// PublishOptions names one applied instance and what it produced.
type PublishOptions struct {
	Manifest   *stacks.Manifest
	Instance   string // the applied instance id
	Revision   string // source_revision: the git object id of the applied tree
	TofuOutput []byte // `tofu output -json` of the applied root
	Schemas    fs.FS  // schemas/outputs
	Account    stacks.BoundAccount
	Store      ObjectStore
}

// Publish builds the instance's envelope (sensitive outputs dropped), validates it as its consumers
// will and writes it to stacks.ArtifactKey in stacks.ArtifactBucket. It returns the sha256 (hex) of
// the written object. Every check runs before the one Put.
func Publish(o PublishOptions) (string, error) {
	// Only a row is applied here; a spec.external producer is published by the repository that
	// applies it.
	if _, err := o.Manifest.Row(o.Instance); err != nil {
		return "", err
	}
	want, err := stacks.ExpectationOf(o.Manifest, o.Instance, o.Account)
	if err != nil {
		return "", err
	}
	env, err := stacks.BuildEnvelope(o.TofuOutput, stacks.Producer{InstanceID: want.InstanceID, Stage: want.Stage, SourceRevision: o.Revision})
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	checked, err := stacks.ValidateEnvelope(o.Schemas, data, want)
	if err != nil {
		return "", err
	}
	if want.Stage == "runtime" {
		if err := runtimeScopeBound(o.Manifest, want.InstanceID, o.Account, checked); err != nil {
			return "", err
		}
	}
	bucket, err := stacks.ArtifactBucket(o.Manifest, want.InstanceID)
	if err != nil {
		return "", err
	}
	if err := o.Store.Put(bucket, stacks.ArtifactKey(want.InstanceID), data); err != nil {
		return "", fmt.Errorf("publish the artefact of %s: %w", want.InstanceID, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// runtimeScopeBound refuses (unbound-project) a runtime envelope whose scope.project_id is not the
// bound project of the instance's own tenant and environment.
func runtimeScopeBound(m *stacks.Manifest, id string, a stacks.BoundAccount, env stacks.Envelope) error {
	b, err := stacks.InstanceBinding(m, id, a)
	if err != nil {
		return err
	}
	var scope struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(env.Values["scope"], &scope); err != nil {
		return &stacks.RefusalError{Reason: stacks.ReasonSchema, Detail: "values.scope does not decode"}
	}
	if scope.ProjectID != b.ProjectID {
		return &stacks.RefusalError{Reason: stacks.ReasonUnboundProject, Detail: "runtime scope.project_id of " + id + " differs from the bound reference"}
	}
	return nil
}

var (
	// projectIDPattern is an OVHcloud Public Cloud project id as account.env holds it.
	projectIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	// projectRefPattern is a manifest project ref (the <REF> of LZ_PROJECT_ID_<REF>).
	projectRefPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	// instanceIDPattern is a manifest instance id (schemas/deployments.schema.json).
	instanceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,62}$`)
)

// BoundAccountFromEnv reads the resolved references of the bound account from account.env's
// variables: the endpoint (OVH_ENDPOINT, required) and each LZ_PROJECT_ID_<REF>, whose ref must be
// upper case and whose id a 32-hex lower-case project id. A refusal names the variable, never its
// value.
func BoundAccountFromEnv(env map[string]string) (stacks.BoundAccount, error) {
	if env["OVH_ENDPOINT"] == "" {
		return stacks.BoundAccount{}, errors.New("account.env: no OVH_ENDPOINT")
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	acct := stacks.BoundAccount{Endpoint: env["OVH_ENDPOINT"], ProjectIDs: map[string]string{}}
	for _, k := range keys {
		ref, ok := strings.CutPrefix(k, "LZ_PROJECT_ID_")
		if !ok {
			continue
		}
		if !projectRefPattern.MatchString(ref) {
			return stacks.BoundAccount{}, fmt.Errorf("account.env: %s is not LZ_PROJECT_ID_<REF> with an upper-case ref", k)
		}
		if !projectIDPattern.MatchString(env[k]) {
			return stacks.BoundAccount{}, fmt.Errorf("account.env: %s is not a project id", k)
		}
		acct.ProjectIDs[ref] = env[k]
	}
	return acct, nil
}

// Record is an instance's last-applied record, `records/<id>.json` (data-model *Live run record*,
// FR-009): what selection compares the next run against.
type Record struct {
	AppliedAt      string            `json:"applied_at"`
	SourceRevision string            `json:"source_revision"`
	CodeDigest     string            `json:"code_digest"`
	Consumed       map[string]string `json:"consumed"`           // producer instance id → sha256 of its outputs.json
	Resolved       string            `json:"resolved,omitempty"` // sha256 of the resolved-reference input
}

// WriteRecord writes r as <dir>/<id>.json (0600), replacing an earlier record; an id that is not an
// instance id is refused before anything is written, so a record never lands outside dir.
func WriteRecord(dir, id string, r Record) error {
	if !instanceIDPattern.MatchString(id) {
		return fmt.Errorf("record id %q is not an instance id", id)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeRecord(filepath.Join(dir, id+".json"), data)
}
