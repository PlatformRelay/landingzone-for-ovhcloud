package stacks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// Output-contract controls of 005 T017 (FR-005, SC-005, V004; ADR-0004, ADR-0017).
//
// Fixtures: tests/fixtures/outputs/captures/tofu-output.json is the pinned `tofu output -json` of
// the provider-free root tests/fixtures/outputs/root/, captured by tests/fixtures/outputs/capture.sh
// through the offline entry (command and identities in its .meta.json sidecar). The envelopes under
// tests/fixtures/outputs/envelopes/ play each in-scope stage's producer with synthetic ids.
//
// Reporting order the refusal cases rely on: a malformed document, then the envelope header, then
// the producer (instance_id, stage), then the deny-list checks on values (secret-pattern key,
// null/"" placeholder), then the stage schema, then the KD-3 project binding. Each case changes one
// thing, so the order only decides the reason where a deny-list hit would also break the schema.

const (
	fixtureDir  = "../../../tests/fixtures/outputs"
	schemaDir   = "../../../schemas/outputs"
	testRev     = "0d1e2f3a4b5c6d7e8f90a1b2c3d4e5f60718293a"
	boundID     = "fedcba9876543210fedcba9876543210"
	boundURN    = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
	seedSecret1 = "lz-seed-t017-secret-access-key"
	seedSecret2 = "lz-seed-t017-deployer-secret"
	seedAccess  = "LZSEEDT017ACCESSKEY"
	seedNeutral = "lz-seed-t017-neutral-handle"
	foreignID   = "00000000000000000000000000000000"
	foreignURN  = "urn:v1:eu:resource:publicCloudProject:00000000000000000000000000000000"
)

var seeds = []string{seedSecret1, seedSecret2, seedAccess, seedNeutral}

