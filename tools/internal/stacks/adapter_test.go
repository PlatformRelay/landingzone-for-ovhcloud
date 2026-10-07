//go:build offlinetools

package stacks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Envelope-to-input adapter controls of 005 T060 (FR-005, FR-009, FR-013; V012; ADR-0004,
// ADR-0007, ADR-0017; data-model *Envelope-to-input adapter*, *Resolved-reference input*; KD-3).
// The adapter is the trust boundary between independent states: a consumer reads values another
// stack published, so every control here is about what it refuses and what it never lets through.
// The producer round trip on generated roots (apply, publish, adapt, plan) is in
// internal/live/publish_test.go (TestExchange*).
//
// Readings pinned here (evidence/T060.md):
//   - an artefact lives at ArtifactKey(id) = artifacts/<id>/outputs.json in ArtifactBucket(id): the
//     instance's own state bucket, the account state bucket for the local-state bootstrap; an
//     artefact anywhere else is not the producer's and leaves the consumer blocked;
//   - a data producer's file is <stage>.tfvars.json holding exactly {"<stage, - → _>": values};
//   - the record digest of a producer is the sha256 of the outputs.json consumed (FR-009 "consumed
//     outputs.json digest"), the resolved-reference digest the sha256 of resolved.tfvars.json;
//   - resolved references per stage: bootstrap and tenant-state take state_project_id (the state
//     ref), account-governance tenants {<t>: {project_id, project_urn}} for every manifest tenant,
//     project its environment's project_id; the URN is the endpoint's
//     urn:v1:<eu>:resource:publicCloudProject:<id> (research R6 policy example, ovh-eu only);
//   - the refusal reason follows T018's validator order (malformed, header, producer, deny-list,
//     schema, project binding), so a secret-pattern key that is also an unknown field reports
//     secret-key;
//   - a refused consumer gets no input file at all: a partial set must not be planned.

const (
	adaptStateID   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	adaptRuntimeID = "demo-dev-gra11-runtime"
	adaptProjectID = "demo-dev-project"
	tenantBucket   = "lz-demo-bkt-state"
	accountBucket  = "lz-bkt-state"
)

// dirStore is a scratch bucket directory: <root>/<bucket>/<key>.
type dirStore struct{ root string }

func (s dirStore) Get(bucket, key string) ([]byte, error) {
	if bucket == "" || strings.Contains(bucket, "/") {
		return nil, fs.ErrNotExist
	}
	return os.ReadFile(filepath.Join(s.root, bucket, filepath.FromSlash(key)))
}

func (s dirStore) put(t *testing.T, bucket, key string, data []byte) {
	t.Helper()
	writeFile(t, filepath.Join(s.root, bucket, filepath.FromSlash(key)), data)
}

// adaptManifest is the sandbox fixture with distinct state and environment project refs, so a
// swapped reference cannot pass.
func adaptManifest(t *testing.T) *Manifest {
	t.Helper()
	data := edit(t, manifestOf(t, "sandbox"), `"project": {"mode": "reference", "ref": "SANDBOX"}`, `"project": {"mode": "reference", "ref": "DEMO_DEV"}`, 1)
	return decoded(t, data)
}

func adaptAccount() BoundAccount {
	return BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"SANDBOX": adaptStateID, "DEMO_DEV": boundID}}
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// adapt runs Adapt for consumer against store into a fresh directory and returns the result, the
// directory and the error.
func adapt(t *testing.T, m *Manifest, consumer string, store dirStore, acct BoundAccount) (Inputs, string, error) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "inputs", consumer)
	in, err := Adapt(AdaptOptions{Manifest: m, Consumer: consumer, Store: store, Schemas: schemas(), Account: acct, Dir: dir})
	return in, dir, err
}

// dirFiles lists the regular files under dir (relative, sorted); a missing dir is empty.
func dirFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

func decodeJSON(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	return v
}

