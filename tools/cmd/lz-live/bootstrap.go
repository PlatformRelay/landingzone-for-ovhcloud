package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// `lz-live bootstrap [--fresh-account]` (spec 005 FR-010, FR-012, research R13; T043): after the
// host guard admitted the run, the bootstrap phases (internal/live) run against the account the
// credential names, with the org and project references of the checkout's stacks/deployments.yaml.
// The operator's terminal is opened only under --fresh-account, for the root keys (echo off) and
// missing project references (echo on). The state, publish and verify phases (internal/live
// NewBootstrapState, T057) are not wired here yet: they need a live S3 ObjectStore for the account
// bucket, which no task has built (evidence/T057.md, gap), so this entry still stops after admin.

// bootstrapEndpoint is the only endpoint the bootstrap serves: the admin policy's URNs are the eu
// form (urn:v1:eu:…, research R13), and the sandbox is on ovh-eu (AGENTS.md).
const bootstrapEndpoint = "ovh-eu"

// manifestBinding reads the org and the project references account.env must hold from the
// checkout's manifest: the state project's reference first, then each environment's in order.
func manifestBinding(checkout string) (string, []string, error) {
	raw, err := os.ReadFile(filepath.Join(checkout, stacks.ManifestPath))
	if err != nil {
		return "", nil, err
	}
	m, err := stacks.DecodeManifest(raw)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", stacks.ManifestPath, err)
	}
	refs := []string{m.StateProjectRef}
	for _, t := range m.Tenants {
		for _, e := range t.Environments {
			if !slices.Contains(refs, e.ProjectRef) {
				refs = append(refs, e.ProjectRef)
			}
		}
	}
	return m.Org, refs, nil
}

// noTerminal is the terminal of a run without --fresh-account: nothing is ever prompted.
type noTerminal struct{}

var errNoTerminal = errors.New("no prompt without --fresh-account")

func (noTerminal) ReadSecret(string) (string, error) { return "", errNoTerminal }
func (noTerminal) ReadLine(string) (string, error)   { return "", errNoTerminal }

// bootstrap runs the phases after the guard admitted the host (live.Run ran it first).
func bootstrap(ctx context.Context, d deps, checkout, cfg string, fresh bool) error {
	org, refs, err := manifestBinding(checkout)
	if err != nil {
		return err
	}
	api, err := d.API(bootstrapEndpoint)
	if err != nil {
		return err
	}
	var term live.Terminal = noTerminal{}
	if fresh {
		if term, err = d.Terminal(ctx); err != nil {
			return fmt.Errorf("--fresh-account needs the operator's terminal: %w", err)
		}
	}
	_, err = live.Bootstrap(ctx, live.BootstrapOptions{
		ConfigRoot:   cfg,
		Checkout:     checkout,
		Endpoint:     bootstrapEndpoint,
		Org:          org,
		ProjectRefs:  refs,
		FreshAccount: fresh,
		API:          api,
		Terminal:     term,
		Stdout:       d.Stdout,
		// live.Run admitted the host before calling here: the guard phase reports that.
		Guard: func() error { return nil },
	})
	return err
}
