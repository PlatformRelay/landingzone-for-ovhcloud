// This file is the envelope-to-input adapter (005 FR-005, FR-009; data-model *Envelope-to-input
// adapter*, *Resolved-reference input*; ADR-0004, KD-3): the one place a consumer stack receives
// values from another stack's state, as typed var files written from validated outputs.json
// artefacts and from the bound account's resolved project references.
//
// Every check runs before anything is written, and the set lands in a fresh directory in one
// rename: a refused consumer gets no input file at all, so a partial or stale input set is never
// planned. Refusal details never carry a value from an artefact, account.env or a project id.
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
	"sort"
	"strings"
)

// ErrBlocked is the outcome of a consumer whose producer has published no artefact (FR-009): the
// consumer is blocked, never planned with a placeholder.
var ErrBlocked = errors.New("blocked: the producer has published no outputs.json")

// ReasonTenantEnvironments refuses account-governance's resolved input for a tenant that does not
// have exactly one environment: the stage takes one project per tenant (known slice limit,
// coordinator decision 2026-10-07; data-model *Resolved-reference input*).
const ReasonTenantEnvironments = "tenant-environments"

// ResolvedFile is the name of the resolved-reference input in a consumer's input directory.
const ResolvedFile = "resolved.tfvars.json"

// ArtifactKey is the object key of an instance's published outputs.json (ADR-0004 `artifacts/`).
func ArtifactKey(instanceID string) string {
	return "artifacts/" + instanceID + "/outputs.json"
}

// ArtifactBucket is the bucket producer id (a manifest row or a spec.external entry) publishes its
// artefact to: its own state bucket, and for the local-state bootstrap the account state bucket it
// creates (FR-008). The name comes from the manifest's one bucket rule (stateBucket).
func ArtifactBucket(m *Manifest, id string) (string, error) {
	p, err := m.producer(id)
	if err != nil {
		return "", err
	}
	st, ok := stageNamed(p.Stage)
	if !ok {
		return "", fmt.Errorf("producer %s has no stage %s", id, p.Stage)
	}
	return stateBucket(m.Org, p.Tenant, st), nil
}

// Row is the manifest row with id, the only instances this repository plans, applies and
// publishes; an id that is not a row (spec.external included) is an error naming it.
func (m *Manifest) Row(id string) (Instance, error) {
	for _, in := range m.Instances {
		if in.ID == id {
			return in, nil
		}
	}
	return Instance{}, fmt.Errorf("no instance %s in the manifest", id)
}

// producer is the manifest row or spec.external entry with id: what a consumer may read from.
func (m *Manifest) producer(id string) (Instance, error) {
	if in, err := m.Row(id); err == nil {
		return in, nil
	}
	for _, e := range m.External {
		if e.ID == id {
			return Instance{ID: e.ID, Stage: e.Stage, Tenant: e.Tenant, Environment: e.Environment, Region: e.Region, Slot: e.Slot}, nil
		}
	}
	return Instance{}, fmt.Errorf("no instance or external entry %s in the manifest", id)
}

