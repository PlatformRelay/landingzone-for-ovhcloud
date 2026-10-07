package live

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Leftover parser on real captured listings (T065; FR-011, FR-013; constitution III). The fixture
// is the anonymised record of the plan-only run 20261007T125154Z-14a6 at the reviewed commit
// 337ee08 (evidence/T084.md, Live re-run): its listings replace the synthetic ones of T054 as the
// qualifying evidence for the kinds it captured. provenance.json says, per matrix kind, whether it
// was captured, and holds the answers the run asked for but did not record (derived from the
// record, shaped by the API schema, each with its provenance).
//
// Two facts of that run the synthetic world could not show:
//   - GET /cloud/project/<p>/region/<r>/storage answers 404 for a region without Object Storage
//     (the project's eight compute regions). The schema gives no other per-region signal than the
//     region detail: GET /cloud/project/{serviceName}/region/{regionName} answers cloud.Region
//     (kb/api/v1/cloud.json:52297), whose services are cloud.Component[] with name, status,
//     endpoint (cloud.json:67651, 64709); the storage path answers the S3-compatible containers
//     (cloud.json:58768). So the check asks every region's detail and lists storage in exactly the
//     regions with an Object Storage service; a 404 there stays an error.
//   - GET /iam/resource/<urn> has no tags field when the resource has none: iam.resource.Resource
//     marks tags "required": false, "canBeNull": true (kb/api/v2/iam.json:2661-2667), so an absent
//     or null tags field is no tags.

const capturedRun = "20261007T125154Z-14a6"

var capturedDir = filepath.Join(filepath.Dir(syntheticDir), "captured", capturedRun)

type capturedKind struct {
	Status string `json:"status"` // captured, not-captured, not-listed
	File   string `json:"file"`
	Note   string `json:"note"`
}

type capturedAnswer struct {
	Provenance string            `json:"provenance"`
	From       string            `json:"from"`
	Pages      []json.RawMessage `json:"pages"`
}

type capturedProvenance struct {
	RunID          string `json:"run_id"`
	ReviewedCommit string `json:"reviewed_commit"`
	Captured       string `json:"captured"`
	Project        struct {
		ID  string `json:"id"`
		URN string `json:"urn"`
	} `json:"project"`
	Exempt struct {
		ClientID string `json:"client_id"`
		PolicyID string `json:"policy_id"`
	} `json:"exempt"`
	Kinds  map[string]capturedKind      `json:"kinds"`
	Replay map[string]json.RawMessage   `json:"replay"`
	Files  map[string]string            `json:"files"`
	files  map[string][]byte            // contents, verified against Files
	answer map[string][]json.RawMessage // replay with {p} filled and from resolved
}

