// This file is the reconciler (FR-007, ADR-0007): it makes the stacks under stacks/ correspond to
// the rows of stacks/deployments.yaml by creating a missing stack with the pinned
// `terramate create`, and refuses every other mismatch with UNSUPPORTED_CHANGE until the rename
// and retirement workflows exist (research R3). It also holds the freshness comparison of
// `stacks:check` (research R16) and the resolution of the pinned Terramate binary.
package stacks

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// CodeUnsupportedChange refuses any mismatch between the manifest and the stack tree other than a
// missing stack: a directory without a row, a changed id, a changed dimension, or stack metadata
// (tags, after) that differs from the derived one. Drifted metadata stays refused until the first
// change to the derived tags or after filters brings a metadata-rewrite step (005 T036 decision 2).
const CodeUnsupportedChange = "UNSUPPORTED_CHANGE"

// ManifestPath is the manifest's path relative to the project root.
const ManifestPath = "stacks/deployments.yaml"

// ErrNoManifest is returned when the project root has no stacks/deployments.yaml.
var ErrNoManifest = errors.New("no manifest")

// ReconcileOptions configures one reconcile run.
type ReconcileOptions struct {
	Root      string // Terramate project root holding stacks/deployments.yaml
	Terramate string // the pinned terramate binary
	Check     bool   // report what would be created without writing (stacks:check)
}

// ReconcileReport is what a run did, or under Check would do.
type ReconcileReport struct {
	Created []string // stack paths (stacks/…), in manifest row order
}

// ReconcileError is a mismatch the reconciler refuses; a decoder refusal is returned as the
// decoder's *ManifestError instead.
type ReconcileError struct {
	Code   string
	Path   string // the stack path concerned
	Detail string
}

func (e *ReconcileError) Error() string { return e.Code + ": " + e.Path + ": " + e.Detail }

func unsupported(path, format string, a ...any) error {
	return &ReconcileError{Code: CodeUnsupportedChange, Path: path, Detail: fmt.Sprintf(format, a...)}
}

// existingStack is the metadata of a stack found under stacks/.
type existingStack struct {
	id          string
	tags, after []string
}

// Reconcile decodes Root/stacks/deployments.yaml and reconciles the stack tree under Root with it.
// Every mismatch is found before anything is written: a refused run leaves the tree unchanged. A
// `terramate create` that fails after that (a tool error, not a mismatch) can leave the stacks
// created before it; the report lists them, and a repeat run continues from there.
func Reconcile(opts ReconcileOptions) (ReconcileReport, error) {
	data, err := os.ReadFile(filepath.Join(opts.Root, ManifestPath))
	if errors.Is(err, fs.ErrNotExist) {
		return ReconcileReport{}, ErrNoManifest
	}
	if err != nil {
		return ReconcileReport{}, err
	}
	m, err := DecodeManifest(data)
	if err != nil {
		return ReconcileReport{}, err
	}
	existing, order, err := discoverStacks(opts.Root)
	if err != nil {
		return ReconcileReport{}, err
	}
	// order is sorted, so the first path of a duplicated id is the lexically first one.
	pathOfID := map[string]string{}
	for _, p := range order {
		id := existing[p].id
		if first, dup := pathOfID[id]; dup {
			return ReconcileReport{}, unsupported(p, "stack id %q is also the id of %s", id, first)
		}
		pathOfID[id] = p
	}
	rows, rowPaths := map[string]Instance{}, map[string]bool{}
	for _, in := range m.Instances {
		rows[in.ID], rowPaths[in.Path] = in, true
	}
	// Rows in manifest order, then stacks without a row in sorted order: the first refusal is
	// the same on every run.
	var create []Instance
	for _, in := range m.Instances {
		got, ok := existing[in.Path]
		if !ok {
			if p, moved := pathOfID[in.ID]; moved {
				return ReconcileReport{}, unsupported(p, "stack %s is at %s, the manifest places it at %s (changed dimension)", in.ID, p, in.Path)
			}
			if err := obstructed(opts.Root, in.Path); err != nil {
				return ReconcileReport{}, err
			}
			create = append(create, in)
			continue
		}
		after := afterFilters(in, rows)
		switch {
		case got.id != in.ID:
			return ReconcileReport{}, unsupported(in.Path, "stack id %q, the row says %q (ids are immutable)", got.id, in.ID)
		case !sameSet(got.tags, in.Tags):
			return ReconcileReport{}, unsupported(in.Path, "tags %q, derived %q", got.tags, in.Tags)
		case !sameSet(got.after, after):
			return ReconcileReport{}, unsupported(in.Path, "after %q, derived %q", got.after, after)
		}
	}
	for _, p := range order {
		if !rowPaths[p] {
			return ReconcileReport{}, unsupported(p, "stack %s has no row in the manifest", existing[p].id)
		}
	}
	var report ReconcileReport
	for _, in := range create {
		if !opts.Check {
			args := []string{"-C", opts.Root, "create", "--id", in.ID, "--name", in.ID,
				"--tags", strings.Join(in.Tags, ","), "--no-generate"}
			for _, a := range afterFilters(in, rows) {
				args = append(args, "--after", a)
			}
			if out, err := runTerramate(opts.Terramate, true, append(args, in.Path)...); err != nil {
				return report, fmt.Errorf("terramate create %s: %v: %s", in.Path, err, out)
			}
		}
		report.Created = append(report.Created, in.Path)
	}
	return report, nil
}