// The six in-scope stages, the instance id of each fixture envelope and the stage's required
// values (data-model *Per-stage values*; `unlabelled` on every stage, R14).
var stageFixtures = []struct {
	stage    string
	instance string
	required []string
}{
	{"bootstrap", "account-bootstrap", []string{"state_bucket", "state_project_id", "state_region", "state_endpoint", "platform_s3_user_id", "unlabelled"}},
	{"tenant-state", "demo-state", []string{"tenant", "state_bucket", "tenant_s3_user_id", "platform_s3_user_id", "unlabelled"}},
	{"account-governance", "account-governance", []string{"platform_deployer", "tenants", "unlabelled"}},
	{"project", "demo-dev-project", []string{"tenant", "environment", "project_id", "project_urn", "regions", "unlabelled"}},
	{"project-network", "demo-dev-gra11-project-network", []string{"network_id", "regions_openstack_ids", "subnet_id", "cidr", "unlabelled"}},
	{"runtime", "demo-dev-gra11-runtime-blue", []string{"kind", "scope", "readiness", "pending_actions", "capabilities", "unlabelled"}},
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// captured returns the captured `tofu output -json` after checking that its sidecar vouches for
// exactly this file, made from the current root by the pinned OpenTofu through the isolated entry.
func captured(t *testing.T) []byte {
	t.Helper()
	data := readFile(t, fixtureDir+"/captures/tofu-output.json")
	var meta struct {
		File        string `json:"file"`
		Command     string `json:"command"`
		Toolchain   string `json:"toolchain"`
		Tool        string `json:"tool"`
		ToolVersion string `json:"tool_version"`
		EntryExit   int    `json:"entry_exit"`
		SHA256      string `json:"sha256"`
		Input       string `json:"input"`
		InputSHA256 string `json:"input_sha256"`
	}
	if err := json.Unmarshal(readFile(t, fixtureDir+"/captures/tofu-output.json.meta.json"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.File != "tofu-output.json" || meta.SHA256 != digest(data) || meta.EntryExit != 0 ||
		!strings.Contains(meta.Command, "capture:outputs-root") || meta.Tool != "tofu" || meta.ToolVersion != "1.13.0" ||
		!strings.Contains(meta.Toolchain, " tofu=1.13.0 ") || !strings.HasSuffix(meta.Toolchain, " network=none") {
		t.Fatalf("capture sidecar does not vouch for tofu-output.json from the pinned tofu: %+v", meta)
	}
	if meta.Input != "tests/fixtures/outputs/root/main.tf" || meta.InputSHA256 != digest(readFile(t, fixtureDir+"/root/main.tf")) {
		t.Fatalf("capture is stale: root/main.tf changed since it was captured; rerun tests/fixtures/outputs/capture.sh")
	}
	return data
}

func expectation(stage, instance string) Expectation {
	want := Expectation{InstanceID: instance, Stage: stage}
	if stage == "project" {
		want.Project = &ProjectBinding{ProjectID: boundID, ProjectURN: boundURN}
	}
	return want
}

func schemas() fs.FS { return os.DirFS(schemaDir) }

// refusal asserts err is a *RefusalError with the given reason whose text echoes no seeded value.
func refusal(t *testing.T, err error, reason string, seeds ...string) {
	t.Helper()
	var r *RefusalError
	if !errors.As(err, &r) {
		t.Fatalf("want refusal %q, got %v", reason, err)
	}
	if r.Reason != reason {
		t.Errorf("want refusal %q, got %q (%v)", reason, r.Reason, err)
	}
	for _, seed := range seeds {
		if seed != "" && strings.Contains(err.Error(), seed) {
			t.Errorf("refusal echoes a value from the document: %v", err)
		}
	}
}

// The control: the capture holds plain and sensitive outputs and the seeds, so the builder
// tests below can only pass by filtering.
func TestOutputsCaptureControl(t *testing.T) {
	var doc map[string]struct {
		Sensitive *bool           `json:"sensitive"`
		Value     json.RawMessage `json:"value"`
	}
	data := captured(t)
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var sensitive, plain int
	for _, entry := range doc {
		if entry.Sensitive == nil {
			t.Fatal("captured entry without a sensitive marker")
		}
		if *entry.Sensitive {
			sensitive++
		} else {
			plain++
		}
	}
	if sensitive != 3 || plain != 6 {
		t.Fatalf("capture has %d sensitive and %d plain outputs, want 3 and 6", sensitive, plain)
	}
	for _, seed := range seeds {
		if !bytes.Contains(data, []byte(seed)) {
			t.Fatalf("capture lacks seed %q", seed)
		}
	}
}

// The builder keeps every plain value unchanged and drops every sensitive entry (G2, envelope
// part); no seeded secret reaches the envelope.
func TestOutputsBuildEnvelope(t *testing.T) {
	data := captured(t)
	p := Producer{InstanceID: "demo-dev-gra11-runtime", Stage: "runtime", SourceRevision: testRev}
	env, err := BuildEnvelope(data, p)
	if err != nil {
		t.Fatal(err)
	}
	if env.APIVersion != OutputsAPIVersion || env.Kind != OutputsKind || env.InstanceID != p.InstanceID ||
		env.Stage != p.Stage || env.SourceRevision != p.SourceRevision {
		t.Errorf("envelope header %+v does not carry the producer %+v", env, p)
	}
	var names []string
	for name := range env.Values {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"regions", "replicas", "scope", "state_bucket", "unlabelled", "versioned"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("envelope values %v, want the plain outputs %v", names, want)
	}
	var doc map[string]struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range want {
		var got, exp any
		if err := json.Unmarshal(env.Values[name], &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := json.Unmarshal(doc[name].Value, &exp); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, exp) {
			t.Errorf("%s = %v, want the captured value %v", name, got, exp)
		}
	}
	out, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range append([]string{"platform_s3", "platform_deployer_secret", "bootstrap_handle"}, seeds...) {
		if bytes.Contains(out, []byte(seed)) {
			t.Errorf("envelope carries %q from a sensitive output", seed)
		}
	}
}

// An entry tofu did not mark is not assumed plain, and a broken document is refused; neither
// refusal echoes the value.
func TestOutputsBuildEnvelopeRefuses(t *testing.T) {
	p := Producer{InstanceID: "account-bootstrap", Stage: "bootstrap", SourceRevision: testRev}
	unmarked := []byte(`{"state_bucket":{"sensitive":false,"type":"string","value":"lz-bkt-state"},` +
		`"platform_s3":{"type":"string","value":"` + seedSecret1 + `"}}`)
	_, err := BuildEnvelope(unmarked, p)
	refusal(t, err, ReasonSensitive, seedSecret1)

	// A plain output whose name matches the secret pattern never reaches an envelope either
	// (data-model: no such key anywhere in values), even if a publish path skipped validation.
	plainSecretName := []byte(`{"state_bucket":{"sensitive":false,"type":"string","value":"lz-bkt-state"},` +
		`"api_token":{"sensitive":false,"type":"string","value":"` + seedSecret2 + `"}}`)
	_, err = BuildEnvelope(plainSecretName, p)
	refusal(t, err, ReasonSecretKey, seedSecret2)

	// The same for a secret-pattern key nested in a plain object output, any case.
	plainNestedSecret := []byte(`{"state_bucket":{"sensitive":false,"type":"string","value":"lz-bkt-state"},` +
		`"scope":{"sensitive":false,"type":["object",{"region":"string","creds":["object",{"Access_Key":"string"}]}],` +
		`"value":{"region":"GRA11","creds":{"Access_Key":"` + seedAccess + `"}}}}`)
	_, err = BuildEnvelope(plainNestedSecret, p)
	refusal(t, err, ReasonSecretKey, seedAccess)

	data := captured(t)
	_, err = BuildEnvelope(data[:len(data)/2], p)
	refusal(t, err, ReasonMalformed, seeds...)
}

// One schema per in-scope stage plus the envelope (plan, schemas/outputs/).
func TestOutputsSchemasPresent(t *testing.T) {
	names := []string{"envelope"}
	for _, s := range stageFixtures {
		names = append(names, s.stage)
	}
	for _, name := range names {
		data, err := os.ReadFile(schemaDir + "/" + name + ".schema.json")
		if err != nil {
			t.Errorf("schema %s: %v", name, err)
			continue
		}
		if !json.Valid(data) {
			t.Errorf("schema %s is not JSON", name)
		}
	}
}

// Every stage's fixture envelope validates against the repository's schemas and decodes intact.
func TestOutputsFixturesValidate(t *testing.T) {
	for _, s := range stageFixtures {
		t.Run(s.stage, func(t *testing.T) {
			env, err := ValidateEnvelope(schemas(), readFile(t, fixtureDir+"/envelopes/"+s.stage+".json"), expectation(s.stage, s.instance))
			if err != nil {
				t.Fatalf("fixture refused: %v", err)
			}
			if env.Stage != s.stage || env.InstanceID != s.instance || env.SourceRevision != testRev {
				t.Errorf("decoded header %+v", env)
			}
			for _, key := range s.required {
				if _, ok := env.Values[key]; !ok {
					t.Errorf("decoded values lack %s", key)
				}
			}
		})
	}
}

// schemasWithout is the repository's schema directory minus one file.
func schemasWithout(t *testing.T, name string) fs.FS {
	t.Helper()
	entries, err := os.ReadDir(schemaDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	m := fstest.MapFS{}
	for _, e := range entries {
		if e.Name() != name {
			m[e.Name()] = &fstest.MapFile{Data: readFile(t, schemaDir+"/"+e.Name())}
		}
	}
	return m
}

// Without the stage's schema, or without the envelope schema, nothing validates: the validator
// reads both, it does not assume them.
func TestOutputsValidateNeedsSchema(t *testing.T) {
	for _, s := range stageFixtures {
		t.Run(s.stage, func(t *testing.T) {
			data := readFile(t, fixtureDir+"/envelopes/"+s.stage+".json")
			_, err := ValidateEnvelope(schemasWithout(t, s.stage+".schema.json"), data, expectation(s.stage, s.instance))
			refusal(t, err, ReasonUnknownStage)
			_, err = ValidateEnvelope(schemasWithout(t, "envelope.schema.json"), data, expectation(s.stage, s.instance))
			refusal(t, err, ReasonEnvelope)
		})
	}
}

// fixtureDoc decodes a stage's fixture envelope for mutation.
func fixtureDoc(t *testing.T, stage string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(readFile(t, fixtureDir+"/envelopes/"+stage+".json"), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func values(doc map[string]any) map[string]any { return doc["values"].(map[string]any) }

func encode(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Every required value of every stage, removed alone, is refused by the schema.
func TestOutputsValidateRefusesMissingValue(t *testing.T) {
	for _, s := range stageFixtures {
		for _, key := range s.required {
			t.Run(s.stage+"/"+key, func(t *testing.T) {
				doc := fixtureDoc(t, s.stage)
				delete(values(doc), key)
				_, err := ValidateEnvelope(schemas(), encode(t, doc), expectation(s.stage, s.instance))
				refusal(t, err, ReasonSchema)
			})
		}
	}
}

// An unknown value is refused on every stage (additionalProperties: false).
func TestOutputsValidateRefusesUnknownField(t *testing.T) {
	for _, s := range stageFixtures {
		t.Run(s.stage, func(t *testing.T) {
			doc := fixtureDoc(t, s.stage)
			values(doc)["extra"] = "lz-extra"
			_, err := ValidateEnvelope(schemas(), encode(t, doc), expectation(s.stage, s.instance))
			refusal(t, err, ReasonSchema)
		})
	}
}

type mutation struct {
	name   string
	stage  string
	mutate func(doc map[string]any)
	want   func(e *Expectation) // optional change to the consumer's expectation
	reason string
}

func TestOutputsValidateRefuses(t *testing.T) {
	cases := []mutation{
		// Kept sensitive entries: the data-model's sensitive outputs, published by mistake.
		{"kept sensitive platform_s3", "bootstrap", func(d map[string]any) {
			values(d)["platform_s3"] = map[string]any{"access_key_id": seedAccess, "secret_access_key": seedSecret1}
		}, nil, ReasonSecretKey},
		{"kept sensitive deployer secret", "account-governance", func(d map[string]any) {
			values(d)["platform_deployer_secret"] = seedSecret2
		}, nil, ReasonSecretKey},
		{"kept sensitive tenant_s3", "tenant-state", func(d map[string]any) {
			values(d)["tenant_s3"] = map[string]any{"access_key_id": seedAccess, "secret_access_key": seedSecret1}
		}, nil, ReasonSecretKey},
		// Secret-pattern keys anywhere in values, any case, in places the schema leaves open.
		{"secret-pattern tenant name", "account-governance", func(d map[string]any) {
			tenants := values(d)["tenants"].(map[string]any)
			tenants["tokenco"] = tenants["demo"]
		}, nil, ReasonSecretKey},
		{"secret-pattern region key upper case", "project-network", func(d map[string]any) {
			values(d)["regions_openstack_ids"].(map[string]any)["PASSWORD1"] = seedSecret1
		}, nil, ReasonSecretKey},
		{"secret-pattern nested key", "runtime", func(d map[string]any) {
			values(d)["capabilities"].(map[string]any)["object-storage"].(map[string]any)["Access_Key"] = seedAccess
		}, nil, ReasonSecretKey},
		{"secret-pattern private_key", "project", func(d map[string]any) {
			values(d)["private_key"] = seedSecret1
		}, nil, ReasonSecretKey},
		// Unknown fields beyond the top level of values and in the header.
		{"unknown nested field", "runtime", func(d map[string]any) {
			values(d)["scope"].(map[string]any)["zone"] = "a"
		}, nil, ReasonSchema},
		{"unknown header field", "bootstrap", func(d map[string]any) { d["generation"] = 1 }, nil, ReasonEnvelope},
		// Wrong type for a value.
		{"regions not a list", "project", func(d map[string]any) { values(d)["regions"] = "GRA11" }, nil, ReasonSchema},
		{"runtime kind not managed-only", "runtime", func(d map[string]any) { values(d)["kind"] = "vm" }, nil, ReasonSchema},
		// A capability that does not exist is absent, never a placeholder (FR-005, ADR-0017).
		{"null network capability", "runtime", func(d map[string]any) {
			values(d)["capabilities"].(map[string]any)["network"] = nil
		}, nil, ReasonPlaceholder},
		{"empty network capability", "runtime", func(d map[string]any) {
			values(d)["capabilities"].(map[string]any)["network"] = ""
		}, nil, ReasonPlaceholder},
		{"null bucket in capability", "runtime", func(d map[string]any) {
			values(d)["capabilities"].(map[string]any)["object-storage"].(map[string]any)["bucket"] = nil
		}, nil, ReasonPlaceholder},
		{"empty bucket in capability", "runtime", func(d map[string]any) {
			values(d)["capabilities"].(map[string]any)["object-storage"].(map[string]any)["bucket"] = ""
		}, nil, ReasonPlaceholder},
		{"empty slot", "runtime", func(d map[string]any) { values(d)["slot"] = "" }, nil, ReasonPlaceholder},
		{"null budget alert", "project", func(d map[string]any) { values(d)["budget_alert_id"] = nil }, nil, ReasonPlaceholder},
		// Header.
		{"wrong apiVersion", "bootstrap", func(d map[string]any) { d["apiVersion"] = "lz.platformrelay.dev/v1" }, nil, ReasonEnvelope},
		{"wrong kind", "bootstrap", func(d map[string]any) { d["kind"] = "Outputs" }, nil, ReasonEnvelope},
		{"missing source_revision", "bootstrap", func(d map[string]any) { delete(d, "source_revision") }, nil, ReasonEnvelope},
		{"source_revision not a sha", "bootstrap", func(d map[string]any) { d["source_revision"] = "main" }, nil, ReasonEnvelope},
		{"missing values", "bootstrap", func(d map[string]any) { delete(d, "values") }, nil, ReasonEnvelope},
		{"values not an object", "bootstrap", func(d map[string]any) { d["values"] = []any{} }, nil, ReasonEnvelope},
		// Wrong producer: the artefact's stage or instance is not the derived edge's.
		{"wrong stage", "runtime", nil, func(e *Expectation) { e.Stage = "project-network" }, ReasonWrongProducer},
		{"wrong instance", "runtime", nil, func(e *Expectation) { e.InstanceID = "demo-dev-gra11-runtime" }, ReasonWrongProducer},
		{"stage relabelled", "bootstrap", func(d map[string]any) { d["stage"] = "tenant-state" }, nil, ReasonWrongProducer},
		// Wrong stage for the schema: the producer matches, the values are another stage's.
		{"values of another stage", "bootstrap", func(d map[string]any) { d["stage"] = "tenant-state" },
			func(e *Expectation) { e.Stage = "tenant-state" }, ReasonSchema},
		{"stage without a schema", "bootstrap", func(d map[string]any) { d["stage"] = "account-fabric" },
			func(e *Expectation) { e.Stage = "account-fabric" }, ReasonUnknownStage},
		{"stage escaping the schema directory", "bootstrap", func(d map[string]any) { d["stage"] = "../outputs/bootstrap" },
			func(e *Expectation) { e.Stage = "../outputs/bootstrap" }, ReasonUnknownStage},
		// KD-3: a project artefact binds to the account's resolved reference, or is refused. The
		// coherent cases (id and URN of one other project) need a comparison with the binding;
		// an internal-consistency check alone does not refuse them.
		{"coherent foreign project", "project", func(d map[string]any) {
			values(d)["project_id"] = foreignID
			values(d)["project_urn"] = foreignURN
		}, nil, ReasonUnboundProject},
		{"binding names another coherent project", "project", nil, func(e *Expectation) {
			e.Project = &ProjectBinding{ProjectID: foreignID, ProjectURN: foreignURN}
		}, ReasonUnboundProject},
		{"project id not bound", "project", func(d map[string]any) {
			values(d)["project_id"] = foreignID
		}, nil, ReasonUnboundProject},
		{"project urn not bound", "project", func(d map[string]any) {
			values(d)["project_urn"] = foreignURN
		}, nil, ReasonUnboundProject},
		{"binding names another project", "project", nil, func(e *Expectation) {
			e.Project = &ProjectBinding{ProjectID: foreignID, ProjectURN: boundURN}
		}, ReasonUnboundProject},
		{"project without a binding", "project", nil, func(e *Expectation) { e.Project = nil }, ReasonUnboundProject},
	}
	instances := map[string]string{}
	for _, s := range stageFixtures {
		instances[s.stage] = s.instance
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := fixtureDoc(t, c.stage)
			if c.mutate != nil {
				c.mutate(doc)
			}
			want := expectation(c.stage, instances[c.stage])
			if c.want != nil {
				c.want(&want)
			}
			_, err := ValidateEnvelope(schemas(), encode(t, doc), want)
			refusal(t, err, c.reason, seeds...)
		})
	}
}

// Documents that are not exactly one JSON object are refused before anything is read from them:
// a duplicated header key would let the last one win in a lenient decoder.
func TestOutputsValidateRefusesMalformed(t *testing.T) {
	bootstrap := readFile(t, fixtureDir+"/envelopes/bootstrap.json")
	want := expectation("bootstrap", "account-bootstrap")
	duplicate := bytes.Replace(bootstrap, []byte(`"stage": "bootstrap",`), []byte(`"stage": "bootstrap", "stage": "tenant-state",`), 1)
	if bytes.Equal(duplicate, bootstrap) {
		t.Fatal("fixture layout changed: no stage line to duplicate")
	}
	nested := bytes.Replace(bootstrap, []byte(`"state_bucket": "lz-bkt-state",`), []byte(`"state_bucket": "lz-bkt-state", "state_bucket": "lz-other",`), 1)
	if bytes.Equal(nested, bootstrap) {
		t.Fatal("fixture layout changed: no state_bucket line to duplicate")
	}
	for name, data := range map[string][]byte{
		"not JSON":             []byte("not json"),
		"truncated":            bootstrap[:len(bootstrap)/2],
		"array":                []byte("[]"),
		"trailing document":    append(append([]byte{}, bootstrap...), []byte(`{"stage":"tenant-state"}`)...),
		"duplicate header key": duplicate,
		"duplicate value key":  nested,
		"empty":                {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateEnvelope(schemas(), data, want)
			refusal(t, err, ReasonMalformed)
		})
	}
}
