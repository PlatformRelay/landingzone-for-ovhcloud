package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// `lz-live bootstrap [--fresh-account]` (spec 005 FR-010, FR-012, research R13; T043, T089): after
// the host guard admitted the run, the bootstrap phases (internal/live) run against the account the
// credential names, with the org and project references of the checkout's stacks/deployments.yaml.
// The operator's terminal is opened only under --fresh-account, for the root keys (echo off) and
// missing project references (echo on). After admin, the state, publish and verify phases
// (live.NewBootstrapState, T057) run as Rest with the account bucket's live S3 store (T089), in the
// region and at the endpoint of the manifest's spec.state.

// bootstrapEndpoint is the only endpoint the bootstrap serves: the admin policy's URNs are the eu
// form (urn:v1:eu:…, research R13), and the sandbox is on ovh-eu (AGENTS.md).
const bootstrapEndpoint = "ovh-eu"

// manifestBinding reads the checkout's manifest and the project references account.env must
// hold: the state project's reference first, then each environment's in order.
func manifestBinding(checkout string) (*stacks.Manifest, []string, error) {
	raw, err := os.ReadFile(filepath.Join(checkout, stacks.ManifestPath))
	if err != nil {
		return nil, nil, err
	}
	m, err := stacks.DecodeManifest(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", stacks.ManifestPath, err)
	}
	refs := []string{m.StateProjectRef}
	for _, t := range m.Tenants {
		for _, e := range t.Environments {
			if !slices.Contains(refs, e.ProjectRef) {
				refs = append(refs, e.ProjectRef)
			}
		}
	}
	return m, refs, nil
}

// stateOptions configures the state, publish and verify phases: the account bucket's region as
// the API names it (GRA, from spec.state.region), the reviewed commit as the envelope's
// source_revision, the checkout's schemas/outputs, and a store that opens the bucket over S3 with
// state.env's keys. spec.state.endpoint must be the endpoint its region derives: the generated S3
// backends use the manifest's endpoint, the store the derived one, and they must not diverge.
func stateOptions(d deps, checkout string, m *stacks.Manifest, reviewed, tofu string, api live.API) (live.StateOptions, error) {
	endpoint, err := live.ObjectStorageEndpoint(m.StateRegion)
	if err != nil {
		return live.StateOptions{}, fmt.Errorf("%s: spec.state.region: %w", stacks.ManifestPath, err)
	}
	if m.StateEndpoint != endpoint {
		return live.StateOptions{}, fmt.Errorf("%s: spec.state.endpoint %q is not %s, the endpoint of spec.state.region %q",
			stacks.ManifestPath, m.StateEndpoint, endpoint, m.StateRegion)
	}
	region := m.StateRegion
	return live.StateOptions{
		Checkout: checkout,
		Manifest: m,
		Region:   strings.ToUpper(region),
		Schemas:  os.DirFS(filepath.Join(checkout, "schemas", "outputs")),
		Revision: reviewed,
		Tofu:     tofu,
		API:      api,
		Store: func(keys map[string]string) (live.ObjectStore, error) {
			return live.NewS3Store(live.S3Options{Region: region, Keys: keys, HTTP: d.S3HTTP})
		},
		Stdout: d.Stdout,
	}, nil
}

// noTerminal is the terminal of a run without --fresh-account: nothing is ever prompted.
type noTerminal struct{}

var errNoTerminal = errors.New("no prompt without --fresh-account")

func (noTerminal) ReadSecret(string) (string, error) { return "", errNoTerminal }
func (noTerminal) ReadLine(string) (string, error)   { return "", errNoTerminal }

// bootstrap runs the phases after the guard admitted the host (live.Run ran it first). What the
// state phases need (tofu, a consistent spec.state) is checked before any phase starts.
func bootstrap(ctx context.Context, d deps, checkout, cfg, reviewed string, fresh bool) error {
	m, refs, err := manifestBinding(checkout)
	if err != nil {
		return err
	}
	tofu, err := d.LookPath("tofu")
	if err != nil {
		return fmt.Errorf("tofu: %w", err)
	}
	api, err := d.API(bootstrapEndpoint)
	if err != nil {
		return err
	}
	so, err := stateOptions(d, checkout, m, reviewed, tofu, api)
	if err != nil {
		return err
	}
	var term live.Terminal = noTerminal{}
	if fresh {
		if term, err = d.Terminal(ctx); err != nil {
			return fmt.Errorf("--fresh-account needs the operator's terminal: %w", err)
		}
	}
	boot := d.Bootstrap
	if boot == nil {
		boot = live.Bootstrap
	}
	rest := d.State
	if rest == nil {
		rest = func(o live.StateOptions) func(context.Context, live.BootstrapAccount) error {
			return live.NewBootstrapState(o).Rest
		}
	}
	_, err = boot(ctx, live.BootstrapOptions{
		ConfigRoot:   cfg,
		Checkout:     checkout,
		Endpoint:     bootstrapEndpoint,
		Org:          m.Org,
		ProjectRefs:  refs,
		FreshAccount: fresh,
		API:          api,
		Terminal:     term,
		Stdout:       d.Stdout,
		// live.Run admitted the host before calling here: the guard phase reports that.
		Guard: func() error { return nil },
		Rest:  rest(so),
	})
	return err
}
