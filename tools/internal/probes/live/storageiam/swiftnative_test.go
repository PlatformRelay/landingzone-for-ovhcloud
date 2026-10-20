package storageiam

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// T041: what the Swift-native control records beyond T040's cases, in T025's world (both S3 bucket
// names 404 on Swift, the native containers 2xx).

// TestSwiftNotApplicableRecord: every step not-applicable is the run's result, not a failure:
// Observe returns no error, and the record keeps the containers' create answers and each tenant's
// control with its three statuses.
func TestSwiftNotApplicableRecord(t *testing.T) {
	// Exposes not-applicable counted as "C005.1 not shown", or a control the record does not show.
	w := newSwWorld(t)
	w.s3Absent = true
	raw, term, err := swObserve(t, w, SwiftReach(w.options()))
	if err != nil {
		t.Errorf("Observe = %v, want nil: not-applicable is neither a finding nor unproven", err)
	}
	var rec struct {
		Containers    map[string]string `json:"containers"`
		NativeControl []struct {
			Credential, Container string
			List                  int  `json:"list_status"`
			Write                 int  `json:"write_status"`
			Read                  int  `json:"read_status"`
			Works                 bool `json:"works"`
		} `json:"native_control"`
	}
	if jerr := json.Unmarshal(raw, &rec); jerr != nil {
		t.Fatalf("record %s: %v", raw, jerr)
	}
	if got := slices.Sorted(maps.Keys(rec.Containers)); !slices.Equal(got, []string{swContA, swContB}) ||
		!strings.HasPrefix(rec.Containers[swContA], "created 200") || !strings.HasPrefix(rec.Containers[swContB], "created 200") {
		t.Errorf("containers %v, want both created 200", rec.Containers)
	}
	want := map[string]string{"user-a": swContA, "user-b": swContB}
	for _, c := range rec.NativeControl {
		if want[c.Credential] != c.Container || c.List != 200 || c.Write != 201 || c.Read != 200 || !c.Works {
			t.Errorf("native control %+v, want its own container, 200/201/200 and works", c)
		}
		delete(want, c.Credential)
	}
	if len(want) != 0 {
		t.Errorf("no native control recorded for %v", slices.Sorted(maps.Keys(want)))
	}
	if !strings.Contains(term, "LZ-LIVE observe a-list-b not-applicable") {
		t.Errorf("terminal does not show a-list-b not-applicable:\n%s", term)
	}
}
