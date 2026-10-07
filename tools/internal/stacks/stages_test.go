package stacks

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Controls of 005 T034: the stage table in code is the one data-model.md publishes. The test reads
// the document, not a copy of it, so an edit on either side without the other is red.

const dataModelPath = "../../../specs/005-first-landing-zone-slice/data-model.md"

// cellList reads a stage-table cell naming stages: "—" is none, a parenthesised remark is dropped.
func cellList(cell string) []string {
	if i := strings.Index(cell, "("); i >= 0 {
		cell = cell[:i]
	}
	var out []string
	for _, s := range strings.Split(cell, ",") {
		if s = strings.TrimSpace(s); s != "" && s != "—" {
			out = append(out, s)
		}
	}
	return out
}

// documentedStages parses data-model *Stage table* into Stage rows and the stage enum of the
// manifest rules (in its written order).
func documentedStages(t *testing.T) ([]Stage, []string) {
	t.Helper()
	data, err := os.ReadFile(dataModelPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	_, section, found := strings.Cut(doc, "\n## Stage table")
	if !found {
		t.Fatal("data-model.md has no Stage table section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var rows []Stage
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Stage ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 7 {
			t.Fatalf("stage table row with %d cells: %s", len(cells), line)
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		backend, bucket, _ := strings.Cut(cells[5], " (")
		bucket, _, _ = strings.Cut(bucket, " bucket")
		rows = append(rows, Stage{
			Name: cells[0], Scope: strings.TrimSpace(strings.TrimSuffix(cells[1], "(+ slot)")),
			Data: cellList(cells[2]), Authority: cellList(cells[3]), Principal: cells[4],
			Backend: backend, Bucket: bucket, Chain: cells[6],
			Slots: strings.Contains(cells[1], "+ slot"), Implemented: true,
		})
	}
	enum := regexp.MustCompile("`stage`: enum `([^`]+)`").FindStringSubmatch(doc)
	if enum == nil {
		t.Fatal("data-model.md has no `stage` enum rule")
	}
	var names []string
	for _, s := range strings.Split(enum[1], "|") {
		names = append(names, strings.TrimSpace(s))
	}
	return rows, names
}

// normalised drops the difference between a nil and an empty list.
func normalised(s Stage) Stage {
	if len(s.Data) == 0 {
		s.Data = nil
	}
	if len(s.Authority) == 0 {
		s.Authority = nil
	}
	return s
}

// TestStageTableMatchesDataModel: the code's table holds every stage of the enum in its order; each
// implemented stage equals its data-model row (scope, data and authority producers, principal,
// backend and bucket, live chain, slot); every enum stage without a row is reserved, not
// implemented and has no scope, and the reserved set is exactly account-admin, account-fabric and
// observability (research R10).
func TestStageTableMatchesDataModel(t *testing.T) {
	rows, enum := documentedStages(t)
	var names []string
	byName := map[string]Stage{}
	for _, s := range stageTable {
		names = append(names, s.Name)
		byName[s.Name] = s
	}
	if !reflect.DeepEqual(names, enum) {
		t.Errorf("code stages %v, data-model enum %v", names, enum)
	}
	documented := map[string]bool{}
	for _, want := range rows {
		documented[want.Name] = true
		got, ok := byName[want.Name]
		if !ok {
			t.Errorf("stage %s: in the data-model table, not in code", want.Name)
			continue
		}
		if !reflect.DeepEqual(normalised(got), normalised(want)) {
			t.Errorf("stage %s: code %+v, data-model %+v", want.Name, got, want)
		}
	}
	var reserved []string
	for _, s := range stageTable {
		if documented[s.Name] {
			continue
		}
		reserved = append(reserved, s.Name)
		if s.Implemented || s.Scope != "" || len(s.Data)+len(s.Authority) != 0 {
			t.Errorf("reserved stage %s: %+v, want not implemented, no scope, no producers", s.Name, s)
		}
	}
	if want := []string{"account-admin", "account-fabric", "observability"}; !reflect.DeepEqual(reserved, want) {
		t.Errorf("reserved stages %v, want %v", reserved, want)
	}
	if s := byName["tenant-state"]; s.Scope != ScopeAccountTenant || s.Bucket != "account" {
		t.Errorf("tenant-state: scope %q bucket %q, want account-tenant in the account bucket", s.Scope, s.Bucket)
	}
}

// TestStageTableProducersAreStages: every producer the table names is an implemented stage whose
// scope is no narrower than its consumer's, so a producer instance can always be resolved from the
// consumer's dimensions.
func TestStageTableProducersAreStages(t *testing.T) {
	depth := map[string]int{ScopeAccount: 0, ScopeAccountTenant: 1, ScopeEnvironment: 2, ScopeRegion: 3}
	byName := map[string]Stage{}
	for _, s := range stageTable {
		byName[s.Name] = s
	}
	for _, s := range stageTable {
		for _, p := range append(append([]string{}, s.Data...), s.Authority...) {
			prod, ok := byName[p]
			if !ok || !prod.Implemented {
				t.Errorf("%s: producer %s is not an implemented stage", s.Name, p)
				continue
			}
			if depth[prod.Scope] > depth[s.Scope] {
				t.Errorf("%s (%s): producer %s has the narrower scope %s", s.Name, s.Scope, p, prod.Scope)
			}
		}
	}
}
