package configure

import (
	"slices"
	"strings"
	"testing"
)

// Controls of 004 T019 (FR-006, V004): the consequence notes of data-model.md *Consequence notes*,
// in its order, each only when its trigger holds; account.env variables by name, never a value.
// Cases are written from spec.md FR-006, data-model.md and the task text.

const (
	notesOneProject = "note: one project per tenant and environment is the only isolation boundary, and tags do not scope bucket access; " +
		"two refs whose LZ_PROJECT_ID_<REF> name one project are refused at bind (except the state ref under KD-1), " +
		"so configure: ok and a green stacks:check do not mean the bind passes."
	notesKD1         = "note: KD-1: the state buckets share the project of STATE with tenant resources; tenant state isolation is not claimed."
	notesModeTail    = "; reference uses the existing project, adopt plans a replacement of it (P5 refuted)."
	notesVLAN        = "note: each private network in a project needs its own VLAN id."
	notesOrgLZ       = `note: bucket names are unique across OVHcloud; org "lz" keeps them apart, and a taken name fails.`
	notesObjectLock  = "note: state buckets are Object-Locked in governance mode; tearing one down needs the governance bypass or the end of the retention."
	notesAccountHead = "note: account.env needs "
	notesAccountTail = " (names only; this command never reads the file)."
)

// TestNotesTwoTenants: the two-tenant answers give every note, in data-model order (acceptance;
// exposes a missing or misordered note, and a project id or value printed).
func TestNotesTwoTenants(t *testing.T) {
	want := []string{
		notesAccountHead + "LZ_PROJECT_ID_STATE, LZ_PROJECT_ID_DEMO_DEV, LZ_PROJECT_ID_LAB_DEV" + notesAccountTail,
		notesOneProject,
		notesKD1,
		"note: project mode: demo/dev reference, lab/dev reference" + notesModeTail,
		notesVLAN,
		notesOrgLZ,
		notesObjectLock,
	}
	got := Notes(exportAnswers(t, "sandbox-two-tenants.yaml"))
	if !slices.Equal(got, want) {
		t.Errorf("notes:\n got  %q\n want %q", got, want)
	}
	for _, line := range got {
		if strings.Contains(line, "=") {
			t.Errorf("a note holds a value assignment: %q", line)
		}
	}
	if again := Notes(exportAnswers(t, "sandbox-two-tenants.yaml")); !slices.Equal(again, got) {
		t.Errorf("two runs differ:\n%q\n%q", got, again)
	}
}

// TestNotesTriggers: without shared_state_project and without any cidr there is no KD-1 note and
// no VLAN note; control: the same answers with the flag and one cidr give both (rejection of a
// note whose trigger does not hold).
func TestNotesTriggers(t *testing.T) {
	plain := exportAnswers(t, "sandbox-two-tenants.yaml")
	plain.SharedStateProject = false
	plain.StateRef = "ACME_STATE"
	for i := range plain.Tenants {
		plain.Tenants[i].Environments[0].Regions[0].CIDR = ""
	}
	plain.Tenants[1].Environments[0].ProjectMode = "adopt"
	want := []string{
		notesAccountHead + "LZ_PROJECT_ID_ACME_STATE, LZ_PROJECT_ID_DEMO_DEV, LZ_PROJECT_ID_LAB_DEV" + notesAccountTail,
		notesOneProject,
		"note: project mode: demo/dev reference, lab/dev adopt" + notesModeTail,
		notesOrgLZ,
		notesObjectLock,
	}
	if got := Notes(plain); !slices.Equal(got, want) {
		t.Errorf("without the flag and any cidr:\n got  %q\n want %q", got, want)
	}

	kd1 := strings.Replace(notesKD1, "STATE", "ACME_STATE", 1)
	flagOnly := plain
	flagOnly.SharedStateProject = true
	if got := Notes(flagOnly); !slices.Contains(got, kd1) || slices.Contains(got, notesVLAN) {
		t.Errorf("control: the flag alone: want the KD-1 note and no VLAN note, got %q", got)
	}

	cidrOnly := plain
	cidrOnly.Tenants = slices.Clone(plain.Tenants)
	cidrOnly.Tenants[1].Environments = slices.Clone(plain.Tenants[1].Environments)
	cidrOnly.Tenants[1].Environments[0].Regions = []Region{{Name: "GRA11", CIDR: "10.40.0.0/24"}}
	if got := Notes(cidrOnly); !slices.Contains(got, notesVLAN) || slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, "note: KD-1") }) {
		t.Errorf("control: one cidr alone: want the VLAN note and no KD-1 note, got %q", got)
	}
}

// TestNotesNoEnvironment: answers without an environment give no one-project note, name only the
// state ref and say there is no environment to name a mode for; org is the answer's (acceptance).
func TestNotesNoEnvironment(t *testing.T) {
	a := exportAnswers(t, "sandbox.yaml")
	a.SharedStateProject = false
	a.Org = "acme"
	a.Tenants[0].Environments = nil
	want := []string{
		notesAccountHead + "LZ_PROJECT_ID_STATE" + notesAccountTail,
		"note: project mode: no environment" + notesModeTail,
		strings.Replace(notesOrgLZ, `"lz"`, `"acme"`, 1),
		notesObjectLock,
	}
	if got := Notes(a); !slices.Equal(got, want) {
		t.Errorf("no environment:\n got  %q\n want %q", got, want)
	}
}

// TestNotesRefsOnce: a ref named twice (an environment on the state ref, two environments on one
// ref) is listed once, at its first place (acceptance).
func TestNotesRefsOnce(t *testing.T) {
	a := exportAnswers(t, "sandbox-two-tenants.yaml")
	a.Tenants[0].Environments[0].ProjectRef = "STATE"
	a.Tenants[1].Environments[0].ProjectRef = "SHARED"
	a.Tenants[1].Environments = append(a.Tenants[1].Environments, Environment{Name: "test", ProjectRef: "SHARED"})
	want := notesAccountHead + "LZ_PROJECT_ID_STATE, LZ_PROJECT_ID_SHARED" + notesAccountTail
	got := Notes(a)
	if len(got) == 0 || got[0] != want {
		t.Errorf("account.env note:\n got  %q\n want %q", got, want)
	}
}