// The artefact location: key and bucket per instance; the local-state bootstrap publishes to the
// account bucket it creates.
func TestAdapterArtifactLocation(t *testing.T) {
	m := adaptManifest(t)
	if got, want := ArtifactKey(adaptProjectID), "artifacts/demo-dev-project/outputs.json"; got != want {
		t.Errorf("ArtifactKey = %q, want %q", got, want)
	}
	want := map[string]string{
		"account-bootstrap": accountBucket, "account-governance": accountBucket, "demo-state": accountBucket,
		adaptProjectID: tenantBucket, "demo-dev-gra11-network": tenantBucket, adaptRuntimeID: tenantBucket,
	}
	for id, bucket := range want {
		got, err := ArtifactBucket(m, id)
		if err != nil || got != bucket {
			t.Errorf("ArtifactBucket(%s) = %q, %v; want %q", id, got, err, bucket)
		}
	}
	if got, err := ArtifactBucket(m, "demo-prod-project"); err == nil {
		t.Errorf("ArtifactBucket of an instance not in the manifest = %q, want an error", got)
	}
}

// A data producer's validated values become the consumer's typed var file, nothing else of the
// artefact does, and the record digest is the consumed artefact's.
func TestAdapterWritesProducerInputs(t *testing.T) {
	m := adaptManifest(t)
	artefact := readFile(t, fixtureDir+"/envelopes/project.json")
	for _, consumer := range []string{adaptRuntimeID, "demo-dev-gra11-network"} {
		t.Run(consumer, func(t *testing.T) {
			store := dirStore{t.TempDir()}
			store.put(t, tenantBucket, ArtifactKey(adaptProjectID), artefact)
			in, dir, err := adapt(t, m, consumer, store, adaptAccount())
			if err != nil {
				t.Fatalf("Adapt: %v", err)
			}
			if got := dirFiles(t, dir); !reflect.DeepEqual(got, []string{"project.tfvars.json"}) {
				t.Fatalf("input files %v, want [project.tfvars.json]", got)
			}
			file := filepath.Join(dir, "project.tfvars.json")
			if !reflect.DeepEqual(in.Files, []string{file}) {
				t.Errorf("Files = %v, want [%s]", in.Files, file)
			}
			want := map[string]any{"project": values(fixtureDoc(t, "project"))}
			if got := decodeJSON(t, readFile(t, file)); !reflect.DeepEqual(got, any(want)) {
				t.Errorf("project.tfvars.json = %v, want %v", got, want)
			}
			if !reflect.DeepEqual(in.Consumed, map[string]string{adaptProjectID: sha(artefact)}) {
				t.Errorf("Consumed = %v, want {%s: sha256 of the artefact}", in.Consumed, adaptProjectID)
			}
			if in.Resolved != "" {
				t.Errorf("Resolved = %q for a stage that takes no resolved reference", in.Resolved)
			}
		})
	}
}

