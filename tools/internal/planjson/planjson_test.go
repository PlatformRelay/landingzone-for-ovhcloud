package planjson_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/planjson"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// The one plan decoder (T088). Its inputs are captured with the pinned OpenTofu: the
// `tofu show -json` plans of tests/fixtures/tofu-probes/protect/ (T063/T064, capture.sh) and the
// `tofu test -json` test_plan of the runtime stage with a slot (T032,
// tests/fixtures/outputs/captures/runtime-slot-plan.json). The test reads them a second way, as
// untyped JSON, so the decoder is checked against a reading that is not its own.

var (
	fixtures   = filepath.Join("..", "..", "..", "tests", "fixtures")
	protectDir = filepath.Join(fixtures, "tofu-probes", "protect")
)

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// slotPlan is the runtime-slot test_plan: one ovh_cloud_project_storage create.
func slotPlan(t *testing.T) []byte {
	t.Helper()
	var msg struct {
		Plan json.RawMessage `json:"test_plan"`
	}
	if err := json.Unmarshal(read(t, filepath.Join(fixtures, "outputs", "captures", "runtime-slot-plan.json")), &msg); err != nil || len(msg.Plan) == 0 {
		t.Fatalf("runtime-slot capture: %v", err)
	}
	return msg.Plan
}

type row struct {
	Address, PreviousAddress, Mode, Type, Deposed string
	Actions                                       []string
	After                                         string
}

// untyped reads the changes of a plan without the decoder.
func untyped(t *testing.T, plan []byte) []row {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(plan, &doc); err != nil {
		t.Fatal(err)
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	var out []row
	for _, c := range doc["resource_changes"].([]any) {
		rc := c.(map[string]any)
		change := rc["change"].(map[string]any)
		r := row{Address: str(rc, "address"), PreviousAddress: str(rc, "previous_address"), Mode: str(rc, "mode"), Type: str(rc, "type"), Deposed: str(rc, "deposed")}
		for _, a := range change["actions"].([]any) {
			r.Actions = append(r.Actions, a.(string))
		}
		after, _ := json.Marshal(change["after"])
		r.After = string(after)
		out = append(out, r)
	}
	return out
}

func typed(t *testing.T, plan []byte) []row {
	t.Helper()
	p, err := planjson.Decode(plan)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var out []row
	for _, c := range p.Changes {
		after := "null"
		if len(c.After) > 0 {
			var v any
			if err := json.Unmarshal(c.After, &v); err != nil {
				t.Fatalf("%s: after: %v", c.Address, err)
			}
			b, _ := json.Marshal(v)
			after = string(b)
		}
		mode, typ, err := c.Resource()
		if err != nil {
			t.Fatalf("%s: resource: %v", c.Address, err)
		}
		out = append(out, row{c.Address, c.PreviousAddress, mode, typ, c.Deposed, c.Actions, after})
	}
	return out
}

// TestDecodeCaptured: every captured plan decodes to the changes an untyped reading finds, in
// plan order: address, previous address, mode, type, deposed key, actions and the after object.
func TestDecodeCaptured(t *testing.T) {
	entries, err := os.ReadDir(protectDir)
	if err != nil {
		t.Fatal(err)
	}
	plans := map[string][]byte{"runtime-slot": slotPlan(t)}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || strings.HasSuffix(name, ".meta") || name == "truncated" || name == "state-not-plan" {
			continue
		}
		plans[name] = read(t, filepath.Join(protectDir, e.Name()))
	}
	if len(plans) < 18 {
		t.Fatalf("%d captured plans, want at least 18", len(plans))
	}
	moved := false
	for name, plan := range plans {
		want, got := untyped(t, plan), typed(t, plan)
		if len(want) == 0 {
			t.Errorf("%s: captured plan has no changes", name)
		}
		if !slices.EqualFunc(want, got, func(a, b row) bool {
			return a.Address == b.Address && a.PreviousAddress == b.PreviousAddress && a.Mode == b.Mode && a.Type == b.Type &&
				a.Deposed == b.Deposed && slices.Equal(a.Actions, b.Actions) && a.After == b.After
		}) {
			t.Errorf("%s: decoded %+v, want %+v", name, got, want)
		}
		for _, r := range got {
			moved = moved || r.PreviousAddress != ""
		}
	}
	if !moved {
		t.Error("no captured plan carries a previous address: the moved rows are not exercised")
	}
	slot := typed(t, plans["runtime-slot"])
	if !slices.ContainsFunc(slot, func(r row) bool {
		return r.Mode == "managed" && r.Type == "ovh_cloud_project_storage" && strings.Contains(r.After, `"name":"lz-demo-dev-gra11-bkt-runtime-blue"`)
	}) {
		t.Errorf("runtime-slot: no managed bucket with its planned name in %+v", slot)
	}
}

