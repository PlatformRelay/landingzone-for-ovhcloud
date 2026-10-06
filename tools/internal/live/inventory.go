package live

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Run inventory (FR-011, research R12, premise P24): one JSON line per created resource in
// <run dir>/inventory.jsonl, appended as each `apply_complete` event of `tofu apply -json`
// arrives, so a crash before the state write still leaves the id recorded. When P24 is refuted the
// fallback records what `tofu state list` prints after each stack.

// InventoryEntry is one line of inventory.jsonl.
type InventoryEntry struct {
	Stack   string `json:"stack"`
	Address string `json:"address"`
	Type    string `json:"type"`         // resource type, from the address
	ID      string `json:"id,omitempty"` // id_value of apply_complete; empty from the fallback
}

// Inventory appends to <run dir>/inventory.jsonl through the run's Redactor.
type Inventory struct {
	path string
	r    *Redactor
	seen map[string]bool // stack + address already recorded
}

// OpenInventory opens (creating) <runDir>/inventory.jsonl for appending; what a previous process
// recorded there is kept.
func OpenInventory(runDir string, r *Redactor) (*Inventory, error) {
	if err := ensureDir(runDir); err != nil {
		return nil, err
	}
	inv := &Inventory{path: filepath.Join(runDir, "inventory.jsonl"), r: r, seen: map[string]bool{}}
	old, err := ReadInventory(runDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range old {
		inv.seen[e.Stack+"\x00"+e.Address] = true
	}
	return inv, nil
}

// Append writes e as one line (synced) before it returns.
func (i *Inventory) Append(e InventoryEntry) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := appendLine(i.path, []byte(i.r.Redact(string(raw)))); err != nil {
		return err
	}
	i.seen[e.Stack+"\x00"+e.Address] = true
	return nil
}

// typeOf returns the resource type of a managed resource address ("" for a data source): the
// first segment after any `module.<name>[key]` pairs, with instance keys ignored.
func typeOf(addr string) string {
	var parts []string
	depth, cur := 0, strings.Builder{}
	for _, c := range addr {
		switch {
		case c == '[':
			depth++
		case c == ']':
			depth--
		case depth > 0:
		case c == '.':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(c)
		}
	}
	parts = append(parts, cur.String())
	for len(parts) >= 2 && parts[0] == "module" {
		parts = parts[2:]
	}
	if len(parts) < 2 || parts[0] == "data" {
		return ""
	}
	return parts[0]
}

// AppendStateList records each managed resource that `tofu state list` printed for stack and the
// inventory does not hold yet (P24 fallback); data sources are not resources.
func (i *Inventory) AppendStateList(stack string, r io.Reader) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		addr := strings.TrimSpace(sc.Text())
		typ := typeOf(addr)
		if addr == "" || typ == "" || i.seen[stack+"\x00"+addr] {
			continue
		}
		if err := i.Append(InventoryEntry{Stack: stack, Address: addr, Type: typ}); err != nil {
			return err
		}
	}
	return sc.Err()
}

// Close closes the inventory (each Append opens and closes the file, so nothing is held).
func (i *Inventory) Close() error { return nil }

// ReadInventory reads <runDir>/inventory.jsonl.
func ReadInventory(runDir string) ([]InventoryEntry, error) {
	raw, err := os.ReadFile(filepath.Join(runDir, "inventory.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []InventoryEntry
	for n, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		var e InventoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("inventory.jsonl line %d: %w", n+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// applyLine is the part of a `tofu apply -json` line the run core reads.
type applyLine struct {
	Type    string `json:"type"`
	Message string `json:"@message"`
	Hook    struct {
		Resource struct {
			Addr         string `json:"addr"`
			ResourceType string `json:"resource_type"`
		} `json:"resource"`
		IDValue string `json:"id_value"`
	} `json:"hook"`
}

// ConsumeApply reads a `tofu apply -json` stream of stack, appends every apply_complete to inv
// before it reads the next event, and writes each event's message (a line that is not JSON as it
// is) to out. After a failed append it keeps reading, so the child never blocks on a full pipe,
// and returns the first error at the end.
func ConsumeApply(r io.Reader, stack string, inv *Inventory, out io.Writer) error {
	br := bufio.NewReader(r)
	var first error
	for {
		line, err := br.ReadString('\n')
		if t := strings.TrimSpace(line); t != "" {
			var e applyLine
			if json.Unmarshal([]byte(t), &e) != nil {
				fmt.Fprintln(out, t)
			} else {
				if e.Type == "apply_complete" {
					typ := e.Hook.Resource.ResourceType
					if typ == "" {
						typ = typeOf(e.Hook.Resource.Addr)
					}
					if aerr := inv.Append(InventoryEntry{Stack: stack, Address: e.Hook.Resource.Addr, Type: typ, ID: e.Hook.IDValue}); first == nil {
						first = aerr
					}
				}
				fmt.Fprintln(out, e.Message)
			}
		}
		if err == io.EOF {
			return first
		}
		if err != nil {
			return errors.Join(first, err)
		}
	}
}