// obstructed refuses a stack path to be created when it, or a directory on the way to it, is
// something other than a real directory (a file, a symlink), so a later `terramate create` cannot
// fail on it after earlier stacks were written, nor write through a link.
func obstructed(root, path string) error {
	dir := root
	for _, part := range strings.Split(path, "/") {
		dir = filepath.Join(dir, part)
		info, err := os.Lstat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			kind := "a file"
			if info.Mode()&fs.ModeSymlink != 0 {
				kind = "a symlink"
			}
			rel, _ := filepath.Rel(root, dir)
			return unsupported(path, "%s is in the way: %s, not a directory", filepath.ToSlash(rel), kind)
		}
	}
	return nil
}

// afterFilters derives one tag filter per edge whose producer is a row of the manifest, in edge
// order: tag:lz-stage-<stage> plus the producer's tenant, env, region and slot tags where set. An
// external producer (tenant-only manifest) gives none: it is not a stack of this repository.
func afterFilters(in Instance, rows map[string]Instance) []string {
	var out []string
	for _, e := range in.Edges {
		p, ok := rows[e.Producer]
		if !ok {
			continue
		}
		f := "tag:lz-stage-" + p.Stage
		for _, tag := range [][2]string{{"lz-tenant-", p.Tenant}, {"lz-env-", p.Environment},
			{"lz-region-", strings.ToLower(p.Region)}, {"lz-slot-", p.Slot}} {
			if tag[1] != "" {
				f += ":" + tag[0] + tag[1]
			}
		}
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// sameSet compares two lists as sets; a list with duplicates never equals a derived one.
func sameSet(got, want []string) bool {
	x, y := slices.Clone(got), slices.Clone(want)
	slices.Sort(x)
	slices.Sort(y)
	return len(slices.Compact(slices.Clone(x))) == len(x) && slices.Equal(x, y)
}

// discoverStacks finds the directories under root/stacks whose Terramate config holds a stack
// block, keyed by slash-separated path relative to root; the second result is those paths sorted.
func discoverStacks(root string) (map[string]existingStack, []string, error) {
	found := map[string]existingStack{}
	var order []string
	err := filepath.WalkDir(filepath.Join(root, "stacks"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == filepath.Join(root, "stacks") {
				return fs.SkipAll
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if tofuWorkingFile(d.Name(), true) {
			return fs.SkipDir // as stacks:check: a module cache may hold another repository's stacks
		}
		s, ok, err := readStackBlock(path)
		if err != nil || !ok {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		found[rel] = s
		order = append(order, rel)
		return nil
	})
	slices.Sort(order)
	return found, order, err
}

// readStackBlock parses the Terramate files of one directory (*.tm, *.tm.hcl, sorted) and returns
// its stack block's id, tags and after.
func readStackBlock(dir string) (existingStack, bool, error) {
	entries, err := os.ReadDir(dir) // sorted by name
	if err != nil {
		return existingStack{}, false, err
	}
	var s existingStack
	var where string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".tm.hcl") || strings.HasSuffix(name, ".tm")) {
			continue
		}
		f := filepath.Join(dir, name)
		src, err := os.ReadFile(f)
		if err != nil {
			return existingStack{}, false, err
		}
		file, diags := hclsyntax.ParseConfig(src, f, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			return existingStack{}, false, diags
		}
		for _, b := range file.Body.(*hclsyntax.Body).Blocks {
			if b.Type != "stack" {
				continue
			}
			if where != "" {
				return existingStack{}, false, fmt.Errorf("%s: a second stack block (first in %s)", f, where)
			}
			where = f
			for _, attr := range sortedAttributes(b.Body.Attributes) {
				if attr.Name != "id" && attr.Name != "tags" && attr.Name != "after" {
					continue // not read: an expression elsewhere in the block is Terramate's to judge
				}
				v, diags := attr.Expr.Value(nil)
				if diags.HasErrors() {
					return existingStack{}, false, diags
				}
				switch attr.Name {
				case "id":
					if v.IsNull() || !v.IsKnown() || v.Type() != cty.String {
						return existingStack{}, false, fmt.Errorf("%s: stack id is not a string", f)
					}
					s.id = v.AsString()
				case "tags", "after":
					list, err := stringList(v)
					if err != nil {
						return existingStack{}, false, fmt.Errorf("%s: stack %s: %v", f, attr.Name, err)
					}
					if attr.Name == "tags" {
						s.tags = list
					} else {
						s.after = list
					}
				}
			}
		}
	}
	return s, where != "", nil
}

func sortedAttributes(attrs hclsyntax.Attributes) []*hclsyntax.Attribute {
	out := make([]*hclsyntax.Attribute, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b *hclsyntax.Attribute) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func stringList(v cty.Value) ([]string, error) {
	if v.IsNull() || !v.IsKnown() || !(v.Type().IsTupleType() || v.Type().IsListType() || v.Type().IsSetType()) {
		return nil, errors.New("not a list of strings")
	}
	var out []string
	for it := v.ElementIterator(); it.Next(); {
		_, e := it.Element()
		if e.IsNull() || !e.IsKnown() || e.Type() != cty.String {
			return nil, errors.New("not a list of strings")
		}
		out = append(out, e.AsString())
	}
	return out, nil
}

// terramateCLIConfig is the Terramate CLI configuration every run gets through TM_CLI_CONFIG_FILE
// (keys of Terramate 0.17.3's ui/tui/cliconfig: disable_checkpoint, disable_checkpoint_signature,
// disable_telemetry, user_terramate_dir; 005 T038). Without it the pinned Terramate opens an
// HTTPS connection on every run and writes ~/.terramate.d (checkpoint cache, checkpoint and
// analytics signatures) even with TM_DISABLE_CHECKPOINT set, which 0.17.3 does not read;
// user_terramate_dir moves the analytics signature it still writes into the run's own directory.
const terramateCLIConfig = "disable_checkpoint = true\ndisable_checkpoint_signature = true\ndisable_telemetry = true\nuser_terramate_dir = %q\n"

// runTerramate runs the pinned Terramate with its own CLI configuration in a temporary directory,
// removed afterwards, and returns its trimmed output: stdout and stderr together when combined,
// else stdout only.
func runTerramate(bin string, combined bool, args ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "lz-terramate-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	config := filepath.Join(dir, "terramaterc")
	if err := os.WriteFile(config, fmt.Appendf(nil, terramateCLIConfig, filepath.Join(dir, "user")), 0o600); err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "TM_CLI_CONFIG_FILE="+config)
	var out []byte
	if combined {
		out, err = cmd.CombinedOutput()
	} else {
		out, err = cmd.Output()
	}
	return bytes.TrimSpace(out), err
}

// TerramateTCB is the pinned Terramate inside the offline entry.
const TerramateTCB = "/tcb/terramate"

var terramatePin = regexp.MustCompile(`(?m)^"aqua:terramate-io/terramate"\s*=\s*"([^"]+)"`)

// PinnedTerramate returns the Terramate binary to run for the project at root, resolved as the
// reconciler controls do: LZ_TERRAMATE, else the entry's /tcb/terramate, else terramate on PATH.
// The binary must report exactly the version root/mise.toml pins; another one is refused, never
// skipped for the next candidate.
func PinnedTerramate(root string) (string, error) {
	return resolveTerramate(root, os.Getenv("LZ_TERRAMATE"), TerramateTCB)
}

func resolveTerramate(root, override, tcb string) (string, error) {
	mise, err := os.ReadFile(filepath.Join(root, "mise.toml"))
	if err != nil {
		return "", fmt.Errorf("reading the terramate pin: %w", err)
	}
	pin := terramatePin.FindSubmatch(mise)
	if pin == nil {
		return "", errors.New("mise.toml pins no terramate version")
	}
	path := override
	if path == "" {
		if _, err := os.Stat(tcb); err == nil {
			path = tcb
		} else if path, err = exec.LookPath("terramate"); err != nil {
			return "", fmt.Errorf("no terramate binary (set LZ_TERRAMATE to the pinned %s): %v", pin[1], err)
		}
	}
	out, err := runTerramate(path, false, "version")
	if got := string(out); err != nil || got != string(pin[1]) {
		return "", fmt.Errorf("%s reports version %q (%v), want the pinned %s; set LZ_TERRAMATE", path, got, err, pin[1])
	}
	return path, nil
}

// CheckStacks is the stacks:check comparison (research R16 *Freshness*): it copies the project's
// root Terramate files and stacks/ to a scratch directory, reconciles there under Check and runs
// `terramate generate`, and returns one finding per missing stack (manifest row order), then one
// per generated file that differs from, is absent from or is extra in the candidate (sorted).
// The candidate is never written. A reconciler refusal or a Terramate failure is the error.
func CheckStacks(root, terramate string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, ManifestPath)); errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoManifest
	}
	scratch, err := os.MkdirTemp("", "lz-stacks-check-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	candidate, err := checkedFiles(root)
	if err != nil {
		return nil, err
	}
	for rel, data := range candidate {
		dst := filepath.Join(scratch, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, err
		}
	}
	report, err := Reconcile(ReconcileOptions{Root: scratch, Terramate: terramate, Check: true})
	if err != nil {
		return nil, err
	}
	var findings []string
	for _, p := range report.Created {
		findings = append(findings, "missing stack "+p)
	}
	if out, err := runTerramate(terramate, true, "-C", scratch, "generate"); err != nil {
		return nil, fmt.Errorf("terramate generate: %v: %s", err, out)
	}
	generated, err := checkedFiles(scratch)
	if err != nil {
		return nil, err
	}
	var stale []string
	for rel, data := range generated {
		if old, ok := candidate[rel]; !ok || !bytes.Equal(old, data) {
			stale = append(stale, rel)
		}
	}
	for rel := range candidate {
		if _, ok := generated[rel]; !ok {
			stale = append(stale, rel)
		}
	}
	slices.Sort(stale)
	for _, rel := range stale {
		findings = append(findings, "stale generated file "+rel)
	}
	return findings, nil
}

