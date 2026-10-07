// This file is the run order and the selected set of a run (FR-009, research R21, ADR-0007).
//
// T040 stub: the signatures selection_test.go pins, with permissive bodies (row order, every row
// selected, no digest). T041 implements them.
package stacks

// Reason kinds of a selected stack (research R21).
const (
	ReasonNoRecord = "no-record" // the stack has no record of a last apply
	ReasonCode     = "code"      // its code digest differs from the recorded one
	ReasonInput    = "input"     // a consumed producer digest differs from the recorded one (From: the producer)
	ReasonResolved = "resolved"  // its resolved-reference digest differs from the recorded one
	ReasonConsumer = "consumer"  // it consumes, over a data edge, a selected stack (From: that producer)
)

// Record is what a stack's last apply recorded (data-model *Live run record*, records/<id>.json),
// with the field names of the live lane's record.
type Record struct {
	CodeDigest string            `json:"code_digest"`
	Consumed   map[string]string `json:"consumed"`           // producer id → sha256 of the outputs.json consumed
	Resolved   string            `json:"resolved,omitempty"` // sha256 of the resolved-reference input
}

// Reason is why a stack is selected.
type Reason struct {
	Kind string // one of the Reason constants
	From string // the producer, for ReasonInput and ReasonConsumer; empty otherwise
}

// Selected is one stack of the selected set.
type Selected struct {
	ID      string
	Reasons []Reason
	Blocked []string // data producers that have published no outputs.json, sorted; non-empty = blocked
}

// SelectOptions is the state a selection compares: the current digests against the records.
type SelectOptions struct {
	Manifest  *Manifest
	Code      map[string]string // instance id → current code digest (CodeDigest); every row needs one
	Resolved  map[string]string // instance id → current resolved-reference digest (Adapt's Inputs.Resolved); absent for a stage that takes none
	Artifacts map[string]string // producer id → sha256 of its published outputs.json; absent: nothing published
	Records   map[string]Record // instance id → record of its last apply; absent: no record
}

// Order returns the ids of the manifest's rows in run order.
func Order(m *Manifest) ([]string, error) {
	ids := make([]string, 0, len(m.Instances))
	for _, in := range m.Instances {
		ids = append(ids, in.ID)
	}
	return ids, nil
}

// Select returns the selected set in run order.
func Select(opts SelectOptions) ([]Selected, error) {
	var out []Selected
	for _, in := range opts.Manifest.Instances {
		out = append(out, Selected{ID: in.ID})
	}
	return out, nil
}

// CodeDigest returns the code digest of a generated stack under root: its directory plus the
// stage, component and module closure it calls.
func CodeDigest(root string, in Instance) (string, error) {
	return "", nil
}
