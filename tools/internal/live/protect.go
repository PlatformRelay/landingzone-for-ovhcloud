package live

// Retained-resource guard G7, plan part (FR-004, FR-011, research R12): the one shared check every
// verb that applies or destroys (the run core, bootstrap `state`, plan|apply, destroy|chain) is to
// run on `tofu show -json` of its saved plan before applying exactly that plan (callers T055, T057,
// T059, T047). It refuses a plan that deletes, replaces (in either order) or forgets a resource of a
// retained instance, also one whose block is no longer in the configuration (where
// `prevent_destroy` no longer applies); a plan whose `moved` block takes a retained resource to an
// address no retained entry covers (a later plan could then delete it unguarded); and a document
// it cannot read as a plan.

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/planjson"
)

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

// harmless are the plan actions that keep a resource's object in state. A change of a covered
// address is admitted only when it has at least one action and every action is harmless; delete,
// forget, an action this guard does not know, or no action at all is refused (fail closed).
var harmless = []string{"no-op", "create", "read", "update"}

// covers reports whether retained address r covers resource address a: a itself, an instance of
// it (`r[`) or something under it (`r.`), never a sibling sharing a prefix.
func covers(r, a string) bool {
	return a == r || strings.HasPrefix(a, r+"[") || strings.HasPrefix(a, r+".")
}

// owner returns the instance of the first retained entry covering address a.
func owner(retained []Retained, a string) (string, bool) {
	for _, ret := range retained {
		if slices.ContainsFunc(ret.Addresses, func(r string) bool { return covers(r, a) }) {
			return ret.Instance, true
		}
	}
	return "", false
}

// Protect returns a *Refusal (CondRetained) naming every retained change the plan deletes, replaces
// or forgets (judged by the actions, whatever the action reason) or moves out of the retained set,
// or any deposed object it holds, or when plan is not a complete `tofu show -json` plan of JSON
// format version 1.x or is marked errored; nil otherwise. A change is retained when its address
// or, after a `moved` block, its previous address is covered.
// The refusal names each address verbatim (unquoted), the previous address, the actions as the
// plan spells them and the instance; never an attribute value of the plan.
func Protect(plan []byte, retained []Retained) error {
	// The guard reads addresses and actions only (never Change.After), so no attribute value of
	// the plan can reach a refusal.
	p, err := planjson.Decode(plan)
	if errors.Is(err, planjson.ErrErrored) {
		return refuse(CondRetained, "plan is marked errored: what it would change is unknown")
	}
	if err != nil {
		return refuse(CondRetained, "plan is not a complete `tofu show -json` plan")
	}
	var hits []string
	for _, rc := range p.Changes {
		acts := rc.Actions
		inst, covered := owner(retained, rc.Address)
		addr := rc.Address
		if rc.PreviousAddress != "" && rc.PreviousAddress != rc.Address {
			prevInst, prevCovered := owner(retained, rc.PreviousAddress)
			if !covered && prevCovered {
				// Moved out of the retained set: refused whatever the actions.
				inst, covered, acts = prevInst, true, append(slices.Clone(acts), "moved-out")
			}
			addr += " (moved from " + rc.PreviousAddress + ")"
		}
		if !covered {
			continue
		}
		if rc.Deposed != "" {
			// A deposed object of a retained address: refused whatever its actions (fail closed).
			hits = append(hits, fmt.Sprintf("%s deposed object %s: %s (instance %s)", addr, rc.Deposed, strings.Join(acts, "+"), inst))
			continue
		}
		if len(acts) > 0 && !slices.ContainsFunc(acts, func(a string) bool { return !slices.Contains(harmless, a) }) {
			continue
		}
		hits = append(hits, fmt.Sprintf("%s %s (instance %s)", addr, strings.Join(acts, "+"), inst))
	}
	if len(hits) > 0 {
		return refuse(CondRetained, "plan deletes, replaces, forgets or moves out retained resources: %s", strings.Join(hits, "; "))
	}
	return nil
}