// projectRef is the manifest project ref of a tenant's environment; "" when there is none.
func (m *Manifest) projectRef(tenant, environment string) string {
	for _, t := range m.Tenants {
		if t.Name != tenant {
			continue
		}
		for _, e := range t.Environments {
			if e.Name == environment {
				return e.ProjectRef
			}
		}
	}
	return ""
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

// urnRegion is the URN region segment per API endpoint. Only ovh-eu is known (research R6 policy
// example); any other endpoint is refused rather than guessed (UNVERIFIED for the others).
var urnRegion = map[string]string{"ovh-eu": "eu"}

// Binding resolves a manifest project ref to the bound project id and its URN; an unknown endpoint
// or an unresolved ref is an unbound-project refusal.
func (a BoundAccount) Binding(ref string) (ProjectBinding, error) {
	region, ok := urnRegion[a.Endpoint]
	if !ok {
		return ProjectBinding{}, refuse(ReasonUnboundProject, "the bound account's API endpoint has no known project URN form")
	}
	id := a.ProjectIDs[ref]
	if ref == "" || id == "" {
		return ProjectBinding{}, refuse(ReasonUnboundProject, "project ref %q is not resolved by the bound account", ref)
	}
	return ProjectBinding{ProjectID: id, ProjectURN: "urn:v1:" + region + ":resource:publicCloudProject:" + id}, nil
}

// InstanceBinding is the bound project of id's own tenant and environment in the manifest (a row
// or a spec.external entry; KD-3): never one an artefact or an output names.
func InstanceBinding(m *Manifest, id string, a BoundAccount) (ProjectBinding, error) {
	in, err := m.producer(id)
	if err != nil {
		return ProjectBinding{}, err
	}
	return a.Binding(m.projectRef(in.Tenant, in.Environment))
}

// ExpectationOf is what an artefact of producer id (a row or a spec.external entry) must match:
// its instance id and stage and, for a `project` producer, the bound project of its tenant and
// environment.
func ExpectationOf(m *Manifest, id string, a BoundAccount) (Expectation, error) {
	in, err := m.producer(id)
	if err != nil {
		return Expectation{}, err
	}
	want := Expectation{InstanceID: in.ID, Stage: in.Stage}
	if in.Stage == "project" {
		b, err := InstanceBinding(m, id, a)
		if err != nil {
			return Expectation{}, err
		}
		want.Project = &b
	}
	return want, nil
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

// Adapt writes the consumer's inputs: one `<stage>.tfvars.json` per data producer, holding exactly
// {"<stage, - → _>": values} of the validated artefact, and, for a stage that takes resolved
// references, resolved.tfvars.json. A missing artefact wraps ErrBlocked and names the producer; a
// refused artefact or reference is a *RefusalError. Nothing is written unless every check passed.
func Adapt(opts AdaptOptions) (Inputs, error) {
	m := opts.Manifest
	consumer, err := m.Row(opts.Consumer)
	if err != nil {
		return Inputs{}, err
	}
	files := map[string][]byte{}
	consumed := map[string]string{}
	// The decoder derives exactly one producer per data stage of a row (UNRESOLVED_PRODUCER
	// otherwise), so the per-stage file names cannot collide.
	for _, e := range consumer.Edges {
		if e.Kind != EdgeData {
			continue
		}
		name, body, digest, err := producerInput(opts, e.Producer)
		if err != nil {
			return Inputs{}, err
		}
		files[name] = body
		consumed[e.Producer] = digest
	}
	resolved, err := resolvedInput(m, consumer, opts.Account)
	if err != nil {
		return Inputs{}, err
	}
	out := Inputs{Consumed: consumed}
	if resolved != nil {
		body, err := json.Marshal(resolved)
		if err != nil {
			return Inputs{}, err
		}
		files[ResolvedFile] = body
		out.Resolved = digestOf(body)
	}
	written, err := writeInputs(opts.Dir, files)
	if err != nil {
		return Inputs{}, err
	}
	out.Files = written
	return out, nil
}

// producerInput reads and validates producer id's artefact and returns its var file name, body and
// the sha256 of the consumed outputs.json.
func producerInput(opts AdaptOptions, id string) (string, []byte, string, error) {
	want, err := ExpectationOf(opts.Manifest, id, opts.Account)
	if err != nil {
		return "", nil, "", err
	}
	bucket, err := ArtifactBucket(opts.Manifest, id)
	if err != nil {
		return "", nil, "", err
	}
	data, err := opts.Store.Get(bucket, ArtifactKey(id))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, "", fmt.Errorf("%s: %w", id, ErrBlocked)
	}
	if err != nil {
		return "", nil, "", fmt.Errorf("read the artefact of %s: %w", id, err)
	}
	env, err := ValidateEnvelope(opts.Schemas, data, want)
	if err != nil {
		return "", nil, "", err
	}
	body, err := json.Marshal(map[string]map[string]json.RawMessage{strings.ReplaceAll(want.Stage, "-", "_"): env.Values})
	if err != nil {
		return "", nil, "", err
	}
	return want.Stage + ".tfvars.json", body, digestOf(data), nil
}

// resolvedInput is the resolved-reference input of in's stage, nil for a stage that takes none:
// bootstrap and tenant-state take state_project_id (the state ref), account-governance tenants
// {<t>: {project_id, project_urn}} for every manifest tenant, project its environment's
// project_id.
func resolvedInput(m *Manifest, in Instance, a BoundAccount) (map[string]any, error) {
	switch in.Stage {
	case "bootstrap", "tenant-state":
		b, err := a.Binding(m.StateProjectRef)
		if err != nil {
			return nil, err
		}
		return map[string]any{"state_project_id": b.ProjectID}, nil
	case "project":
		b, err := InstanceBinding(m, in.ID, a)
		if err != nil {
			return nil, err
		}
		return map[string]any{"project_id": b.ProjectID}, nil
	case "account-governance":
		tenants := map[string]any{}
		for _, t := range m.Tenants {
			if len(t.Environments) != 1 {
				return nil, refuse(ReasonTenantEnvironments, "tenant %s has %d environments; account-governance takes one project per tenant", t.Name, len(t.Environments))
			}
			b, err := a.Binding(t.Environments[0].ProjectRef)
			if err != nil {
				return nil, err
			}
			tenants[t.Name] = map[string]string{"project_id": b.ProjectID, "project_urn": b.ProjectURN}
		}
		return map[string]any{"tenants": tenants}, nil
	}
	return nil, nil
}

// writeInputs writes files (0600) into dir and returns their paths in name order. dir must be
// absent or empty: an earlier run's inputs are never mixed with these. The set is written into a
// private (0700) temporary sibling and renamed into place, so dir holds the whole set or nothing.
func writeInputs(dir string, files map[string][]byte) ([]string, error) {
	entries, err := os.ReadDir(dir)
	switch {
	case err == nil && len(entries) > 0:
		return nil, fmt.Errorf("input directory %s is not empty: inputs go into a fresh directory only", dir)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	existed := err == nil
	if len(files) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+"-")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(tmp, n), files[n], 0o600); err != nil {
			_ = os.RemoveAll(tmp)
			return nil, err
		}
	}
	if existed {
		_ = os.Remove(dir) // empty (checked above); os.Rename does not replace a directory, so a failed Remove fails the Rename
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	written := make([]string, 0, len(names))
	for _, n := range names {
		written = append(written, filepath.Join(dir, n))
	}
	return written, nil
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
