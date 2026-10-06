package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Leftover check G9 (FR-011, research R12 *Leftover kind matrix*). The listings in
// tests/fixtures/ovhcloud/synthetic/ follow the response models of kb/api (v1 cloud.json,
// me.json; v2 iam.json); T065 qualifies the parser on T009's captured listings.

var syntheticDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ovhcloud", "synthetic")

// matrixTypes is research R12's kind matrix, by provider type.
var matrixTypes = []string{
	"ovh_cloud_project_storage", "ovh_cloud_project_network_private", "ovh_cloud_project_network_private_subnet",
	"ovh_cloud_project_user", "ovh_cloud_project_user_s3_credential", "ovh_cloud_project_user_s3_policy",
	"ovh_me_api_oauth2_client", "ovh_iam_policy", "ovh_me_identity_group", "ovh_iam_resource_tags",
	"ovh_cloud_project_alerting", "ovh_cloud_quota", "ovh_cloud_project",
}

// unlisted are the matrix kinds with nothing to list: the quota is a property of the project, the
// adopted project is exempt by id.
var unlisted = []string{"ovh_cloud_quota", "ovh_cloud_project"}

type syntheticWorld struct {
	Projects []struct {
		ID  string `json:"id"`
		URN string `json:"urn"`
	} `json:"projects"`
	Prefix string `json:"prefix"`
	RunID  string `json:"run_id"`
	Exempt struct {
		ClientID string `json:"client_id"`
		PolicyID string `json:"policy_id"`
	} `json:"exempt"`
	Retained  map[string][]string          `json:"retained"`
	Inventory []InventoryEntry             `json:"inventory"`
	Responses map[string][]json.RawMessage `json:"responses"`
}

type syntheticSeed struct {
	Replace   map[string][]json.RawMessage `json:"replace"`
	Inventory []InventoryEntry             `json:"inventory"`
	Expect    []Leftover                   `json:"expect"`
	Error     string                       `json:"error"`
	ErrorPage int                          `json:"error_cursor_page"`
	Projects  *[]json.RawMessage           `json:"projects"`
}

type syntheticSeeds struct {
	Seeds  map[string]syntheticSeed `json:"seeds"`
	Broken map[string]syntheticSeed `json:"broken"`
}

func loadSynthetic(t testing.TB) (syntheticWorld, syntheticSeeds) {
	t.Helper()
	var w syntheticWorld
	var s syntheticSeeds
	readJSONTB(t, filepath.Join(syntheticDir, "clean.json"), &w)
	readJSONTB(t, filepath.Join(syntheticDir, "seeds.json"), &s)
	return w, s
}

func readJSONTB(t testing.TB, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// fakeLister serves the synthetic listings page by page (cursor "" then "2", "3", …) and records
// every request; a path it does not know is an error, as an unknown API path would be.
type fakeLister struct {
	mu        sync.Mutex
	responses map[string][]json.RawMessage
	errPath   string
	errPage   int
	asked     []string
	log       string // optional file the requests are appended to (fresh-process tests)
}

func (f *fakeLister) Get(_ context.Context, path, cursor string) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	page := 1
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 2 {
			return nil, "", fmt.Errorf("fake lister: bad cursor %q", cursor)
		}
		page = n
	}
	f.asked = append(f.asked, fmt.Sprintf("%s#%d", path, page))
	if f.log != "" {
		if fh, err := os.OpenFile(f.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintf(fh, "%s#%d\n", path, page)
			fh.Close()
		}
	}
	if path == f.errPath && (f.errPage == 0 || f.errPage == page) {
		return nil, "", errors.New("fake lister: 503 Service Unavailable")
	}
	pages, ok := f.responses[path]
	if !ok {
		return nil, "", fmt.Errorf("fake lister: no such path %s", path)
	}
	if page > len(pages) {
		return nil, "", fmt.Errorf("fake lister: %s has no page %d", path, page)
	}
	body := []byte(pages[page-1])
	var raw struct {
		Raw *string `json:"_raw"`
	}
	if json.Unmarshal(body, &raw) == nil && raw.Raw != nil {
		body = []byte(*raw.Raw)
	}
	next := ""
	if page < len(pages) {
		next = strconv.Itoa(page + 1)
	}
	return body, next, nil
}

func (f *fakeLister) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}

