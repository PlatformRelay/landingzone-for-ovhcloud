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
	"os"
	"path/filepath"
	"slices"

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
)

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

// Generate decodes Root/stacks/deployments.yaml strictly, refuses a `git` stage source before
// writing anything, and runs the pinned `terramate generate` in Root/stacks. It reads no
// credential and calls no host guard: it runs in any checkout, including an authoring worktree
// with a dirty tree.
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
	// Run from stacks/: Terramate generates only below its working directory, and a whole-project
	// run deletes every other file carrying its generated header as an orphan, such as the
	// dependency-check fixtures under tests/ (005 T039). Orphans below stacks/ are stacks:check's.
	if out, err := runTerramate(opts.Terramate, true, "-C", filepath.Join(opts.Root, "stacks"), "generate"); err != nil {
		return fmt.Errorf("terramate generate: %v: %s", err, out)
	}
	return nil
}

// PlannedBucketNames returns the bucket names (ovh_cloud_project_storage `name`) a plan creates
// or keeps, sorted, read through the shared plan decoder (planjson: a document that is not a
// complete plan, or is marked errored, is an error). A bucket whose actions are not known, or
// whose name is not known at plan time, is an error: uniqueness cannot be judged on it.
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
		if slices.Equal(c.Actions, []string{"delete"}) {
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