// readCaptured reads and verifies a captured fixture: every file named in Files exists with its
// digest and no other listing exists; every matrix kind has a status; a captured kind has its
// listing file, any other kind has none; not-listed only for the kinds with nothing to list; every
// replay answer has a provenance, and one taken from a file names a listed file.
func readCaptured(dir string) (capturedProvenance, error) {
	var p capturedProvenance
	raw, err := os.ReadFile(filepath.Join(dir, "provenance.json"))
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("provenance.json: %v", err)
	}
	if p.RunID != filepath.Base(dir) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(p.ReviewedCommit) ||
		!regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(p.Captured) {
		return p, errors.New("provenance.json: run id, reviewed commit or capture date missing")
	}
	if p.Project.ID == "" || p.Project.URN == "" || p.Exempt.ClientID == "" || p.Exempt.PolicyID == "" {
		return p, errors.New("provenance.json: project or exemption missing")
	}
	p.files = map[string][]byte{}
	for name, digest := range p.Files {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return p, err
		}
		if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != digest {
			return p, fmt.Errorf("%s: digest differs from provenance.json (truncated or edited capture)", name)
		}
		if !json.Valid(b) {
			return p, fmt.Errorf("%s: not JSON", name)
		}
		p.files[name] = b
	}
	listed, err := filepath.Glob(filepath.Join(dir, "listings", "*"))
	if err != nil {
		return p, err
	}
	for _, f := range listed {
		if _, ok := p.Files["listings/"+filepath.Base(f)]; !ok {
			return p, fmt.Errorf("listings/%s: not in provenance.json files", filepath.Base(f))
		}
	}
	for _, typ := range matrixTypes {
		k, ok := p.Kinds[typ]
		if !ok {
			return p, fmt.Errorf("kind %s: no status in provenance.json", typ)
		}
		_, has := p.files["listings/"+typ+".json"]
		switch {
		case k.Status == "captured" && (k.File != "listings/"+typ+".json" || !has):
			return p, fmt.Errorf("kind %s: recorded as captured without its listing file", typ)
		case k.Status == "not-captured" && (k.File != "" || has):
			return p, fmt.Errorf("kind %s: recorded as not captured but has a listing", typ)
		case k.Status == "not-listed" && (!slices.Contains(unlisted, typ) || has):
			return p, fmt.Errorf("kind %s: not-listed is only for the kinds with nothing to list", typ)
		case k.Status != "captured" && k.Status != "not-captured" && k.Status != "not-listed":
			return p, fmt.Errorf("kind %s: unknown status %q", typ, k.Status)
		}
		if k.Note == "" {
			return p, fmt.Errorf("kind %s: no note", typ)
		}
	}
	for typ := range p.Kinds {
		if !slices.Contains(matrixTypes, typ) {
			return p, fmt.Errorf("kind %s: not a matrix kind", typ)
		}
	}
	p.answer = map[string][]json.RawMessage{}
	for path, rawAnswer := range p.Replay {
		if strings.HasPrefix(path, "_") {
			continue
		}
		var a capturedAnswer
		if err := json.Unmarshal(bytes.ReplaceAll(rawAnswer, []byte("{p}"), []byte(p.Project.ID)), &a); err != nil {
			return p, fmt.Errorf("replay %s: %v", path, err)
		}
		if a.Provenance == "" {
			return p, fmt.Errorf("replay %s: no provenance", path)
		}
		if a.From != "" {
			b, ok := p.files[a.From]
			if !ok || len(a.Pages) != 0 {
				return p, fmt.Errorf("replay %s: from %q is not a listed file", path, a.From)
			}
			a.Pages = []json.RawMessage{b}
		}
		p.answer[strings.ReplaceAll(path, "{p}", p.Project.ID)] = a.Pages
	}
	return p, nil
}

// capturedWorld is the API the captured run saw: the replay answers plus the OAuth2 client and
// identity group records served as the id list and one detail answer per element. The run id is
// the capture's own, the prefix as given ("lzprobe-" as the run had, "<org>-" for the slice).
func capturedWorld(t testing.TB, p capturedProvenance, prefix string) syntheticWorld {
	t.Helper()
	w := syntheticWorld{Prefix: prefix, RunID: p.RunID, Responses: map[string][]json.RawMessage{}}
	w.Projects = append(w.Projects, struct {
		ID  string `json:"id"`
		URN string `json:"urn"`
	}{p.Project.ID, p.Project.URN})
	w.Exempt.ClientID, w.Exempt.PolicyID = p.Exempt.ClientID, p.Exempt.PolicyID
	for path, pages := range p.answer {
		w.Responses[path] = pages
	}
	split := func(file, list, field string) {
		var elems []map[string]any
		if err := json.Unmarshal(p.files[file], &elems); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		ids := []string{}
		for _, e := range elems {
			id, _ := e[field].(string)
			ids = append(ids, id)
			raw, _ := json.Marshal(e)
			w.Responses[list+"/"+id] = []json.RawMessage{raw}
		}
		raw, _ := json.Marshal(ids)
		w.Responses[list] = []json.RawMessage{raw}
	}
	split("listings/ovh_me_api_oauth2_client.json", "/me/api/oauth2/client", "clientId")
	split("listings/ovh_me_identity_group.json", "/me/identity/group", "name")
	return w
}