// worldCheck builds the check and inventory of the clean world with seed applied.
func worldCheck(t testing.TB, w syntheticWorld, s syntheticSeed) (LeftoverCheck, *fakeLister, []InventoryEntry) {
	t.Helper()
	resp := map[string][]json.RawMessage{}
	for k, v := range w.Responses {
		resp[k] = v
	}
	for k, v := range s.Replace {
		resp[k] = v
	}
	l := &fakeLister{responses: resp, errPath: s.Error, errPage: s.ErrorPage}
	c := LeftoverCheck{
		Lister:   l,
		Prefix:   w.Prefix,
		RunID:    w.RunID,
		Retained: w.Retained,
		Exempt:   Exemption{ClientID: w.Exempt.ClientID, PolicyID: w.Exempt.PolicyID},
	}
	for _, p := range w.Projects {
		c.Projects = append(c.Projects, Project{ID: p.ID, URN: p.URN})
	}
	if s.Projects != nil {
		c.Projects = nil
	}
	inv := append(slices.Clone(w.Inventory), s.Inventory...)
	return c, l, inv
}

func leftoverKeys(ls []Leftover) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Type+" "+l.ID)
	}
	sort.Strings(out)
	return out
}

func TestLeftoversMatrixKinds(t *testing.T) {
	got := slices.Clone(MatrixTypes())
	want := slices.Clone(matrixTypes)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("MatrixTypes() = %v, want research R12's %v", got, want)
	}
}

// TestLeftoversCleanMatrix: a clean listing of every kind passes; every listing is read to its
// last page and every parent's children are listed.
func TestLeftoversCleanMatrix(t *testing.T) {
	w, _ := loadSynthetic(t)
	c, l, inv := worldCheck(t, w, syntheticSeed{})
	rep := c.Check(context.Background(), inv)
	if rep.Outcome != "pass" || len(rep.Leftovers) != 0 || len(rep.Errors) != 0 {
		t.Fatalf("clean listings: outcome %q, leftovers %v, errors %v; want pass", rep.Outcome, leftoverKeys(rep.Leftovers), rep.Errors)
	}
	asked := l.Asked()
	for path, pages := range w.Responses {
		for p := 1; p <= len(pages); p++ {
			if !slices.Contains(asked, fmt.Sprintf("%s#%d", path, p)) {
				t.Errorf("never listed %s page %d", path, p)
			}
		}
	}
	// Both pages of the networks reach the recorded listing.
	if n := len(rep.Listings["ovh_cloud_project_network_private"]); n != 2 {
		t.Errorf("recorded network listing holds %d items, want 2 (two pages)", n)
	}
	for _, typ := range matrixTypes {
		if !slices.Contains(unlisted, typ) && rep.Listings[typ] == nil {
			t.Errorf("no recorded listing for %s", typ)
		}
	}
}

// TestLeftoversSeeded: one seeded leftover per kind, each reported exactly (kind and id); a
// child of a leftover parent is reported with it. Every listable kind has a seed.
func TestLeftoversSeeded(t *testing.T) {
	w, seeds := loadSynthetic(t)
	covered := map[string]bool{}
	for name, s := range seeds.Seeds {
		for _, e := range s.Expect {
			covered[e.Type] = true
		}
		t.Run(name, func(t *testing.T) {
			c, _, inv := worldCheck(t, w, s)
			rep := c.Check(context.Background(), inv)
			if rep.Outcome != "fail" {
				t.Errorf("outcome %q with a seeded leftover, want fail", rep.Outcome)
			}
			if got, want := leftoverKeys(rep.Leftovers), leftoverKeys(s.Expect); !slices.Equal(got, want) {
				t.Errorf("leftovers = %v, want %v", got, want)
			}
		})
	}
	for _, typ := range matrixTypes {
		if !slices.Contains(unlisted, typ) && !covered[typ] {
			t.Errorf("no seeded leftover for kind %s", typ)
		}
	}
}

// TestLeftoversAdminExemption: the admin client and policy pass only through their recorded ids.
func TestLeftoversAdminExemption(t *testing.T) {
	w, _ := loadSynthetic(t)
	c, _, inv := worldCheck(t, w, syntheticSeed{})
	c.Exempt = Exemption{}
	rep := c.Check(context.Background(), inv)
	want := []string{"ovh_iam_policy " + w.Exempt.PolicyID, "ovh_me_api_oauth2_client " + w.Exempt.ClientID}
	if got := leftoverKeys(rep.Leftovers); rep.Outcome != "fail" || !slices.Equal(got, want) {
		t.Errorf("without the exemption: outcome %q, leftovers %v; want fail with %v", rep.Outcome, got, want)
	}
	// Swapped ids exempt nothing: the client id is not a policy id and vice versa.
	c.Exempt = Exemption{ClientID: w.Exempt.PolicyID, PolicyID: w.Exempt.ClientID}
	if rep := c.Check(context.Background(), inv); rep.Outcome != "fail" || len(rep.Leftovers) != 2 {
		t.Errorf("swapped exemption ids: outcome %q, leftovers %v; want both admin objects reported", rep.Outcome, leftoverKeys(rep.Leftovers))
	}
}

