package stacks

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/planjson"
)

// TestPlannedBucketNamesRefusesUnreadable (T088, review round 1): the bucket name check reads
// plans through planjson, so test:stacks judges its own refusals: the captured runtime-slot
// test_plan (T032) altered one way at a time is refused when it is marked errored or is not a
// complete plan (the decoder's sentinels), and when its bucket has an unknown, no or a
// wrong-typed action or resource field.
func TestPlannedBucketNamesRefusesUnreadable(t *testing.T) {
	var msg struct {
		Plan json.RawMessage `json:"test_plan"`
	}
	if err := json.Unmarshal(stagePlanFile(t, "runtime-slot"), &msg); err != nil {
		t.Fatal(err)
	}
	alter := func(t *testing.T, edit func(doc, bucket map[string]any)) []byte {
		t.Helper()
		var doc map[string]any
		if err := json.Unmarshal(msg.Plan, &doc); err != nil {
			t.Fatal(err)
		}
		var bucket map[string]any
		for _, c := range doc["resource_changes"].([]any) {
			if rc := c.(map[string]any); rc["type"] == "ovh_cloud_project_storage" {
				bucket = rc
			}
		}
		if bucket == nil {
			t.Fatal("no bucket in the captured slot plan")
		}
		edit(doc, bucket)
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	actions := func(a any) func(doc, bucket map[string]any) {
		return func(_, bucket map[string]any) { bucket["change"].(map[string]any)["actions"] = a }
	}
	for _, c := range []struct {
		name string
		edit func(doc, bucket map[string]any)
		want error // the decoder's sentinel; nil: a refusal of the bucket change
	}{
		{"errored", func(doc, _ map[string]any) { doc["errored"] = true }, planjson.ErrErrored},
		{"no-planned-values", func(doc, _ map[string]any) { delete(doc, "planned_values") }, planjson.ErrNotAPlan},
		{"format-version-2", func(doc, _ map[string]any) { doc["format_version"] = "2.0" }, planjson.ErrNotAPlan},
		{"actions-wrong-type", actions("create"), planjson.ErrNotAPlan},
		{"unknown-action", actions([]any{"purge"}), nil},
		{"unknown-action-in-replacement", actions([]any{"delete", "purge"}), nil},
		{"no-actions", actions([]any{}), nil},
		{"mode-wrong-type", func(_, bucket map[string]any) { bucket["mode"] = 42 }, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			names, err := PlannedBucketNames(alter(t, c.edit))
			if err == nil {
				t.Fatalf("%q and no error, want a refusal", names)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("%v, want it to carry %v", err, c.want)
			}
		})
	}
	t.Run("unaltered", func(t *testing.T) {
		if names, err := PlannedBucketNames(alter(t, func(_, _ map[string]any) {})); err != nil || len(names) != 1 {
			t.Fatalf("%q, %v; want the one slot bucket", names, err)
		}
	})
}

