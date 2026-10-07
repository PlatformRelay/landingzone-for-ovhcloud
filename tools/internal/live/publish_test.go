//go:build offlinetools

package live

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Output-exchange integration controls of 005 T060 (FR-005, FR-009, FR-013; V012; ADR-0004,
// ADR-0007, ADR-0017; research R4; data-model *Envelope-to-input adapter*, *Resolved-reference
// input*, *Live run record*; KD-3). Offline: the sandbox manifest fixture is generated with the
// pinned Terramate into a scratch project root, its stacks run with the pinned OpenTofu under
// their generated mocked tests, and a scratch directory per bucket plays the state buckets. No
// credential, no network, no OVHcloud API.
//
// Readings pinned here (evidence/T060.md):
//   - "applies under mocks" is the stack's generated offline test with its run switched to
//     `command = apply`; the root's `tofu output -json` is that run's `test_state` `values.outputs`
//     (TestPublishStateOutputsAreTofuOutput holds the two equal on the T017 capture);
//   - the publisher validates its own envelope as a consumer will (KD-3 binding included) before
//     writing, so a refused envelope never reaches the bucket;
//   - the record of a consumer holds, per producer, the sha256 of the outputs.json it consumed
//     (= the publisher's digest of what it wrote) and the sha256 of its resolved-reference input;
//   - account.env's OVH_ENDPOINT and LZ_PROJECT_ID_<REF> are the bound account the adapter and the
//     publisher resolve against.
// The adapter's own refusals are in internal/stacks/adapter_test.go (TestAdapter*).

const (
	repoRoot      = "../../.."
	exStateID     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	exProjectID   = "0123456789abcdef0123456789abcdef" // the generated offline test's project id (stacks/_lz/offline.tm.hcl)
	exForeignID   = "00000000000000000000000000000000"
	exRevision    = "0d1e2f3a4b5c6d7e8f90a1b2c3d4e5f60718293a"
	exProject     = "demo-dev-project"
	exRuntime     = "demo-dev-gra11-runtime"
	exBootstrap   = "account-bootstrap"
	exTenantBkt   = "lz-demo-bkt-state"
	exAccountBkt  = "lz-bkt-state"
	exAccountDir  = "/offline/accounts/fixture"
	exSeedSecret  = "lz-seed-t060-secret-access-key"
	exSeedNeutral = "lz-seed-t060-neutral-handle"
)

// bucketDir is a scratch directory per bucket: <root>/<bucket>/<key>.
type bucketDir struct{ root string }

func (b bucketDir) path(bucket, key string) (string, error) {
	if bucket == "" || strings.ContainsAny(bucket, `/\`) || bucket == "." || bucket == ".." {
		return "", fmt.Errorf("bucket %q is not a bucket name", bucket)
	}
	return filepath.Join(b.root, bucket, filepath.FromSlash(key)), nil
}

func (b bucketDir) Put(bucket, key string, data []byte) error {
	p, err := b.path(bucket, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func (b bucketDir) Get(bucket, key string) ([]byte, error) {
	p, err := b.path(bucket, key)
	if err != nil {
		return nil, fs.ErrNotExist
	}
	return os.ReadFile(p)
}

// objects lists every object in the store as bucket/key.
func (b bucketDir) objects(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(b.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(b.root, path)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonOf(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	return v
}

func exSchemas() fs.FS { return os.DirFS(filepath.Join(repoRoot, "schemas", "outputs")) }

// exManifestBytes is the sandbox fixture with distinct state (SANDBOX) and environment (DEMO_DEV)
// project refs, so a swapped reference cannot pass.
func exManifestBytes(t *testing.T) []byte {
	t.Helper()
	data := string(mustRead(t, filepath.Join(repoRoot, "tests/fixtures/manifests/sandbox/deployments.yaml")))
	const old = `"project": {"mode": "reference", "ref": "SANDBOX"}`
	if strings.Count(data, old) != 1 {
		t.Fatalf("sandbox fixture drifted: %q not found once", old)
	}
	return []byte(strings.Replace(data, old, `"project": {"mode": "reference", "ref": "DEMO_DEV"}`, 1))
}

// exTwoTenantManifest is the exchange manifest plus a second tenant `ops` (environment dev, ref
// OPS_DEV) with its tenant-state and project rows; decoded only, never generated.
func exTwoTenantManifest(t *testing.T) *stacks.Manifest {
	t.Helper()
	data := string(exManifestBytes(t))
	replace := func(old, new string) {
		if strings.Count(data, old) != 1 {
			t.Fatalf("sandbox fixture drifted: %q not found once", old)
		}
		data = strings.Replace(data, old, new, 1)
	}
	replace("      }\n    ],\n    \"instances\": [",
		"      },\n      {\"name\": \"ops\", \"environments\": [{\"name\": \"dev\", \"project\": {\"mode\": \"reference\", \"ref\": \"OPS_DEV\"}, "+
			"\"regions\": [{\"name\": \"GRA11\", \"network\": {\"cidr\": \"10.30.0.0/24\", \"vlan_id\": 1}}], "+
			"\"budget_alert\": {\"enabled\": false}, \"quota_guard\": {\"enabled\": false}}]}\n    ],\n    \"instances\": [")
	row := `{"id": "demo-dev-gra11-runtime", "stage": "runtime", "tenant": "demo", "environment": "dev", "region": "GRA11"}`
	replace(row, row+`,
      {"id": "ops-state", "stage": "tenant-state", "tenant": "ops"},
      {"id": "ops-dev-project", "stage": "project", "tenant": "ops", "environment": "dev"}`)
	m, err := stacks.DecodeManifest([]byte(data))
	if err != nil {
		t.Fatalf("two-tenant manifest: %v", err)
	}
	return m
}

// exAccountEnv writes a bound account file as the bootstrap writes it and reads it back as the
// lane does.
func exAccountEnv(t *testing.T, demoDev string) stacks.BoundAccount {
	t.Helper()
	path := filepath.Join(t.TempDir(), "account.env")
	body := "# fixture account, no secret\nLZ_ACCOUNT_ID=fixture-ovh\nOVH_ENDPOINT=ovh-eu\nLZ_ORG=lz\n" +
		"LZ_PROJECT_ID_SANDBOX=" + exStateID + "\nLZ_PROJECT_ID_DEMO_DEV=" + demoDev + "\nLZ_ADMIN_CLIENT_ID=EU.fixture\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := ReadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	acct, err := BoundAccountFromEnv(env)
	if err != nil {
		t.Fatalf("BoundAccountFromEnv: %v", err)
	}
	return acct
}

// exchangeRoot generates the manifest into a scratch Terramate project root holding this
// repository's generation configuration, with stages/, components/ and modules/ linked.
func exchangeRoot(t *testing.T) (string, *stacks.Manifest) {
	t.Helper()
	root := t.TempDir()
	for _, glob := range []string{"*.tm.hcl", "stacks/*.tm.hcl", "stacks/_lz/*.tm.hcl"} {
		files, err := filepath.Glob(filepath.Join(repoRoot, glob))
		if err != nil || len(files) == 0 {
			t.Fatalf("generation configuration %s: %v (none found)", glob, err)
		}
		for _, f := range files {
			rel, _ := filepath.Rel(repoRoot, f)
			mustWrite(t, filepath.Join(root, rel), mustRead(t, f))
		}
	}
	manifest := exManifestBytes(t)
	mustWrite(t, filepath.Join(root, stacks.ManifestPath), manifest)
	for _, dir := range []string{"stages", "components", "modules"} {
		src, err := filepath.Abs(filepath.Join(repoRoot, dir))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(src, filepath.Join(root, dir)); err != nil {
			t.Fatal(err)
		}
	}
	tm, err := stacks.PinnedTerramate(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stacks.Reconcile(stacks.ReconcileOptions{Root: root, Terramate: tm}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := stacks.Generate(stacks.GenerateOptions{Root: root, Terramate: tm}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	m, err := stacks.DecodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return root, m
}

func instanceOf(t *testing.T, m *stacks.Manifest, id string) stacks.Instance {
	t.Helper()
	for _, in := range m.Instances {
		if in.ID == id {
			return in
		}
	}
	t.Fatalf("no instance %s", id)
	return stacks.Instance{}
}

// runStack runs one generated stack under its generated mocked test with the run switched to
// command (apply or plan) and the given var files, and returns, for apply, the root's
// `tofu output -json` (the run's test_state outputs) and, for plan, the run's test_plan.
func runStack(t *testing.T, root string, in stacks.Instance, command string, varFiles ...string) []byte {
	t.Helper()
	tofu, err := stacks.PinnedTofu(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, filepath.FromSlash(in.Path))
	generated := string(mustRead(t, filepath.Join(dir, "tests", "_lz_offline.tftest.hcl")))
	if strings.Count(generated, "run \"plan\" {\n  command = plan\n}") != 1 {
		t.Fatalf("%s: the generated offline test has no single run \"plan\"", in.Path)
	}
	test := strings.Replace(generated, "run \"plan\" {\n  command = plan\n}", "run \"exchange\" {\n  command = "+command+"\n}", 1)
	if in.Stage == "project" {
		// The generated test pins project_id itself; drop it so the id can only come from the
		// resolved-reference var file (the mocked project's URN override stays keyed to it).
		pin := "  project_id       = \"" + exProjectID + "\"\n"
		if strings.Count(test, pin) != 1 {
			t.Fatalf("%s: the generated offline test does not pin project_id once", in.Path)
		}
		test = strings.Replace(test, pin, "", 1)
	}
	const testFile = "tests/_lz_exchange.tftest.hcl"
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(testFile)), []byte(test))
	if lock, err := os.ReadFile(filepath.Join(root, "stages", in.Stage, ".terraform.lock.hcl")); err == nil {
		mustWrite(t, filepath.Join(dir, ".terraform.lock.hcl"), lock)
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	// One plugin cache per scratch root: its stacks run one after another, never concurrently.
	cache := filepath.Join(root, ".plugins")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		cmd := exec.Command(tofu, append([]string{"-chdir=" + dir}, args...)...)
		cmd.Env = append(os.Environ(), "TF_PLUGIN_CACHE_DIR="+cache, "TF_IN_AUTOMATION=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: tofu %s: %v: %s %s", in.Path, args[0], err, stderr.String(), tail(out))
		}
		return out
	}
	run("init", "-backend=false", "-lockfile=readonly", "-input=false", "-no-color")
	args := []string{"test", "-json", "-verbose", "-no-color", "-filter=" + testFile}
	for _, f := range varFiles {
		args = append(args, "-var-file="+f)
	}
	out := run(args...)
	var result []byte
	passed := false
	for _, line := range bytes.Split(out, []byte("\n")) {
		var msg struct {
			Type  string          `json:"type"`
			File  string          `json:"@testfile"`
			Run   string          `json:"@testrun"`
			Plan  json.RawMessage `json:"test_plan"`
			State struct {
				Values struct {
					Outputs json.RawMessage `json:"outputs"`
				} `json:"values"`
			} `json:"test_state"`
			Summary struct {
				Status string `json:"status"`
			} `json:"test_summary"`
		}
		if json.Unmarshal(line, &msg) != nil || (msg.File != "" && msg.File != testFile) {
			continue
		}
		switch {
		case command == "apply" && msg.Type == "test_state" && msg.Run == "exchange":
			result = msg.State.Values.Outputs
		case command == "plan" && msg.Type == "test_plan" && msg.Run == "exchange":
			result = msg.Plan
		case msg.Type == "test_summary":
			passed = msg.Summary.Status == "pass"
		}
	}
	if result == nil || !passed {
		t.Fatalf("%s: no passing %s of run \"exchange\": %s", in.Path, command, tail(out))
	}
	return result
}

func tail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.Join(lines[max(0, len(lines)-5):], "\n")
}

// adaptInputs runs the adapter for consumer into a fresh input directory.
func adaptInputs(t *testing.T, m *stacks.Manifest, consumer string, store bucketDir, acct stacks.BoundAccount) (stacks.Inputs, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "inputs", consumer)
	in, err := stacks.Adapt(stacks.AdaptOptions{Manifest: m, Consumer: consumer, Store: store, Schemas: exSchemas(), Account: acct, Dir: dir})
	if err != nil {
		t.Fatalf("Adapt %s: %v", consumer, err)
	}
	return in, dir
}

// plainOutputs are the non-sensitive entries of a `tofu output -json` document, name → value;
// sensitiveLeaves every scalar inside a sensitive entry, as text.
func plainOutputs(t *testing.T, doc []byte) (plain map[string]any, sensitiveNames, sensitiveLeaves []string) {
	t.Helper()
	var entries map[string]struct {
		Sensitive bool `json:"sensitive"`
		Value     any  `json:"value"`
	}
	if err := json.Unmarshal(doc, &entries); err != nil {
		t.Fatal(err)
	}
	plain = map[string]any{}
	for name, e := range entries {
		if !e.Sensitive {
			plain[name] = e.Value
			continue
		}
		sensitiveNames = append(sensitiveNames, name)
		var walk func(v any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				for _, e := range x {
					walk(e)
				}
			case []any:
				for _, e := range x {
					walk(e)
				}
			case nil:
			default:
				if s := fmt.Sprint(x); len(s) >= 6 {
					sensitiveLeaves = append(sensitiveLeaves, s)
				}
			}
		}
		walk(e.Value)
	}
	return plain, sensitiveNames, sensitiveLeaves
}

// The fixture rule for the round trip: on the T017 root, the test_state outputs of an applied run
// equal the pinned `tofu output -json` captured from that root (null and sensitive entries
// included), so the round trip reads exactly what the live lane will read.
func TestPublishStateOutputsAreTofuOutput(t *testing.T) {
	captures := filepath.Join(repoRoot, "tests/fixtures/outputs/captures")
	var meta struct {
		InputSHA256 string `json:"input_sha256"`
		SHA256      string `json:"sha256"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(captures, "tofu-output.json.meta.json")), &meta); err != nil {
		t.Fatal(err)
	}
	captured := mustRead(t, filepath.Join(captures, "tofu-output.json"))
	main := mustRead(t, filepath.Join(repoRoot, "tests/fixtures/outputs/root/main.tf"))
	if meta.SHA256 != sha(captured) || meta.InputSHA256 != sha(main) {
		t.Fatal("tofu-output.json capture is stale or altered; rerun tests/fixtures/outputs/capture.sh")
	}
	root := t.TempDir()
	stack := filepath.Join(root, "stacks", "fixture")
	mustWrite(t, filepath.Join(stack, "main.tf"), main)
	mustWrite(t, filepath.Join(stack, "tests", "_lz_offline.tftest.hcl"), []byte("run \"plan\" {\n  command = plan\n}\n"))
	// A provider-free root has no lock file (runStack copies a stage lock only when there is one).
	got := runStack(t, root, stacks.Instance{Path: "stacks/fixture", Stage: "fixture"}, "apply")
	if !reflect.DeepEqual(jsonOf(t, got), jsonOf(t, captured)) {
		t.Errorf("test_state outputs differ from the captured tofu output -json:\n got %s\nwant %s", got, captured)
	}
}

