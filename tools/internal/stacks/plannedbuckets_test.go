package stacks

import (
	"encoding/json"
	"errors"
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
