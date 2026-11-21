package live

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"testing"
)

// Spec 007 FR-017 (T042): a leftover of a kind listed under a project carries `project`, the id
// of the project whose listing returned it, and a bucket also `region`, the region whose listing
// returned it; the account-level kinds carry neither, and a child's project is its parent's. The
// report's `id` keeps the listed form. The fields are read from leftovers.json as the writer
// produces it (json.Marshal, then the run's redactor), so an absent key is asserted, not an empty
// Go string. The fixtures are the idWorld fake listings of leftovers_ids_test.go.

// scopeJSON is the report's leftovers as leftovers.json holds them, one key map per leftover.
func scopeJSON(t *testing.T, rep Report) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Leftovers []map[string]any `json:"leftovers"`
	}
	if err := json.Unmarshal([]byte(NewRedactor().Redact(string(raw))), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Leftovers
}

// scopeOf is a serialized leftover's type, id and scope, an absent key printed as <nil>.
func scopeOf(l map[string]any) string {
	return fmt.Sprintf("%v %v %v/%v", l["type"], l["id"], l["project"], l["region"])
}

// Case (1): one bucket name listed in A's GRA, B's GRA and A's SBG; each leftover carries its own
// listing's project and region and the same listed id. A's SBG listing answers with a bucket
// whose own region field says GRA: the region is the listing's, not the payload's (review r2).
func TestLeftoverScopeBucketListings(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject, idOther)
	w.bucket(idProject, "GRA", scopeBucket)
	w.bucket(idOther, "GRA", scopeBucket)
	w.add("/cloud/project/"+idProject+"/region/SBG/storage", `{"name":"`+scopeBucket+`","region":"GRA"}`)
	var got []string
	for _, l := range scopeJSON(t, w.check(nil, idProject, idOther)) {
		got = append(got, scopeOf(l))
	}
	sort.Strings(got)
	want := []string{
		tStorage + " " + scopeBucket + " " + idProject + "/GRA",
		tStorage + " " + scopeBucket + " " + idProject + "/SBG",
		tStorage + " " + scopeBucket + " " + idOther + "/GRA",
	}
	if !slices.Equal(got, want) {
		t.Errorf("one name listed at A/GRA, B/GRA, A/SBG: leftovers (type id project/region) %v, want %v (FR-017: each its own listing's)", got, want)
	}
}

// The project-listed kinds (FR-017) and the account-level ones.
var (
	scopeProjectKinds = []string{tStorage, tNetwork, tSubnet, tUser, tCred, tS3Pol, tAlert, tSwift, tTags}
	scopeAccountKinds = []string{tClient, tPolicy, tGroup}
)

// scopeAllKinds lists, in project B (idOther) of the two listed projects A and B, one leftover
// of every project-listed kind, and one of every account-level kind: each matches by the prefix,
// or (subnet, S3 credential, S3 policy) by its parent. Project A lists nothing.
func scopeAllKinds(t *testing.T) []map[string]any {
	t.Helper()
	w := newIDWorld(t, idProject, idOther)
	w.bucket(idOther, "GRA", "lz2-scope-bkt")
	w.network(idOther, "pn-1500071_0", "lz2-scope-net", "00000000-0000-4000-8000-000000004871")
	w.user(idOther, "4871", "lz2-scope-s3", true, "c0000000000000000000000000004871")
	w.alert(idOther, "al-4871", "lz2-scope-budget")
	w.add("/cloud/project/"+idOther+"/storage", `{"id":"ct-4871","name":"lz2-scope-ctr"}`)
	w.tags(idOther, map[string]string{"lz2-scope-tag": "x"})
	w.client("EU.0000000000004871", "lz2-scope-deployer")
	w.policy("00000000-0000-4000-8000-0000000048c1", "lz2-scope-policy")
	w.group("lz2-scope-grp")
	return scopeJSON(t, w.check(nil, idProject, idOther))
}

// scopeOfKinds is the leftovers of the given kinds, failing unless each kind is there once.
func scopeOfKinds(t *testing.T, ls []map[string]any, kinds []string) []map[string]any {
	t.Helper()
	var out []map[string]any
	var seen []string
	for _, l := range ls {
		if typ, _ := l["type"].(string); slices.Contains(kinds, typ) {
			out, seen = append(out, l), append(seen, typ)
		}
	}
	sort.Strings(seen)
	want := slices.Sorted(slices.Values(kinds))
	if !slices.Equal(seen, want) {
		t.Fatalf("leftovers of kinds %v, want each of %v once (fixture)", seen, want)
	}
	return out
}

