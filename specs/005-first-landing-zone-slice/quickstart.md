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
2. `task stacks:reconcile && task stacks:generate` (host, no credentials, no host guard; runs in your
   authoring worktree with the edit uncommitted; writes `stacks/…`). Adding a tenant also regenerates
   `account-governance`'s tenant map, so the next run selects it with the new `tenant-state`.
3. Commit the manifest and generated files together; offline `stacks:check` must pass.
Changing an `id` or a dimension of an existing row fails with `UNSUPPORTED_CHANGE` (rename/retirement
postponed).

## Owner session (maintainer workstation only)
Prerequisites: `~/.config/ovh-lz/sandbox.env` (existing); a dedicated owner clone used only for live
runs, e.g. `~/Projects/PlatformRelay/lz-live` — a plain `git clone` with its own `.git`, never a
linked worktree, never `--shared`, `--reference` or `--separate-git-dir`, and no `git worktree add`
from it (D92: agent worktrees share their main checkout's `.git`, which the guard cannot defend);
`~/.config/ovh-lz/live.env` (mode 0600) with `LZ_OWNER_CHECKOUT=<canonical absolute path of that
clone>` and `LZ_AGENT_WORKTREE_ROOT=<absolute path of the existing directory holding agent
worktrees>` — on the maintainer's workstation `/home/koni/Projects/PlatformRelay/lz-live` and
`/home/koni/Projects/PlatformRelay/worktrees`; `MISE_ENV=live mise install`. `lz-live`
reads credentials itself and builds each child environment from scratch, so a `.envrc` is not needed
and anything it exports is ignored. Run from the dedicated clone, clean, at the reviewed commit,
passing its SHA (D87); the guard refuses anything else before reading a credential — a linked
worktree, a git directory listing linked worktrees, a shared or alternates-backed repository, a
checkout at or under the agent worktree root, and an agent root that is unset, relative, absent on
disk or not a directory.

```sh
S=<reviewed sha on origin/main>
task bootstrap:account -- --reviewed-sha $S   # bind account, passphrase, admin check, account bucket, publish, verify
#   first run on the existing account: fill LZ_PROJECT_ID_STATE and LZ_PROJECT_ID_DEMO_DEV in
#   ~/.config/ovh-lz/accounts/<account>/account.env when the run reports `blocked`, then re-run
#   (the admin credential already exists, so the re-run needs no root keys)
task live:chain -- --reviewed-sha $S all      # apply 5 stacks (account-bootstrap is bootstrap:account's) → assertions → destroy runtime + network (trap) → leftover check
task live:plan -- --reviewed-sha $S all       # afterwards: plans the selected set; consumers of an unpublished producer show `blocked`
# a probe whose destroy failed: task live:probe -- --reviewed-sha $S --cleanup <run-id>
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
   the previous account's `sandbox.env` moves into `accounts/<old>/`; `identify` lists the account's
   projects and asks for each project reference (`STATE`, `DEMO_DEV`; in the one-project sandbox
   both are that project); the run creates the admin client and policy, writes `sandbox.env`, and
   revokes the root credential at the end — also when it fails or you abort it.
   If it stopped after the admin was created, re-run without the flag; if before, re-run with the
   flag and fresh root keys. Completed phases report `unchanged`.
5. `task bootstrap:account -- --reviewed-sha $S` again → every phase `unchanged`.

## What not to expect
No cost gate, no spend ledger, no reaper, no GitLab, no TACO, no tenant self-service, no OKMS or key
recovery, no artefact generations/fencing, no demonstrated tenant isolation of state (KD-1, KD-3). See spec
*Out of scope / postponed* and *Known deviations*.
