package live

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Spec 007 FR-015 (T038): a leftover taken for the run's because its parent is a candidate
// (`match: parent`) names that parent: `parent_id`, the parent's listed id as the report's `id`
// lists it, and `parent_match`, the reason the parent was taken. Every other leftover, and the
// rest of leftovers.json, is unchanged. The fixtures are the idWorld fake listings of
// leftovers_ids_test.go; the ids are this file's own.

// parentOf is the parent type a child kind implies (FR-015): a subnet's is a network, an S3
// credential's and an S3 policy's a user.
var parentOf = map[string]string{tSubnet: tNetwork, tCred: tUser, tS3Pol: tUser}

// parentFixture is one case's world: its listings, the run's inventory, the retained instances'
// ids, and, for each child the fixture puts under a parent, that parent ("<type> <id>" both).
type parentFixture struct {
	name     string
	build    func(w *idWorld)
	inv      []InventoryEntry
	retained map[string][]string
	parents  map[string]string
}

// Case (1): two run users, 4811 in the inventory and 4812 matched only by its prefix, each with
// an S3 credential.
var parentTwoUsers = parentFixture{
	name: "two-users",
	build: func(w *idWorld) {
		w.user(idProject, "4811", "demo-platform-s3", false, "c0000000000000000000000000004811")
		w.user(idProject, "4812", "lz2-other-s3", false, "c0000000000000000000000000004812")
	},
	inv: []InventoryEntry{inv(tUser, "4811")},
	parents: map[string]string{
		tCred + " c0000000000000000000000000004811": tUser + " 4811",
		tCred + " c0000000000000000000000000004812": tUser + " 4812",
	},
}

// Case (2): a subnet under a network whose name holds the run id, and an S3 policy under a user
// whose description holds it.
var parentRunID = parentFixture{
	name: "run-id-parents",
	build: func(w *idWorld) {
		w.network(idProject, "pn-1500021_0", "demo-net-"+idRun, "00000000-0000-4000-8000-000000004821")
		w.user(idProject, "4821", "demo-"+idRun, true)
	},
	parents: map[string]string{
		tSubnet + " 00000000-0000-4000-8000-000000004821": tNetwork + " pn-1500021_0",
		tS3Pol + " 4821": tUser + " 4821",
	},
}

// Case (3): a credential under a retained user whose name carries the prefix.
var parentRetained = parentFixture{
	name: "retained-user",
	build: func(w *idWorld) {
		w.user(idProject, "4831", "lz2-retained-s3", false, "c0000000000000000000000000004831")
	},
	retained: map[string][]string{tUser: {"4831"}},
	parents:  map[string]string{tCred + " c0000000000000000000000000004831": tUser + " 4831"},
}

// Case (5), first half: a credential in the run's inventory (matches directly) under a user that
// matches by prefix, next to a bucket, a network and a client matched for other reasons.
var parentDirect = parentFixture{
	name: "direct-child",
	build: func(w *idWorld) {
		w.user(idProject, "4851", "lz2-direct-s3", false, "c0000000000000000000000000004851")
		w.bucket(idProject, "GRA", "lz2-demo-bkt")
		w.network(idProject, "pn-1500051_0", "demo-net")
		w.client("EU.0000000000004851", "deployer-"+idRun)
	},
	inv: []InventoryEntry{inv(tCred, "c0000000000000000000000000004851"), inv(tNetwork, "pn-1500051_0")},
	parents: map[string]string{
		tCred + " c0000000000000000000000000004851": tUser + " 4851",
	},
}

var parentFixtures = []parentFixture{parentTwoUsers, parentRunID, parentRetained, parentDirect}

