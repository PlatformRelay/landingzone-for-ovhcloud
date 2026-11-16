package live

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"testing"
	"time"
)

// Spec 007 FR-013 (T033): the leftover check matches a created resource's inventory id with a
// listed resource in the listed form. The inventory ids below take the forms run
// 20261116T120049Z-6301 recorded (evidence/T033.md): a bucket as <project>/<region>/<name>, an S3
// policy as <project>/<user-id>, every other listed kind as the id its listing reports (an alert's
// form is believed from the provider docs: `id` - Alert ID). The report's id keeps the listed form.

const (
	idProject = "a0000000000000000000000000000013" // the run's project
	idOther   = "b0000000000000000000000000000013" // another project of the account
	idRun     = "20261116T120049Z-0013"
)

// idWorld is the fake API of projects whose every listing starts empty: regions GRA and SBG with
// a bucket service, no network, container, user or alert, no tags, client, policy or group.
type idWorld struct {
	t    *testing.T
	resp map[string][]json.RawMessage
}

func newIDWorld(t *testing.T, projects ...string) *idWorld {
	t.Helper()
	w := &idWorld{t: t, resp: map[string][]json.RawMessage{}}
	for _, p := range projects {
		pr := "/cloud/project/" + p
		w.set(pr+"/region", `["GRA","SBG"]`)
		for _, r := range []string{"GRA", "SBG"} {
			w.set(pr+"/region/"+r, `{"name":"`+r+`","services":[{"name":"storage-s3-standard","status":"UP"}]}`)
			w.set(pr+"/region/"+r+"/storage", `[]`)
		}
		for _, path := range []string{"/network/private", "/storage", "/user", "/alerting"} {
			w.set(pr+path, `[]`)
		}
		w.set("/iam/resource/"+idURN(p), `{"urn":"`+idURN(p)+`","tags":{}}`)
	}
	for _, path := range []string{"/me/api/oauth2/client", "/iam/policy", "/me/identity/group"} {
		w.set(path, `[]`)
	}
	return w
}

func idURN(project string) string { return "urn:v1:eu:resource:publicCloudProject:" + project }

func (w *idWorld) set(path, body string) { w.resp[path] = []json.RawMessage{json.RawMessage(body)} }

// add appends elem to the array listing at path.
func (w *idWorld) add(path, elem string) {
	w.t.Helper()
	var arr []json.RawMessage
	if err := json.Unmarshal(w.resp[path][0], &arr); err != nil {
		w.t.Fatalf("%s: %v", path, err)
	}
	raw, _ := json.Marshal(append(arr, json.RawMessage(elem)))
	w.set(path, string(raw))
}

func (w *idWorld) bucket(project, region, name string) {
	w.add("/cloud/project/"+project+"/region/"+region+"/storage", `{"name":"`+name+`","region":"`+region+`"}`)
}

func (w *idWorld) network(project, id, name string, subnets ...string) {
	pr := "/cloud/project/" + project + "/network/private"
	w.add(pr, `{"id":"`+id+`","name":"`+name+`"}`)
	w.set(pr+"/"+id+"/subnet", `[]`)
	for _, s := range subnets {
		w.add(pr+"/"+id+"/subnet", `{"id":"`+s+`","cidr":"10.0.0.0/24"}`)
	}
}

// user lists a user with its credentials and, when policy is set, an S3 policy.
func (w *idWorld) user(project, id, desc string, policy bool, access ...string) {
	pr := "/cloud/project/" + project + "/user"
	w.add(pr, `{"id":`+id+`,"username":"user-`+id+`","description":"`+desc+`"}`)
	w.set(pr+"/"+id+"/s3Credentials", `[]`)
	for _, a := range access {
		w.add(pr+"/"+id+"/s3Credentials", `{"access":"`+a+`","userId":`+id+`}`)
	}
	if policy {
		w.set(pr+"/"+id+"/policy", `{"policy":"{\"Statement\":[]}"}`)
	} else {
		w.set(pr+"/"+id+"/policy", `{"policy":""}`)
	}
}

func (w *idWorld) alert(project, id, name string) {
	pr := "/cloud/project/" + project + "/alerting"
	w.add(pr, `"`+id+`"`)
	w.set(pr+"/"+id, `{"id":"`+id+`","name":"`+name+`"}`)
}

func (w *idWorld) client(id, name string) {
	w.add("/me/api/oauth2/client", `"`+id+`"`)
	w.set("/me/api/oauth2/client/"+id, `{"clientId":"`+id+`","name":"`+name+`"}`)
}

func (w *idWorld) policy(id, name string) { w.add("/iam/policy", `{"id":"`+id+`","name":"`+name+`"}`) }

