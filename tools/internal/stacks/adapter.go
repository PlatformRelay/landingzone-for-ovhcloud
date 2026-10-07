// This file is the envelope-to-input adapter (005 FR-005, FR-009; data-model *Envelope-to-input
// adapter*, *Resolved-reference input*; ADR-0004): the one place a consumer stack receives values
// from another stack's state, as typed var files written from validated outputs.json artefacts and
// from the bound account's resolved project references.
//
// T060 stub: a pass-through that checks nothing, so the T060 controls are red for their refusals.
// T061 replaces it. Nothing calls it yet.
package stacks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrBlocked is the outcome of a consumer whose producer has published no artefact (FR-009): the
// consumer is blocked, never planned with a placeholder.
var ErrBlocked = errors.New("blocked: the producer has published no outputs.json")

// ArtifactKey is the object key of an instance's published outputs.json (ADR-0004 `artifacts/`).
func ArtifactKey(instanceID string) string {
	return "artifacts/" + instanceID + "/outputs.json"
}

// ArtifactBucket is the bucket instance id publishes its artefact to: its own state bucket, and
// for the local-state bootstrap the account state bucket it creates (FR-008).
func ArtifactBucket(m *Manifest, id string) (string, error) {
	for _, in := range m.Instances {
		if in.ID == id {
			return in.StateBucket, nil
		}
	}
	return "", fmt.Errorf("no instance %s in the manifest", id)
}

// ArtifactStore reads published objects; a missing object is an error matching fs.ErrNotExist.
type ArtifactStore interface {
	Get(bucket, key string) ([]byte, error)
}

// BoundAccount is what references resolve against: the bound account's API endpoint and its
// project ids by manifest ref (`LZ_PROJECT_ID_<REF>` of account.env, read by the live lane).
type BoundAccount struct {
	Endpoint   string            // e.g. ovh-eu
	ProjectIDs map[string]string // manifest project ref → project id
}

// AdaptOptions configures the inputs of one consumer instance.
type AdaptOptions struct {
	Manifest *Manifest
	Consumer string        // the consumer's instance id
	Store    ArtifactStore // its producers' state buckets
	Schemas  fs.FS         // schemas/outputs
	Account  BoundAccount
	Dir      string // the consumer's input directory (.local/live/<run-id>/inputs/<consumer>/)
}

// Inputs is what Adapt wrote for one consumer.
type Inputs struct {
	Files    []string          // the var files written in Dir, sorted; each is passed with -var-file
	Consumed map[string]string // producer instance id → sha256 (hex) of the outputs.json consumed
	Resolved string            // sha256 (hex) of resolved.tfvars.json; "" when the stage takes none
}

// Adapt writes the consumer's inputs: one `<stage>.tfvars.json` per data producer and, for a stage
// that takes resolved references, `resolved.tfvars.json`.
func Adapt(opts AdaptOptions) (Inputs, error) {
	var consumer *Instance
	stageOf := map[string]string{}
	for i, in := range opts.Manifest.Instances {
		stageOf[in.ID] = in.Stage
		if in.ID == opts.Consumer {
			consumer = &opts.Manifest.Instances[i]
		}
	}
	if consumer == nil {
		return Inputs{}, fmt.Errorf("no instance %s in the manifest", opts.Consumer)
	}
	out := Inputs{Consumed: map[string]string{}}
	for _, e := range consumer.Edges {
		if e.Kind != EdgeData {
			continue
		}
		bucket, _ := ArtifactBucket(opts.Manifest, e.Producer)
		data, err := opts.Store.Get(bucket, ArtifactKey(e.Producer))
		if err != nil {
			return Inputs{}, err
		}
		var doc struct {
			Values json.RawMessage `json:"values"`
		}
		_ = json.Unmarshal(data, &doc)
		stage := stageOf[e.Producer]
		file := filepath.Join(opts.Dir, stage+".tfvars.json")
		body := []byte(`{"` + strings.ReplaceAll(stage, "-", "_") + `":` + string(doc.Values) + `}`)
		if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
			return Inputs{}, err
		}
		if err := os.WriteFile(file, body, 0o600); err != nil {
			return Inputs{}, err
		}
		sum := sha256.Sum256(data)
		out.Consumed[e.Producer] = hex.EncodeToString(sum[:])
		out.Files = append(out.Files, file)
	}
	return out, nil
}