// scopeWantProject checks that the serialized leftover l carries project and, only when it is a
// bucket, region GRA (its listing's); a non-bucket has no region key at all.
func scopeWantProject(t *testing.T, l map[string]any, project string) {
	t.Helper()
	region, hasRegion := l["region"]
	wantRegion := l["type"] == tStorage
	if l["project"] != project || hasRegion != wantRegion || (wantRegion && region != "GRA") {
		t.Errorf("%s: want project %s and a region key (GRA) on a bucket only (FR-017: the listing's project and region)", scopeOf(l), project)
	}
}

// Case (2): every project-listed kind's leftover carries project B; only the bucket carries a
// region (its listing's, GRA).
func TestLeftoverScopeProjectKinds(t *testing.T) {
	t.Parallel()
	for _, l := range scopeOfKinds(t, scopeAllKinds(t), scopeProjectKinds) {
		scopeWantProject(t, l, idOther)
	}
}

// Case (2), children matched directly (review r1): a subnet, an S3 credential and an S3 policy
// the run's inventory recorded, under a network and a user that are not the run's, carry
// project B too, not only children taken through their parent.
func TestLeftoverScopeDirectChildren(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject, idOther)
	w.network(idOther, "pn-1500091_0", "demo-net", "00000000-0000-4000-8000-000000004891")
	w.user(idOther, "4891", "demo-s3", true, "c0000000000000000000000000004891")
	inv := []InventoryEntry{inv(tSubnet, "00000000-0000-4000-8000-000000004891"),
		inv(tCred, "c0000000000000000000000000004891"), inv(tS3Pol, idOther+"/4891")}
	kids := []string{tSubnet, tCred, tS3Pol}
	for _, l := range scopeOfKinds(t, scopeJSON(t, w.check(inv, idProject, idOther)), kids) {
		if l["match"] != MatchInventory {
			t.Fatalf("%s: match %v, want %s (fixture)", scopeOf(l), l["match"], MatchInventory)
		}
		scopeWantProject(t, l, idOther)
	}
}

// Case (3): the account-level kinds' leftovers, in the same report, have neither a project nor a
// region key in leftovers.json (holds with the empty stub; fails once a project leaks onto them).
func TestLeftoverScopeAccountKinds(t *testing.T) {
	t.Parallel()
	for _, l := range scopeOfKinds(t, scopeAllKinds(t), scopeAccountKinds) {
		for _, key := range []string{"project", "region"} {
			if v, ok := l[key]; ok {
				t.Errorf("%s: %s %v, want no %s key (FR-017: account-level kinds carry neither)", scopeOf(l), key, v, key)
			}
		}
	}
}

// Case (4): two projects each list a user of the same id with a credential; each parent-matched
// credential's project is its own listing's, and a user leftover of that project and the
// credential's parent_id is in the report.
func TestLeftoverScopeChildOfParent(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject, idOther)
	w.user(idProject, "4881", "lz2-scope-a-s3", false, "ca000000000000000000000000004881")
	w.user(idOther, "4881", "lz2-scope-b-s3", false, "cb000000000000000000000000004881")
	ls := scopeJSON(t, w.check(nil, idProject, idOther))
	scopeWantChild(t, ls, "ca000000000000000000000000004881", idProject)
	scopeWantChild(t, ls, "cb000000000000000000000000004881", idOther)
}

// scopeWantChild checks the S3 credential leftover access: parent-matched, of project, and its
// parent user, of the same project, a leftover of the report.
func scopeWantChild(t *testing.T, ls []map[string]any, access, project string) {
	t.Helper()
	i := slices.IndexFunc(ls, func(l map[string]any) bool { return l["type"] == tCred && l["id"] == access })
	if i < 0 || ls[i]["match"] != MatchParent {
		t.Fatalf("credential %s: not a parent-matched leftover in %v (fixture)", access, ls)
	}
	cred := ls[i]
	if cred["project"] != project {
		t.Errorf("%s: project %v, want %s (FR-017: its own listing's, its parent's)", scopeOf(cred), cred["project"], project)
	}
	if !slices.ContainsFunc(ls, func(l map[string]any) bool {
		return l["type"] == tUser && l["id"] == cred["parent_id"] && l["project"] == cred["project"]
	}) {
		t.Errorf("%s: no user leftover %v of project %v (FR-017: a child's project is its parent's)", scopeOf(cred), cred["parent_id"], cred["project"])
	}
}