// The resolved-reference input of each stage that takes one, from the bound account, and its
// digest; a changed reference changes the digest; a stage with no data producer consumes nothing.
func TestAdapterResolvedInputs(t *testing.T) {
	m := adaptManifest(t)
	urn := "urn:v1:eu:resource:publicCloudProject:" + boundID
	cases := map[string]map[string]any{
		"account-bootstrap":  {"state_project_id": adaptStateID},
		"demo-state":         {"state_project_id": adaptStateID},
		"account-governance": {"tenants": map[string]any{"demo": map[string]any{"project_id": boundID, "project_urn": urn}}},
		adaptProjectID:       {"project_id": boundID},
	}
	for consumer, want := range cases {
		t.Run(consumer, func(t *testing.T) {
			in, dir, err := adapt(t, m, consumer, dirStore{t.TempDir()}, adaptAccount())
			if err != nil {
				t.Fatalf("Adapt: %v", err)
			}
			if got := dirFiles(t, dir); !reflect.DeepEqual(got, []string{"resolved.tfvars.json"}) {
				t.Fatalf("input files %v, want [resolved.tfvars.json]", got)
			}
			file := filepath.Join(dir, "resolved.tfvars.json")
			data := readFile(t, file)
			if got := decodeJSON(t, data); !reflect.DeepEqual(got, any(want)) {
				t.Errorf("resolved.tfvars.json = %v, want %v", got, want)
			}
			if !reflect.DeepEqual(in.Files, []string{file}) {
				t.Errorf("Files = %v, want [%s]", in.Files, file)
			}
			if in.Resolved != sha(data) {
				t.Errorf("Resolved = %q, want the sha256 of resolved.tfvars.json %q", in.Resolved, sha(data))
			}
			if len(in.Consumed) != 0 {
				t.Errorf("Consumed = %v, want none (no data producer)", in.Consumed)
			}
			changed := adaptAccount()
			changed.ProjectIDs = map[string]string{"SANDBOX": foreignID, "DEMO_DEV": foreignID}
			again, _, err := adapt(t, m, consumer, dirStore{t.TempDir()}, changed)
			if err != nil || again.Resolved == "" || again.Resolved == in.Resolved {
				t.Errorf("a changed LZ_PROJECT_ID_<REF> gives Resolved %q (%v), want a digest other than %q", again.Resolved, err, in.Resolved)
			}
		})
	}
}

// A reference the bound account does not resolve, or an endpoint without a known URN form, is
// refused, and nothing is written.
func TestAdapterRefusesUnresolvedReference(t *testing.T) {
	m := adaptManifest(t)
	for name, acct := range map[string]BoundAccount{
		"ref-missing": {Endpoint: "ovh-eu", ProjectIDs: map[string]string{"SANDBOX": adaptStateID}},
		"ref-empty":   {Endpoint: "ovh-eu", ProjectIDs: map[string]string{"SANDBOX": adaptStateID, "DEMO_DEV": ""}},
		"endpoint":    {Endpoint: "ovh-xx", ProjectIDs: map[string]string{"SANDBOX": adaptStateID, "DEMO_DEV": boundID}},
	} {
		// Every path that resolves a reference: the governance map, a project's own id, the state
		// ref of bootstrap (only "endpoint" breaks it), and the binding a project artefact is
		// checked against when runtime consumes it.
		for _, consumer := range []string{"account-governance", adaptProjectID, "account-bootstrap", adaptRuntimeID} {
			if consumer == "account-bootstrap" && name != "endpoint" {
				continue
			}
			t.Run(name+"/"+consumer, func(t *testing.T) {
				store := dirStore{t.TempDir()}
				store.put(t, tenantBucket, ArtifactKey(adaptProjectID), readFile(t, fixtureDir+"/envelopes/project.json"))
				_, dir, err := adapt(t, m, consumer, store, acct)
				refusal(t, err, ReasonUnboundProject)
				if files := dirFiles(t, dir); len(files) > 0 {
					t.Errorf("refused, but wrote %v", files)
				}
			})
		}
	}
}

// artefactCase is one artefact the runtime consumer must refuse.
type artefactCase struct {
	name    string
	bucket  string               // where the artefact is stored; tenantBucket when empty
	key     string               // its key; the producer's ArtifactKey when empty
	raw     []byte               // the stored bytes, when not built from the fixture
	mutate  func(map[string]any) // a change to the project fixture envelope
	reason  string               // the RefusalError reason; "" for blocked
	blocked bool
}