// The round trip: project applies, its outputs become an envelope the publisher writes to the
// tenant bucket at artifacts/<id>/outputs.json, the adapter turns it into project.tfvars.json, and
// runtime plans with that file; the runtime's record holds the digest of what it consumed.
func TestExchangeRoundTrip(t *testing.T) {
	root, m := exchangeRoot(t)
	acct := exAccountEnv(t, exProjectID)
	store := bucketDir{t.TempDir()}

	resolved, _ := adaptInputs(t, m, exProject, store, acct)
	if len(resolved.Files) != 1 {
		t.Fatalf("project got resolved-reference inputs %v, want one file (project_id)", resolved.Files)
	}
	outputs := runStack(t, root, instanceOf(t, m, exProject), "apply", resolved.Files...)
	plain, _, _ := plainOutputs(t, outputs)
	if plain["project_id"] != exProjectID {
		t.Fatalf("control: the applied project's project_id is %v, want %s", plain["project_id"], exProjectID)
	}

	digest, err := Publish(PublishOptions{Manifest: m, Instance: exProject, Revision: exRevision, TofuOutput: outputs, Schemas: exSchemas(), Account: acct, Store: store})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	key := exTenantBkt + "/artifacts/" + exProject + "/outputs.json"
	if got := store.objects(t); !reflect.DeepEqual(got, []string{key}) {
		t.Fatalf("store holds %v, want [%s]", got, key)
	}
	object := mustRead(t, filepath.Join(store.root, filepath.FromSlash(key)))
	if digest != sha(object) {
		t.Errorf("Publish returned %s, want the sha256 of the written object %s", digest, sha(object))
	}
	binding := &stacks.ProjectBinding{ProjectID: exProjectID, ProjectURN: "urn:v1:eu:resource:publicCloudProject:" + exProjectID}
	env, err := stacks.ValidateEnvelope(exSchemas(), object, stacks.Expectation{InstanceID: exProject, Stage: "project", Project: binding})
	if err != nil {
		t.Fatalf("the published artefact does not validate: %v", err)
	}
	if env.SourceRevision != exRevision {
		t.Errorf("source_revision %q, want %q", env.SourceRevision, exRevision)
	}
	published := jsonOf(t, object).(map[string]any)["values"]
	if !reflect.DeepEqual(published, any(plain)) {
		t.Errorf("published values %v, want the applied root's plain outputs %v", published, plain)
	}

	inputs, dir := adaptInputs(t, m, exRuntime, store, acct)
	file := filepath.Join(dir, "project.tfvars.json")
	if !reflect.DeepEqual(inputs.Files, []string{file}) {
		t.Fatalf("runtime inputs %v, want [%s]", inputs.Files, file)
	}
	if got := jsonOf(t, mustRead(t, file)); !reflect.DeepEqual(got, any(map[string]any{"project": plain})) {
		t.Errorf("project.tfvars.json = %v, want {project: the published values}", got)
	}
	if inputs.Consumed[exProject] != digest {
		t.Errorf("consumed digest %q, want the published object's %q", inputs.Consumed[exProject], digest)
	}

	plan := runStack(t, root, instanceOf(t, m, exRuntime), "plan", inputs.Files...)
	var doc struct {
		Outputs map[string]struct {
			After json.RawMessage `json:"after"`
		} `json:"output_changes"`
	}
	if err := json.Unmarshal(plan, &doc); err != nil {
		t.Fatal(err)
	}
	var scope struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(doc.Outputs["scope"].After, &scope); err != nil || scope.ProjectID != exProjectID {
		t.Errorf("runtime planned scope.project_id %q (%v), want the consumed %s", scope.ProjectID, err, exProjectID)
	}

	// runtime publishes too, and its scope must be the bound project of its environment (T018
	// decision 2: KD-3 for runtime scope.project_id belongs to the exchange).
	rtOutputs := runStack(t, root, instanceOf(t, m, exRuntime), "apply", inputs.Files...)
	foreign := bucketDir{t.TempDir()}
	if _, err := Publish(PublishOptions{Manifest: m, Instance: exRuntime, Revision: exRevision, TofuOutput: rtOutputs, Schemas: exSchemas(), Account: exAccountEnv(t, exForeignID), Store: foreign}); err == nil {
		t.Errorf("runtime published with a scope.project_id the bound account does not resolve to")
	} else {
		var r *stacks.RefusalError
		if !errors.As(err, &r) || r.Reason != stacks.ReasonUnboundProject {
			t.Errorf("runtime with an unbound scope: want refusal %q, got %v", stacks.ReasonUnboundProject, err)
		}
	}
	if got := foreign.objects(t); len(got) > 0 {
		t.Errorf("refused runtime publish wrote %v", got)
	}
	if _, err := Publish(PublishOptions{Manifest: m, Instance: exRuntime, Revision: exRevision, TofuOutput: rtOutputs, Schemas: exSchemas(), Account: acct, Store: store}); err != nil {
		t.Fatalf("Publish runtime: %v", err)
	}
	rtKey := exTenantBkt + "/artifacts/" + exRuntime + "/outputs.json"
	if got := store.objects(t); !reflect.DeepEqual(got, []string{rtKey, key}) {
		t.Errorf("store holds %v, want [%s %s]", got, rtKey, key)
	}

	records := filepath.Join(t.TempDir(), "records")
	if err := WriteRecord(records, exRuntime, Record{AppliedAt: "2026-10-07T00:00:00Z", SourceRevision: exRevision, CodeDigest: sha([]byte("code")), Consumed: inputs.Consumed, Resolved: inputs.Resolved}); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	var rec struct {
		Consumed map[string]string `json:"consumed"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(records, exRuntime+".json")), &rec); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.Consumed, map[string]string{exProject: sha(object)}) {
		t.Errorf("record consumed %v, want {%s: sha256 of the consumed outputs.json}", rec.Consumed, exProject)
	}
}

// bootstrap (local state) publishes to the account bucket's artefact path; nothing sensitive of
// its outputs reaches the object.
func TestExchangeBootstrapPublishesToAccountBucket(t *testing.T) {
	root, m := exchangeRoot(t)
	acct := exAccountEnv(t, exProjectID)
	store := bucketDir{t.TempDir()}
	resolved, _ := adaptInputs(t, m, exBootstrap, store, acct)
	if len(resolved.Files) != 1 {
		t.Fatalf("bootstrap got resolved-reference inputs %v, want one file (state_project_id)", resolved.Files)
	}
	accountDir := filepath.Join(t.TempDir(), "account.tfvars.json")
	mustWrite(t, accountDir, []byte(`{"lz_account_dir": "`+exAccountDir+`"}`))
	outputs := runStack(t, root, instanceOf(t, m, exBootstrap), "apply", append(resolved.Files, accountDir)...)
	plain, names, leaves := plainOutputs(t, outputs)
	if len(names) == 0 || len(leaves) == 0 {
		t.Fatalf("control: bootstrap's applied outputs carry no sensitive value (names %v)", names)
	}
	if plain["state_project_id"] != exStateID {
		t.Fatalf("control: bootstrap's state_project_id is %v, want the resolved %s", plain["state_project_id"], exStateID)
	}
	if _, err := Publish(PublishOptions{Manifest: m, Instance: exBootstrap, Revision: exRevision, TofuOutput: outputs, Schemas: exSchemas(), Account: acct, Store: store}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	key := exAccountBkt + "/artifacts/" + exBootstrap + "/outputs.json"
	if got := store.objects(t); !reflect.DeepEqual(got, []string{key}) {
		t.Fatalf("store holds %v, want [%s]", got, key)
	}
	object := mustRead(t, filepath.Join(store.root, filepath.FromSlash(key)))
	if _, err := stacks.ValidateEnvelope(exSchemas(), object, stacks.Expectation{InstanceID: exBootstrap, Stage: "bootstrap"}); err != nil {
		t.Fatalf("the published artefact does not validate: %v", err)
	}
	for _, s := range leaves {
		if bytes.Contains(object, []byte(s)) {
			t.Errorf("the published object carries a value of a sensitive output")
		}
	}
	got := jsonOf(t, object).(map[string]any)["values"]
	for _, name := range names {
		if _, ok := got.(map[string]any)[name]; ok {
			t.Errorf("the published values carry the sensitive output %s", name)
		}
	}
	if !reflect.DeepEqual(got, any(plain)) {
		t.Errorf("published values %v, want the plain outputs %v", got, plain)
	}
}

// The resolved-reference input is written from account.env and its digest is recorded the same
// way as a consumed artefact's: account-governance's tenants map and project's project_id.
func TestExchangeResolvedInputs(t *testing.T) {
	_, m := exchangeRoot(t)
	acct := exAccountEnv(t, exProjectID)
	urn := "urn:v1:eu:resource:publicCloudProject:" + exProjectID
	for consumer, want := range map[string]any{
		"account-governance": map[string]any{"tenants": map[string]any{"demo": map[string]any{"project_id": exProjectID, "project_urn": urn}}},
		exProject:            map[string]any{"project_id": exProjectID},
	} {
		t.Run(consumer, func(t *testing.T) {
			inputs, dir := adaptInputs(t, m, consumer, bucketDir{t.TempDir()}, acct)
			file := filepath.Join(dir, "resolved.tfvars.json")
			if !reflect.DeepEqual(inputs.Files, []string{file}) {
				t.Fatalf("inputs %v, want [%s]", inputs.Files, file)
			}
			data := mustRead(t, file)
			if got := jsonOf(t, data); !reflect.DeepEqual(got, want) {
				t.Errorf("resolved.tfvars.json = %v, want %v", got, want)
			}
			records := filepath.Join(t.TempDir(), "records")
			if err := WriteRecord(records, consumer, Record{AppliedAt: "2026-10-07T00:00:00Z", SourceRevision: exRevision, CodeDigest: sha([]byte("code")), Consumed: inputs.Consumed, Resolved: inputs.Resolved}); err != nil {
				t.Fatalf("WriteRecord: %v", err)
			}
			var rec struct {
				Resolved string `json:"resolved"`
			}
			if err := json.Unmarshal(mustRead(t, filepath.Join(records, consumer+".json")), &rec); err != nil {
				t.Fatal(err)
			}
			if rec.Resolved != sha(data) {
				t.Errorf("record resolved %q, want the sha256 of resolved.tfvars.json %q", rec.Resolved, sha(data))
			}
			changed, _ := adaptInputs(t, m, consumer, bucketDir{t.TempDir()}, exAccountEnv(t, exForeignID))
			if changed.Resolved == inputs.Resolved {
				t.Errorf("a changed LZ_PROJECT_ID_DEMO_DEV leaves the resolved digest unchanged")
			}
		})
	}
}

// outputEntry is one `tofu output -json` entry.
func outputEntry(sensitive bool, value any) map[string]any {
	return map[string]any{"sensitive": sensitive, "value": value}
}

// The publisher refuses what a consumer would refuse and writes nothing then; a sensitive output
// never reaches the object, whatever its name.
func TestExchangePublishRefuses(t *testing.T) {
	root, m := exchangeRoot(t)
	acct := exAccountEnv(t, exProjectID)
	// The project id is given directly (not through the adapter), so these publisher controls
	// do not depend on the adapter under test.
	idFile := filepath.Join(t.TempDir(), "project_id.tfvars.json")
	mustWrite(t, idFile, []byte(`{"project_id": "`+exProjectID+`"}`))
	outputs := runStack(t, root, instanceOf(t, m, exProject), "apply", idFile)
	withAll := func(entries map[string]any) []byte {
		var doc map[string]any
		if err := json.Unmarshal(outputs, &doc); err != nil {
			t.Fatal(err)
		}
		for name, entry := range entries {
			doc[name] = entry
		}
		data, _ := json.Marshal(doc)
		return data
	}
	with := func(name string, entry any) []byte { return withAll(map[string]any{name: entry}) }
	twoTenants := exTwoTenantManifest(t)
	puts := []string{}
	publish := func(o PublishOptions) (bucketDir, string, error) {
		store := bucketDir{t.TempDir()}
		puts = puts[:0]
		o.Schemas, o.Store = exSchemas(), recordingStore{store, &puts}
		if o.Manifest == nil {
			o.Manifest = m
		}
		if o.Instance == "" {
			o.Instance = exProject
		}
		if o.Revision == "" {
			o.Revision = exRevision
		}
		if o.TofuOutput == nil {
			o.TofuOutput = outputs
		}
		if o.Account.ProjectIDs == nil {
			o.Account = acct
		}
		d, err := Publish(o)
		return store, d, err
	}

	t.Run("sensitive-dropped", func(t *testing.T) {
		store, digest, err := publish(PublishOptions{TofuOutput: with("handle", outputEntry(true, map[string]any{"value": exSeedNeutral, "secret_access_key": exSeedSecret}))})
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		key := exTenantBkt + "/artifacts/" + exProject + "/outputs.json"
		if got := store.objects(t); !reflect.DeepEqual(got, []string{key}) {
			t.Fatalf("store holds %v, want [%s]", got, key)
		}
		object := mustRead(t, filepath.Join(store.root, filepath.FromSlash(key)))
		plain, _, _ := plainOutputs(t, outputs)
		if got := jsonOf(t, object).(map[string]any)["values"]; !reflect.DeepEqual(got, any(plain)) || digest != sha(object) {
			t.Errorf("published values %v (digest %s), want the plain outputs %v (digest %s)", got, digest, plain, sha(object))
		}
		for _, o := range store.objects(t) {
			data := mustRead(t, filepath.Join(store.root, filepath.FromSlash(o)))
			for _, s := range []string{exSeedNeutral, exSeedSecret, `"handle"`} {
				if bytes.Contains(data, []byte(s)) {
					t.Errorf("%s carries the sensitive output (%s)", o, s)
				}
			}
		}
	})

	cases := []struct {
		name   string
		opts   PublishOptions
		reason string // "" when any error will do
	}{
		{"unbound-project", PublishOptions{Account: exAccountEnv(t, exForeignID)}, stacks.ReasonUnboundProject},
		// The binding is the producer's own tenant/environment in the manifest, never the one its
		// outputs name (round-2 review): demo/dev publishing a second bound tenant's project.
		{"other-tenants-project", PublishOptions{Manifest: twoTenants,
			Account:    stacks.BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"SANDBOX": exStateID, "DEMO_DEV": exProjectID, "OPS_DEV": exForeignID}},
			TofuOutput: withAll(map[string]any{"tenant": outputEntry(false, "ops"), "project_id": outputEntry(false, exForeignID), "project_urn": outputEntry(false, "urn:v1:eu:resource:publicCloudProject:"+exForeignID)})},
			stacks.ReasonUnboundProject},
		{"output-id-only", PublishOptions{TofuOutput: with("project_id", outputEntry(false, exForeignID))}, stacks.ReasonUnboundProject},
		{"output-urn-only", PublishOptions{TofuOutput: with("project_urn", outputEntry(false, "urn:v1:eu:resource:publicCloudProject:"+exForeignID))}, stacks.ReasonUnboundProject},
		{"no-binding", PublishOptions{Account: stacks.BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"SANDBOX": exStateID}}}, stacks.ReasonUnboundProject},
		{"unmarked-entry", PublishOptions{TofuOutput: with("handle", map[string]any{"value": exSeedNeutral})}, stacks.ReasonSensitive},
		{"secret-named-plain", PublishOptions{TofuOutput: with("tenant_s3_secret", outputEntry(false, exSeedNeutral))}, stacks.ReasonSecretKey},
		{"secret-key-nested", PublishOptions{TofuOutput: with("extra", outputEntry(false, map[string]any{"secret_access_key": exSeedSecret}))}, stacks.ReasonSecretKey},
		{"schema-unknown-output", PublishOptions{TofuOutput: with("zone", outputEntry(false, "a"))}, stacks.ReasonSchema},
		{"other-stage", PublishOptions{Instance: exRuntime}, stacks.ReasonSchema},
		{"revision", PublishOptions{Revision: "main"}, stacks.ReasonEnvelope},
		{"malformed", PublishOptions{TofuOutput: []byte(`{"tenant": `)}, stacks.ReasonMalformed},
		{"unknown-instance", PublishOptions{Instance: "demo-prod-project"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _, err := publish(c.opts)
			if err == nil {
				t.Fatalf("Publish succeeded, want a refusal %q", c.reason)
			}
			if c.reason != "" {
				var r *stacks.RefusalError
				if !errors.As(err, &r) || r.Reason != c.reason {
					t.Errorf("want refusal %q, got %v", c.reason, err)
				}
			}
			for _, s := range []string{exSeedNeutral, exSeedSecret} {
				if strings.Contains(err.Error(), s) {
					t.Errorf("refusal echoes a value: %v", err)
				}
			}
			if got := store.objects(t); len(got) > 0 || len(puts) > 0 {
				t.Errorf("refused, but the store holds %v (Put calls %v)", got, puts)
			}
			if c.name == "unknown-instance" && !strings.Contains(err.Error(), "demo-prod-project") {
				t.Errorf("the refusal of an unknown instance does not name it: %v", err)
			}
		})
	}
}

// recordingStore records every Put, so "nothing written" does not rest on the scratch store
// rejecting a malformed bucket or key.
type recordingStore struct {
	bucketDir
	puts *[]string
}

func (r recordingStore) Put(bucket, key string, data []byte) error {
	*r.puts = append(*r.puts, bucket+"/"+key)
	return r.bucketDir.Put(bucket, key, data)
}

// An instance a tenant manifest lists in spec.external is applied by the platform, never here, so
// publishing it is refused with nothing written; the same outputs publish when it is a row
// (T061 review round 1: the adapter reads external producers, the publisher must not write them).
func TestPublishRefusesExternalInstance(t *testing.T) {
	fixture := string(mustRead(t, filepath.Join(repoRoot, "tests/fixtures/manifests/tenant-only/deployments.yaml")))
	row := `{"id": "demo-dev-project", "stage": "project", "tenant": "demo", "environment": "dev"}`
	state := `{"id": "demo-state", "stage": "tenant-state", "tenant": "demo"}`
	if strings.Count(fixture, "      "+row+",\n") != 1 || strings.Count(fixture, state) != 1 {
		t.Fatal("tenant-only fixture drifted")
	}
	external := strings.Replace(strings.Replace(fixture, "      "+row+",\n", "", 1), state, state+",\n      "+row, 1)
	var envelope struct {
		Values map[string]any `json:"values"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(repoRoot, "tests/fixtures/outputs/envelopes/project.json")), &envelope); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{}
	for name, v := range envelope.Values {
		doc[name] = outputEntry(false, v)
	}
	outputs, _ := json.Marshal(doc)
	acct := stacks.BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"STATE": exStateID, "DEMO_DEV": envelope.Values["project_id"].(string)}}
	for name, src := range map[string]string{"row": fixture, "external": external} {
		t.Run(name, func(t *testing.T) {
			m, err := stacks.DecodeManifest([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			var puts []string
			_, err = Publish(PublishOptions{Manifest: m, Instance: exProject, Revision: exRevision, TofuOutput: outputs, Schemas: exSchemas(), Account: acct, Store: recordingStore{bucketDir{t.TempDir()}, &puts}})
			if name == "row" {
				if err != nil || len(puts) != 1 {
					t.Fatalf("control: Publish of the row: %v (Put calls %v)", err, puts)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), exProject) {
				t.Errorf("Publish of an external instance: %v, want a refusal naming %s", err, exProject)
			}
			if len(puts) > 0 {
				t.Errorf("refused, but Put %v", puts)
			}
		})
	}
}

// account.env → the bound account: the endpoint and every LZ_PROJECT_ID_<REF>, nothing else; a
// missing endpoint or an empty or malformed project id is refused.
func TestPublishBoundAccount(t *testing.T) {
	env := map[string]string{"LZ_ACCOUNT_ID": "fixture-ovh", "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz",
		"LZ_PROJECT_ID_STATE": exStateID, "LZ_PROJECT_ID_DEMO_DEV": exProjectID, "LZ_ADMIN_CLIENT_ID": "EU.fixture", "LZ_ADMIN_POLICY_ID": "p"}
	acct, err := BoundAccountFromEnv(env)
	if err != nil {
		t.Fatalf("BoundAccountFromEnv: %v", err)
	}
	want := stacks.BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"STATE": exStateID, "DEMO_DEV": exProjectID}}
	if !reflect.DeepEqual(acct, want) {
		t.Errorf("bound account %+v, want %+v", acct, want)
	}
	for name, change := range map[string]func(map[string]string){
		"no-endpoint":    func(e map[string]string) { delete(e, "OVH_ENDPOINT") },
		"empty-project":  func(e map[string]string) { e["LZ_PROJECT_ID_DEMO_DEV"] = "" },
		"project-not-id": func(e map[string]string) { e["LZ_PROJECT_ID_DEMO_DEV"] = "../" + exProjectID },
		"project-upper":  func(e map[string]string) { e["LZ_PROJECT_ID_DEMO_DEV"] = strings.ToUpper(exProjectID) },
		"ref-empty":      func(e map[string]string) { e["LZ_PROJECT_ID_"] = exProjectID },
		"ref-lower":      func(e map[string]string) { e["LZ_PROJECT_ID_demo"] = exProjectID },
	} {
		t.Run(name, func(t *testing.T) {
			e := map[string]string{}
			for k, v := range env {
				e[k] = v
			}
			change(e)
			if got, err := BoundAccountFromEnv(e); err == nil {
				t.Errorf("accepted: %+v", got)
			} else if strings.Contains(err.Error(), exProjectID) {
				t.Errorf("refusal echoes a value: %v", err)
			}
		})
	}
}

