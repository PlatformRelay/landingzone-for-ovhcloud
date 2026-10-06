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
$E --candidate "$C" -- task stacks:order -- all       # run order
$E --candidate "$C" -- task check:specs -- specs/005-first-landing-zone-slice
```

## Adding a stack (platform maintainer, host)
1. Add a row to `stacks/deployments.yaml` (new `id`, `stage`, dimensions).
2. `task stacks:reconcile && task stacks:generate` (host; writes `stacks/…`).
3. Commit the manifest and generated files together; offline `stacks:check` must pass.
Changing an `id` or a dimension of an existing row fails with `UNSUPPORTED_CHANGE` (rename/retirement
postponed).

## Owner session (maintainer workstation only)
Prerequisites: `~/.config/ovh-lz/sandbox.env` (existing), `.envrc` with
`dotenv_if_exists "$HOME/.config/ovh-lz/sandbox.env"`, `direnv allow`, `MISE_ENV=live mise install`,
`~/.config/ovh-lz/project.env` with `LZ_PROJECT_ID_DEMO_DEV=<sandbox project id>`. Run from the owner's
own main checkout on a reviewed commit, never from an agent worktree (D87).

```sh
task bootstrap:account                 # admin import, passphrase, state buckets, state.env; re-run = no change
task live:plan -- all                  # plans in order; shows consumers needing re-plan
task live:chain -- all                 # apply → assertions → destroy ephemeral (trap) → leftover check
```
Single stacks: `task live:apply -- demo-dev-gra11-runtime`, `task live:destroy -- demo-dev-gra11-runtime`.
After the run: copy the printed run id and the approximate cost (Control Panel → Public Cloud →
billing, or `ovhcloud`) into the PR description.

## Fresh account (account migration)
1. Create the account and one Public Cloud project in the Control Panel (ordering is not automated).
2. Create a short-lived application key + consumer key; put AK/AS/CK in
   `~/.config/ovh-lz/root-bootstrap.env` (mode 600).
3. Set a new `spec.org` (default `lz`) in `stacks/deployments.yaml` if the old account's state
   buckets still exist (bucket names are global), regenerate, commit.
4. `task bootstrap:account -- --fresh-account` → follow the printed revocation step; the tool deletes
   `root-bootstrap.env` after you confirm.
5. `task bootstrap:account` again → every phase `unchanged`.

## What not to expect
No cost gate, no spend ledger, no reaper, no GitLab, no TACO, no tenant self-service, no OKMS or key
recovery, no artefact generations/fencing. See spec *Out of scope / postponed*.
