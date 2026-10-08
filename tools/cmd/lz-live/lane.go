package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// `lz-live plan|apply <instance|all>` (spec 005 FR-009, FR-010, FR-011, research R21; T059): after
// the host guard admitted the run, live.Apply acts on the selected stacks of the checkout's
// stacks/deployments.yaml in the account the sandbox admin's credential names (GET /auth/details),
// with the account's run locks (accounts/<account>/locks), the state buckets of spec.state over
// S3, and a run directory .local/live/<run-id>/ in the checkout beside the records
// (.local/live/records). Every credential live.Apply loads is bound to that account before use.

// lane runs plan or apply. A TMPDIR inside the checkout is refused first (the children's scratch
// HOME goes under it); a missing sandbox.env is blocked (exit 2) naming bootstrap:account.
func lane(ctx context.Context, d deps, checkout, cfg, reviewed, verb, target string) error {
	if err := live.ScratchOutside(checkout); err != nil {
		return err
	}
	m, _, err := manifestBinding(checkout)
	if err != nil {
		return err
	}
	tofu, err := d.LookPath("tofu")
	if err != nil {
		return fmt.Errorf("tofu: %w", err)
	}
	store, err := stateStore(d, m)
	if err != nil {
		return err
	}
	sandbox, err := live.ReadCredentialFile(filepath.Join(cfg, "sandbox.env"))
	if errors.Is(err, fs.ErrNotExist) {
		return &live.Blocked{Phase: "credentials", Detail: "sandbox.env does not exist; bootstrap:account writes it"}
	}
	if err != nil {
		return fmt.Errorf("sandbox.env: %w", err)
	}
	cred := live.Credential{Endpoint: sandbox["OVH_ENDPOINT"], ClientID: sandbox["OVH_CLIENT_ID"], ClientSecret: sandbox["OVH_CLIENT_SECRET"]}
	if cred.Endpoint == "" || cred.ClientID == "" || cred.ClientSecret == "" {
		return errors.New("sandbox.env: OVH_ENDPOINT, OVH_CLIENT_ID and OVH_CLIENT_SECRET are required")
	}
	api, err := d.API(cred.Endpoint)
	if err != nil {
		return err
	}
	// The account to act in; live.Apply binds this credential (and every other) to it again.
	account, err := api.Account(ctx, cred)
	if err != nil {
		return err
	}
	accDir, err := live.AccountDir(cfg, account)
	if err != nil {
		return err
	}
	runID, err := live.NewRunID(d.Now())
	if err != nil {
		return err
	}
	apply := d.Apply
	if apply == nil {
		apply = live.Apply
	}
	fmt.Fprintf(d.Stdout, "LZ-LIVE run %s start %s %s\n", runID, verb, target)
	return apply(ctx, live.ApplyOptions{
		Verb:       verb,
		Target:     target,
		Checkout:   checkout,
		Manifest:   m,
		ConfigRoot: cfg,
		Account:    account,
		RunID:      runID,
		RunDir:     filepath.Join(checkout, ".local", "live", runID),
		Records:    filepath.Join(checkout, ".local", "live", "records"),
		Revision:   reviewed,
		Tofu:       tofu,
		Schemas:    os.DirFS(filepath.Join(checkout, "schemas", "outputs")),
		Locks:      stacks.DirLocks(filepath.Join(accDir, "locks")),
		Store:      store,
		Terminal:   d.Stdout,
		API:        api,
	})
}
