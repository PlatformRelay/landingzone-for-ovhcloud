package configure

import (
	"slices"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Controls of 004 T019 (FR-005, V004): the dry-run diff compares instance rows by id, removed rows
// first, each with the mark data-model.md *Dry-run diff entry* gives, then the count line and, when
// a row is removed, the warning line. Cases are written from spec.md FR-005 and US3, data-model.md
// and the task text.

const (
	diffCreated = "expected: created by stacks:reconcile"
	diffRefused = "expected: refused by stacks:reconcile (UNSUPPORTED_CHANGE)"
)

// diffLabRows are the four rows the two-tenant export holds and the sandbox manifest does not.
var diffLabRows = []string{
	"row lab-state (tenant-state lab)",
	"row lab-dev-project (project lab/dev)",
	"row lab-dev-gra11-network (project-network lab/dev/GRA11)",
	"row lab-dev-gra11-runtime (runtime lab/dev/GRA11)",
}

func diffLines(prefix, mark string, rows []string) []string {
	var lines []string
	for _, r := range rows {
		lines = append(lines, prefix+" "+r+": "+mark)
	}
	return lines
}

func diffExport(t *testing.T, answers string) []byte {
	t.Helper()
	data, err := Export(exportAnswers(t, answers))
	if err != nil {
		t.Fatalf("%s: want exported, got %v", answers, err)
	}
	return data
}

func diffWant(t *testing.T, current, generated []byte, want []string) {
	t.Helper()
	got, err := Diff(current, generated)
	if err != nil {
		t.Fatalf("want a diff, got %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("diff:\n got  %q\n want %q", got, want)
	}
}

// TestDiffRowsAdded: the two-tenant export against the sandbox manifest adds lab's four rows, each
// marked as created, and prints no warning (acceptance; exposes a row-blind diff).
func TestDiffRowsAdded(t *testing.T) {
	want := append(diffLines("+", diffCreated, diffLabRows), "diff: 4 added, 0 removed")
	diffWant(t, answersReadFile(t, exportSandboxManifest), diffExport(t, "sandbox-two-tenants.yaml"), want)
}

// TestDiffRowsRemoved: the sandbox export against the two-tenant manifest removes lab's four rows,
// each marked as refused, and warns (acceptance; exposes removals presented as harmless).
func TestDiffRowsRemoved(t *testing.T) {
	want := append(diffLines("-", diffRefused, diffLabRows),
		"diff: 0 added, 4 removed",
		"configure: warning: stacks:reconcile is expected to refuse this export (4 removed rows)")
	diffWant(t, answersReadFile(t, exportTwoTenantsManifest), diffExport(t, "sandbox.yaml"), want)
}

// TestDiffNone: the sandbox export against the sandbox manifest changes no row (acceptance;
// exposes a diff that invents changes).
func TestDiffNone(t *testing.T) {
	diffWant(t, answersReadFile(t, exportSandboxManifest), diffExport(t, "sandbox.yaml"), []string{"diff: none"})
}

// TestDiffKeptIDs: a kept id whose stage changed (the network and runtime rows swap ids) and a
// changed CIDR are value-level changes FR-005 does not list: `diff: none` (acceptance; exposes a
// diff by whole row instead of by id).
func TestDiffKeptIDs(t *testing.T) {
	current := answersReadFile(t, exportSandboxManifest)
	for _, edit := range [][2]string{
		{`"id": "demo-dev-gra11-network"`, `"id": "swap-network"`},
		{`"id": "demo-dev-gra11-runtime"`, `"id": "demo-dev-gra11-network"`},
		{`"id": "swap-network"`, `"id": "demo-dev-gra11-runtime"`},
		{`"cidr": "10.20.0.0/24"`, `"cidr": "10.30.0.0/24"`},
	} {
		if strings.Count(string(current), edit[0]) != 1 {
			t.Fatalf("fixture: %s not found once", edit[0])
		}
		current = []byte(strings.Replace(string(current), edit[0], edit[1], 1))
	}
	if _, err := stacks.DecodeManifest(current); err != nil {
		t.Fatalf("control: the edited manifest decodes: %v", err)
	}
	diffWant(t, current, diffExport(t, "sandbox.yaml"), []string{"diff: none"})
}

// TestDiffNoCurrentManifest: without a current manifest every row is added, in the generated
// manifest's order (acceptance).
func TestDiffNoCurrentManifest(t *testing.T) {
	want := append(diffLines("+", diffCreated, []string{
		"row account-bootstrap (bootstrap)",
		"row account-governance (account-governance)",
		"row demo-state (tenant-state demo)",
		"row demo-dev-project (project demo/dev)",
		"row demo-dev-gra11-network (project-network demo/dev/GRA11)",
		"row demo-dev-gra11-runtime (runtime demo/dev/GRA11)",
	}), "diff: 6 added, 0 removed")
	diffWant(t, nil, diffExport(t, "sandbox.yaml"), want)
}

// TestDiffRemovedFirst: a renamed tenant removes its rows and adds the new ones; removed rows come
// first, each group in the order of the manifest that holds it, and the same inputs give the same
// lines twice (acceptance; exposes unordered output).
func TestDiffRemovedFirst(t *testing.T) {
	a := exportAnswers(t, "sandbox-two-tenants.yaml")
	a.Tenants[1].Name = "lib"
	generated, err := Export(a)
	if err != nil {
		t.Fatal(err)
	}
	current := answersReadFile(t, exportTwoTenantsManifest)
	var lib []string
	for _, r := range diffLabRows {
		lib = append(lib, strings.ReplaceAll(r, "lab", "lib"))
	}
	want := append(diffLines("-", diffRefused, diffLabRows), diffLines("+", diffCreated, lib)...)
	want = append(want, "diff: 4 added, 4 removed",
		"configure: warning: stacks:reconcile is expected to refuse this export (4 removed rows)")
	diffWant(t, current, generated, want)
	first, _ := Diff(current, generated)
	second, _ := Diff(current, generated)
	if !slices.Equal(first, second) {
		t.Errorf("two runs differ:\n%q\n%q", first, second)
	}
}

// diffSecret is a secret-looking value pasted into a current manifest's apiVersion, which the
// manifest decoder quotes in its own error.
const diffSecret = "s004-AKIAEXAMPLESECRETVALUE"

// TestDiffCurrentManifestInvalid: a current manifest that does not decode (garbage, an empty file,
// a manifest the decoder refuses, one whose refusal would quote a secret-looking value) gives
// CURRENT_MANIFEST_INVALID with the path alone as detail, never a value from the file (T024 a,
// FR-008), and no line; control: the valid manifest diffs (rejection; exposes a diff against
// garbage, or the decoder's error echoed).
func TestDiffCurrentManifestInvalid(t *testing.T) {
	generated := diffExport(t, "sandbox.yaml")
	valid := answersReadFile(t, exportSandboxManifest)
	if _, err := Diff(valid, generated); err != nil {
		t.Fatalf("control: the valid manifest: %v", err)
	}
	secret := []byte(strings.Replace(string(valid), stacks.ManifestAPIVersion, diffSecret, 1))
	if _, err := stacks.DecodeManifest(secret); err == nil || !strings.Contains(err.Error(), diffSecret) {
		t.Fatalf("precondition: the decoder's refusal quotes the value, got %v", err)
	}
	for name, current := range map[string][]byte{
		"garbage":       []byte("not a manifest"),
		"empty":         {},
		"refused value": []byte(strings.Replace(string(valid), `"org": "lz"`, `"org": "LZ!"`, 1)),
		"secret value":  secret,
	} {
		t.Run(name, func(t *testing.T) {
			lines, err := Diff(current, generated)
			e, ok := err.(*AnswersError)
			if !ok || e.Code != CodeCurrentManifestInvalid {
				t.Fatalf("want a %s *AnswersError, got %T %v", CodeCurrentManifestInvalid, err, err)
			}
			if e.Detail != "stacks/deployments.yaml" {
				t.Errorf("detail %q; want the path alone", e.Detail)
			}
			if lines != nil {
				t.Errorf("want no diff, got %q", lines)
			}
		})
	}
}
