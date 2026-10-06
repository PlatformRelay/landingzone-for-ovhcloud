package live

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run inventory (FR-011, research R12, premise P24). The streams in testdata/tofu/ were produced
// by the pinned OpenTofu 1.13.0 (runtime r3-e2) on provider-free terraform_data roots; the
// generating command is in evidence/T054.md. T007 still owes the formal P24 capture.

// applyEvent is the part of a `tofu apply -json` line the tests read.
type applyEvent struct {
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

func streamLines(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tofu", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func parseEvent(t *testing.T, line string) applyEvent {
	t.Helper()
	var e applyEvent
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("captured line is not JSON: %v", err)
	}
	return e
}

// completed returns the inventory entries a stream must yield: one per apply_complete, in order.
func completed(t *testing.T, stack string, lines []string) []InventoryEntry {
	t.Helper()
	var want []InventoryEntry
	for _, l := range lines {
		if e := parseEvent(t, l); e.Type == "apply_complete" {
			want = append(want, InventoryEntry{Stack: stack, Address: e.Hook.Resource.Addr, Type: e.Hook.Resource.ResourceType, ID: e.Hook.IDValue})
		}
	}
	return want
}

func readInventory(t *testing.T, dir string) []InventoryEntry {
	t.Helper()
	got, err := ReadInventory(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return got
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(timeout time.Duration, cond func() bool) bool {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestInventoryAppendsBeforeNextEvent feeds the captured stream through a pipe one event at a
// time and does not send the next event until the entry of an apply_complete is on disk: an
// implementation that buffers the stream, or writes the inventory at the end, never gets it.
func TestInventoryAppendsBeforeNextEvent(t *testing.T) {
	dir := tempPrivate(t)
	lines := streamLines(t, "apply-ok.jsonl")
	want := completed(t, "probe-a", lines)
	if len(want) != 3 {
		t.Fatalf("fixture: %d apply_complete events, want 3", len(want))
	}
	inv, err := OpenInventory(dir, NewRedactor(seedSecret))
	if err != nil {
		t.Fatal(err)
	}
	defer inv.Close()
	pr, pw := io.Pipe()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- ConsumeApply(pr, "probe-a", inv, &out) }()

	// send writes one event, failing (not hanging) when nobody reads it.
	send := func(i int, l string) {
		sent := make(chan error, 1)
		go func() {
			_, err := io.WriteString(pw, l+"\n")
			sent <- err
		}()
		select {
		case err := <-sent:
			if err != nil {
				t.Fatalf("event %d not read: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			pw.CloseWithError(io.ErrClosedPipe)
			t.Fatalf("event %d not read within 5 s", i)
		}
	}
	n := 0
	for i, l := range lines {
		send(i, l)
		if parseEvent(t, l).Type != "apply_complete" {
			continue
		}
		n++
		if !waitFor(5*time.Second, func() bool { return len(readInventory(t, dir)) >= n }) {
			pw.CloseWithError(io.ErrClosedPipe)
			t.Fatalf("after event %d (apply_complete %d) inventory.jsonl holds %d entries, want %d before the next event", i, n, len(readInventory(t, dir)), n)
		}
	}
	pw.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ConsumeApply did not return at the end of the stream")
	}
	got := readInventory(t, dir)
	if len(got) != len(want) {
		t.Fatalf("inventory = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if !strings.Contains(out.String(), "Creation complete") {
		t.Errorf("event messages did not reach the output: %q", out.String())
	}
}

// TestInventoryFailedApply: the captured failing apply records the resource that completed, not
// the one whose apply errored.
func TestInventoryFailedApply(t *testing.T) {
	dir := tempPrivate(t)
	lines := streamLines(t, "apply-fail.jsonl")
	inv, err := OpenInventory(dir, NewRedactor())
	if err != nil {
		t.Fatal(err)
	}
	_ = ConsumeApply(strings.NewReader(strings.Join(lines, "\n")+"\n"), "probe-b", inv, io.Discard)
	inv.Close()
	got := readInventory(t, dir)
	want := completed(t, "probe-b", lines)
	if len(want) != 1 || len(got) != 1 || got[0] != want[0] {
		t.Errorf("inventory = %+v, want %+v", got, want)
	}
}

// TestInventoryStateListFallback is the P24 fallback: after a stack, what `tofu state list`
// prints and the inventory does not hold yet is appended (without an id), data sources excepted.
func TestInventoryStateListFallback(t *testing.T) {
	dir := tempPrivate(t)
	inv, err := OpenInventory(dir, NewRedactor())
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.Append(InventoryEntry{Stack: "a", Address: "terraform_data.first", Type: "terraform_data", ID: "id-1"}); err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile(filepath.Join("testdata", "tofu", "state-list-ok.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// The captured list plus two addresses in the same format (believed, not captured): a data
	// source and an indexed resource in a keyed module.
	list := string(captured) + "data.terraform_remote_state.network\nmodule.net[\"gra11\"].ovh_cloud_project_network_private.this[0]\n"
	if err := inv.AppendStateList("a", strings.NewReader(list)); err != nil {
		t.Fatal(err)
	}
	inv.Close()
	got := readInventory(t, dir)
	want := []InventoryEntry{
		{Stack: "a", Address: "terraform_data.first", Type: "terraform_data", ID: "id-1"},
		{Stack: "a", Address: "terraform_data.second", Type: "terraform_data"},
		{Stack: "a", Address: "module.inner.terraform_data.third", Type: "terraform_data"},
		{Stack: "a", Address: "module.net[\"gra11\"].ovh_cloud_project_network_private.this[0]", Type: "ovh_cloud_project_network_private"},
	}
	if len(got) != len(want) {
		t.Fatalf("inventory = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestInventoryReopenAppends: a cleanup run reopens the run's inventory; nothing recorded is lost.
func TestInventoryReopenAppends(t *testing.T) {
	dir := tempPrivate(t)
	for i, addr := range []string{"terraform_data.a", "terraform_data.b"} {
		inv, err := OpenInventory(dir, NewRedactor())
		if err != nil {
			t.Fatal(err)
		}
		if err := inv.Append(InventoryEntry{Stack: "s", Address: addr, Type: "terraform_data", ID: "id"}); err != nil {
			t.Fatal(err)
		}
		inv.Close()
		if got := readInventory(t, dir); len(got) != i+1 {
			t.Fatalf("after open %d: %d entries, want %d", i+1, len(got), i+1)
		}
	}
}

// TestInventoryRedacts: an id or address holding a seeded secret is written redacted (G2).
func TestInventoryRedacts(t *testing.T) {
	dir := tempPrivate(t)
	inv, err := OpenInventory(dir, NewRedactor(seedSecret))
	if err != nil {
		t.Fatal(err)
	}
	line := streamLines(t, "apply-ok.jsonl")[0]
	for _, l := range streamLines(t, "apply-ok.jsonl") {
		if parseEvent(t, l).Type == "apply_complete" {
			line = l
			break
		}
	}
	e := parseEvent(t, line)
	leaky := strings.Replace(line, e.Hook.IDValue, seedSecret, -1)
	if err := ConsumeApply(strings.NewReader(leaky+"\n"), "s", inv, io.Discard); err != nil {
		t.Fatal(err)
	}
	inv.Close()
	raw, err := os.ReadFile(filepath.Join(dir, "inventory.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), seedSecret) {
		t.Errorf("inventory.jsonl holds the seeded secret")
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	if !sc.Scan() || !strings.Contains(sc.Text(), e.Hook.Resource.Addr) {
		t.Errorf("inventory.jsonl lost the entry: %q", raw)
	}
}