// tofuWorkingFile is an OpenTofu working file: the .terraform and .terragrunt-cache directories
// (or links to them), state (*.tfstate, *.tfstate.*), plans (*.tfplan, *.tfplan.json) and
// crash logs. They are never generated, possibly large or secret-bearing, so stacks:check never
// copies or compares them and discovery never descends into them. A Terramate or OpenTofu source
// file (*.tm, *.hcl, *.tf, *.tf.json) is never one, whatever else its name holds, so no generated
// file can be hidden.
func tofuWorkingFile(name string, dir bool) bool {
	if name == ".terraform" || name == ".terragrunt-cache" {
		return true // a directory, or a link to one
	}
	if dir {
		return false
	}
	for _, suffix := range []string{".tm", ".hcl", ".tf", ".tf.json"} {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	for _, suffix := range []string{".tfstate", ".tfplan", ".tfplan.json"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return strings.Contains(name, ".tfstate.") || name == "crash.log" || (strings.HasPrefix(name, "crash.") && strings.HasSuffix(name, ".log"))
}

// checkedFiles reads what stacks:check compares: the regular *.tm and *.tm.hcl files at root and
// every regular file under root/stacks except OpenTofu working files, keyed by slash-separated
// relative path. Anything else there (a symlink, a device) is refused rather than followed.
func checkedFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".tm.hcl") || strings.HasSuffix(name, ".tm") {
			if !e.Type().IsRegular() {
				return nil, fmt.Errorf("%s: not a regular file", name)
			}
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				return nil, err
			}
			files[name] = data
		}
	}
	err = filepath.WalkDir(filepath.Join(root, "stacks"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if tofuWorkingFile(d.Name(), d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: not a regular file", filepath.ToSlash(rel))
		}
		data, err := os.ReadFile(path)
		files[filepath.ToSlash(rel)] = data
		return err
	})
	return files, err
}
