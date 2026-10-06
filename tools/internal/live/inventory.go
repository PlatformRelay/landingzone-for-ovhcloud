package live

import "io"

// Run inventory (FR-011, research R12, premise P24): one JSON line per created resource in
// <run dir>/inventory.jsonl, appended as each `apply_complete` event of `tofu apply -json`
// arrives, so a crash before the state write still leaves the id recorded. When P24 is refuted the
// fallback records what `tofu state list` prints after each stack.
//
// STUB (T054): the tests pin the behaviour; T055 implements it.

// InventoryEntry is one line of inventory.jsonl.
type InventoryEntry struct {
	Stack   string `json:"stack"`
	Address string `json:"address"`
	Type    string `json:"type"`         // resource type, from the address
	ID      string `json:"id,omitempty"` // id_value of apply_complete; empty from the fallback
}

// Inventory appends to <run dir>/inventory.jsonl through the run's Redactor.
type Inventory struct{}

// OpenInventory opens (creating) <runDir>/inventory.jsonl for appending.
func OpenInventory(runDir string, r *Redactor) (*Inventory, error) { return &Inventory{}, nil }

// Append writes e as one line before it returns.
func (i *Inventory) Append(e InventoryEntry) error { return nil }

// AppendStateList records each managed resource that `tofu state list` printed for stack and the
// inventory does not hold yet (P24 fallback); data sources are not resources.
func (i *Inventory) AppendStateList(stack string, r io.Reader) error { return nil }

// Close closes the file.
func (i *Inventory) Close() error { return nil }

// ReadInventory reads <runDir>/inventory.jsonl.
func ReadInventory(runDir string) ([]InventoryEntry, error) { return nil, nil }

// ConsumeApply reads a `tofu apply -json` stream of stack, appends every apply_complete to inv
// before it reads the next event, and writes each event's message to out.
func ConsumeApply(r io.Reader, stack string, inv *Inventory, out io.Writer) error { return nil }
