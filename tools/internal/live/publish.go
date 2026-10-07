package live

// This file is the publishing side of the output exchange (005 FR-005, FR-009; research R4;
// data-model *Output envelope*, *Live run record*; ADR-0004): after an apply the lane turns the
// root's `tofu output -json` into the instance's outputs.json and writes it to the bucket the
// instance publishes to, and keeps each consumer's last-applied record of what it consumed.
//
// T060 stub: a pass-through that checks nothing (every output, sensitive or not, reaches the
// object), so the T060 controls are red for their refusals. T061 replaces it. Nothing calls it yet.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path/filepath"
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

// Publish builds the instance's envelope, validates it as its consumers will and writes it to
// stacks.ArtifactKey in stacks.ArtifactBucket. It returns the sha256 (hex) of the written object.
func Publish(o PublishOptions) (string, error) {
	var stage string
	for _, in := range o.Manifest.Instances {
		if in.ID == o.Instance {
			stage = in.Stage
		}
	}
	var entries map[string]struct {
		Value json.RawMessage `json:"value"`
	}
	_ = json.Unmarshal(o.TofuOutput, &entries)
	values := map[string]json.RawMessage{}
	for name, e := range entries {
		values[name] = e.Value
	}
	data, err := json.Marshal(stacks.Envelope{
		APIVersion: stacks.OutputsAPIVersion, Kind: stacks.OutputsKind,
		InstanceID: o.Instance, Stage: stage, SourceRevision: o.Revision, Values: values,
	})
	if err != nil {
		return "", err
	}
	bucket, _ := stacks.ArtifactBucket(o.Manifest, o.Instance)
	if err := o.Store.Put(bucket, stacks.ArtifactKey(o.Instance), data); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// BoundAccountFromEnv reads the resolved references of the bound account from account.env's
// variables: the endpoint (OVH_ENDPOINT) and each LZ_PROJECT_ID_<REF>.
func BoundAccountFromEnv(env map[string]string) (stacks.BoundAccount, error) {
	acct := stacks.BoundAccount{Endpoint: env["OVH_ENDPOINT"], ProjectIDs: map[string]string{}}
	for k, v := range env {
		if ref, ok := strings.CutPrefix(k, "LZ_PROJECT_ID_"); ok {
			acct.ProjectIDs[ref] = v
		}
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

// WriteRecord writes r as <dir>/<id>.json, replacing an earlier record.
func WriteRecord(dir, id string, r Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeRecord(filepath.Join(dir, id+".json"), data)
}
