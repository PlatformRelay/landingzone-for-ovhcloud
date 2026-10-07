package stacks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Producer→consumer pin of 005 T022 (T018 review gap): the tenant-state stage's real plan, made by
// its own unit tests on the pinned OpenTofu (mocked provider), goes through BuildEnvelope and the
// result validates under schemas/outputs/tenant-state.schema.json with exactly the schema's
// published values and no sensitive value.
//
// Fixture: tests/fixtures/outputs/captures/tenant-state-plan.json is one `tofu test -json -verbose`
// test_plan message (run `published_outputs_match_the_schema`), captured through the offline entry
// by tests/fixtures/outputs/capture-tenant-state.sh; its .meta.json sidecar vouches for the tool
// and for the configuration it was made from. The plan's root `output_changes` are read as the
// `tofu output -json` entries an apply would print: `after` is the value, `after_sensitive` the
// marker (anything but false counts as sensitive), and an unknown value is refused.

const repoRoot = "../../.."

// stagePlanInputs are the directories the tenant-state plan is made from; test configuration
// counts only for the stage itself (its mocks give the values). Keep in step with inputs() in
// capture-tenant-state.sh.
var stagePlanInputs = []string{"stages/tenant-state", "components/state-backend", "modules/naming", "modules/object-storage-protected", "modules/object-storage-user"}

