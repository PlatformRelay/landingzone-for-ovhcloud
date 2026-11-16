package live

import (
	"slices"
	"testing"
)

// TestLeftoverMatchReason: each way a listed resource becomes the run's candidate is named, so a
// report says why an item is a leftover (D132 B1: prefix, inventory, parent), and a resource that
// matches nothing is not one.
func TestLeftoverMatchReason(t *testing.T) {
	t.Parallel()
	const run = "20261116T105653Z-29ec"
	c := LeftoverCheck{Prefix: "lzprobe-", RunID: run, Before: map[string][]string{tAlert: {"alert-before"}}}
	created := map[string]bool{tNetwork + "\x00" + "pn-7": true}
	parent := &item{typ: tStorage, id: "lzprobe-bucket", name: "lzprobe-bucket"}
	for _, tc := range []struct {
		name string
		it   *item
		want string
	}{
		{"prefix", &item{typ: tStorage, id: "x", name: "lzprobe-x"}, MatchPrefix},
		{"run id in a tag", &item{typ: tClient, id: "EU.1", name: "deployer", values: []string{"run=" + run}}, MatchRunID},
		{"inventory", &item{typ: tNetwork, id: "pn-7", name: "lz2-net"}, MatchInventory},
		{"absent from the baseline", &item{typ: tAlert, id: "alert-new"}, MatchBaseline},
		{"parent", &item{typ: tCred, id: "u-1", name: "user", parent: parent}, MatchParent},
		{"in the baseline", &item{typ: tAlert, id: "alert-before"}, ""},
		// Several rules match: the strongest evidence wins (review F1). A chain's prefix is shared
		// with retained infrastructure, so a recorded resource must not read prefix.
		{"inventory over prefix", &item{typ: tNetwork, id: "pn-7", name: "lzprobe-net"}, MatchInventory},
		{"inventory over run id", &item{typ: tNetwork, id: "pn-7", name: "net-" + run}, MatchInventory},
		{"run id over prefix", &item{typ: tStorage, id: "b", name: "lzprobe-" + run}, MatchRunID},
		{"nothing", &item{typ: tStorage, id: "backups", name: "backups"}, ""},
	} {
		if got := c.candidate(tc.it, created); got != tc.want {
			t.Errorf("%s: match %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestLeftoverMatchInReport: every leftover of a seeded capture carries one of the named reasons.
func TestLeftoverMatchInReport(t *testing.T) {
	t.Parallel()
	p, w := loadCapturedWorld(t, "lzprobe-")
	for name, s := range capturedSeeds(p, w) {
		rep, _ := checkCaptured(t, w, s, nil)
		for _, l := range rep.Leftovers {
			if !slices.Contains([]string{MatchPrefix, MatchRunID, MatchInventory, MatchBaseline, MatchParent}, l.Match) {
				t.Errorf("%s: leftover %s %s has match %q", name, l.Type, l.ID, l.Match)
			}
		}
	}
}
