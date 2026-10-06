# Quickstart: First landing-zone slice
All commands are **planned** (creators in `tasks.md`). Nothing below runs today.

## Offline (any contributor, no credentials, no network)
```sh
E=<approved-absolute-path>/lz-offline; C=$(pwd)
$E --candidate "$C" -- task test:slice                # L0 + L1 for every module, component, stage
$E --candidate "$C" -- task test:dependencies         # layers and purity rules
$E --candidate "$C" -- task test:outputs              # outputs.json schemas and redaction
$E --candidate "$C" -- task stacks:check              # manifest ↔ stacks ↔ generated files
$E --candidate "$C" -- task test:stack-plans          # every stack plans with mocks and fixture inputs
$E --candidate "$C" -- task test:exchange             # producer → outputs.json → consumer round trip
$E --candidate "$C" -- task stacks:order -- all       # run order and selected set
$E --candidate "$C" -- task test:live-lane            # guard, run core, chain, collector (fakes)
$E --candidate "$C" -- task test:bootstrap            # bootstrap phases and partial states (fakes)
$E --candidate "$C" -- task check:specs -- specs/005-first-landing-zone-slice
```

## Adding a stack or a tenant (platform maintainer, host)
1. Add a row to `stacks/deployments.yaml` (new `id`, `stage`, dimensions). A new tenant needs its
   `tenant-state` row as well; `bootstrap` is not touched.
2. `task stacks:reconcile && task stacks:generate` (host; writes `stacks/…`).
3. Commit the manifest and generated files together; offline `stacks:check` must pass.
Changing an `id` or a dimension of an existing row fails with `UNSUPPORTED_CHANGE` (rename/retirement
postponed).

## Owner session (maintainer workstation only)
Prerequisites: `~/.config/ovh-lz/sandbox.env` (existing); `~/.config/ovh-lz/live.env` with
`LZ_OWNER_CHECKOUT=<canonical path of your main checkout>`; `MISE_ENV=live mise install`. `lz-live`
reads credentials itself and builds each child environment from scratch, so a `.envrc` is not needed
and anything it exports is ignored. Run from your own main checkout, clean, at the reviewed commit,
passing its SHA (D87); the guard refuses anything else before reading a credential.

```sh
S=<reviewed sha on origin/main>
task bootstrap:account -- --reviewed-sha $S   # bind account, passphrase, admin check, account bucket, publish, verify
#   first run: fill LZ_PROJECT_ID_STATE and LZ_PROJECT_ID_DEMO_DEV in
#   ~/.config/ovh-lz/accounts/<account>/account.env when the run reports `blocked`, then re-run
task live:chain -- --reviewed-sha $S all      # apply 6 stacks → assertions → destroy runtime + network (trap) → leftover check
task live:plan -- --reviewed-sha $S all       # afterwards: plans the selected set; consumers of an unpublished producer show `blocked`
```
Single stacks: `task live:apply -- --reviewed-sha $S demo-dev-gra11-runtime`, `task live:destroy --
--reviewed-sha $S demo-dev-gra11-runtime` (retained instances are refused).
After the run: copy the printed run id, the known-deviation line (KD-1 is expected in the sandbox)
and the approximate cost (Control Panel → Public Cloud → billing, or `ovhcloud`) into the PR
description.

## Fresh account (account migration)
1. Create the account and one Public Cloud project in the Control Panel (ordering is not automated).
2. Create a short-lived application key and consumer key; keep AK/AS/CK at hand — they are typed at
   the prompt, never stored in a file.
3. Set a new `spec.org` (default `lz`) in `stacks/deployments.yaml` if the old account's state
   buckets still exist (bucket names are global), regenerate, commit, review.
4. `task bootstrap:account -- --reviewed-sha $S --fresh-account` → enter the keys at the prompt;
   the previous account's `sandbox.env` moves into `accounts/<old>/`; the run creates the admin
   client and policy, writes `sandbox.env`, fills the binding, and revokes the root credential at
   the end. Fill the project references when it reports `blocked`, then re-run without the flag.
5. `task bootstrap:account -- --reviewed-sha $S` again → every phase `unchanged`.

## What not to expect
No cost gate, no spend ledger, no reaper, no GitLab, no TACO, no tenant self-service, no OKMS or key
recovery, no artefact generations/fencing, no demonstrated tenant isolation of state (KD-1). See spec
*Out of scope / postponed* and *Known deviations*.