// A record is <dir>/<id>.json, private, and an id that would leave dir is refused with nothing
// written.
func TestPublishRecord(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "records")
	rec := Record{AppliedAt: "2026-10-07T00:00:00Z", SourceRevision: exRevision, CodeDigest: sha([]byte("code")),
		Consumed: map[string]string{exProject: sha([]byte("artefact"))}, Resolved: sha([]byte("resolved"))}
	if err := WriteRecord(dir, exRuntime, rec); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	path := filepath.Join(dir, exRuntime+".json")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("record mode %v, want 0600", fi.Mode().Perm())
	}
	var got Record
	if err := json.Unmarshal(mustRead(t, path), &got); err != nil || !reflect.DeepEqual(got, rec) {
		t.Errorf("record %+v (%v), want %+v", got, err, rec)
	}
	for _, id := range []string{"", ".", "..", "../escape", "a/b", "Demo"} {
		t.Run(fmt.Sprintf("%q", id), func(t *testing.T) {
			base := t.TempDir()
			sub := filepath.Join(base, "records")
			if err := WriteRecord(sub, id, rec); err == nil {
				t.Errorf("WriteRecord accepted id %q", id)
			}
			var written []string
			filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					written = append(written, p)
				}
				return nil
			})
			if len(written) > 0 {
				t.Errorf("refused id %q, but wrote %v", id, written)
			}
		})
	}
}
