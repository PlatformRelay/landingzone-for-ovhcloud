// Package planjson is the one decoder of an OpenTofu plan document (T088): the JSON of
// `tofu show -json <planfile>`, or the `test_plan` of `tofu test -json -verbose`, which has the
// same shape. It checks that the document is a complete plan and returns its resource changes,
// typed; it judges none of them. Its callers are live.Protect (the retained-resource guard G7) and
// stacks.PlannedBucketNames (the bucket name check); no other package reads `resource_changes`
// (live's TestProtectOnlyPlanReader).
package planjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

var (
	// ErrNotAPlan: the document is not JSON, has the wrong shape, is not of JSON format version
	// 1.x, or has no planned_values (the state, `tofu show -json` without a plan file, has none).
	ErrNotAPlan = errors.New("not a complete `tofu show -json` plan")
	// ErrErrored: OpenTofu marked the plan errored, so what it would change is unknown.
	ErrErrored = errors.New("plan is marked errored")
)

// Plan is a decoded plan: its resource changes in document order.
type Plan struct {
	Changes []Change
}

// Change is one entry of `resource_changes`. Actions are as the plan spells them (nil when the
// entry has none); After is the planned object, raw (`null` for a delete or forget, empty when
// the entry has no `after`), so a caller that needs no attribute value never decodes one. The
// resource mode and type are read through Resource, so a caller that does not read them is not
// refused over them (live.Protect reads addresses and actions only).
type Change struct {
	Address         string
	PreviousAddress string
	Deposed         string
	Actions         []string
	After           json.RawMessage
	mode, typ       string
	resourceErr     error
}

// Resource returns the change's resource mode (`managed`, `data`) and type, empty when absent or
// null, read as typed fields: the error is the document's when any change's `mode` or `type` is
// not a string or its `after` is neither an object nor null (the same for every change, so a
// caller that reads resources is refused over any of them, as a typed decode of the whole plan
// would be).
func (c Change) Resource() (mode, typ string, err error) {
	if c.resourceErr != nil {
		return "", "", c.resourceErr
	}
	return c.mode, c.typ, nil
}

// resources is the typed reading behind Resource, decoded separately so its errors never fail
// Decode itself.
type resources struct {
	ResourceChanges []struct {
		Mode   string `json:"mode"`
		Type   string `json:"type"`
		Change struct {
			After map[string]any `json:"after"`
		} `json:"change"`
	} `json:"resource_changes"`
}

type document struct {
	FormatVersion   string          `json:"format_version"`
	Errored         bool            `json:"errored"`
	PlannedValues   json.RawMessage `json:"planned_values"`
	ResourceChanges []struct {
		Address         string `json:"address"`
		PreviousAddress string `json:"previous_address"`
		Deposed         string `json:"deposed"`
		Change          struct {
			Actions []string        `json:"actions"`
			After   json.RawMessage `json:"after"`
		} `json:"change"`
	} `json:"resource_changes"`
}

// Decode returns the changes of a plan document, or ErrNotAPlan / ErrErrored (checked in that
// order, before any change is returned).
func Decode(raw []byte) (Plan, error) {
	var d document
	if err := json.Unmarshal(raw, &d); err != nil || !strings.HasPrefix(d.FormatVersion, "1.") ||
		len(d.PlannedValues) == 0 || string(d.PlannedValues) == "null" {
		return Plan{}, ErrNotAPlan
	}
	if d.Errored {
		return Plan{}, ErrErrored
	}
	var r resources
	resourceErr := json.Unmarshal(raw, &r) // the same array as d's, so the same length when nil
	if resourceErr != nil {
		resourceErr = fmt.Errorf("resource mode, type or planned object: %w", resourceErr)
	}
	p := Plan{Changes: make([]Change, 0, len(d.ResourceChanges))}
	for i, rc := range d.ResourceChanges {
		c := Change{
			Address: rc.Address, PreviousAddress: rc.PreviousAddress, Deposed: rc.Deposed,
			Actions: rc.Change.Actions, After: rc.Change.After, resourceErr: resourceErr,
		}
		if resourceErr == nil {
			c.mode, c.typ = r.ResourceChanges[i].Mode, r.ResourceChanges[i].Type
		}
		p.Changes = append(p.Changes, c)
	}
	return p, nil
}

// known is the action vocabulary of a plan's changes as the pinned OpenTofu 1.13.0 was observed
// to plan it (T063 captures: no-op, create, read, update, delete, forget; a replacement is
// delete+create in either order).
var known = []string{"no-op", "create", "read", "update", "delete", "forget"}

// KnownActions reports whether actions has at least one action and every one is known. A caller
// judging a change refuses it otherwise (fail closed).
func KnownActions(actions []string) bool {
	return len(actions) > 0 && !slices.ContainsFunc(actions, func(a string) bool { return !slices.Contains(known, a) })
}
