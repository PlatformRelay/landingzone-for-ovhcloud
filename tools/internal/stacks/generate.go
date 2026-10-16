// This file is stack generation (FR-007, FR-008, ADR-0007, research R16): rendering each stack's
// backend, provider, stage call, typed inputs, outputs, import and offline test from
// stacks/deployments.yaml with the pinned `terramate generate`, and the planned-name check
// (NAME_COLLISION) over the generated stacks' plans.
//
// The rendering itself is the repository's Terramate configuration: terramate.tm.hcl (the project
// root) and stacks/_lz/*.tm.hcl, imported by stacks/lz.tm.hcl (005 T038).
package stacks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/planjson"
)

// Refusal codes of generation.
const (
	// CodeStageSourceNotImplemented refuses a manifest whose spec.stage_source is the `git` seam:
	// nothing is generated from it (research R22).
	CodeStageSourceNotImplemented = "STAGE_SOURCE_NOT_IMPLEMENTED"
	// CodeNameCollision refuses one bucket name planned twice across the generated stacks (FR-007,
	// data-model *Derived instance fields*, research R15).
	CodeNameCollision = "NAME_COLLISION"
	// CodeUnreconciledRow refuses a manifest with a row that stacks:reconcile has not yet made a
	// stack for: nothing is generated from it (006 FR-011, research R12).
	CodeUnreconciledRow = "UNRECONCILED_ROW"
	// CodeStageLockMismatch starts each stacks:check finding of two stages whose dependency locks
	// pin different provider versions or hashes (006 FR-011, research R12).
	CodeStageLockMismatch = "STAGE_LOCK_MISMATCH"
	// CodeNoStageLock refuses a manifest with a stack that has no dependency lock file while its
	// stage has none to copy: nothing is generated from it, so no stack is left without a lock
	// (006 FR-011).
	CodeNoStageLock = "NO_STAGE_LOCK"
)

// lockFile is the OpenTofu dependency lock file of a stage or a stack.
const lockFile = ".terraform.lock.hcl"

// GenerateOptions configures one generation run.
type GenerateOptions struct {
	Root      string // Terramate project root holding stacks/deployments.yaml and the reconciled stacks
	Terramate string // the pinned terramate binary
}

// GenerateError is a refusal of generation or of the planned-name check; a decoder refusal is
// returned as the decoder's *ManifestError instead.
type GenerateError struct {
	Code   string
	Path   string // the stack path concerned; empty for a manifest-wide refusal
	Detail string
}

func (e *GenerateError) Error() string { return e.Code + ": " + e.Path + ": " + e.Detail }

// StackPlan is one generated stack's plan: Stack is its path (stacks/…), Plan the plan JSON
// (`tofu show -json` form, as the `test_plan` of a verbose `tofu test -json` message carries it).
type StackPlan struct {
	Stack string
	Plan  []byte
}

// Generate decodes Root/stacks/deployments.yaml strictly and, before writing anything, refuses a
// `git` stage source, a row stacks:reconcile has not made a stack for (UNRECONCILED_ROW, the first
// such row in manifest order) and a stack without a dependency lock file whose stage has none to
// copy (NO_STAGE_LOCK). It then runs the pinned `terramate generate` in Root/stacks and copies
// Root/stages/<stage>/.terraform.lock.hcl into each row's stack that has no lock file (006
// FR-011); a lock a stack already has is never replaced. It reads no credential and calls no host
// guard: it runs in any checkout, including an authoring worktree with a dirty tree.
func Generate(opts GenerateOptions) error {
	data, err := os.ReadFile(filepath.Join(opts.Root, ManifestPath))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNoManifest
	}
	if err != nil {
		return err
	}
	m, err := DecodeManifest(data)
	if err != nil {
		return err
	}
	if m.StageSource != "local" {
		return &GenerateError{Code: CodeStageSourceNotImplemented,
			Detail: fmt.Sprintf("spec.stage_source.kind %q: only local stage sources generate", m.StageSource)}
	}
	existing, _, err := discoverStacks(opts.Root)
	if err != nil {
		return err
	}
	for _, in := range m.Instances {
		if _, ok := existing[in.Path]; !ok {
			return &GenerateError{Code: CodeUnreconciledRow, Path: in.Path,
				Detail: fmt.Sprintf("row %s has no stack: run stacks:reconcile first", in.ID)}
		}
	}
	locks, err := missingLocks(opts.Root, m)
	if err != nil {
		return err
	}
	// Run from stacks/: Terramate generates only below its working directory, and a whole-project
	// run deletes every other file carrying its generated header as an orphan, such as the
	// dependency-check fixtures under tests/ (005 T039). Orphans below stacks/ are stacks:check's.
	if out, err := runTerramate(opts.Terramate, true, "-C", filepath.Join(opts.Root, "stacks"), "generate"); err != nil {
		return fmt.Errorf("terramate generate: %v: %s", err, out)
	}
	for _, path := range slices.Sorted(maps.Keys(locks)) {
		if err := writeNew(filepath.Join(opts.Root, filepath.FromSlash(path), lockFile), locks[path]); err != nil {
			return fmt.Errorf("copying the stage lock into %s: %w", path, err)
		}
	}
	return nil
}