var artefactCases = []artefactCase{
	{name: "missing", raw: nil, blocked: true},
	{name: "other-bucket", bucket: accountBucket, blocked: true},
	{name: "other-key", key: "artifacts/demo-dev-project/outputs.json.bak", blocked: true},
	{name: "other-instance-key", key: "artifacts/demo-prod-project/outputs.json", blocked: true},
	{name: "malformed", raw: []byte(`{"apiVersion": "lz.platformrelay.dev/v1alpha1",`), reason: ReasonMalformed},
	{name: "two-documents", raw: []byte(`{} {}`), reason: ReasonMalformed},
	{name: "header-unknown", mutate: func(d map[string]any) { d["generation"] = "1" }, reason: ReasonEnvelope},
	{name: "header-version", mutate: func(d map[string]any) { d["apiVersion"] = "lz.platformrelay.dev/v2" }, reason: ReasonEnvelope},
	{name: "schema-unknown-field", mutate: func(d map[string]any) { values(d)["zone"] = "a" }, reason: ReasonSchema},
	{name: "schema-missing-field", mutate: func(d map[string]any) { delete(values(d), "project_urn") }, reason: ReasonSchema},
	{name: "schema-wrong-type", mutate: func(d map[string]any) { values(d)["regions"] = "GRA11" }, reason: ReasonSchema},
	{name: "placeholder", mutate: func(d map[string]any) { values(d)["budget_alert_id"] = "" }, reason: ReasonPlaceholder},
	{name: "wrong-instance", mutate: func(d map[string]any) { d["instance_id"] = "demo-prod-project" }, reason: ReasonWrongProducer},
	{name: "wrong-instance-network", mutate: func(d map[string]any) { d["instance_id"] = "demo-dev-gra11-network" }, reason: ReasonWrongProducer},
	{name: "wrong-stage", mutate: func(d map[string]any) { d["stage"] = "project-network" }, reason: ReasonWrongProducer},
	{name: "unbound-project-id", mutate: func(d map[string]any) {
		values(d)["project_id"], values(d)["project_urn"] = foreignID, foreignURN
	}, reason: ReasonUnboundProject},
	{name: "unbound-project-id-only", mutate: func(d map[string]any) { values(d)["project_id"] = foreignID }, reason: ReasonUnboundProject},
	{name: "unbound-project-urn", mutate: func(d map[string]any) { values(d)["project_urn"] = foreignURN }, reason: ReasonUnboundProject},
	{name: "state-project-instead", mutate: func(d map[string]any) {
		values(d)["project_id"] = adaptStateID
		values(d)["project_urn"] = "urn:v1:eu:resource:publicCloudProject:" + adaptStateID
	}, reason: ReasonUnboundProject},
	{name: "sensitive-key", mutate: func(d map[string]any) {
		values(d)["tenant_s3"] = map[string]any{"access_key_id": seedAccess, "secret_access_key": seedSecret1}
	}, reason: ReasonSecretKey},
	{name: "sensitive-nested", mutate: func(d map[string]any) {
		values(d)["regions"] = []any{map[string]any{"platform_deployer_secret": seedSecret2}}
	}, reason: ReasonSecretKey},
}

// Every artefact the consumer cannot trust is refused with its reason (a missing one blocks), the
// refusal echoes no value, and the consumer gets no input file: nothing of it reaches a plan.
func TestAdapterRefuses(t *testing.T) {
	m := adaptManifest(t)
	for _, c := range artefactCases {
		t.Run(c.name, func(t *testing.T) {
			store := dirStore{t.TempDir()}
			data := c.raw
			if c.mutate != nil {
				doc := fixtureDoc(t, "project")
				c.mutate(doc)
				data = encode(t, doc)
			}
			if c.name != "missing" {
				if data == nil {
					data = readFile(t, fixtureDir+"/envelopes/project.json")
				}
				bucket, key := c.bucket, c.key
				if bucket == "" {
					bucket = tenantBucket
				}
				if key == "" {
					key = ArtifactKey(adaptProjectID)
				}
				store.put(t, bucket, key, data)
			}
			in, dir, err := adapt(t, m, adaptRuntimeID, store, adaptAccount())
			if c.blocked {
				if !errors.Is(err, ErrBlocked) {
					t.Fatalf("want ErrBlocked, got %v", err)
				}
				if !strings.Contains(err.Error(), adaptProjectID) {
					t.Errorf("blocked error does not name the producer %s: %v", adaptProjectID, err)
				}
			} else {
				refusal(t, err, c.reason, seeds...)
			}
			if files := dirFiles(t, dir); len(files) > 0 {
				t.Errorf("refused, but wrote %v", files)
			}
			if len(in.Files) > 0 || len(in.Consumed) > 0 || in.Resolved != "" {
				t.Errorf("refused, but returned inputs %+v", in)
			}
		})
	}
}