// stagePlanInputsDigest is the sha256 of `<sha256>  <path>` lines, in byte order of path, over
// every file of stagePlanInputs a plan can read: configuration, data files such as naming's
// kinds.yaml and the dependency lock files; not Markdown, state files or hidden directories
// (sha256sum's format, so the capture script computes the same digest).
func stagePlanInputsDigest(t *testing.T) string {
	t.Helper()
	var paths []string
	for _, dir := range stagePlanInputs {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repoRoot, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			switch {
			case d.IsDir() && strings.HasPrefix(d.Name(), "."):
				return fs.SkipDir
			case d.IsDir(), strings.HasSuffix(rel, ".md"), strings.Contains(d.Name(), ".tfstate"):
				return nil
			case strings.Contains(rel, "/tests/") && !strings.HasPrefix(rel, "stages/tenant-state/"):
				return nil
			}
			paths = append(paths, rel)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(paths)
	var lines strings.Builder
	for _, p := range paths {
		lines.WriteString(digest(readFile(t, filepath.Join(repoRoot, p))) + "  " + p + "\n")
	}
	sum := sha256.Sum256([]byte(lines.String()))
	return hex.EncodeToString(sum[:])
}

// stagePlan returns the captured plan after checking that its sidecar vouches for exactly this
// file, made from the current configuration by the pinned OpenTofu through the isolated entry.
func stagePlan(t *testing.T) []byte {
	t.Helper()
	data := readFile(t, fixtureDir+"/captures/tenant-state-plan.json")
	var meta struct {
		File         string `json:"file"`
		Command      string `json:"command"`
		Toolchain    string `json:"toolchain"`
		Tool         string `json:"tool"`
		ToolVersion  string `json:"tool_version"`
		EntryExit    int    `json:"entry_exit"`
		SHA256       string `json:"sha256"`
		InputsSHA256 string `json:"inputs_sha256"`
	}
	if err := json.Unmarshal(readFile(t, fixtureDir+"/captures/tenant-state-plan.json.meta.json"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.File != "tenant-state-plan.json" || meta.SHA256 != digest(data) || meta.EntryExit != 0 ||
		!strings.Contains(meta.Command, "capture:tenant-state-plan") || meta.Tool != "tofu" || meta.ToolVersion != "1.13.0" ||
		!strings.Contains(meta.Toolchain, " tofu=1.13.0 ") || !strings.HasSuffix(meta.Toolchain, " network=none") {
		t.Fatalf("capture sidecar does not vouch for tenant-state-plan.json from the pinned tofu: %+v", meta)
	}
	if meta.InputsSHA256 != stagePlanInputsDigest(t) {
		t.Fatalf("capture is stale: the tenant-state configuration changed since it was captured; rerun tests/fixtures/outputs/capture-tenant-state.sh")
	}
	return data
}

// credentialOutputs are the stage's sensitive outputs (data-model *Sensitive outputs*): their
// values are collected whatever marker the plan gives them, so a lost marker shows as a leak.
var credentialOutputs = []string{"tenant_s3", "platform_s3"}

// planOutputs turns the root output changes of one test_plan message into a `tofu output -json`
// document, and returns the string leaves of the sensitive and the credential entries.
func planOutputs(t *testing.T, data []byte) (doc []byte, secrets []string) {
	t.Helper()
	var msg struct {
		Type string `json:"type"`
		Run  string `json:"@testrun"`
		Plan struct {
			Errored bool `json:"errored"`
			Outputs map[string]struct {
				After          json.RawMessage `json:"after"`
				AfterSensitive json.RawMessage `json:"after_sensitive"`
				AfterUnknown   json.RawMessage `json:"after_unknown"`
			} `json:"output_changes"`
		} `json:"test_plan"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "test_plan" || msg.Run != "published_outputs_match_the_schema" || msg.Plan.Errored || len(msg.Plan.Outputs) == 0 {
		t.Fatalf("capture is not the plan of run published_outputs_match_the_schema: type %q run %q errored %v outputs %d", msg.Type, msg.Run, msg.Plan.Errored, len(msg.Plan.Outputs))
	}
	type entry struct {
		Sensitive bool            `json:"sensitive"`
		Value     json.RawMessage `json:"value"`
	}
	entries := map[string]entry{}
	for name, o := range msg.Plan.Outputs {
		if string(o.AfterUnknown) != "false" {
			t.Fatalf("output %s is not known in the plan (after_unknown %s)", name, o.AfterUnknown)
		}
		sensitive := string(o.AfterSensitive) != "false"
		if sensitive || slices.Contains(credentialOutputs, name) {
			var v any
			if err := json.Unmarshal(o.After, &v); err != nil {
				t.Fatal(err)
			}
			secrets = append(secrets, leaves(v)...)
		}
		entries[name] = entry{Sensitive: sensitive, Value: o.After}
	}
	doc, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return doc, secrets
}

// leaves lists the non-empty string leaves of a decoded JSON value.
func leaves(v any) []string {
	switch x := v.(type) {
	case string:
		if x != "" {
			return []string{x}
		}
	case map[string]any:
		var out []string
		for _, e := range x {
			out = append(out, leaves(e)...)
		}
		return out
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, leaves(e)...)
		}
		return out
	}
	return nil
}

func TestOutputsTenantStateStagePlan(t *testing.T) {
	doc, secrets := planOutputs(t, stagePlan(t))
	// The two credentials, two fields each: the control that the secrets scan below can fail.
	if len(secrets) != 4 {
		t.Fatalf("want the four credential strings of tenant_s3 and platform_s3, got %d", len(secrets))
	}
	env, err := BuildEnvelope(doc, Producer{InstanceID: "demo-state", Stage: "tenant-state", SourceRevision: testRev})
	if err != nil {
		t.Fatalf("builder refused the stage's outputs: %v", err)
	}
	published, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ValidateEnvelope(schemas(), published, expectation("tenant-state", "demo-state"))
	if err != nil {
		t.Fatalf("envelope built from the stage's plan does not validate: %v", err)
	}
	var names []string
	for name := range got.Values {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"platform_s3_user_id", "state_bucket", "tenant", "tenant_s3_user_id", "unlabelled"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("published values %v, want exactly the schema's %v", names, want)
	}
	for _, s := range secrets {
		if strings.Contains(string(published), s) {
			t.Errorf("envelope carries a value of a sensitive output")
		}
	}
}