// run lists the fixture's world and runs the leftover check, failing on a listing error.
func (f parentFixture) run(t *testing.T) Report {
	t.Helper()
	w := newIDWorld(t, idProject)
	f.build(w)
	c := LeftoverCheck{Lister: &fakeLister{responses: w.resp}, Prefix: "lz2-", RunID: idRun, Backoff: []time.Duration{},
		Retained: f.retained, Projects: []Project{{ID: idProject, URN: idURN(idProject)}}}
	rep := c.Check(context.Background(), f.inv)
	if len(rep.Errors) > 0 {
		t.Fatalf("%s: listing errors: %v", f.name, rep.Errors)
	}
	return rep
}

// parentLeftover is the leftover (typ, id), failing when the report does not hold it.
func parentLeftover(t *testing.T, rep Report, typ, id string) Leftover {
	t.Helper()
	for _, l := range rep.Leftovers {
		if l.Type == typ && l.ID == id {
			return l
		}
	}
	t.Fatalf("no leftover %s %s in %v", typ, id, leftoverKeys(rep.Leftovers))
	return Leftover{}
}

// wantParent checks that the leftover (typ, id) is parent-matched and names parentID and
// parentMatch.
func wantParent(t *testing.T, rep Report, typ, id, parentID, parentMatch string) {
	t.Helper()
	l := parentLeftover(t, rep, typ, id)
	if l.Match != MatchParent {
		t.Fatalf("%s %s: match %q, want %q (fixture)", typ, id, l.Match, MatchParent)
	}
	if l.ParentID != parentID || l.ParentMatch != parentMatch {
		t.Errorf("%s %s: parent_id %q parent_match %q, want %q %q (FR-015: names its own parent and why it was taken)",
			typ, id, l.ParentID, l.ParentMatch, parentID, parentMatch)
	}
}

func TestLeftoverParentTwoUsers(t *testing.T) {
	t.Parallel()
	rep := parentTwoUsers.run(t)
	wantParent(t, rep, tCred, "c0000000000000000000000000004811", "4811", MatchInventory)
	wantParent(t, rep, tCred, "c0000000000000000000000000004812", "4812", MatchPrefix)
}

func TestLeftoverParentRunID(t *testing.T) {
	t.Parallel()
	rep := parentRunID.run(t)
	wantParent(t, rep, tSubnet, "00000000-0000-4000-8000-000000004821", "pn-1500021_0", MatchRunID)
	wantParent(t, rep, tS3Pol, "4821", "4821", MatchRunID)
}

// Case (3): a retained parent excludes its children; neither the user nor its credential is a
// leftover (holds before FR-015).
func TestLeftoverParentRetained(t *testing.T) {
	t.Parallel()
	rep := parentRetained.run(t)
	if len(rep.Leftovers) != 0 {
		t.Errorf("retained user and its credential: leftovers %v, want none", leftoverKeys(rep.Leftovers))
	}
}

// Case (4): in every case's report, each parent-matched leftover names the parent the fixture put
// it under, of the type its kind implies, which is itself a leftover of the report, and whose
// match is neither empty nor parent; every other leftover of those reports carries neither field
// (case 5 in reports that hold parent matches, review r1).
func TestLeftoverParentConsistent(t *testing.T) {
	t.Parallel()
	for _, f := range parentFixtures {
		rep := f.run(t)
		for _, l := range rep.Leftovers {
			if l.Match == MatchParent {
				parentConsistent(t, f, rep, l)
			} else if l.ParentID != "" || l.ParentMatch != "" {
				t.Errorf("%s: %s %s (match %s): parent_id %q parent_match %q, want neither (FR-015: only match parent)",
					f.name, l.Type, l.ID, l.Match, l.ParentID, l.ParentMatch)
			}
		}
	}
}