func (w *idWorld) group(name string) {
	w.add("/me/identity/group", `"`+name+`"`)
	w.set("/me/identity/group/"+name, `{"name":"`+name+`"}`)
}

func (w *idWorld) tags(project string, tags map[string]string) {
	raw, _ := json.Marshal(tags)
	w.set("/iam/resource/"+idURN(project), `{"urn":"`+idURN(project)+`","tags":`+string(raw)+`}`)
}

// check runs the leftover check over the world's projects with Prefix lz2- and the run's id,
// failing on a listing error (the world must answer every request).
func (w *idWorld) check(inv []InventoryEntry, projects ...string) Report {
	w.t.Helper()
	c := LeftoverCheck{Lister: &fakeLister{responses: w.resp}, Prefix: "lz2-", RunID: idRun, Backoff: []time.Duration{}}
	for _, p := range projects {
		c.Projects = append(c.Projects, Project{ID: p, URN: idURN(p)})
	}
	rep := c.Check(context.Background(), inv)
	if len(rep.Errors) > 0 {
		w.t.Fatalf("listing errors: %v", rep.Errors)
	}
	return rep
}

// idMatch is the match of the leftover (typ, id), "" when it is not a leftover.
func idMatch(rep Report, typ, id string) string {
	for _, l := range rep.Leftovers {
		if l.Type == typ && l.ID == id {
			return l.Match
		}
	}
	return ""
}

func inv(typ, id string) InventoryEntry {
	return InventoryEntry{Stack: "demo-dev-gra11-runtime", Address: "module.x." + typ + ".this", Type: typ, ID: id}
}

// idBucket is the run's runtime bucket (run 20261116T120049Z-6301).
const idBucket = "lz2-demo-dev-gra11-bkt-runtime"

// TestInventoryIDForms runs T033's cases (1) to (5); case (6) is TestChainDestroyGuard's.
func TestInventoryIDForms(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"bucket-provider-form", idCaseBucket},
		{"s3-policy-provider-form", idCaseS3Policy},
		{"bucket-other-region", idCaseBucketOtherRegion},
		{"bucket-other-project", idCaseBucketOtherProject},
		{"s3-policy-other-project", idCaseS3PolicyOtherProject},
		{"forms", idCaseForms},
		{"tags-by-run-id", idCaseTags},
	} {
		t.Run(c.name, c.run)
	}
}

// (1) The run's bucket, recorded as <project>/<region>/<name>, reads inventory under its name.
func idCaseBucket(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject)
	w.bucket(idProject, "GRA", idBucket)
	rep := w.check([]InventoryEntry{inv(tStorage, idProject+"/GRA/"+idBucket)}, idProject)
	if got := leftoverKeys(rep.Leftovers); !slices.Equal(got, []string{tStorage + " " + idBucket}) {
		t.Fatalf("leftovers %v, want the bucket under its listed name", got)
	}
	if got := idMatch(rep, tStorage, idBucket); got != MatchInventory {
		t.Errorf("bucket recorded as <project>/GRA/<name>: match %q, want %q (FR-013)", got, MatchInventory)
	}
}

// (2) The S3 policy, recorded as <project>/<user-id>, of a user the inventory holds reads
// inventory, not parent.
func idCaseS3Policy(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject)
	w.user(idProject, "4801", "demo-platform-s3", true)
	rep := w.check([]InventoryEntry{inv(tUser, "4801"), inv(tS3Pol, idProject+"/4801")}, idProject)
	if got := idMatch(rep, tUser, "4801"); got != MatchInventory {
		t.Errorf("user: match %q, want %q", got, MatchInventory)
	}
	if got := idMatch(rep, tS3Pol, "4801"); got != MatchInventory {
		t.Errorf("S3 policy recorded as <project>/<user-id>: match %q, want %q (FR-013)", got, MatchInventory)
	}
}

// (3) A composing part that disagrees with the listing is not the run's: the same bucket name in
// another region or another project, an S3 policy of the same user id in another project.
func idCaseBucketOtherRegion(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject)
	w.bucket(idProject, "SBG", idBucket)
	rep := w.check([]InventoryEntry{inv(tStorage, idProject+"/GRA/"+idBucket)}, idProject)
	if got := idMatch(rep, tStorage, idBucket); got != MatchPrefix {
		t.Errorf("bucket in SBG, inventory names GRA: match %q, want %q", got, MatchPrefix)
	}
}