func loadCapturedWorld(t testing.TB, prefix string) (capturedProvenance, syntheticWorld) {
	t.Helper()
	p, err := readCaptured(capturedDir)
	if err != nil {
		t.Fatalf("captured fixture %s: %v", capturedDir, err)
	}
	return p, capturedWorld(t, p, prefix)
}

// checkCaptured runs the check through the fake API over the captured world with s applied.
func checkCaptured(t *testing.T, w syntheticWorld, s syntheticSeed, faults map[string]listFault) (Report, *fakeListAPI) {
	t.Helper()
	f := newFakeListAPI(t, seededResponses(w, s))
	for path, fault := range faults {
		f.faults[path] = fault
	}
	c, inv := apiCheck(t, w, s, f)
	return c.Check(context.Background(), inv), f
}

func normJSON(t testing.TB, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// TestLeftoversCapturedFixture: the committed capture is complete and intact, and a truncated
// listing, a kind without a status, a kind recorded as captured without its listing (or as not
// captured with one) and a listing not named in the sidecar are each refused.
func TestLeftoversCapturedFixture(t *testing.T) {
	p, err := readCaptured(capturedDir)
	if err != nil {
		t.Fatalf("captured fixture: %v", err)
	}
	var captured, owed []string
	for _, typ := range matrixTypes {
		switch p.Kinds[typ].Status {
		case "captured":
			captured = append(captured, typ)
		case "not-captured":
			owed = append(owed, typ)
		}
	}
	t.Logf("qualified on the capture: %v; not captured (owed, still synthetic): %v", captured, owed)

	copyFixture := func(t *testing.T) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), capturedRun)
		for _, name := range append([]string{"provenance.json"}, slices.Sorted(maps.Keys(p.Files))...) {
			b, err := os.ReadFile(filepath.Join(capturedDir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	editProv := func(t *testing.T, dir string, edit func(m map[string]any)) {
		t.Helper()
		var m map[string]any
		raw, _ := os.ReadFile(filepath.Join(dir, "provenance.json"))
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		out, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, "provenance.json"), out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	kinds := func(m map[string]any) map[string]any { return m["kinds"].(map[string]any) }
	cases := []struct {
		name, want string
		mutate     func(t *testing.T, dir string)
	}{
		{"truncated-listing", "digest", func(t *testing.T, dir string) {
			f := filepath.Join(dir, "listings", "ovh_iam_policy.json")
			b, _ := os.ReadFile(f)
			os.WriteFile(f, b[:len(b)/2], 0o644)
		}},
		{"truncated-but-valid-json", "digest", func(t *testing.T, dir string) {
			os.WriteFile(filepath.Join(dir, "listings", "ovh_iam_policy.json"), []byte("[]"), 0o644)
		}},
		{"kind-without-status", "no status", func(t *testing.T, dir string) {
			editProv(t, dir, func(m map[string]any) { delete(kinds(m), "ovh_cloud_project_network_private_subnet") })
		}},
		{"captured-without-listing", "without its listing", func(t *testing.T, dir string) {
			editProv(t, dir, func(m map[string]any) {
				kinds(m)["ovh_cloud_project_network_private_subnet"] = map[string]any{"status": "captured", "file": "listings/ovh_cloud_project_network_private_subnet.json", "note": "x"}
			})
		}},
		{"refuted-kind-reported-captured", "without its listing", func(t *testing.T, dir string) {
			editProv(t, dir, func(m map[string]any) {
				kinds(m)["ovh_iam_resource_tags"] = map[string]any{"status": "captured", "file": "listings/ovh_iam_resource_tags.json", "note": "x"}
			})
		}},
		{"not-captured-with-listing", "has a listing", func(t *testing.T, dir string) {
			editProv(t, dir, func(m map[string]any) {
				kinds(m)["ovh_iam_policy"] = map[string]any{"status": "not-captured", "note": "x"}
			})
		}},
		{"listed-kind-marked-not-listed", "nothing to list", func(t *testing.T, dir string) {
			editProv(t, dir, func(m map[string]any) {
				kinds(m)["ovh_cloud_project_alerting"] = map[string]any{"status": "not-listed", "note": "x"}
			})
		}},
		{"listing-not-in-sidecar", "not in provenance.json", func(t *testing.T, dir string) {
			os.WriteFile(filepath.Join(dir, "listings", "ovh_cloud_project_alerting.json"), []byte("[]"), 0o644)
		}},
		{"no-reviewed-commit", "reviewed commit", func(t *testing.T, dir string) {
			editProv(t, dir, func(m map[string]any) { delete(m, "reviewed_commit") })
		}},
		{"replay-without-provenance", "no provenance", func(t *testing.T, dir string) {
			editProv(t, dir, func(m map[string]any) {
				m["replay"].(map[string]any)["/cloud/project/{p}/alerting"] = map[string]any{"pages": []any{[]any{}}}
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t)
			if _, err := readCaptured(dir); err != nil {
				t.Fatalf("unmodified copy refused: %v", err)
			}
			tc.mutate(t, dir)
			_, err := readCaptured(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("readCaptured = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// TestLeftoversCapturedClean: the captured world passes with the run's prefix and with a slice
// prefix that makes the admin client and policy candidates (the exemption by id then carries it);
// every captured listing is re-recorded as captured; every region's detail is asked and storage is
// listed only in the region with an Object Storage service; the untagged project resource is
// recorded as no tags.
func TestLeftoversCapturedClean(t *testing.T) {
	for _, prefix := range []string{"lzprobe-", "lz-"} {
		t.Run(prefix, func(t *testing.T) {
			p, w := loadCapturedWorld(t, prefix)
			rep, f := checkCaptured(t, w, syntheticSeed{}, nil)
			if rep.Outcome != "pass" || len(rep.Leftovers) != 0 || len(rep.Errors) != 0 {
				t.Fatalf("captured listings: outcome %q, leftovers %v, errors %v; want pass", rep.Outcome, leftoverKeys(rep.Leftovers), rep.Errors)
			}
			for _, typ := range matrixTypes {
				if p.Kinds[typ].Status != "captured" {
					continue
				}
				got, _ := json.Marshal(rep.Listings[typ])
				if rep.Listings[typ] == nil || normJSON(t, got) != normJSON(t, p.files[p.Kinds[typ].File]) {
					t.Errorf("%s re-recorded as %s, want the captured %s", typ, got, p.files[p.Kinds[typ].File])
				}
			}
			if got, _ := json.Marshal(rep.Listings[tTags]); string(got) != `[{}]` {
				t.Errorf("untagged project resource recorded as %s, want [{}]", got)
			}
			var regions []string
			json.Unmarshal(w.Responses["/cloud/project/"+p.Project.ID+"/region"][0], &regions)
			asked := map[string]int{}
			for _, c := range f.Calls() {
				asked[c.Route]++
			}
			for _, r := range regions {
				rp := "/cloud/project/" + p.Project.ID + "/region/" + r
				if asked[rp] != 1 {
					t.Errorf("region %s: detail asked %d times, want once", r, asked[rp])
				}
				if want := map[bool]int{true: 1, false: 0}[r == "S3-UNNAMED"]; asked[rp+"/storage"] != want {
					t.Errorf("region %s: storage asked %d times, want %d", r, asked[rp+"/storage"], want)
				}
			}
			if n := len(rep.Listings[tRegion]); n != len(regions) {
				t.Errorf("recorded %d region details, want %d (the next live run's evidence for the service names)", n, len(regions))
			}
		})
	}
}

// capturedSeeds are one leftover per listed kind, injected into the captured world.
func capturedSeeds(p capturedProvenance, w syntheticWorld) map[string]syntheticSeed {
	pr := "/cloud/project/" + p.Project.ID
	pages := func(s ...string) []json.RawMessage {
		var out []json.RawMessage
		for _, x := range s {
			out = append(out, json.RawMessage(x))
		}
		return out
	}
	appendTo := func(path, elem string) []json.RawMessage {
		var arr []json.RawMessage
		json.Unmarshal(w.Responses[path][0], &arr)
		raw, _ := json.Marshal(append(arr, json.RawMessage(elem)))
		return pages(string(raw))
	}
	exp := func(typ, id string) []Leftover { return []Leftover{{Type: typ, ID: id}} }
	plainNet := `{"id":"pn-000001_0","name":"default-net","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}`
	plainUser := func(id string) string {
		return `{"id":` + id + `,"username":"user-` + id + `","description":"ops","status":"ok","roles":[],"creationDate":"2026-10-01T09:00:00Z"}`
	}
	noPolicy := `{"policy":""}`
	return map[string]syntheticSeed{
		"storage": {Replace: map[string][]json.RawMessage{
			pr + "/region/S3-UNNAMED/storage": pages(`[{"name":"lzprobe-bkt-seed","region":"S3-UNNAMED","createdAt":"2026-10-07T13:00:00Z","objectsCount":0,"objectsSize":0,"ownerId":1,"virtualHost":"x","arn":"arn:aws:s3:::lzprobe-bkt-seed","objects":[]}]`),
		}, Expect: exp(tStorage, "lzprobe-bkt-seed")},
		"network": {Replace: map[string][]json.RawMessage{
			pr + "/network/private":                    pages(`[{"id":"pn-000001_9","name":"lzprobe-net","status":"ACTIVE","type":"private","vlanId":9,"regions":[]}]`),
			pr + "/network/private/pn-000001_9/subnet": pages(`[]`),
		}, Expect: exp(tNetwork, "pn-000001_9")},
		"subnet": {Replace: map[string][]json.RawMessage{
			pr + "/network/private":                    pages("[" + plainNet + "]"),
			pr + "/network/private/pn-000001_0/subnet": pages(`[{"id":"sn-seed","cidr":"10.9.0.0/24","ipPools":[],"gatewayIp":null}]`),
		}, Inventory: []InventoryEntry{{Stack: "probe", Address: "ovh_cloud_project_network_private_subnet.p", Type: tSubnet, ID: "sn-seed"}},
			Expect: exp(tSubnet, "sn-seed")},
		"user": {Replace: map[string][]json.RawMessage{
			pr + "/user":                    pages(`[{"id":4242,"username":"user-4242","description":"lzprobe-user","status":"ok","roles":[],"creationDate":"2026-10-07T13:00:00Z"}]`),
			pr + "/user/4242/s3Credentials": pages(`[]`),
			pr + "/user/4242/policy":        pages(noPolicy),
		}, Expect: exp(tUser, "4242")},
		"s3-credential": {Replace: map[string][]json.RawMessage{
			pr + "/user":                    pages("[" + plainUser("4243") + "]"),
			pr + "/user/4243/s3Credentials": pages(`[{"access":"AKSEED0000000001","userId":4243,"tenantId":"t"}]`),
			pr + "/user/4243/policy":        pages(noPolicy),
		}, Inventory: []InventoryEntry{{Stack: "probe", Address: "ovh_cloud_project_user_s3_credential.p", Type: tCred, ID: "AKSEED0000000001"}},
			Expect: exp(tCred, "AKSEED0000000001")},
		"s3-policy": {Replace: map[string][]json.RawMessage{
			pr + "/user":                    pages("[" + plainUser("4244") + "]"),
			pr + "/user/4244/s3Credentials": pages(`[]`),
			pr + "/user/4244/policy":        pages(`{"policy":"{\"Statement\":[]}"}`),
		}, Inventory: []InventoryEntry{{Stack: "probe", Address: "ovh_cloud_project_user_s3_policy.p", Type: tS3Pol, ID: "4244"}},
			Expect: exp(tS3Pol, "4244")},
		"oauth2-client": {Replace: map[string][]json.RawMessage{
			"/me/api/oauth2/client":                     pages(`["` + p.Exempt.ClientID + `","EU.00000000000000a1"]`),
			"/me/api/oauth2/client/EU.00000000000000a1": pages(`{"clientId":"EU.00000000000000a1","name":"lzprobe-client"}`),
		}, Expect: exp(tClient, "EU.00000000000000a1")},
		"iam-policy": {Replace: map[string][]json.RawMessage{
			"/iam/policy": appendTo("/iam/policy", `{"id":"00000000-0000-4000-8000-0000000000a1","owner":"xx000001-ovh","name":"lzprobe-policy","readOnly":false,"identities":[],"resources":[],"permissions":{},"permissionsGroups":[],"createdAt":"2026-10-07T13:00:00Z"}`),
		}, Expect: exp(tPolicy, "00000000-0000-4000-8000-0000000000a1")},
		"identity-group": {Replace: map[string][]json.RawMessage{
			"/me/identity/group":             pages(`["ADMIN","DEFAULT","UNPRIVILEGED","lzprobe-grp"]`),
			"/me/identity/group/lzprobe-grp": pages(`{"name":"lzprobe-grp"}`),
		}, Expect: exp(tGroup, "lzprobe-grp")},
		"resource-tags": {Replace: map[string][]json.RawMessage{
			"/iam/resource/" + p.Project.URN: pages(`{"urn":"` + p.Project.URN + `","name":"` + p.Project.ID + `","type":"publicCloudProject","tags":{"lz:run-id":"` + p.RunID + `"}}`),
		}, Expect: exp(tTags, "lz:run-id")},
		"alerting": {Replace: map[string][]json.RawMessage{
			pr + "/alerting":         pages(`["al-seed"]`),
			pr + "/alerting/al-seed": pages(`{"id":"al-seed","name":"lzprobe-alert","delay":3600,"monthlyThreshold":1,"email":"user1@example.invalid"}`),
		}, Expect: exp(tAlert, "al-seed")},
	}
}

// TestLeftoversCapturedSeeded: one leftover per listed kind injected into the captured listings is
// reported exactly, with no error.
func TestLeftoversCapturedSeeded(t *testing.T) {
	p, w := loadCapturedWorld(t, "lzprobe-")
	seeds := capturedSeeds(p, w)
	listed := 0
	for _, typ := range matrixTypes {
		if !slices.Contains(unlisted, typ) {
			listed++
		}
	}
	if len(seeds) != listed {
		t.Fatalf("%d seeds, want one per listed matrix kind (%d)", len(seeds), listed)
	}
	for name, s := range seeds {
		t.Run(name, func(t *testing.T) {
			rep, _ := checkCaptured(t, w, s, nil)
			if got, want := leftoverKeys(rep.Leftovers), leftoverKeys(s.Expect); rep.Outcome != "fail" || !slices.Equal(got, want) || len(rep.Errors) != 0 {
				t.Errorf("outcome %q, leftovers %v, errors %v; want fail with exactly %v and no error", rep.Outcome, got, rep.Errors, want)
			}
		})
	}
}

// TestLeftoversCapturedExemption: with a prefix that makes the captured admin client and policy
// candidates, only their exact ids are exempt: another exemption id reports them, and a client or
// policy named like the admin with another id is a leftover.
func TestLeftoversCapturedExemption(t *testing.T) {
	p, w := loadCapturedWorld(t, "lz-")
	cases := []struct {
		name   string
		edit   func(w *syntheticWorld) syntheticSeed
		expect []Leftover
	}{
		{"captured-ids", func(w *syntheticWorld) syntheticSeed { return syntheticSeed{} }, nil},
		{"other-client-id", func(w *syntheticWorld) syntheticSeed {
			w.Exempt.ClientID = "EU.00000000000000ff"
			return syntheticSeed{}
		}, []Leftover{{Type: tClient, ID: p.Exempt.ClientID}}},
		{"other-policy-id", func(w *syntheticWorld) syntheticSeed {
			w.Exempt.PolicyID = "00000000-0000-4000-8000-0000000000ff"
			return syntheticSeed{}
		}, []Leftover{{Type: tPolicy, ID: p.Exempt.PolicyID}}},
		{"admin-named-client-other-id", func(w *syntheticWorld) syntheticSeed {
			return syntheticSeed{Replace: map[string][]json.RawMessage{
				"/me/api/oauth2/client":                     {json.RawMessage(`["` + p.Exempt.ClientID + `","EU.00000000000000b2"]`)},
				"/me/api/oauth2/client/EU.00000000000000b2": {json.RawMessage(`{"clientId":"EU.00000000000000b2","name":"lz-sandbox-admin"}`)},
			}}
		}, []Leftover{{Type: tClient, ID: "EU.00000000000000b2"}}},
		{"admin-named-policy-other-id", func(w *syntheticWorld) syntheticSeed {
			var arr []json.RawMessage
			json.Unmarshal(w.Responses["/iam/policy"][0], &arr)
			raw, _ := json.Marshal(append(arr, json.RawMessage(`{"id":"00000000-0000-4000-8000-0000000000b2","name":"lz-sandbox-admin","readOnly":false}`)))
			return syntheticSeed{Replace: map[string][]json.RawMessage{"/iam/policy": {raw}}}
		}, []Leftover{{Type: tPolicy, ID: "00000000-0000-4000-8000-0000000000b2"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w2 := w
			s := tc.edit(&w2)
			rep, _ := checkCaptured(t, w2, s, nil)
			want := map[bool]string{true: "pass", false: "fail"}[len(tc.expect) == 0]
			if got := leftoverKeys(rep.Leftovers); rep.Outcome != want || !slices.Equal(got, leftoverKeys(tc.expect)) || len(rep.Errors) != 0 {
				t.Errorf("outcome %q, leftovers %v, errors %v; want %s with exactly %v", rep.Outcome, got, rep.Errors, want, leftoverKeys(tc.expect))
			}
		})
	}
}

// TestLeftoversCapturedRed: on the captured world, a listing error, a truncated or malformed
// answer, a region detail that cannot say whether the region offers Object Storage, a storage
// region answering 404 (a DOWN storage service included), and no region offering Object Storage are
// each a failed check naming the cause, never zero leftovers. An absent or null tags field is no
// tags; a tags field of another type is an error.
func TestLeftoversCapturedRed(t *testing.T) {
	p, w := loadCapturedWorld(t, "lzprobe-")
	pr := "/cloud/project/" + p.Project.ID
	region := func(name string, services string) []json.RawMessage {
		return []json.RawMessage{json.RawMessage(`{"name":"` + name + `","type":"region","status":"UP"` + services + `}`)}
	}
	cases := []struct {
		name   string
		seed   syntheticSeed
		faults map[string]listFault
		want   string // "" = pass
	}{
		{name: "storage-region-404", faults: map[string]listFault{pr + "/region/S3-UNNAMED/storage": {Kind: "status", Status: 404}},
			want: pr + "/region/S3-UNNAMED/storage"},
		{name: "compute-region-claiming-storage-404", seed: syntheticSeed{Replace: map[string][]json.RawMessage{
			pr + "/region/GRA11": region("GRA11", `,"services":[{"name":"instance","status":"UP"},{"name":"storage-s3-high-perf","status":"UP"}]`),
		}}, want: pr + "/region/GRA11/storage"},
		{name: "region-detail-404", faults: map[string]listFault{pr + "/region/GRA11": {Kind: "status", Status: 404}},
			want: pr + "/region/GRA11"},
		{name: "region-detail-without-services", seed: syntheticSeed{Replace: map[string][]json.RawMessage{
			pr + "/region/GRA11": region("GRA11", ""),
		}}, want: "no services field"},
		{name: "region-detail-services-null", seed: syntheticSeed{Replace: map[string][]json.RawMessage{
			pr + "/region/GRA11": region("GRA11", `,"services":null`),
		}}, want: "no services field"},
		{name: "region-detail-truncated", faults: map[string]listFault{pr + "/region/S3-UNNAMED": {Kind: "truncated-length"}},
			want: pr + "/region/S3-UNNAMED"},
		{name: "policy-listing-500", faults: map[string]listFault{"/iam/policy": {Kind: "status", Status: 500}}, want: "/iam/policy"},
		{name: "policy-listing-truncated", faults: map[string]listFault{"/iam/policy": {Kind: "truncated-length"}}, want: "/iam/policy"},
		{name: "client-list-malformed", faults: map[string]listFault{"/me/api/oauth2/client": {Kind: "malformed"}}, want: "/me/api/oauth2/client"},
		{name: "group-detail-truncated", faults: map[string]listFault{"/me/identity/group/ADMIN": {Kind: "truncated-chunked"}}, want: "/me/identity/group/ADMIN"},
		{name: "no-storage-region", seed: syntheticSeed{Replace: map[string][]json.RawMessage{
			pr + "/region/S3-UNNAMED": region("S3-UNNAMED", `,"services":[{"name":"instance","status":"UP"}]`),
		}}, want: "offers Object Storage"},
		{name: "storage-service-down-still-listed", seed: syntheticSeed{Replace: map[string][]json.RawMessage{
			pr + "/region/S3-UNNAMED": region("S3-UNNAMED", `,"services":[{"name":"storage-s3-standard","status":"DOWN"}]`),
		}}, faults: map[string]listFault{pr + "/region/S3-UNNAMED/storage": {Kind: "status", Status: 503}},
			want: pr + "/region/S3-UNNAMED/storage"}, {name: "tags-null", seed: syntheticSeed{Replace: map[string][]json.RawMessage{
			"/iam/resource/" + p.Project.URN: {json.RawMessage(`{"urn":"` + p.Project.URN + `","tags":null}`)},
		}}, want: ""},
		{name: "tags-not-a-map", seed: syntheticSeed{Replace: map[string][]json.RawMessage{
			"/iam/resource/" + p.Project.URN: {json.RawMessage(`{"urn":"` + p.Project.URN + `","tags":"lz:run-id"}`)},
		}}, want: "/iam/resource/"},
		{name: "resource-not-an-object", seed: syntheticSeed{Replace: map[string][]json.RawMessage{
			"/iam/resource/" + p.Project.URN: {json.RawMessage(`[]`)},
		}}, want: "/iam/resource/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, _ := checkCaptured(t, w, tc.seed, tc.faults)
			if tc.want == "" {
				if rep.Outcome != "pass" || len(rep.Errors) != 0 || len(rep.Leftovers) != 0 {
					t.Errorf("outcome %q, leftovers %v, errors %v; want pass", rep.Outcome, leftoverKeys(rep.Leftovers), rep.Errors)
				}
				return
			}
			if rep.Outcome != "fail" || len(rep.Leftovers) != 0 || !slices.ContainsFunc(rep.Errors, func(e string) bool { return strings.Contains(e, tc.want) }) {
				t.Errorf("outcome %q, leftovers %v, errors %v; want fail with an error naming %q", rep.Outcome, leftoverKeys(rep.Leftovers), rep.Errors, tc.want)
			}
		})
	}
}