// twoTenantManifest is adaptManifest plus a second tenant `ops` (environment dev, ref OPS_DEV)
// with its tenant-state and project rows.
func twoTenantManifest(t *testing.T) *Manifest {
	t.Helper()
	data := edit(t, manifestOf(t, "sandbox"), `"project": {"mode": "reference", "ref": "SANDBOX"}`, `"project": {"mode": "reference", "ref": "DEMO_DEV"}`, 1)
	data = edit(t, data, "      }\n    ],\n    \"instances\": [",
		"      },\n      {\"name\": \"ops\", \"environments\": [{\"name\": \"dev\", \"project\": {\"mode\": \"reference\", \"ref\": \"OPS_DEV\"}, "+
			"\"regions\": [{\"name\": \"GRA11\", \"network\": {\"cidr\": \"10.30.0.0/24\", \"vlan_id\": 1}}], "+
			"\"budget_alert\": {\"enabled\": false}, \"quota_guard\": {\"enabled\": false}}]}\n    ],\n    \"instances\": [", 1)
	runtimeRow := `{"id": "demo-dev-gra11-runtime", "stage": "runtime", "tenant": "demo", "environment": "dev", "region": "GRA11"}`
	data = edit(t, data, runtimeRow, runtimeRow+`,
      {"id": "ops-state", "stage": "tenant-state", "tenant": "ops"},
      {"id": "ops-dev-project", "stage": "project", "tenant": "ops", "environment": "dev"}`, 1)
	return decoded(t, data)
}

// The binding a project artefact is checked against is the one of the producer's own tenant and
// environment in the manifest, never one the artefact names: demo/dev's producer publishing the
// values of another bound tenant (its tenant, id and URN) is refused (round-2 review).
func TestAdapterBindsTheProducersEnvironment(t *testing.T) {
	m := twoTenantManifest(t)
	acct := BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"SANDBOX": adaptStateID, "DEMO_DEV": boundID, "OPS_DEV": foreignID}}
	put := func(t *testing.T, mutate func(map[string]any)) dirStore {
		store := dirStore{t.TempDir()}
		doc := fixtureDoc(t, "project")
		mutate(doc)
		store.put(t, tenantBucket, ArtifactKey(adaptProjectID), encode(t, doc))
		return store
	}
	t.Run("own-binding", func(t *testing.T) {
		if _, _, err := adapt(t, m, adaptRuntimeID, put(t, func(map[string]any) {}), acct); err != nil {
			t.Fatalf("control: demo/dev's own artefact refused: %v", err)
		}
	})
	t.Run("other-tenants-project", func(t *testing.T) {
		store := put(t, func(d map[string]any) {
			values(d)["tenant"], values(d)["project_id"], values(d)["project_urn"] = "ops", foreignID, foreignURN
		})
		_, dir, err := adapt(t, m, adaptRuntimeID, store, acct)
		refusal(t, err, ReasonUnboundProject)
		if files := dirFiles(t, dir); len(files) > 0 {
			t.Errorf("refused, but wrote %v", files)
		}
	})
}

// A consumer that is not a manifest row is refused, nothing is written.
func TestAdapterRefusesUnknownConsumer(t *testing.T) {
	_, dir, err := adapt(t, adaptManifest(t), "demo-prod-project", dirStore{t.TempDir()}, adaptAccount())
	if err == nil {
		t.Fatal("Adapt of an instance not in the manifest succeeded")
	}
	if files := dirFiles(t, dir); len(files) > 0 {
		t.Errorf("refused, but wrote %v", files)
	}
}
