package stacks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Manifest controls of 005 T033 (FR-006, FR-008, SC-002; ADR-0004, ADR-0005, ADR-0007, ADR-0017).
//
// Fixtures under tests/fixtures/manifests/: sandbox (one tenant, environment and region; KD-1
// shared state project; reference-mode project after T009), growth (two tenants x two
// environments x two regions, two runtime slots in demo/dev/GRA11, spec.scope absent) and
// tenant-only (spec.scope tenant, platform producers as external entries). Each has a
// derived.json written from data-model *Derived instance fields*, *Stage table* and research
// R2/R5, independently of the Go code. invalid/ holds one file per refusal case, each a single
// change to the sandbox or tenant-only fixture; invalid/cases.json names the base, the expected
// code and why. When a document breaks several rules the first code group in the order documented
// on the Code constants wins; the single-change cases rely on that order only where a later group
// would also fire (a missing bootstrap also leaves authority edges unresolved).

const manifestFixtureDir = "../../../tests/fixtures/manifests"

type derivedInstance struct {
	ID          string   `json:"id"`
	Scope       string   `json:"scope"`
	Path        string   `json:"path"`
	Backend     string   `json:"backend"`
	StateBucket string   `json:"state_bucket"`
	StateKey    string   `json:"state_key"`
	Tags        []string `json:"tags"`
	Data        []string `json:"data"`
	Authority   []string `json:"authority"`
}

// manifestRow is a spec.instances row as the test reads it from the fixture itself.
type manifestRow struct {
	ID          string `json:"id"`
	Stage       string `json:"stage"`
	Tenant      string `json:"tenant"`
	Environment string `json:"environment"`
	Region      string `json:"region"`
	Slot        string `json:"slot"`
}

func readManifestFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(manifestFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeFixture(t *testing.T, name string) *Manifest {
	t.Helper()
	m, err := DecodeManifest(readManifestFixture(t, filepath.Join(name, "deployments.yaml")))
	if err != nil {
		t.Fatalf("%s: DecodeManifest: %v", name, err)
	}
	if m == nil {
		t.Fatalf("%s: DecodeManifest returned no manifest", name)
	}
	return m
}

// fixtureRows reads spec.instances from a fixture without the code under test: whole-line
// comments dropped, then plain JSON.
func fixtureRows(t *testing.T, name string) []manifestRow {
	t.Helper()
	var kept []string
	for _, line := range strings.Split(string(readManifestFixture(t, filepath.Join(name, "deployments.yaml"))), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	var doc struct {
		Spec struct {
			Instances []manifestRow `json:"instances"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(strings.Join(kept, "\n")), &doc); err != nil {
		t.Fatalf("%s: fixture is not JSON after dropping comments: %v", name, err)
	}
	return doc.Spec.Instances
}

func sortedCopy(s []string) []string {
	c := append([]string{}, s...)
	slices.Sort(c)
	return c
}

func edgeProducers(t *testing.T, in Instance, kind string) []string {
	t.Helper()
	var out []string
	for _, e := range in.Edges {
		if e.Kind != EdgeData && e.Kind != EdgeAuthority {
			t.Errorf("%s: edge to %s has kind %q, want data or authority", in.ID, e.Producer, e.Kind)
		}
		if e.Kind == kind {
			out = append(out, e.Producer)
		}
	}
	return sortedCopy(out)
}

// TestManifestDerivedFields: every fixture decodes, keeps its rows in order with their dimensions,
// and derives exactly the scope, path, backend, state bucket, state key, tags and data and
// authority edges its derived.json expects.
func TestManifestDerivedFields(t *testing.T) {
	for _, name := range []string{"sandbox", "growth", "tenant-only"} {
		t.Run(name, func(t *testing.T) {
			m := decodeFixture(t, name)
			var want struct {
				Instances []derivedInstance `json:"instances"`
			}
			if err := json.Unmarshal(readManifestFixture(t, filepath.Join(name, "derived.json")), &want); err != nil {
				t.Fatal(err)
			}
			rows := fixtureRows(t, name)
			if len(rows) != len(want.Instances) {
				t.Fatalf("fixture has %d rows, derived.json %d", len(rows), len(want.Instances))
			}
			if len(m.Instances) != len(rows) {
				t.Fatalf("decoded %d instances, want %d", len(m.Instances), len(rows))
			}
			for i, got := range m.Instances {
				row, w := rows[i], want.Instances[i]
				if row.ID != w.ID {
					t.Fatalf("fixture row %d is %s, derived.json has %s", i, row.ID, w.ID)
				}
				dims := manifestRow{got.ID, got.Stage, got.Tenant, got.Environment, got.Region, got.Slot}
				if dims != row {
					t.Errorf("instance %d: row %+v, want %+v", i, dims, row)
				}
				if got.Scope != w.Scope {
					t.Errorf("%s: scope %q, want %q", w.ID, got.Scope, w.Scope)
				}
				if got.Path != w.Path {
					t.Errorf("%s: path %q, want %q", w.ID, got.Path, w.Path)
				}
				if got.Backend != w.Backend {
					t.Errorf("%s: backend %q, want %q", w.ID, got.Backend, w.Backend)
				}
				if got.StateBucket != w.StateBucket {
					t.Errorf("%s: state bucket %q, want %q", w.ID, got.StateBucket, w.StateBucket)
				}
				if got.StateKey != w.StateKey {
					t.Errorf("%s: state key %q, want %q", w.ID, got.StateKey, w.StateKey)
				}
				if g, e := sortedCopy(got.Tags), sortedCopy(w.Tags); !slices.Equal(g, e) {
					t.Errorf("%s: tags %v, want %v", w.ID, g, e)
				}
				if g, e := edgeProducers(t, got, EdgeData), sortedCopy(w.Data); !slices.Equal(g, e) {
					t.Errorf("%s: data producers %v, want %v", w.ID, g, e)
				}
				if g, e := edgeProducers(t, got, EdgeAuthority), sortedCopy(w.Authority); !slices.Equal(g, e) {
					t.Errorf("%s: authority producers %v, want %v", w.ID, g, e)
				}
				if len(got.Edges) != len(w.Data)+len(w.Authority) {
					t.Errorf("%s: %d edges, want %d (no duplicates)", w.ID, len(got.Edges), len(w.Data)+len(w.Authority))
				}
			}
		})
	}
}

// TestManifestStateKeys: the state keys the task names, asserted literally
// (`<path minus "stacks/">/terraform.tfstate`).
func TestManifestStateKeys(t *testing.T) {
	cases := []struct{ fixture, id, key string }{
		{"sandbox", "account-governance", "account/account-governance/terraform.tfstate"},
		{"sandbox", "demo-state", "account/tenant-state/demo/terraform.tfstate"},
		{"sandbox", "demo-dev-project", "tenants/demo/dev/project/terraform.tfstate"},
		{"sandbox", "demo-dev-gra11-runtime", "tenants/demo/dev/gra11/runtime/terraform.tfstate"},
		{"growth", "demo-dev-gra11-runtime-blue", "tenants/demo/dev/gra11/runtime-blue/terraform.tfstate"},
		{"growth", "demo-dev-gra11-runtime-green", "tenants/demo/dev/gra11/runtime-green/terraform.tfstate"},
	}
	manifests := map[string]*Manifest{}
	for _, c := range cases {
		if manifests[c.fixture] == nil {
			manifests[c.fixture] = decodeFixture(t, c.fixture)
		}
		found := false
		for _, in := range manifests[c.fixture].Instances {
			if in.ID == c.id {
				found = true
				if in.StateKey != c.key {
					t.Errorf("%s/%s: state key %q, want %q", c.fixture, c.id, in.StateKey, c.key)
				}
				if want := "stacks/" + strings.TrimSuffix(c.key, "/terraform.tfstate"); in.Path != want {
					t.Errorf("%s/%s: path %q, want %q", c.fixture, c.id, in.Path, want)
				}
			}
		}
		if !found {
			t.Errorf("%s: no instance %s", c.fixture, c.id)
		}
	}
}

// TestManifestDecodedSpec: the manifest-level values the derivation and later tasks read.
func TestManifestDecodedSpec(t *testing.T) {
	t.Run("sandbox", func(t *testing.T) {
		m := decodeFixture(t, "sandbox")
		if m.Name != "sandbox" || m.Scope != ManifestScopePlatform || m.Org != "lz" {
			t.Errorf("name/scope/org = %q/%q/%q, want sandbox/platform/lz", m.Name, m.Scope, m.Org)
		}
		if !m.SharedStateProject || m.StateProjectRef != "SANDBOX" {
			t.Errorf("shared state project %v ref %q, want true SANDBOX (KD-1)", m.SharedStateProject, m.StateProjectRef)
		}
		want := []Tenant{{Name: "demo", Environments: []Environment{{Name: "dev", ProjectMode: "reference", ProjectRef: "SANDBOX", Regions: []string{"GRA11"}}}}}
		if !reflect.DeepEqual(m.Tenants, want) {
			t.Errorf("tenants %+v, want %+v (reference mode after T009)", m.Tenants, want)
		}
		if len(m.External) != 0 {
			t.Errorf("external %+v, want none", m.External)
		}
	})
	t.Run("growth", func(t *testing.T) {
		m := decodeFixture(t, "growth")
		if m.Scope != ManifestScopePlatform {
			t.Errorf("scope %q, want platform when spec.scope is absent", m.Scope)
		}
		if m.SharedStateProject || m.StateProjectRef != "STATE" {
			t.Errorf("shared state project %v ref %q, want false STATE", m.SharedStateProject, m.StateProjectRef)
		}
		regions := []string{"GRA11", "SBG5"}
		want := []Tenant{
			{Name: "demo", Environments: []Environment{
				{Name: "dev", ProjectMode: "adopt", ProjectRef: "DEMO_DEV", Regions: regions},
				{Name: "prod", ProjectMode: "reference", ProjectRef: "DEMO_PROD", Regions: regions}}},
			{Name: "shop", Environments: []Environment{
				{Name: "dev", ProjectMode: "adopt", ProjectRef: "SHOP_DEV", Regions: regions},
				{Name: "prod", ProjectMode: "adopt", ProjectRef: "SHOP_PROD", Regions: regions}}},
		}
		if !reflect.DeepEqual(m.Tenants, want) {
			t.Errorf("tenants %+v, want %+v", m.Tenants, want)
		}
	})
	t.Run("tenant-only", func(t *testing.T) {
		m := decodeFixture(t, "tenant-only")
		if m.Name != "demo" || m.Scope != ManifestScopeTenant {
			t.Errorf("name/scope = %q/%q, want demo/tenant", m.Name, m.Scope)
		}
		want := []ExternalRef{
			{ID: "account-governance", Stage: "account-governance"},
			{ID: "demo-state", Stage: "tenant-state", Tenant: "demo"},
		}
		if !reflect.DeepEqual(m.External, want) {
			t.Errorf("external %+v, want %+v", m.External, want)
		}
	})
}

// TestManifestGrowthUnique: SC-002 — unique ids, paths and (bucket, key), and one state bucket
// per tenant, distinct from the account bucket.
func TestManifestGrowthUnique(t *testing.T) {
	m := decodeFixture(t, "growth")
	if len(m.Instances) != 25 {
		t.Fatalf("%d instances, want 25", len(m.Instances))
	}
	ids, paths, keys := map[string]bool{}, map[string]bool{}, map[string]bool{}
	bucketOf := map[string]map[string]bool{}
	for _, in := range m.Instances {
		if ids[in.ID] || paths[in.Path] || keys[in.StateBucket+"/"+in.StateKey] {
			t.Errorf("%s: id, path or state location repeats (%s, %s/%s)", in.ID, in.Path, in.StateBucket, in.StateKey)
		}
		ids[in.ID], paths[in.Path], keys[in.StateBucket+"/"+in.StateKey] = true, true, true
		owner := "account"
		if in.Scope == ScopeEnvironment || in.Scope == ScopeRegion {
			owner = in.Tenant
		}
		if in.Backend == "local" {
			continue
		}
		if bucketOf[owner] == nil {
			bucketOf[owner] = map[string]bool{}
		}
		bucketOf[owner][in.StateBucket] = true
	}
	want := map[string]map[string]bool{
		"account": {"lz-bkt-state": true},
		"demo":    {"lz-demo-bkt-state": true},
		"shop":    {"lz-shop-bkt-state": true},
	}
	if !reflect.DeepEqual(bucketOf, want) {
		t.Errorf("state buckets by owner %v, want %v", bucketOf, want)
	}
}

// TestManifestNamingInput: every instance's naming input is org plus its own dimensions, and the
// two growth runtime slots differ in slot and nothing else.
func TestManifestNamingInput(t *testing.T) {
	for _, name := range []string{"sandbox", "growth", "tenant-only"} {
		m := decodeFixture(t, name)
		for _, in := range m.Instances {
			want := NamingInput{Org: "lz", Tenant: in.Tenant, Environment: in.Environment, Region: in.Region, Slot: in.Slot}
			if in.Naming != want {
				t.Errorf("%s/%s: naming %+v, want %+v", name, in.ID, in.Naming, want)
			}
		}
	}
	m := decodeFixture(t, "growth")
	slot := map[string]NamingInput{}
	for _, in := range m.Instances {
		slot[in.ID] = in.Naming
	}
	blue, green := slot["demo-dev-gra11-runtime-blue"], slot["demo-dev-gra11-runtime-green"]
	if blue.Slot != "blue" || green.Slot != "green" {
		t.Fatalf("slots %q and %q, want blue and green", blue.Slot, green.Slot)
	}
	blue.Slot, green.Slot = "", ""
	if blue != green || blue != (NamingInput{Org: "lz", Tenant: "demo", Environment: "dev", Region: "GRA11"}) {
		t.Errorf("blue %+v and green %+v differ in more than slot", blue, green)
	}
}

// TestManifestRefusesInvalid: each case of invalid/ is refused with its code (V005 negative).
func TestManifestRefusesInvalid(t *testing.T) {
	var cases map[string]struct {
		Base string `json:"base"`
		Code string `json:"code"`
		Why  string `json:"why"`
	}
	if err := json.Unmarshal(readManifestFixture(t, "invalid/cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(manifestFixtureDir, "invalid", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(cases) {
		t.Errorf("%d invalid fixtures, cases.json lists %d", len(files), len(cases))
	}
	codes := map[string]bool{}
	for _, f := range files {
		name := filepath.Base(f)
		c, listed := cases[name]
		if !listed {
			t.Errorf("%s: not in cases.json", name)
			continue
		}
		codes[c.Code] = true
		t.Run(strings.TrimSuffix(name, ".yaml"), func(t *testing.T) {
			_, err := DecodeManifest(readManifestFixture(t, filepath.Join("invalid", name)))
			if err == nil {
				t.Fatalf("want refusal %s (%s), got <nil>", c.Code, c.Why)
			}
			var me *ManifestError
			if !errors.As(err, &me) {
				t.Fatalf("want a *ManifestError %s (%s), got %T: %v", c.Code, c.Why, err, err)
			}
			if me.Code != c.Code {
				t.Errorf("code %s (%v), want %s (%s)", me.Code, err, c.Code, c.Why)
			}
			if strings.TrimSpace(me.Detail) == "" {
				t.Errorf("code %s without a detail naming what was refused", me.Code)
			}
		})
	}
	for _, code := range []string{
		CodeManifestSyntax, CodeDuplicateKey, CodeUnknownField, CodeMissingField, CodeUnsupportedVersion,
		CodeInvalidValue, CodeStageNotImplemented, CodeSlotNotAllowed, CodeScopeViolation, CodeDuplicateID,
		CodeDuplicateScope, CodePlatformRows, CodeMissingTenantState, CodeTenantScopeAccount,
		CodeUnresolvedProducer, CodeSharedStateProject,
	} {
		if !codes[code] {
			t.Errorf("no invalid case expects %s", code)
		}
	}
}