// missingLocks returns, by stack path, the stage lock to copy into each row's stack that has no
// dependency lock file, and refuses with NO_STAGE_LOCK a stack whose stage has no lock either.
// Anything at the stack's lock path, a link included, counts as its lock.
func missingLocks(root string, m *Manifest) (map[string][]byte, error) {
	locks := map[string][]byte{}
	for _, in := range m.Instances {
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(in.Path), lockFile))
		if err == nil {
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		lock, err := os.ReadFile(filepath.Join(root, "stages", in.Stage, lockFile))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &GenerateError{Code: CodeNoStageLock, Path: in.Path,
				Detail: fmt.Sprintf("no %s here and none in stages/%s to copy", lockFile, in.Stage)}
		}
		if err != nil {
			return nil, err
		}
		locks[in.Path] = lock
	}
	return locks, nil
}

// writeNew creates path with data, refusing a path that exists, so a copy never writes through a
// link or over a lock that appeared since it was looked for.
//
// The data goes to a temporary file in the same directory first, which is then linked to path
// (link(2) refuses an existing path) and removed: a write that fails leaves no partial lock at
// path for a later run to take as the stack's own.
//
// The lock keeps the temporary file's owner-only mode (0600). Git records no permission but the
// executable bit, so a wider mode would change nothing in the repository.
func writeNew(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := errors.Join(writeLockData(tmp, data), tmp.Close()); err != nil {
		return err
	}
	return os.Link(tmp.Name(), path)
}

// writeLockData writes a copied lock's bytes; a seam so a test can fail the write.
var writeLockData = func(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}

// CheckStageLocks compares the dependency lock files root/stages/<stage>/.terraform.lock.hcl and
// returns one finding, starting with CodeStageLockMismatch, per stage whose lock pins a provider
// at another version or with another set of hashes than the first stage, in name order, that pins
// the same provider (006 FR-011): every stack shares one plugin cache, so two such locks race.
// Comments, constraints, the order of hashes and a repeated hash do not count; a provider only one
// stage pins is no finding, nor is a stage without a lock. A lock that cannot be read or parsed,
// or holds anything but provider blocks, is a finding too, so a check never passes on a lock it
// did not compare.
func CheckStageLocks(root string) []string {
	dir := filepath.Join(root, "stages")
	entries, err := os.ReadDir(dir) // sorted by name
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []string{fmt.Sprintf("%s stages: %v", CodeStageLockMismatch, err)}
	}
	first := map[string]stagePin{}
	var findings []string
	for _, e := range entries {
		stage := e.Name()
		providers, err := readStageLock(dir, stage)
		if err != nil {
			findings = append(findings, fmt.Sprintf("%s stages/%s: lock not compared: %v", CodeStageLockMismatch, stage, err))
			continue
		}
		findings = append(findings, comparePins(first, stage, providers)...)
	}
	return findings
}

// stagePin is a provider's pin with the stage whose lock first pinned it.
type stagePin struct {
	stage string
	providerPin
}