// TestPlannedBucketNamesForget (T112, T038 gap 12): a bucket the plan forgets (a `removed`
// block with `destroy = false`: actions ["forget"], `after` null) leaves the stack's state but
// not the account; it is skipped like a delete: neither a planned name nor an error, so the
// check judges planned names only and a clash with the forgotten bucket is the apply's. The
// forget change is the pinned OpenTofu's own (tests/fixtures/tofu-probes/protect/forget.json,
// T063), put on the bucket of the captured runtime-slot plan (T032). A replacement still yields
// its new name, an unknown action next to forget is still refused, and NAME_COLLISION still
// holds for created and kept buckets.
func TestPlannedBucketNamesForget(t *testing.T) {
	const bucket = "lz-demo-dev-gra11-bkt-runtime-blue"
	var msg struct {
		Plan json.RawMessage `json:"test_plan"`
	}
	if err := json.Unmarshal(stagePlanFile(t, "runtime-slot"), &msg); err != nil {
		t.Fatal(err)
	}
	forget := forgetChange(t)
	// withBucket returns the captured slot plan with its one bucket's change replaced by what
	// edit returns for the captured change.
	withBucket := func(t *testing.T, edit func(captured map[string]any) map[string]any) []byte {
		t.Helper()
		var doc map[string]any
		if err := json.Unmarshal(msg.Plan, &doc); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range doc["resource_changes"].([]any) {
			if rc := c.(map[string]any); rc["type"] == "ovh_cloud_project_storage" {
				rc["change"] = edit(rc["change"].(map[string]any))
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%d buckets in the captured slot plan, want 1", n)
		}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	forgotten := withBucket(t, func(c map[string]any) map[string]any {
		f := map[string]any{}
		for k, v := range forget {
			f[k] = v
		}
		f["before"] = c["after"] // the bucket as it stands in state
		f["before_sensitive"] = c["after_sensitive"]
		return f
	})
	actions := func(a ...any) []byte {
		return withBucket(t, func(c map[string]any) map[string]any {
			if len(a) == 1 && a[0] == "no-op" {
				c["before"] = c["after"]
			}
			c["actions"] = a
			return c
		})
	}
	kept := actions("no-op")
	created := actions("create")

	t.Run("forget-skipped", func(t *testing.T) {
		names, err := PlannedBucketNames(forgotten)
		if err != nil || len(names) != 0 {
			t.Fatalf("%q, %v; a forgotten bucket is neither a planned name nor an error", names, err)
		}
	})
	t.Run("forget-does-not-collide", func(t *testing.T) {
		if err := CheckPlannedNames([]StackPlan{
			{Stack: "stacks/tenants/demo/dev/gra11/runtime-blue", Plan: forgotten},
			{Stack: "stacks/tenants/demo/dev/gra11/runtime-green", Plan: created},
		}); err != nil {
			t.Fatalf("a bucket one stack forgets and another creates: %v, want no collision", err)
		}
	})
	for _, a := range [][]any{{"delete", "create"}, {"create", "delete"}} {
		t.Run("replace-"+a[0].(string)+"-first", func(t *testing.T) {
			names, err := PlannedBucketNames(actions(a...))
			if err != nil || !slices.Equal(names, []string{bucket}) {
				t.Fatalf("%q, %v; a replacement yields its new name", names, err)
			}
		})
	}
	t.Run("forget-with-unknown", func(t *testing.T) {
		if names, err := PlannedBucketNames(actions("forget", "purge")); err == nil {
			t.Fatalf("%q and no error; an unknown action is refused", names)
		}
	})
	for name, plans := range map[string][2][]byte{"kept-twice": {kept, kept}, "created-and-kept": {created, kept}} {
		t.Run(name, func(t *testing.T) {
			err := CheckPlannedNames([]StackPlan{
				{Stack: "stacks/tenants/demo/dev/gra11/runtime-blue", Plan: plans[0]},
				{Stack: "stacks/tenants/demo/dev/gra11/runtime-green", Plan: plans[1]},
			})
			var ge *GenerateError
			if !errors.As(err, &ge) || ge.Code != CodeNameCollision || ge.Path != "stacks/tenants/demo/dev/gra11/runtime-green" || !strings.Contains(ge.Detail, bucket) {
				t.Fatalf("%v, want %s at the second stack naming %s", err, CodeNameCollision, bucket)
			}
		})
	}
}

// forgetChange returns the `change` of the one ["forget"] entry the pinned OpenTofu wrote in
// tests/fixtures/tofu-probes/protect/forget.json, after checking that its sidecar vouches for it.
func forgetChange(t *testing.T) map[string]any {
	t.Helper()
	const dir = "../../../tests/fixtures/tofu-probes/protect/"
	data := readFile(t, dir+"forget.json")
	var meta struct {
		File        string `json:"file"`
		SHA256      string `json:"sha256"`
		ToolVersion string `json:"tool_version"`
		EntryExit   int    `json:"entry_exit"`
	}
	if err := json.Unmarshal(readFile(t, dir+"forget.meta.json"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.File != "forget.json" || meta.SHA256 != digest(data) || meta.ToolVersion != "1.13.0" || meta.EntryExit != 0 {
		t.Fatalf("forget.meta.json does not vouch for forget.json from the pinned tofu: %+v", meta)
	}
	var doc struct {
		ResourceChanges []struct {
			Change map[string]any `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var found []map[string]any
	for _, rc := range doc.ResourceChanges {
		if a, _ := rc.Change["actions"].([]any); len(a) == 1 && a[0] == "forget" {
			found = append(found, rc.Change)
		}
	}
	if len(found) != 1 || found[0]["after"] != nil {
		t.Fatalf("%d forget changes in forget.json (want 1, with a null after): %v", len(found), found)
	}
	return found[0]
}