// TestDecodeSynthetic covers what no capture holds: a deposed object, no change object, empty
// actions, an unknown action (decoded as spelled, judged by the caller), no changes at all.
func TestDecodeSynthetic(t *testing.T) {
	const head = `{"format_version":"1.2","planned_values":{},`
	p, err := planjson.Decode([]byte(head + `"resource_changes":[` +
		`{"address":"a.b","mode":"managed","type":"a","deposed":"0a1b","change":{"actions":["delete"],"after":null}},` +
		`{"address":"a.c"},` +
		`{"address":"a.d","change":{"actions":[]}},` +
		`{"address":"a.e","change":{"actions":["purge"],"after":{"name":"x"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 4 || p.Changes[0].Deposed != "0a1b" || !slices.Equal(p.Changes[0].Actions, []string{"delete"}) ||
		p.Changes[1].Address != "a.c" || len(p.Changes[1].Actions) != 0 || len(p.Changes[2].Actions) != 0 ||
		!slices.Equal(p.Changes[3].Actions, []string{"purge"}) || string(p.Changes[3].After) != `{"name":"x"}` {
		t.Fatalf("decoded %+v", p.Changes)
	}
	p, err = planjson.Decode([]byte(head + `"resource_changes":[]}`))
	if err != nil || len(p.Changes) != 0 {
		t.Fatalf("minimal plan: %+v, %v", p, err)
	}
	p, err = planjson.Decode([]byte(`{"format_version":"1.0","planned_values":{}}`))
	if err != nil || len(p.Changes) != 0 {
		t.Fatalf("plan without changes: %+v, %v", p, err)
	}
}

// TestDecodeLeavesResourceToCaller (review round 1): `mode` and `type`, which live.Protect never
// read, do not fail the document when wrong-typed; Change.Resource reports it to the caller that
// reads them. So Protect admits and refuses exactly what it did before T088 (a covered delete
// refused, an uncovered one admitted), and stacks.PlannedBucketNames still refuses the plan.
func TestDecodeLeavesResourceToCaller(t *testing.T) {
	for _, c := range []struct{ name, fields string }{
		{"mode-number", `"mode":42,"type":"ovh_cloud_project_storage"`},
		{"type-array", `"mode":"managed","type":["ovh_cloud_project_storage"]`},
		{"mode-object", `"mode":{},"type":"ovh_cloud_project_storage"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			raw := []byte(`{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"a.b",` + c.fields +
				`,"change":{"actions":["delete"],"after":null}}]}`)
			p, err := planjson.Decode(raw)
			if err != nil || len(p.Changes) != 1 {
				t.Fatalf("decode: %+v, %v; want the change, its resource left to the caller", p, err)
			}
			if _, _, err := p.Changes[0].Resource(); err == nil {
				t.Error("Resource: no error for a wrong-typed mode or type")
			}
			if err := live.Protect(raw, []live.Retained{{Instance: "other", Addresses: []string{"a.c"}}}); err != nil {
				t.Errorf("live.Protect, address not retained: %v, want nil as before T088", err)
			}
			var r *live.Refusal
			if err := live.Protect(raw, []live.Retained{{Instance: "demo", Addresses: []string{"a.b"}}}); !errors.As(err, &r) || !strings.Contains(r.Detail, "a.b delete") {
				t.Errorf("live.Protect, address retained: %v, want the delete refused", err)
			}
			if names, err := stacks.PlannedBucketNames(raw); err == nil {
				t.Errorf("stacks.PlannedBucketNames: %q and no error, want a refusal", names)
			}
		})
	}
	p, err := planjson.Decode([]byte(`{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"a.b","mode":null,"change":{"actions":["no-op"]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if mode, typ, err := p.Changes[0].Resource(); err != nil || mode != "" || typ != "" {
		t.Errorf("null mode, absent type: %q, %q, %v; want empty, no error", mode, typ, err)
	}
}

// TestResourceKeepsTypedSemantics (review round 2): Resource reads `mode`, `type` and the planned
// object as typed fields would (the reading stacks.PlannedBucketNames had before T088): a
// duplicate key keeps a string over a later null, a wrong type anywhere is an error even when a
// later duplicate is a string, and a non-object `after` on any change is an error. live.Protect,
// which reads none of them, admits each of these documents on an uncovered address.
func TestResourceKeepsTypedSemantics(t *testing.T) {
	const head = `{"format_version":"1.2","planned_values":{},"resource_changes":[`
	bucket := func(fields string) string {
		return `{"address":"ovh_cloud_project_storage.b",` + fields + `,"change":{"actions":["create"],"after":{"name":"lz-b"}}}`
	}
	for _, c := range []struct {
		name, raw string
		names     []string // nil: refused
	}{
		{"duplicate-mode-null-after-string", head + bucket(`"mode":"managed","mode":null,"type":"ovh_cloud_project_storage"`) + `]}`, []string{"lz-b"}},
		{"duplicate-mode-string-after-number", head + bucket(`"mode":42,"mode":"managed","type":"ovh_cloud_project_storage"`) + `]}`, nil},
		{"after-number-on-other-resource", head + bucket(`"mode":"managed","type":"ovh_cloud_project_storage"`) +
			`,{"address":"a.b","mode":"managed","type":"a","change":{"actions":["create"],"after":42}}]}`, nil},
		{"after-number-on-deleted-bucket", head +
			`{"address":"ovh_cloud_project_storage.c","mode":"managed","type":"ovh_cloud_project_storage","change":{"actions":["delete"],"after":42}}]}`, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := live.Protect([]byte(c.raw), []live.Retained{{Instance: "other", Addresses: []string{"x.y"}}}); err != nil {
				t.Errorf("live.Protect, nothing covered: %v, want nil as before T088", err)
			}
			names, err := stacks.PlannedBucketNames([]byte(c.raw))
			if c.names == nil && err == nil {
				t.Errorf("stacks.PlannedBucketNames: %q and no error, want a refusal", names)
			}
			if c.names != nil && (err != nil || !slices.Equal(names, c.names)) {
				t.Errorf("stacks.PlannedBucketNames: %q, %v; want %q", names, err, c.names)
			}
		})
	}
}

// TestDecodeRefuses: a document that is not a complete plan of JSON format 1.x, or has the wrong
// shape, is ErrNotAPlan; a plan marked errored is ErrErrored. Both are checked before any change.
func TestDecodeRefuses(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  string
		want error
	}{
		{"empty", "", planjson.ErrNotAPlan},
		{"text", "Error: Failed to read plan file", planjson.ErrNotAPlan},
		{"null", "null", planjson.ErrNotAPlan},
		{"array", "[]", planjson.ErrNotAPlan},
		{"no-format-version", "{}", planjson.ErrNotAPlan},
		{"format-version-2", `{"format_version":"2.0","planned_values":{},"resource_changes":[]}`, planjson.ErrNotAPlan},
		{"format-version-10", `{"format_version":"10.1","planned_values":{},"resource_changes":[]}`, planjson.ErrNotAPlan},
		{"format-version-bare", `{"format_version":"1","planned_values":{},"resource_changes":[]}`, planjson.ErrNotAPlan},
		{"format-version-number", `{"format_version":1.2,"planned_values":{},"resource_changes":[]}`, planjson.ErrNotAPlan},
		{"no-planned-values", `{"format_version":"1.2","resource_changes":[]}`, planjson.ErrNotAPlan},
		{"null-planned-values", `{"format_version":"1.2","planned_values":null,"resource_changes":[]}`, planjson.ErrNotAPlan},
		{"wrong-type-changes", `{"format_version":"1.2","planned_values":{},"resource_changes":"none"}`, planjson.ErrNotAPlan},
		{"wrong-type-actions", `{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"a.b","change":{"actions":"delete"}}]}`, planjson.ErrNotAPlan},
		{"wrong-type-errored", `{"format_version":"1.2","planned_values":{},"errored":"no","resource_changes":[]}`, planjson.ErrNotAPlan},
		{"errored", `{"format_version":"1.2","planned_values":{},"errored":true,"resource_changes":[]}`, planjson.ErrErrored},
		{"errored-with-changes", `{"format_version":"1.2","planned_values":{},"errored":true,"resource_changes":[{"address":"a.b","change":{"actions":["create"]}}]}`, planjson.ErrErrored},
		{"truncated", string(read(t, filepath.Join(protectDir, "truncated.json"))), planjson.ErrNotAPlan},
		{"state-not-plan", string(read(t, filepath.Join(protectDir, "state-not-plan.json"))), planjson.ErrNotAPlan},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, err := planjson.Decode([]byte(c.raw))
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
			if len(p.Changes) != 0 {
				t.Fatalf("refused document still returned changes %+v", p.Changes)
			}
		})
	}
}

// TestKnownActions: the actions OpenTofu 1.13.0 was observed to plan (T063 captures: no-op,
// create, read, update, delete, forget, and both replacement orders) are known; anything else,
// or no action at all, is not.
func TestKnownActions(t *testing.T) {
	for _, a := range [][]string{{"no-op"}, {"create"}, {"read"}, {"update"}, {"delete"}, {"forget"}, {"delete", "create"}, {"create", "delete"}} {
		if !planjson.KnownActions(a) {
			t.Errorf("%q not known", a)
		}
	}
	for _, a := range [][]string{nil, {}, {"purge"}, {"create", "purge"}, {"purge", "create"}, {""}, {"Delete"}, {"replace"}} {
		if planjson.KnownActions(a) {
			t.Errorf("%q known", a)
		}
	}
}

// TestCallersRefuseIdentically: the same documents go to both callers, live.Protect (with the
// bucket retained) and stacks.PlannedBucketNames. Both refuse a document the decoder refuses and
// an unknown or missing action on the change they judge, and both admit a plain bucket create.
func TestCallersRefuseIdentically(t *testing.T) {
	const bucket = "ovh_cloud_project_storage.bucket"
	retained := []live.Retained{{Instance: "demo", Addresses: []string{bucket}}}
	change := func(actions string) string {
		return `{"address":"` + bucket + `","mode":"managed","type":"ovh_cloud_project_storage","change":{"actions":` + actions + `,"after":{"name":"lz-b"}}}`
	}
	plan := func(extra, changes string) string {
		return `{"format_version":"1.2","planned_values":{}` + extra + `,"resource_changes":[` + changes + `]}`
	}
	for _, c := range []struct {
		name, raw string
		decoder   error // the decoder's refusal both callers rest on; nil: a refusal of the change
	}{
		{"wrong-type", plan("", `"none"`), planjson.ErrNotAPlan},
		{"wrong-type-actions", plan("", change(`"create"`)), planjson.ErrNotAPlan},
		{"errored", plan(`,"errored":true`, change(`["create"]`)), planjson.ErrErrored},
		{"no-planned-values", `{"format_version":"1.2","resource_changes":[` + change(`["create"]`) + `]}`, planjson.ErrNotAPlan},
		{"format-version-2", `{"format_version":"2.0","planned_values":{},"resource_changes":[` + change(`["create"]`) + `]}`, planjson.ErrNotAPlan},
		{"state-not-plan", string(read(t, filepath.Join(protectDir, "state-not-plan.json"))), planjson.ErrNotAPlan},
		{"unknown-action", plan("", change(`["purge"]`)), nil},
		{"unknown-action-in-replacement", plan("", change(`["create","purge"]`)), nil},
		{"empty-actions", plan("", change(`[]`)), nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, decErr := planjson.Decode([]byte(c.raw))
			if c.decoder != nil && !errors.Is(decErr, c.decoder) {
				t.Fatalf("decoder: %v, want %v", decErr, c.decoder)
			}
			if c.decoder == nil && decErr != nil {
				t.Fatalf("decoder refused a readable plan: %v", decErr)
			}
			var r *live.Refusal
			if err := live.Protect([]byte(c.raw), retained); !errors.As(err, &r) || r.Condition != live.CondRetained {
				t.Errorf("live.Protect: %v, want a retained refusal", err)
			}
			names, err := stacks.PlannedBucketNames([]byte(c.raw))
			if err == nil {
				t.Errorf("stacks.PlannedBucketNames: %q and no error, want a refusal", names)
			} else if c.decoder != nil && !errors.Is(err, c.decoder) {
				t.Errorf("stacks.PlannedBucketNames: %v, want it to carry %v", err, c.decoder)
			}
		})
	}
	t.Run("admit-create", func(t *testing.T) {
		raw := []byte(plan("", change(`["create"]`)))
		if err := live.Protect(raw, retained); err != nil {
			t.Errorf("live.Protect: %v", err)
		}
		if names, err := stacks.PlannedBucketNames(raw); err != nil || !slices.Equal(names, []string{"lz-b"}) {
			t.Errorf("stacks.PlannedBucketNames: %q, %v", names, err)
		}
	})
}