// readStageLock reads the pins of dir/<stage>/.terraform.lock.hcl. An entry that is not a
// directory, or a stage without a lock, pins nothing and is no error.
func readStageLock(dir, stage string) (map[string]providerPin, error) {
	if info, err := os.Stat(filepath.Join(dir, stage)); err != nil || !info.IsDir() {
		return nil, nil
	}
	path := filepath.Join(dir, stage, lockFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readLock(path, data)
}

// comparePins returns a finding per provider of stage's pins that differs from its first pin,
// in provider order, and records in first each provider stage is the first to pin.
func comparePins(first map[string]stagePin, stage string, providers map[string]providerPin) []string {
	var findings []string
	for _, addr := range slices.Sorted(maps.Keys(providers)) {
		p := providers[addr]
		ref, seen := first[addr]
		if !seen {
			first[addr] = stagePin{stage, p}
			continue
		}
		if ref.version != p.version || !slices.Equal(ref.hashes, p.hashes) {
			findings = append(findings, fmt.Sprintf("%s stages/%s and stages/%s: %s %s (%d hashes) and %s (%d hashes)",
				CodeStageLockMismatch, ref.stage, stage, addr, ref.version, len(ref.hashes), p.version, len(p.hashes)))
		}
	}
	return findings
}

// providerPin is what a lock pins of one provider: its version and its hashes, sorted.
type providerPin struct {
	version string
	hashes  []string
}

// readLock parses a dependency lock file's provider blocks into their pins by provider address.
// A lock holds provider blocks only: any other block or a top-level attribute is an error, so a
// misspelt block is never read as a lock that pins nothing.
func readLock(path string, data []byte) (map[string]providerPin, error) {
	file, diags := hclsyntax.ParseConfig(data, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, diags
	}
	body := file.Body.(*hclsyntax.Body)
	for name := range body.Attributes {
		return nil, fmt.Errorf("top-level attribute %s: a lock holds provider blocks only", name)
	}
	pins := map[string]providerPin{}
	for _, b := range body.Blocks {
		addr, err := providerAddr(b)
		if err != nil {
			return nil, err
		}
		if _, dup := pins[addr]; dup {
			return nil, fmt.Errorf("provider %s pinned twice", addr)
		}
		p, err := readPin(addr, b.Body)
		if err != nil {
			return nil, err
		}
		pins[addr] = p
	}
	return pins, nil
}

// providerAddr returns the address a lock's block pins, refusing any block but a provider block
// with one label.
func providerAddr(b *hclsyntax.Block) (string, error) {
	if b.Type != "provider" {
		return "", fmt.Errorf("a %s block: a lock holds provider blocks only", b.Type)
	}
	if len(b.Labels) != 1 {
		return "", fmt.Errorf("a provider block with %d labels", len(b.Labels))
	}
	return b.Labels[0], nil
}

// readPin reads a provider block's version, which must be a string, and its hashes, sorted and
// without repeats.
func readPin(addr string, body *hclsyntax.Body) (providerPin, error) {
	version, err := pinAttr(addr, body, "version")
	if err != nil {
		return providerPin{}, err
	}
	if version.IsNull() || !version.IsKnown() || version.Type() != cty.String {
		return providerPin{}, fmt.Errorf("provider %s: version is not a string", addr)
	}
	v, err := pinAttr(addr, body, "hashes")
	if err != nil {
		return providerPin{}, err
	}
	hashes, err := stringList(v)
	if err != nil {
		return providerPin{}, fmt.Errorf("provider %s hashes: %v", addr, err)
	}
	return providerPin{version: version.AsString(), hashes: slices.Compact(slices.Sorted(slices.Values(hashes)))}, nil
}

// pinAttr evaluates a provider block's attribute name, refusing a block without it.
func pinAttr(addr string, body *hclsyntax.Body, name string) (cty.Value, error) {
	attr, ok := body.Attributes[name]
	if !ok {
		return cty.NilVal, fmt.Errorf("provider %s has no %s", addr, name)
	}
	v, diags := attr.Expr.Value(nil)
	if diags.HasErrors() {
		return cty.NilVal, diags
	}
	return v, nil
}

// PlannedBucketNames returns the bucket names (ovh_cloud_project_storage `name`) a plan creates
// or keeps, sorted, read through the shared plan decoder (planjson: a document that is not a
// complete plan, or is marked errored, is an error). A bucket the plan deletes or forgets (a
// `removed` block with `destroy = false`; `after` is null) is skipped: it leaves the stack's
// state, though a forgotten one stays in the account, so uniqueness here is among planned
// names only and a clash with a forgotten bucket is left to the apply (T112). A bucket whose actions are not known, or whose name is not known at plan
// time, is an error: uniqueness cannot be judged on it.
func PlannedBucketNames(plan []byte) ([]string, error) {
	p, err := planjson.Decode(plan)
	if err != nil {
		return nil, fmt.Errorf("reading the plan: %w", err)
	}
	var names []string
	for _, c := range p.Changes {
		mode, typ, err := c.Resource()
		if err != nil {
			return nil, fmt.Errorf("reading the plan: %w", err)
		}
		if mode != "managed" || typ != "ovh_cloud_project_storage" {
			continue
		}
		if !planjson.KnownActions(c.Actions) {
			return nil, fmt.Errorf("%s: plan actions %q not known", c.Address, c.Actions)
		}
		if slices.Equal(c.Actions, []string{"delete"}) || slices.Equal(c.Actions, []string{"forget"}) {
			continue
		}
		var after map[string]any
		if len(c.After) > 0 {
			if err := json.Unmarshal(c.After, &after); err != nil {
				return nil, fmt.Errorf("%s: planned object: %w", c.Address, err)
			}
		}
		name, ok := after["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("%s: bucket name not known at plan time", c.Address)
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// CheckPlannedNames refuses with NAME_COLLISION a bucket name that two stacks, or one stack
// twice, plan; Path is the later stack in plans order. An unreadable plan is an error.
func CheckPlannedNames(plans []StackPlan) error {
	owner := map[string]string{}
	for _, p := range plans {
		names, err := PlannedBucketNames(p.Plan)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Stack, err)
		}
		for _, n := range names {
			if other, dup := owner[n]; dup {
				return &GenerateError{Code: CodeNameCollision, Path: p.Stack, Detail: fmt.Sprintf("bucket %s is also planned by %s", n, other)}
			}
			owner[n] = p.Stack
		}
	}
	return nil
}
