package live

// Retained-resource guard G7, plan part (FR-004, FR-011, research R12): the one shared check every
// verb that applies (probe, bootstrap `state`, apply, chain) runs on `tofu show -json` of its saved
// plan before applying exactly that plan. It refuses a plan that deletes, replaces (in either
// order) or forgets a resource of a retained instance, also one whose block is no longer in the
// configuration (where `prevent_destroy` no longer applies), and a plan it cannot read.
//
// STUB (T063): the tests pin the behaviour; T064 implements it.

// CondRetained is the condition of a retained-resource refusal.
const CondRetained = "retained"

// Retained is one retained instance and its resource addresses in the planned root. An address
// covers itself, its instances (`a.b` covers `a.b["k"]`) and, for a module address, every resource
// under it and under each of its instances (`module.m` covers `module.m.x.y` and
// `module.m["k"].x.y`); never a sibling that only shares a prefix. An entry with no addresses
// covers nothing (a retained instance's first apply has an empty state).
type Retained struct {
	Instance  string
	Addresses []string
}

// Protect returns a *Refusal (CondRetained) naming every covered address the plan deletes,
// replaces or forgets (judged by the actions, whatever the action reason), or when plan is not a
// complete `tofu show -json` plan or is marked errored; nil otherwise. The refusal names each
// address verbatim (unquoted) with its instance, never a value from the plan.
func Protect(plan []byte, retained []Retained) error { return nil }