func idCaseBucketOtherProject(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject, idOther)
	w.bucket(idOther, "GRA", idBucket)
	rep := w.check([]InventoryEntry{inv(tStorage, idProject+"/GRA/"+idBucket)}, idProject, idOther)
	if got := idMatch(rep, tStorage, idBucket); got != MatchPrefix {
		t.Errorf("bucket in another project: match %q, want %q", got, MatchPrefix)
	}
}

func idCaseS3PolicyOtherProject(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject, idOther)
	w.user(idOther, "4801", "lz2-other-s3", true)
	rep := w.check([]InventoryEntry{inv(tS3Pol, idProject+"/4801")}, idProject, idOther)
	if got := idMatch(rep, tS3Pol, "4801"); got != MatchParent {
		t.Errorf("S3 policy in another project: match %q, want %q (its prefixed user only)", got, MatchParent)
	}
}

// idForm is one listed kind: its inventory id in the provider's form, its listed id and listing.
type idForm struct {
	typ, invID, listedID string
	list                 func(w *idWorld)
}

// idForms are the listed kinds of the matrix, each with the form the run's inventory records.
var idForms = []idForm{
	{tStorage, idProject + "/GRA/demo-bkt", "demo-bkt", func(w *idWorld) { w.bucket(idProject, "GRA", "demo-bkt") }},
	{tNetwork, "pn-1300013_0", "pn-1300013_0", func(w *idWorld) { w.network(idProject, "pn-1300013_0", "demo-net") }},
	{tSubnet, "00000000-0000-4000-8000-000000000013", "00000000-0000-4000-8000-000000000013", func(w *idWorld) {
		w.network(idProject, "pn-1300014_0", "demo-net", "00000000-0000-4000-8000-000000000013")
	}},
	{tUser, "4802", "4802", func(w *idWorld) { w.user(idProject, "4802", "demo-s3", false) }},
	{tCred, "c0000000000000000000000000000013", "c0000000000000000000000000000013", func(w *idWorld) {
		w.user(idProject, "4803", "demo-s3", false, "c0000000000000000000000000000013")
	}},
	{tS3Pol, idProject + "/4804", "4804", func(w *idWorld) { w.user(idProject, "4804", "demo-s3", true) }},
	{tClient, "EU.0000000000000013", "EU.0000000000000013", func(w *idWorld) { w.client("EU.0000000000000013", "demo-deployer") }},
	{tPolicy, "00000000-0000-4000-8000-0000000000c1", "00000000-0000-4000-8000-0000000000c1", func(w *idWorld) {
		w.policy("00000000-0000-4000-8000-0000000000c1", "demo-deployer")
	}},
	{tGroup, "demo-grp-tenant", "demo-grp-tenant", func(w *idWorld) { w.group("demo-grp-tenant") }},
	{tAlert, "al-0013", "al-0013", func(w *idWorld) { w.alert(idProject, "al-0013", "demo-budget") }},
}

// (4) Every listed kind of the matrix, recorded in the provider's form, reads inventory. The
// listed resource carries no prefix and no run id, and its parent is not the run's, so only the
// inventory can make it a leftover. The table names every listed kind.
func idCaseForms(t *testing.T) {
	t.Parallel()
	var named, listed []string
	for _, f := range idForms {
		named = append(named, f.typ)
		t.Run(f.typ, func(t *testing.T) { idCheckForm(t, f) })
	}
	for _, typ := range MatrixTypes() {
		if !slices.Contains([]string{tTags, tQuota, tProject}, typ) {
			listed = append(listed, typ)
		}
	}
	sort.Strings(named)
	sort.Strings(listed)
	if !slices.Equal(named, listed) {
		t.Errorf("the form table names %v, the matrix lists %v: every listed kind needs its inventory form", named, listed)
	}
}

func idCheckForm(t *testing.T, f idForm) {
	t.Parallel()
	w := newIDWorld(t, idProject)
	f.list(w)
	rep := w.check([]InventoryEntry{inv(f.typ, f.invID)}, idProject)
	if got := idMatch(rep, f.typ, f.listedID); got != MatchInventory {
		t.Errorf("%s recorded as %q, listed as %q: match %q, want %q (FR-013)", f.typ, f.invID, f.listedID, got, MatchInventory)
	}
}

// (5) Tags have no form: the inventory's one URN lists as one item per key, and a key holding the
// run id is matched by the run id.
func idCaseTags(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject)
	key := "demo-run-" + idRun
	w.tags(idProject, map[string]string{key: "x"})
	rep := w.check([]InventoryEntry{inv(tTags, idURN(idProject))}, idProject)
	if got := idMatch(rep, tTags, key); got != MatchRunID {
		t.Errorf("tag key holding the run id: match %q, want %q", got, MatchRunID)
	}
}
