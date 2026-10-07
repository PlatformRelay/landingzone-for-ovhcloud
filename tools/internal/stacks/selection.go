// This file is the run order and the selected set of a run (FR-009, research R21, ADR-0007).
//
// Run order: topological levels over the data and authority edges between rows (a row's level is
// its longest producer chain), ties by stack path. That is what the pinned Terramate prints for
// `list --run-order` (evidence/T040.md reading 1). Selection compares the current code digest,
// consumed producer digests and resolved-reference digest of each row with the record of its
// last apply; the transitive data consumers of a selected stack are selected too, authority edges
// only order. A selected consumer of a producer that has published nothing is blocked.
package stacks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Reason kinds of a selected stack (research R21).
const (
	ReasonNoRecord = "no-record" // the stack has no record of a last apply
	ReasonCode     = "code"      // its code digest differs from the recorded one
	ReasonInput    = "input"     // a consumed producer digest differs from the recorded one (From: the producer)
	ReasonResolved = "resolved"  // its resolved-reference digest differs from the recorded one
	ReasonConsumer = "consumer"  // it consumes, over a data edge, a selected stack (From: that producer)
)

// Record is what a stack's last apply recorded (data-model *Live run record*, records/<id>.json).
// The live lane writes it (live.Record is this type); selection reads CodeDigest, Consumed and
// Resolved.
type Record struct {
	AppliedAt      string            `json:"applied_at"`
	SourceRevision string            `json:"source_revision"`
	CodeDigest     string            `json:"code_digest"`
	Consumed       map[string]string `json:"consumed"`           // producer id → sha256 of the outputs.json consumed
	Resolved       string            `json:"resolved,omitempty"` // sha256 of the resolved-reference input
}

// Reason is why a stack is selected.
type Reason struct {
	Kind string // one of the Reason constants
	From string // the producer, for ReasonInput and ReasonConsumer; empty otherwise
}

// String is the reason as `stacks:order` prints it: the kind, then `:<producer>` when it has one.
func (r Reason) String() string {
	if r.From == "" {
		return r.Kind
	}
	return r.Kind + ":" + r.From
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

// Order returns the ids of the manifest's rows in run order: topological levels over the data and
// authority edges between rows (level = longest producer chain), ties by stack path. A producer
// that is not a row (an external one) does not order; a cycle is an error.
func Order(m *Manifest) ([]string, error) {
	if m == nil {
		return nil, errors.New("order: no manifest")
	}
	rows := map[string]Instance{}
	for _, in := range m.Instances {
		rows[in.ID] = in
	}
	level := map[string]int{}
	visiting := map[string]bool{}
	var depth func(id string) (int, error)
	depth = func(id string) (int, error) {
		if l, ok := level[id]; ok {
			return l, nil
		}
		if visiting[id] {
			return 0, fmt.Errorf("order: dependency cycle through %s", id)
		}
		visiting[id] = true
		l := 0
		for _, e := range rows[id].Edges {
			if _, ok := rows[e.Producer]; !ok {
				continue
			}
			p, err := depth(e.Producer)
			if err != nil {
				return 0, err
			}
			l = max(l, p+1)
		}
		level[id] = l
		return l, nil
	}
	ids := make([]string, 0, len(m.Instances))
	for _, in := range m.Instances {
		if _, err := depth(in.ID); err != nil {
			return nil, err
		}
		ids = append(ids, in.ID)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		if level[a] != level[b] {
			return level[a] < level[b]
		}
		return rows[a].Path < rows[b].Path
	})
	return ids, nil
}

// Levels returns the run order grouped by level: the rows of one group have no edge between them
// and may run in any order once the groups before them have run (`stacks:order` prints it).
func Levels(m *Manifest) ([][]string, error) {
	ids, err := Order(m)
	if err != nil {
		return nil, err
	}
	rows := map[string]Instance{}
	for _, in := range m.Instances {
		rows[in.ID] = in
	}
	group := map[string]int{}
	var out [][]string
	for _, id := range ids {
		g := 0
		for _, e := range rows[id].Edges {
			if p, ok := group[e.Producer]; ok {
				g = max(g, p+1)
			}
		}
		group[id] = g
		if g == len(out) {
			out = append(out, nil)
		}
		out[g] = append(out[g], id)
	}
	return out, nil
}

