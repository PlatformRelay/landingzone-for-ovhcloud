package configure

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Controls of 004 T017 (FR-002, FR-003, SC-001, V002, guard G30): the manifest is derived from
// the answers by data-model.md *Derivation* and returned only when stacks.DecodeManifest accepts
// it. Value-equality is data-model's: whole-line comments removed, the JSON values compared.

const (
	exportSandboxManifest    = "../../../stacks/deployments.yaml"
	exportTwoTenantsManifest = "../../../tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml"
)

func exportAnswers(t *testing.T, name string) Answers {
	t.Helper()
	a, err := DecodeAnswers(answersFixture(t, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return a
}

// exportValue is a manifest's JSON value, whole-line comments removed.
func exportValue(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(stripComments(data), &v); err != nil {
		t.Fatalf("not one JSON-compatible document: %v\n%s", err, data)
	}
	return v
}

// exportSpec exports the answers, requires success, and returns the manifest's spec object.
func exportSpec(t *testing.T, a Answers) map[string]any {
	t.Helper()
	got, err := Export(a)
	if err != nil {
		t.Fatalf("want exported, got %v", err)
	}
	spec, _ := exportValue(t, got)["spec"].(map[string]any)
	return spec
}

// exportAt walks a decoded JSON value along keys and array indices.
func exportAt(v any, path ...any) any {
	for _, step := range path {
		switch s := step.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[s]
		case int:
			a, _ := v.([]any)
			if s >= len(a) {
				return nil
			}
			v = a[s]
		}
	}
	return v
}

// exportRowIDs lists the instance ids of a spec in manifest order.
func exportRowIDs(spec map[string]any) []string {
	var ids []string
	rows, _ := spec["instances"].([]any)
	for _, r := range rows {
		id, _ := exportAt(r, "id").(string)
		ids = append(ids, id)
	}
	return ids
}

// TestExportMatchesCommittedManifests: the fixtures export value-equal to the committed manifests
// and the bytes pass stacks.DecodeManifest (acceptance; exposes a wrong default, a missing toggle,
// a row in the wrong order, rows not grouped per tenant).
func TestExportMatchesCommittedManifests(t *testing.T) {
	for _, row := range []struct{ name, answers, manifest string }{
		{"sandbox", "sandbox.yaml", exportSandboxManifest},
		{"two-tenants", "sandbox-two-tenants.yaml", exportTwoTenantsManifest},
	} {
		t.Run(row.name, func(t *testing.T) { exportMatchRow(t, row.answers, row.manifest) })
	}
}

func exportMatchRow(t *testing.T, answers, manifest string) {
	got, err := Export(exportAnswers(t, answers))
	if err != nil {
		t.Fatalf("want exported, got %v", err)
	}
	if _, err := stacks.DecodeManifest(got); err != nil {
		t.Errorf("DecodeManifest refuses the export: %v", err)
	}
	want := answersReadFile(t, manifest)
	if !reflect.DeepEqual(exportValue(t, got), exportValue(t, want)) {
		t.Errorf("export is not value-equal to %s:\n%s", manifest, got)
	}
}

// TestExportDeterministic: the same answers, decoded twice, give identical bytes (acceptance;
// exposes map-order output).
func TestExportDeterministic(t *testing.T) {
	for _, name := range []string{"sandbox-two-tenants.yaml", "all-fields.yaml"} {
		first, err := Export(exportAnswers(t, name))
		if err != nil {
			t.Fatalf("%s: want exported, got %v", name, err)
		}
		for range 20 {
			again, _ := Export(exportAnswers(t, name))
			if !bytes.Equal(first, again) {
				t.Fatalf("%s: two exports differ:\n%s\n---\n%s", name, first, again)
			}
		}
		exportKeyOrder(t, name, first)
	}
}

// exportKeyOrder: the keys appear in data-model *Derivation* order, which value-equality cannot
// see (each key's first occurrence; the fixtures set shared_state_project, so sandbox is written).
func exportKeyOrder(t *testing.T, name string, data []byte) {
	t.Helper()
	at := -1
	for _, key := range []string{`"apiVersion"`, `"kind"`, `"metadata"`, `"spec"`, `"scope"`, `"org"`, `"forge"`,
		`"stage_source"`, `"sandbox"`, `"state"`, `"tenants"`, `"instances"`} {
		i := bytes.Index(data, []byte(key+": ")) // a key, not a value such as metadata.name "sandbox"
		if i <= at {
			t.Errorf("%s: key %s at byte %d, not after the previous key (byte %d)", name, key, i, at)
		}
		at = i
	}
}

// TestExportNoTenants: an answers file with no tenant exports the two account rows only
// (acceptance; exposes a null tenants list or a refused empty manifest).
func TestExportNoTenants(t *testing.T) {
	a := exportAnswers(t, "sandbox.yaml")
	a.Tenants = []Tenant{}
	spec := exportSpec(t, a)
	if got, ok := spec["tenants"].([]any); !ok || len(got) != 0 {
		t.Errorf("tenants %v, want []", spec["tenants"])
	}
	if ids := exportRowIDs(spec); !slices.Equal(ids, []string{"account-bootstrap", "account-governance"}) {
		t.Errorf("rows %v, want the two account rows", ids)
	}
}

// TestExportSandboxBlock: shared_state_project false writes no sandbox key, true writes
// {"shared_state_project": true} (acceptance; exposes a false block the decoder refuses).
func TestExportSandboxBlock(t *testing.T) {
	a := exportAnswers(t, "sandbox.yaml")
	a.SharedStateProject = false
	spec := exportSpec(t, a)
	if spec == nil {
		t.Fatal("shared_state_project false: no spec object")
	}
	if _, present := spec["sandbox"]; present {
		t.Errorf("shared_state_project false: sandbox written: %v", spec["sandbox"])
	}
	a.SharedStateProject = true
	want := map[string]any{"shared_state_project": true}
	if got := exportAt(exportSpec(t, a), "sandbox"); !reflect.DeepEqual(got, want) {
		t.Errorf("shared_state_project true: sandbox %v, want %v", got, want)
	}
}

// TestExportRegionWithoutCIDR: all-fields.yaml's second region of shop/prod (GRA11) has no cidr:
// no network key and no network row, its runtime row kept; the first region (DE1) keeps both
// (acceptance; exposes a network row without a network).
func TestExportRegionWithoutCIDR(t *testing.T) {
	spec := exportSpec(t, exportAnswers(t, "all-fields.yaml"))
	regions := exportAt(spec, "tenants", 0, "environments", 0, "regions")
	if got, _ := exportAt(regions, 1).(map[string]any); got == nil || got["name"] != "GRA11" {
		t.Fatalf("regions of shop/prod: %v", regions)
	} else if _, present := got["network"]; present {
		t.Errorf("GRA11 without cidr has a network: %v", got)
	}
	if exportAt(regions, 0, "network") == nil {
		t.Errorf("DE1 with cidr has no network: %v", regions)
	}
	ids := exportRowIDs(spec)
	if slices.Contains(ids, "shop-prod-gra11-network") || !slices.Contains(ids, "shop-prod-gra11-runtime") {
		t.Errorf("rows %v: want shop-prod-gra11-runtime and no shop-prod-gra11-network", ids)
	}
	want := []string{"account-bootstrap", "account-governance", "shop-state", "shop-prod-project",
		"shop-prod-de1-network", "shop-prod-de1-runtime", "shop-prod-gra11-runtime", "shop-test-project", "empty-state"}
	if !slices.Equal(ids, want) {
		t.Errorf("rows %v\nwant %v", ids, want)
	}
}

// TestExportOverrides: every overridable default of all-fields.yaml is set off its default and
// appears in the manifest; shop/test omits them and gets the defaults (acceptance; exposes an
// answer dropped in favour of its default).
func TestExportOverrides(t *testing.T) {
	spec := exportSpec(t, exportAnswers(t, "all-fields.yaml"))
	prod := []any{"tenants", 0, "environments", 0}
	test := []any{"tenants", 0, "environments", 1}
	for _, row := range []struct {
		path []any
		want any
	}{
		{[]any{"state", "endpoint"}, "https://s3.gra.io.cloud.ovh.net"},
		{[]any{"state", "project", "ref"}, "ACME_STATE"},
		{[]any{"state", "region"}, "de"},
		{append(slices.Clone(prod), "project"), map[string]any{"mode": "adopt", "ref": "SHOP_PRODUCTION"}},
		{append(slices.Clone(prod), "regions", 0, "network", "vlan_id"), float64(4000)},
		{append(slices.Clone(prod), "budget_alert"), map[string]any{"enabled": true}},
		{append(slices.Clone(prod), "quota_guard"), map[string]any{"enabled": true}},
		{append(slices.Clone(test), "project"), map[string]any{"mode": "reference", "ref": "SHOP_TEST"}},
		{append(slices.Clone(test), "budget_alert"), map[string]any{"enabled": false}},
		{append(slices.Clone(test), "quota_guard"), map[string]any{"enabled": false}},
	} {
		if got := exportAt(spec, row.path...); !reflect.DeepEqual(got, row.want) {
			t.Errorf("%v = %v, want %v", row.path, got, row.want)
		}
	}
}

// TestExportFixedKeysNotOverridable (pin): scope and stage_source are fixed by the derivation;
// in the answers they are UNKNOWN_FIELD (T016's decoder). Control: the document without the key
// decodes, and the key was inserted.
func TestExportFixedKeysNotOverridable(t *testing.T) {
	base := string(answersFixture(t, "sandbox.yaml"))
	if _, err := DecodeAnswers([]byte(base)); err != nil {
		t.Fatalf("control: %v", err)
	}
	for key, insert := range map[string]string{
		"scope":        `"scope": "platform",`,
		"stage_source": `"stage_source": {"kind": "local"},`,
	} {
		doc := strings.Replace(base, `"org": "lz",`, `"org": "lz", `+insert, 1)
		if doc == base {
			t.Fatalf("%s: not inserted", key)
		}
		answersWantRefusal(t, []byte(doc), CodeUnknownField, key, "fixed by the derivation")
	}
}

// exportGateCase is an answers file whose derived manifest the decoder refuses, and the edit
// that removes the defect (its positive control).
type exportGateCase struct {
	name    string
	code    string
	defect  func(*Answers)
	control func(*Answers)
}

// exportGateCases puts each defect in the second tenant, lab, of the two-tenant answers.
var exportGateCases = []exportGateCase{
	{"two regions with VLAN 0", stacks.CodeDuplicateVLAN,
		func(a *Answers) {
			exportLab(a).Regions = append(exportLab(a).Regions, Region{Name: "DE1", CIDR: "10.41.0.0/24"})
		},
		func(a *Answers) {
			exportLab(a).Regions = append(exportLab(a).Regions, Region{Name: "DE1", CIDR: "10.41.0.0/24", VlanID: answersVlan(1)})
		}},
	{"project_ref STATE without the flag", stacks.CodeSharedStateProject,
		func(a *Answers) { a.SharedStateProject = false; exportLab(a).ProjectRef = "STATE" },
		func(a *Answers) { a.SharedStateProject = true; exportLab(a).ProjectRef = "STATE" }},
	{"two environments with one project_ref", stacks.CodeDuplicateName,
		func(a *Answers) { exportLabEnvs(a, "LAB_DEV") },
		func(a *Answers) { exportLabEnvs(a, "") }},
	{"region name of 60 letters and a digit", stacks.CodeInvalidValue,
		func(a *Answers) { exportLab(a).Regions[0].Name = strings.Repeat("A", 60) + "1" },
		func(a *Answers) { exportLab(a).Regions[0].Name = strings.Repeat("A", 2) + "1" }},
}

func exportLab(a *Answers) *Environment { return &a.Tenants[1].Environments[0] }

// exportLabEnvs adds environment prod to lab, with project_ref ref ("" for its default LAB_PROD).
func exportLabEnvs(a *Answers, ref string) {
	a.Tenants[1].Environments = append(a.Tenants[1].Environments,
		Environment{Name: "prod", ProjectRef: ref, Regions: []Region{}})
}

// exportGateAnswers applies edit to the two-tenant answers and round-trips the result through
// DecodeAnswers, so the case is an answers file T016's decoder accepts.
func exportGateAnswers(t *testing.T, edit func(*Answers)) Answers {
	t.Helper()
	a := exportAnswers(t, "sandbox-two-tenants.yaml")
	edit(&a)
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAnswers(data)
	if err != nil {
		t.Fatalf("the answers decoder refuses the case: %v", err)
	}
	return decoded
}

// TestExportGateRefuses: answers whose derived manifest stacks.DecodeManifest refuses return the
// decoder's code and no bytes; the same answers with the defect removed export (rejection cases
// with positive controls; G30: exposes output after a refusal or a skipped gate).
func TestExportGateRefuses(t *testing.T) {
	for _, c := range exportGateCases {
		t.Run(c.name, func(t *testing.T) { exportGateRow(t, c) })
	}
}

func exportGateRow(t *testing.T, c exportGateCase) {
	if got, err := Export(exportGateAnswers(t, c.control)); err != nil || got == nil {
		t.Errorf("control: want exported, got %d bytes, %v", len(got), err)
	}
	got, err := Export(exportGateAnswers(t, c.defect))
	// the decoder's own error, unwrapped, with its detail naming the second tenant's place
	refusal, ok := err.(*stacks.ManifestError)
	if !ok || refusal.Code != c.code || !strings.Contains(refusal.Detail, "lab") {
		t.Errorf("want the decoder's %s naming lab, got %T: %v", c.code, err, err)
	}
	if got != nil {
		t.Errorf("%d bytes returned after a refusal", len(got))
	}
}

// TestExportValueCounts (pin): the answers fixtures hold 11 and 15 values, the manifests they
// export to 41 and 67 (research R2, SC-001). Control: the sandbox manifest with one value more
// counts 42.
func TestExportValueCounts(t *testing.T) {
	for _, row := range []struct {
		data []byte
		want int
	}{
		{answersFixture(t, "sandbox.yaml"), 11},
		{answersFixture(t, "sandbox-two-tenants.yaml"), 15},
		{answersReadFile(t, exportSandboxManifest), 41},
		{answersReadFile(t, exportTwoTenantsManifest), 67},
	} {
		if got := answersValueCount(t, row.data); got != row.want {
			t.Errorf("%d values, want %d:\n%s", got, row.want, row.data)
		}
	}
	more := strings.Replace(string(answersReadFile(t, exportSandboxManifest)), `"org": "lz",`, `"org": "lz", "x": 1,`, 1)
	if got := answersValueCount(t, []byte(more)); got != 42 {
		t.Errorf("control: one value more counts %d, want 42", got)
	}
}