func parentConsistent(t *testing.T, f parentFixture, rep Report, l Leftover) {
	t.Helper()
	key := l.Type + " " + l.ID
	want, ok := f.parents[key]
	if !ok {
		t.Errorf("%s: %s is parent-matched but the fixture put it under no parent", f.name, key)
		return
	}
	if got := parentOf[l.Type] + " " + l.ParentID; got != want {
		t.Errorf("%s: %s names parent %q, want %q (the fixture's, of type %s)", f.name, key, got, want, parentOf[l.Type])
	}
	if l.ParentMatch == "" || l.ParentMatch == MatchParent {
		t.Errorf("%s: %s parent_match %q: never empty or parent (parents are one level deep)", f.name, key, l.ParentMatch)
	}
	for _, p := range rep.Leftovers {
		if p.Type == parentOf[l.Type] && p.ID == l.ParentID {
			if p.Match != l.ParentMatch {
				t.Errorf("%s: %s parent_match %q, its parent's match is %q", f.name, key, l.ParentMatch, p.Match)
			}
			return
		}
	}
	t.Errorf("%s: %s names parent %s %q, which is not a leftover of the report", f.name, key, parentOf[l.Type], l.ParentID)
}

// Case (5): a child that matches directly under a matching parent, and every leftover matched for
// another reason, carry neither field (holds before FR-015; fails once the fields are filled
// whenever a parent exists).
func TestLeftoverParentDirectChild(t *testing.T) {
	t.Parallel()
	rep := parentDirect.run(t)
	if l := parentLeftover(t, rep, tCred, "c0000000000000000000000000004851"); l.Match != MatchInventory {
		t.Fatalf("credential in the inventory: match %q, want %q (fixture)", l.Match, MatchInventory)
	}
	if l := parentLeftover(t, rep, tUser, "4851"); l.Match != MatchPrefix {
		t.Fatalf("its user: match %q, want %q (fixture)", l.Match, MatchPrefix)
	}
	for _, l := range rep.Leftovers {
		if l.Match != MatchParent && (l.ParentID != "" || l.ParentMatch != "") {
			t.Errorf("%s %s (match %s): parent_id %q parent_match %q, want neither (FR-015: only match parent)",
				l.Type, l.ID, l.Match, l.ParentID, l.ParentMatch)
		}
	}
}

// parentNoneJSON is leftovers.json for parentNone's report as the writer produced it before FR-015
// (taken at 326e5da, before the stub fields existed).
const parentNoneJSON = `{"outcome":"fail","leftovers":[{"type":"ovh_cloud_project_storage","id":"lz2-demo-bkt","name":"lz2-demo-bkt","match":"prefix"},{"type":"ovh_cloud_project_network_private","id":"pn-1500052_0","name":"demo-net","match":"inventory"},{"type":"ovh_cloud_project_user","id":"4852","name":"demo-20261116T120049Z-0013","match":"run-id"},{"type":"ovh_me_api_oauth2_client","id":"EU.0000000000004852","name":"lz2-deployer","match":"prefix"}],"errors":null,"attempts":null}`

// parentNone is a non-empty report without parent matches: a bucket by prefix, a network by the
// inventory, a user by run id (no credential, no policy) and a client by prefix.
var parentNone = parentFixture{
	name: "no-parent",
	build: func(w *idWorld) {
		w.bucket(idProject, "GRA", "lz2-demo-bkt")
		w.network(idProject, "pn-1500052_0", "demo-net")
		w.user(idProject, "4852", "demo-"+idRun, false)
		w.client("EU.0000000000004852", "lz2-deployer")
	},
	inv: []InventoryEntry{inv(tNetwork, "pn-1500052_0")},
}

// Case (5), second half: leftovers.json of a report without parent matches is byte for byte what
// it was. The bytes go through the writer's own path (json.Marshal, then the run's redactor).
func TestLeftoverParentReportUnchanged(t *testing.T) {
	t.Parallel()
	rep := parentNone.run(t)
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if got := NewRedactor().Redact(string(raw)); got != parentNoneJSON {
		t.Errorf("leftovers.json changed (FR-015: a reader that ignores unknown fields reads it as before)\n got: %s\nwant: %s", got, parentNoneJSON)
	}
}
