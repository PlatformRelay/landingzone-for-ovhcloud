package live

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// P24 (005 T007): tests/fixtures/tofu-probes/captures/p24-apply-json.jsonl is `tofu apply -json`
// of three terraform_data resources (`second` depends on `first`, `third` is independent),
// captured by tests/fixtures/tofu-probes/capture.sh through the offline entry's capture admission.

var capturesDir = filepath.Join("..", "..", "..", "tests", "fixtures", "tofu-probes", "captures")

// TestInventoryCapturedP24Fixture: the stream and its committed root are unedited since the
// capture, and the sidecar records the entry's qualification line. The sidecar is found by the
// stream's file name, and its own `file` and `case` fields must name that stream. This shows the
// sidecar is consistent, not that the entry wrote it: capture.sh writes it on the host.
func TestInventoryCapturedP24Fixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(capturesDir, "p24-apply-json.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	metaRaw, err := os.ReadFile(filepath.Join(capturesDir, "p24-apply-json.jsonl.meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Case        string `json:"case"`
		File        string `json:"file"`
		Command     string `json:"command"`
		Tool        string `json:"tool"`
		ToolVersion string `json:"tool_version"`
		SHA         string `json:"sha256"`
		EntryExit   *int   `json:"entry_exit"`
		Toolchain   string `json:"toolchain"`
		Input       string `json:"input"`
		InputSHA    string `json:"input_sha256"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.EntryExit == nil {
		t.Fatal("sidecar has no entry_exit")
	}
	if meta.Case != "p24-apply-json" || meta.File != "p24-apply-json.jsonl" {
		t.Errorf("sidecar names case %q, file %q; want p24-apply-json, p24-apply-json.jsonl", meta.Case, meta.File)
	}
	if !strings.HasPrefix(meta.Command, "lz-offline ") || !strings.HasSuffix(meta.Command, " task capture:p24-apply-json") || *meta.EntryExit != 0 {
		t.Errorf("command %q exit %d, want the capture target through lz-offline, exit 0", meta.Command, *meta.EntryExit)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != meta.SHA {
		t.Error("p24-apply-json.jsonl differs from its sidecar digest: edited after capture")
	}
	first := strings.SplitN(string(raw), "\n", 2)[0]
	var v struct {
		Type string `json:"type"`
		Tofu string `json:"tofu"`
	}
	if meta.Tool != "tofu" || meta.ToolVersion != pinnedTofu || json.Unmarshal([]byte(first), &v) != nil || v.Type != "version" || v.Tofu != pinnedTofu {
		t.Errorf("tool %q %q, stream version %q, want tofu %s", meta.Tool, meta.ToolVersion, v.Tofu, pinnedTofu)
	}
	if !strings.HasPrefix(meta.Toolchain, "TOOLCHAIN_QUALIFIED ") || !strings.Contains(meta.Toolchain, " tofu="+pinnedTofu+" ") || !strings.HasSuffix(meta.Toolchain, " network=none") {
		t.Errorf("toolchain %q, want the entry's qualification line with tofu %s and network=none", meta.Toolchain, pinnedTofu)
	}
	if meta.Input != "tests/fixtures/tofu-probes/p24-apply-json" || treeDigest(t, filepath.Join("..", "..", "..", meta.Input)) != meta.InputSHA {
		t.Errorf("input %q: the committed root differs from the one captured", meta.Input)
	}
}

// treeDigest is capture.sh's digest of a root: sha256 over `sha256sum` lines of its files, in
// byte order of their `./`-relative paths.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		paths = append(paths, "./"+filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	var lines strings.Builder
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		s := sha256.Sum256(b)
		lines.WriteString(hex.EncodeToString(s[:]) + "  " + p + "\n")
	}
	s := sha256.Sum256([]byte(lines.String()))
	return hex.EncodeToString(s[:])
}

// TestInventoryCapturedP24: the run core's ConsumeApply records exactly one inventory entry per
// resource of the captured stream, with its address, type and id; and the stream itself shows the
// premise: one apply_complete per resource carrying address and id_value, and a dependant starts
// only after its dependency's apply_complete, so the id is recorded before the next resource
// that could need it is created.
func TestInventoryCapturedP24(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(capturesDir, "p24-apply-json.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	addrs := []string{"terraform_data.first", "terraform_data.second", "terraform_data.third"}
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	completeAt, startAt, ids := map[string]int{}, map[string]int{}, map[string]string{}
	for i, l := range lines {
		e := parseEvent(t, l)
		a := e.Hook.Resource.Addr
		switch e.Type {
		case "apply_start":
			startAt[a] = i
		case "apply_complete":
			if _, dup := completeAt[a]; dup {
				t.Errorf("%s: second apply_complete", a)
			}
			completeAt[a] = i
			ids[a] = e.Hook.IDValue
			if e.Hook.Resource.ResourceType != "terraform_data" || !uuid.MatchString(e.Hook.IDValue) {
				t.Errorf("%s: apply_complete type %q id %q, want terraform_data and an id", a, e.Hook.Resource.ResourceType, e.Hook.IDValue)
			}
		}
	}
	for _, a := range addrs {
		if _, ok := completeAt[a]; !ok || len(completeAt) != len(addrs) {
			t.Fatalf("apply_complete for %v, want exactly one for each of %v", completeAt, addrs)
		}
	}
	f, okF := completeAt["terraform_data.first"]
	s, okS := startAt["terraform_data.second"]
	if !okF || !okS || f > s {
		t.Errorf("second starts at event %d, first completes at %d: the dependency's id is not out first", s, f)
	}

	dir := tempPrivate(t)
	inv, err := OpenInventory(dir, NewRedactor())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := ConsumeApply(strings.NewReader(string(raw)), "p24", inv, &out); err != nil {
		t.Fatal(err)
	}
	got := readInventory(t, dir)
	if len(got) != len(addrs) {
		t.Fatalf("inventory holds %d entries %v, want %d", len(got), got, len(addrs))
	}
	seen := map[string]bool{}
	for _, g := range got {
		want, ok := ids[g.Address]
		if !ok || seen[g.Address] || g.Stack != "p24" || g.Type != "terraform_data" || g.ID != want {
			t.Errorf("inventory entry %+v: want one entry per captured resource with stack p24, type terraform_data, id %q", g, want)
		}
		seen[g.Address] = true
	}
}