// TestLeftoversFailClosed: a listing error (first page, second page, a child listing), an
// unparseable or wrongly shaped listing, a created type outside the matrix and a check with no
// project to list are each `fail` with an error, never `pass`.
func TestLeftoversFailClosed(t *testing.T) {
	w, seeds := loadSynthetic(t)
	for name, s := range seeds.Broken {
		t.Run(name, func(t *testing.T) {
			c, _, inv := worldCheck(t, w, s)
			rep := c.Check(context.Background(), inv)
			if rep.Outcome != "fail" || len(rep.Errors) == 0 {
				t.Errorf("outcome %q, errors %v; want fail with an error", rep.Outcome, rep.Errors)
			}
		})
	}
	if len(seeds.Broken) < 7 {
		t.Errorf("fixture: %d broken cases, want 7", len(seeds.Broken))
	}
}

// TestLeftoversMissingOvhcloud: without a lister the check runs the ovhcloud binary; a missing
// binary is `fail`.
func TestLeftoversMissingOvhcloud(t *testing.T) {
	w, _ := loadSynthetic(t)
	for name, bin := range map[string]string{
		"absent path": filepath.Join(t.TempDir(), "ovhcloud"),
		"empty":       "",
	} {
		t.Run(name, func(t *testing.T) {
			c, _, inv := worldCheck(t, w, syntheticSeed{})
			c.Lister = nil
			c.Ovhcloud = bin
			rep := c.Check(context.Background(), inv)
			if rep.Outcome != "fail" || len(rep.Errors) == 0 {
				t.Errorf("outcome %q, errors %v; want fail with an error", rep.Outcome, rep.Errors)
			}
		})
	}
}

// TestLeftoversLoadExemption: the client id comes from sandbox.env, the policy id from the
// account's account.env (not LZ_ADMIN_CLIENT_ID); a missing or open file, or a missing id, is an
// error.
func TestLeftoversLoadExemption(t *testing.T) {
	const account = "ab12345-ovh"
	setup := func(t *testing.T, sandbox, accountEnv string) string {
		root := tempPrivate(t)
		if sandbox != "" {
			if err := WriteCredentialFile(root, "sandbox.env", parseKV(sandbox)); err != nil {
				t.Fatal(err)
			}
		}
		if accountEnv != "" {
			if err := WriteCredentialFile(root, filepath.Join("accounts", account, "account.env"), parseKV(accountEnv)); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	sandbox := "OVH_ENDPOINT=ovh-eu OVH_CLIENT_ID=EU.sandboxadmin01 OVH_CLIENT_SECRET=" + seedSecret
	accountEnv := "LZ_ACCOUNT_ID=ab12345-ovh LZ_ADMIN_CLIENT_ID=EU.notthisone0002 LZ_ADMIN_POLICY_ID=6c1e5d0a-adm1n"

	root := setup(t, sandbox, accountEnv)
	ex, err := LoadExemption(root, account)
	if err != nil {
		t.Fatal(err)
	}
	if ex != (Exemption{ClientID: "EU.sandboxadmin01", PolicyID: "6c1e5d0a-adm1n"}) {
		t.Errorf("exemption = %+v, want the sandbox.env client id and the account.env policy id", ex)
	}

	for name, c := range map[string]struct{ sandbox, account string }{
		"no sandbox.env":           {"", accountEnv},
		"no account.env":           {sandbox, ""},
		"no client id":             {"OVH_ENDPOINT=ovh-eu", accountEnv},
		"no policy id":             {sandbox, "LZ_ACCOUNT_ID=ab12345-ovh LZ_ADMIN_CLIENT_ID=EU.sandboxadmin01"},
		"empty policy id":          {sandbox, "LZ_ACCOUNT_ID=ab12345-ovh LZ_ADMIN_POLICY_ID="},
		"policy id only as client": {sandbox, "LZ_ADMIN_CLIENT_ID=6c1e5d0a-adm1n"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadExemption(setup(t, c.sandbox, c.account), account); err == nil {
				t.Error("loaded an incomplete exemption")
			}
		})
	}
	t.Run("group-readable sandbox.env", func(t *testing.T) {
		root := setup(t, sandbox, accountEnv)
		if err := os.Chmod(filepath.Join(root, "sandbox.env"), 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadExemption(root, account); ExitCode(err) != RefusalExit {
			t.Errorf("err = %v, want a file-mode refusal", err)
		}
	})
}

// parseKV turns "K=v K2=v2" into a map (values hold no spaces).
func parseKV(s string) map[string]string {
	out := map[string]string{}
	for _, f := range strings.Fields(s) {
		k, v, _ := strings.Cut(f, "=")
		out[k] = v
	}
	return out
}