// Select returns the selected set in run order. Every row needs a current code digest.
func Select(opts SelectOptions) ([]Selected, error) {
	m := opts.Manifest
	order, err := Order(m)
	if err != nil {
		return nil, err
	}
	rows := map[string]Instance{}
	for _, in := range m.Instances {
		rows[in.ID] = in
		if opts.Code[in.ID] == "" {
			return nil, fmt.Errorf("select: %s has no current code digest", in.ID)
		}
	}
	reasons := map[string][]Reason{}
	// Producers come before their consumers in run order, so a producer's reasons are final
	// when its consumers are visited.
	for _, id := range order {
		in := rows[id]
		rec, ok := opts.Records[id]
		var rs []Reason
		if !ok {
			rs = append(rs, Reason{Kind: ReasonNoRecord})
		} else {
			if opts.Code[id] != rec.CodeDigest {
				rs = append(rs, Reason{Kind: ReasonCode})
			}
			if opts.Resolved[id] != rec.Resolved {
				rs = append(rs, Reason{Kind: ReasonResolved})
			}
			for _, e := range in.Edges {
				if e.Kind != EdgeData {
					continue
				}
				cur, published := opts.Artifacts[e.Producer]
				if !published || cur != rec.Consumed[e.Producer] {
					rs = append(rs, Reason{Kind: ReasonInput, From: e.Producer})
				}
			}
		}
		for _, e := range in.Edges {
			if e.Kind == EdgeData && len(reasons[e.Producer]) > 0 {
				rs = append(rs, Reason{Kind: ReasonConsumer, From: e.Producer})
			}
		}
		reasons[id] = rs
	}
	var out []Selected
	for _, id := range order {
		if len(reasons[id]) == 0 {
			continue
		}
		s := Selected{ID: id, Reasons: reasons[id]}
		for _, e := range rows[id].Edges {
			if _, published := opts.Artifacts[e.Producer]; e.Kind == EdgeData && !published {
				s.Blocked = append(s.Blocked, e.Producer)
			}
		}
		slices.Sort(s.Blocked)
		out = append(out, s)
	}
	return out, nil
}

// ReadRecords reads every records/<id>.json in dir for the manifest's rows; a row without a file
// has no record. A missing dir means no stack has a record (a first run). A record that is not
// strict JSON of Record is an error, never a missing record.
func ReadRecords(dir string, m *Manifest) (map[string]Record, error) {
	out := map[string]Record{}
	for _, in := range m.Instances {
		data, err := os.ReadFile(filepath.Join(dir, in.ID+".json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var r Record
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("record %s: %w", in.ID, err)
		}
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("record %s: data after the record", in.ID)
		}
		out[in.ID] = r
	}
	return out, nil
}

// localModuleSources returns the local sources ("./…" or "../…") of the module blocks in one
// OpenTofu file. The file is parsed as HCL, so a one-line block counts and a comment does not; a
// file that does not parse, or a module whose source is not a literal string, is an error rather
// than a call the digest would miss.
func localModuleSources(path string, src []byte) ([]string, error) {
	file, diags := hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, diags
	}
	var out []string
	for _, b := range file.Body.(*hclsyntax.Body).Blocks {
		if b.Type != "module" {
			continue
		}
		attr, ok := b.Body.Attributes["source"]
		if !ok {
			return nil, fmt.Errorf("%s: module %q has no source", path, strings.Join(b.Labels, "."))
		}
		v, diags := attr.Expr.Value(nil)
		if diags.HasErrors() || !v.IsKnown() || v.IsNull() || v.Type() != cty.String {
			return nil, fmt.Errorf("%s: module %q: source is not a literal string", path, strings.Join(b.Labels, "."))
		}
		if s := v.AsString(); strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
			out = append(out, s)
		}
	}
	return out, nil
}

// CodeDigest returns the code digest of a generated stack under root: sha256 over the path and
// content of every file in its directory plus the stage, component and module closure it calls
// through local `source` arguments, recursively. OpenTofu working directories (.terraform) are
// left out. A symbolic-link file in the closure, or a module path that leaves root, is an error.
// A module directory reached through a linked ancestor directory (as generated fixture roots link
// stages/) is digested by the content found there, so a change behind the link still changes the
// digest.
func CodeDigest(root string, in Instance) (string, error) {
	start := filepath.Join(root, filepath.FromSlash(in.Path))
	seen := map[string]bool{}
	queue := []string{filepath.Clean(start)}
	files := map[string]string{}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		if seen[dir] {
			continue
		}
		seen[dir] = true
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("code digest of %s: module directory %s outside the root", in.ID, dir)
		}
		err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".terraform" {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("code digest of %s: %s is not a regular file", in.ID, path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			// A module is the configuration files of one directory; a subdirectory is reached only
			// as a module of its own (queued from a source).
			if filepath.Dir(path) == dir {
				switch {
				case strings.HasSuffix(path, ".tf.json") || strings.HasSuffix(path, ".tofu.json"):
					return fmt.Errorf("code digest of %s: %s: JSON configuration is not read for module sources", in.ID, path)
				case strings.HasSuffix(path, ".tf") || strings.HasSuffix(path, ".tofu"):
					sources, err := localModuleSources(path, data)
					if err != nil {
						return fmt.Errorf("code digest of %s: %w", in.ID, err)
					}
					for _, s := range sources {
						queue = append(queue, filepath.Clean(filepath.Join(dir, s)))
					}
				}
			}
			r, _ := filepath.Rel(root, path)
			sum := sha256.Sum256(data)
			files[filepath.ToSlash(r)] = hex.EncodeToString(sum[:])
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%s\n", n, files[n])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
