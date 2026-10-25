package configure

import (
	"slices"
	"strconv"
	"strings"
)

// This file is the consequence notes (FR-006, data-model.md *Consequence notes*): what the answers
// mean beyond the manifest, printed after a successful check or export, in a fixed order, each
// note only when its trigger holds. The notes name account.env variables, never values.

// Notes returns the consequence notes of decoded answers, one line each, in data-model order. It
// reads the answers with their defaults applied, as Export writes them.
func Notes(a Answers) []string {
	d := derive(a)
	refs, modes, cidr := notesScan(d)
	// D113; the binding of tools/internal/live/publish.go.
	notes := []string{"note: account.env needs " + strings.Join(refs, ", ") + " (names only; this command never reads the file)."}
	if len(modes) > 0 { // D107, 005 P25, A17 (spec 007 US2)
		notes = append(notes, "note: one project per tenant and environment is the only isolation boundary, and tags do not scope bucket access; "+
			"two refs whose LZ_PROJECT_ID_<REF> name one project are refused at bind (except the state ref under KD-1), "+
			"so configure: ok and a green stacks:check do not mean the bind passes.")
	}
	if d.Spec.Sandbox != nil { // D88, D105
		notes = append(notes, "note: KD-1: the state buckets share the project of "+d.Spec.State.Project.Ref+
			" with tenant resources; tenant state isolation is not claimed.")
	}
	if len(modes) == 0 {
		modes = []string{"no environment"}
	}
	// D94.
	notes = append(notes, "note: project mode: "+strings.Join(modes, ", ")+
		"; reference uses the existing project, adopt plans a replacement of it (P5 refuted).")
	if cidr { // 005 T098
		notes = append(notes, "note: each private network in a project needs its own VLAN id.")
	}
	return append(notes, // 005 P21, T057; D104
		"note: bucket names are unique across OVHcloud; org "+strconv.Quote(d.Spec.Org)+" keeps them apart, and a taken name fails.",
		"note: state buckets are Object-Locked in governance mode; tearing one down needs the governance bypass or the end of the retention.")
}

// notesScan reads the derived manifest once: the account.env names (the state ref first, then each
// environment's ref, each once), the mode of each environment, and whether any region has a cidr.
func notesScan(d exportManifest) (refs, modes []string, cidr bool) {
	refs = []string{"LZ_PROJECT_ID_" + d.Spec.State.Project.Ref}
	for _, t := range d.Spec.Tenants {
		for _, e := range t.Environments {
			if ref := "LZ_PROJECT_ID_" + e.Project.Ref; !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
			modes = append(modes, t.Name+"/"+e.Name+" "+e.Project.Mode)
			cidr = cidr || slices.ContainsFunc(e.Regions, func(r exportRegion) bool { return r.Network != nil })
		}
	}
	return refs, modes, cidr
}
