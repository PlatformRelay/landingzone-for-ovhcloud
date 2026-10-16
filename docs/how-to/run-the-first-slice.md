---
references:
  - path: tools/cmd/lz-live/main.go
    blob: 806b7b39745efff0c4619cf89ac6c8c1e7e06cce
  - path: tools/cmd/lz-live/lane.go
    blob: 51fed2933b252eb2c4ae16776074378d9da69228
  - path: tools/internal/live/apply.go
    blob: 37a13cfb4292d39d98c5f8aefe3073adb5c84354
  - path: tools/internal/live/bootstrap.go
    blob: 60d561756d64e3d1b45349988d4856f5406b7b2f
  - path: tools/internal/live/chain.go
    blob: 518aa41abaa33d8263da347b023c8cd28a418ac7
  - path: tools/internal/live/observe.go
    blob: 5203e71f8c2a9c3092d3873daab9d9be52fe94c9
  - path: stacks/deployments.yaml
    blob: a4e4c7b24494733e88b0fb9e4d3baa3fa8b2d132
  - path: mise.toml
    blob: 2d63ee3ca1e7bb809bed04e4575609ffd4656542
  - path: mise.live.toml
    blob: 6198981ea0e23d5615a95b6192da14f182c42175
  - path: tests/live/probes/README.md
    blob: b34b198ff38ef36a2ef50ef757e5a9c0c47e0d0a
last_verified: 52f9f9b2ea10319dc37176e3866c8c19cb99f14f
---

# Run the first slice on an OVHcloud account

This guide is for the maintainer who runs the first landing-zone slice
([spec 005](../../specs/005-first-landing-zone-slice/spec.md)) against a real OVHcloud account:
bootstrap the account, apply and destroy the stacks, read what a run recorded, and clean up.
Everything here acts on the account and spends credit. **Only the owner runs it, on the
maintainer workstation.** Agents and CI never run `task bootstrap:*` or `task live:*`, except
for the bounded, expiring exception for the coordinating session in
[AGENTS.md](../../AGENTS.md) ("Known deviation", "Bounded extension").

**Status (2026-10-14).** Against the current sandbox account, every create→destroy probe has
passed ([T010](../../specs/005-first-landing-zone-slice/evidence/T010.md)) and the account was
bootstrapped twice ([T044](../../specs/005-first-landing-zone-slice/evidence/T044.md)). The
chain ([T049](../../specs/005-first-landing-zone-slice/evidence/T049.md)) passed in run
20261013T075309Z-443f, its fourth: every assertion passed except the KD-1 canary, reported as
the known deviation it is in the sandbox; the ephemeral stacks were destroyed and the leftover
check found nothing. T049 still has a gap: the six stacks were not all applied within one run
(`demo-state` last applied in run 1). Runs 1–3 stopped on the projects' IAM tags, which the
admin now sets in `account-governance` (T105). The fresh-account path (T045) is the remaining
owner run; expect it to find what the fakes could not. See
[what is still unverified](../reference/verification-status.md#what-is-still-unverified).

## Before you start

### Tools

The toolchain is pinned in `mise.toml` (Go, OpenTofu, Terramate, Task, TFLint). The host-only
tools are pinned in `mise.live.toml`, which loads only with `MISE_ENV=live` (the `ovhcloud` CLI,
used for checking by hand). Install both into the owner clone (next section):

```sh
cd ~/Projects/PlatformRelay/lz-live
MISE_ENV=live mise install
```

Run every command in this guide with these tools on `PATH` and `MISE_ENV=live` set, for example
with the `mise exec --` prefix:

```sh
MISE_ENV=live mise exec -- task live:plan -- --reviewed-sha "$S" all
```

The examples below leave out the prefix. Every `task live:*` and `task bootstrap:account`
target builds `lz-live` from the checkout first (`.local/bin/lz-live`), so Go is needed.
Every `lz-live` verb first resolves OpenTofu at exactly the version `mise.toml` pins: `LZ_TOFU`
(from the environment, else from `live.env`), else `tofu` on `PATH`. Another version, or none,
stops the run with exit 2 before any credential is read. If an older `tofu` sits ahead of mise's
on `PATH`, set `LZ_TOFU` in `live.env` to the absolute path of the pinned binary
(`mise which tofu`).

Keep `TMPDIR` unset or outside the clone. `lz-live` creates each run's scratch HOME under it
([probe run sheet](../../tests/live/probes/README.md#what-every-root-shares)).

### The dedicated owner clone

Live runs start only from a clone used for nothing else (decision D92):
`~/Projects/PlatformRelay/lz-live`. It must be a plain `git clone` with its own `.git`:

- not a linked worktree;
- not made with `--shared`, `--reference` or `--separate-git-dir`;
- no `git worktree add` from it, ever.

Agent worktrees share their main checkout's `.git`, and the guard cannot defend that.

```sh
git clone https://github.com/PlatformRelay/landingzone-for-ovhcloud.git ~/Projects/PlatformRelay/lz-live
```

Before every session, put the clone at the reviewed commit and check that the tree is clean:

```sh
cd ~/Projects/PlatformRelay/lz-live
git fetch origin
git checkout --detach <reviewed sha>
git status --porcelain          # must print nothing
S=<reviewed sha>
```

The host guard runs before any credential is read and refuses (exit 3) when any of these holds
([FR-011](../../specs/005-first-landing-zone-slice/spec.md#requirements)):

- the command runs inside `lz-offline`;
- the canonical checkout is not `LZ_OWNER_CHECKOUT`;
- the checkout is a linked worktree, lists linked worktrees, or uses alternates or a shared
  object store;
- the checkout lies at or below `LZ_AGENT_WORKTREE_ROOT`;
- the tree is dirty;
- `HEAD` is not `--reviewed-sha`, or is not reachable from `origin/main`.

### Local files (never in a repository)

Every credential and binding lives under `~/.config/ovh-lz/` with mode 600 (directories 700).
`lz-live` refuses group- or world-readable files and reads credentials itself. A `.envrc` is not
needed, and whatever it exports is ignored, `LZ_TOFU` apart. The layout
([data-model](../../specs/005-first-landing-zone-slice/data-model.md#account-binding-and-local-files-never-in-the-repository))
has two files at the top level:

| File | Variables | Written by |
| --- | --- | --- |
| `live.env` | `LZ_OWNER_CHECKOUT` (canonical absolute path of the owner clone), `LZ_AGENT_WORKTREE_ROOT` (absolute path of an existing directory that holds the agent worktrees), optional `LZ_TOFU` (absolute path of the pinned OpenTofu) | you, once |
| `sandbox.env` | `OVH_ENDPOINT`, `OVH_CLIENT_ID`, `OVH_CLIENT_SECRET` of the account's admin service account `lz-sandbox-admin` | existing, or `bootstrap:account --fresh-account` |

The rest sits under `accounts/<account>/`, where `<account>` is the account that
`GET /auth/details` names for the credential:

| File under `accounts/<account>/` | Variables | Written by |
| --- | --- | --- |
| `account.env` | `LZ_ACCOUNT_ID`, `OVH_ENDPOINT`, `LZ_ORG`, `LZ_PROJECT_ID_<REF>` for each project reference in `stacks/deployments.yaml` (today `LZ_PROJECT_ID_STATE`, `LZ_PROJECT_ID_DEMO_DEV`), `LZ_ADMIN_CLIENT_ID`, `LZ_ADMIN_POLICY_ID` | bootstrap `identify` and `admin`; you fill in missing project ids |
| `state-passphrase.env` | `TF_VAR_state_passphrase` | bootstrap `passphrase`, once, never overwritten |
| `state.env` | the platform S3 keys for the account state bucket | bootstrap `state` |
| `state/<id>.tfstate` | the encrypted local state of `account-bootstrap` | bootstrap `state` |
| `sets/account-governance/gen-<run-id>/platform-deployer.env` | the platform OAuth2 client | the live lane, after `account-governance` |
| `sets/account-governance/gen-<run-id>/tenants/<t>/deployer.env` | the tenant OAuth2 client | the live lane, after `account-governance` |
| `sets/<tenant-state id>/gen-<run-id>/tenants/<t>/state.env`, `…/tenants/<t>/platform-state.env` | the tenant and platform S3 keys for the tenant bucket | the live lane, after the tenant's `tenant-state` instance |
| `sets/<instance>/current` | the name of the instance's current generation, `gen-<run-id>` | the live lane, with each set |
| `state/probes/<run-id>/` | a probe's encrypted state and per-run passphrase, until its cleanup passes | `lz-live probe` |
| `locks/` | the run locks (`flock`, local to this workstation) | `lz-live` |
| `sandbox.env` | a previous account's admin credential, moved here on migration | bootstrap `identify --fresh-account` |

`<REF>` is a project `ref` from the manifest. In the one-project sandbox, `STATE` and `DEMO_DEV`
hold the same project id. The manifest allows that only with
`spec.sandbox.shared_state_project: true` (KD-1). Any other pair of refs on one project id is
refused before any child process, by the lane (plan, apply, destroy, chain) and by the
bootstrap's `state` phase: `project-alias` (exit 3) names the two refs, never the id. Two tenant
environments never share a project, with or without the flag.

The lane writes each instance's credential files as a set: a new `gen-<run-id>/` directory, made
current by one rename of `current`. Readers use the generation `current` names, so a crash leaves
them on the previous set or the new one, never a mix. The writer keeps the previous generation for
a manual rollback and removes older ones. Files of the earlier flat layout (`platform-deployer.env`,
`tenants/<t>/…` directly under `accounts/<account>/`) are read until the instance's first set
replaces them.

Never copy a value from these files into a commit, a PR, an issue or a chat.

## Bootstrap the account

`task bootstrap:account` brings the account to a bootstrapped state. It runs these phases in
order: `guard`, `identify`, `passphrase`, `admin`, `state`, `publish`, `verify`, and with
`--fresh-account` also `revoke`. Each phase detects work already done and reports `unchanged`,
so a re-run is safe ([FR-012](../../specs/005-first-landing-zone-slice/spec.md#requirements)).
Output lines read `LZ-LIVE <phase> <instance> <status> <detail>`.

### An account that already has `sandbox.env`

```sh
task bootstrap:account -- --reviewed-sha "$S"
```

On the first run, `identify` writes `account.env` and stops with exit 2, naming the missing
`LZ_PROJECT_ID_<REF>` variables. Fill them in by hand: they are project ids, not secrets. Then
run the same command again. `admin` checks that the `sandbox.env` credential works and that the
admin client and policy match what is expected. `state` applies `account-bootstrap`, which
creates the account state bucket or imports an existing one by name, and writes `state.env`.
Every state bucket is created with S3 Object Lock, governance mode, 7 days by default
(`components/state-backend` input `state_retention_days`, spec 005 T099). Object Lock can only be
set when a bucket is created, so an existing bucket without it would have to be replaced
(`terraform-provider-ovh/docs/resources/cloud_project_storage.md:12-14`), which `prevent_destroy`
and the retained-resource guard refuse: such a bucket is not adopted, and a deployment whose state
bucket was applied before this change can no longer plan it. Moving one to a new bucket (new
bucket, state copied, old bucket removed by hand) is an owner decision; none existed when the lock
was introduced (the account bootstrap had not been applied).
When `admin` adopts an admin it did not create (its ids were not yet in `account.env`), it prints
the hand-over once, as `LZ-LIVE admin bootstrap info` lines: the `tofu state rm` commands for
`ovh_me_api_oauth2_client.lz_sandbox_admin` and `ovh_iam_policy.lz_sandbox_admin` (research R13).
Run them in the one-off config's directory (`~/.config/ovh-lz/bootstrap/` on the current
sandbox) after `tofu state list` shows those addresses; they remove the resources from that
state only, not from OVHcloud. Then delete the `terraform.tfstate.*.backup` that `tofu state rm`
writes and any `terraform.tfstate.backup` an earlier apply left: both keep the client secret in
plain text (T044, T102).
`publish` and `verify` follow. A second run must report every phase after `guard` (which always
reports `ran`) as `unchanged` (V009). The bootstrap has no run id and writes no run record: its
`LZ-LIVE` lines are the record, so copy them into the evidence.

`account-bootstrap` belongs to `bootstrap:account` only. The live lane refuses it by name and
leaves it out of `all`.

### A fresh account (migration)

1. In the Control Panel, create the account and one Public Cloud project. Ordering is not
   automated. Whether a trial account lets you create one is UNVERIFIED (P22).
2. Create a short-lived application key, application secret and consumer key on the OVHcloud
   token page (`terraform-provider-ovh/docs/index.md:88`). The bootstrap's calls are listed in
   P23. The narrowest rights that cover them are UNVERIFIED, so do not keep the key beyond this
   run. Root-bound keys bypass IAM (AGENTS.md).
3. Bucket names are global across OVHcloud
   (`ovhcloud-docs/.../object-storage/s3-limitations.mdx:49-53`, P21). If the old account's
   state buckets still exist, set a new `spec.org` in `stacks/deployments.yaml` (default `lz`).
   Then run `task stacks:reconcile` and `task stacks:generate`, commit, get the change reviewed
   and merged, and use the new reviewed SHA.
4. Run:

   ```sh
   task bootstrap:account -- --reviewed-sha "$S" --fresh-account
   ```

   Type the three keys at the no-echo prompt. They are never stored, never passed in argv, and
   never put in a child's environment. The previous account's `sandbox.env` moves to
   `accounts/<old account>/`, and no file of the old account is read. `identify` lists the
   account's projects and asks for each `LZ_PROJECT_ID_<REF>`. In a one-project account, give
   the same id for both. `admin` creates the admin client and policy and writes `sandbox.env`.
   `revoke` revokes the root credential at the end, also when the run fails or you abort it.
5. If the run stopped after the admin was created, re-run without `--fresh-account`. If it
   stopped before that, re-run with the flag and new keys. `--fresh-account` never repairs or
   replaces an existing admin. A working one, as `sandbox.env` names it, is reported
   `unchanged`. One that has drifted, or one the run holds no working credential for, is
   refused (exit 3): fix or delete it by hand first.
6. Run `task bootstrap:account -- --reviewed-sha "$S"` once more. Every phase after `guard`
   must report `unchanged`.

A taken bucket name fails with a message that names `spec.org` as the override.

## Plan and apply

The manifest `stacks/deployments.yaml` defines six stacks. The run order (from
`task stacks:order -- all`) is:

```
account-bootstrap → {account-governance, demo-state} → demo-dev-project → {demo-dev-gra11-network, demo-dev-gra11-runtime}
```

Four stacks are **retained**: `account-bootstrap`, `account-governance`, `demo-state` and
`demo-dev-project`. Two are **ephemeral**: `demo-dev-gra11-network` and
`demo-dev-gra11-runtime` (see the
[stage table](../../specs/005-first-landing-zone-slice/data-model.md#stage-table-fixed-in-code-toolsinternalstacksstagesgo)).
Each stack runs under its own authority (bootstrap, platform or tenant credential).

```sh
task live:plan  -- --reviewed-sha "$S" all                       # every selected stack except account-bootstrap
task live:apply -- --reviewed-sha "$S" all                       # plan, judge, apply in run order, publish outputs
task live:apply -- --reviewed-sha "$S" demo-dev-gra11-runtime    # one instance
```

- Every plan is saved to a file and passed through the retained-resource guard. Only that file
  is applied. A plan that deletes or replaces a resource of a retained instance is refused
  (exit 3).
- Under `plan -- all`, a stack that consumes a selected producer is reported
  `blocked-on=<producer>` and is not planned. `apply -- all` applies the producer first.
  `plan -- <consumer>` refuses while its producer is selected.
- After `account-governance` and `tenant-state` apply, the lane writes the deployer credential
  files listed above.
- `plan`, `apply` and `destroy` run under the default deadline (45 min; they take no
  `--deadline`). `apply` appends every created resource to the run's `inventory.jsonl` as it is
  created. Each ends with `LZ_RUN <run-id> <verb> <outcome>` (`apply` adds `inventory=<path>`);
  `apply` and `destroy` add the cost reminder. At the deadline or on SIGINT the verb stops the
  running `tofu`, keeps the inventory, destroys nothing and exits non-zero (spec 006 FR-001).
  The inventory lists created resources only, not those an apply updated or deleted. A failure
  before the run starts (no `sandbox.env`, the account lookup) prints no `LZ_RUN` line: there is
  no run id yet. Use `chain` when you want a run that cleans up after itself.

For an offline approximation of the next selection, run `task stacks:order -- all` in the clone.
It is credential-free and reads the records under `.local/live/records`. It compares code
digests and the recorded digests only. The published artefacts and resolved project references
are compared by the live lane alone, so `live:plan` can select more.

## Run the chain

The chain is the slice's end-to-end check (V010, SC-003):

```sh
task live:chain -- --reviewed-sha "$S" all            # default deadline 45m
```

It does this:

1. Binds the admin credential through `GET /auth/details`.
2. Lists every leftover kind as a baseline. If a baseline listing fails, the run stops before
   any apply with outcome `blocked` (exit 2), and nothing is spent. Run it again later.
3. Applies the five lane stacks in run order (`account-bootstrap` comes from the bootstrap).
   After each apply it reads that stack's state object from its bucket with the stack's own S3
   keys and fails the stage when the object is missing, the read is refused, or the state is
   plaintext rather than the OpenTofu encryption envelope; the error names the key and prints
   nothing of the body (T102).
4. Collects and judges the eleven L7 assertions: `bucket-tags`, `deployer-binding`, `kd1-canary`,
   `outputs-schema`, `project-tags`, `state-bucket-object-lock`, `state-bucket-versioned`,
   `state-lock-contention`, `state-object-encrypted`, `tenant-iam-write`, `tenant-s3-read`.
   `state-bucket-object-lock` (spec 006 FR-010) reads each state bucket's Object Lock
   (`GetObjectLockConfiguration`) with the S3 keys of the first row whose state lives there: it
   holds for an enabled lock in governance mode with a positive default retention; a read that
   fails makes the assertion `blocked` (never `pass`; `fail` next to a violated bucket). The state
   buckets' S3 users are granted `s3:GetBucketObjectLockConfiguration` on their bucket
   (`modules/object-storage-user`, spec 006 T048); users deployed before that get it only when the
   bootstrap and tenant-state stacks are re-applied, and until then the read is refused and the
   assertion `blocked`. That this action authorises the read on OVHcloud is UNVERIFIED until a
   chain reads `holds`.
5. Destroys the two ephemeral stacks in reverse order, each through a saved `plan -destroy`.
   The destroy also fires when an apply fails, on SIGINT or SIGTERM, and at the deadline.
6. Runs the trap (the KD-1 canary bucket, the lock object, a wrongly created policy).
7. Runs the leftover check over the full kind matrix.

The run prints one `LZ-LIVE assert <assertion> <outcome> [<deviation>]` line per assertion, then
`LZ-LIVE summary <run-id> <outcome> known-deviations=<ids|none>` and a reminder to record the
cost. In the sandbox, expect `kd1-canary known-deviation KD-1`. That outcome is recorded, not
counted as `pass`, and it does not change the exit code.

The chain's destroy-on-exit and final check get their own time budget, counted from the
destroy's start: `--deadline` or the default 45 min, whichever is longer, so a short
`--deadline` no longer shortens the destroy (spec 006 FR-002). A destroy-on-exit that runs out
of that budget fails the run; what it did not destroy keeps its inventory and state for `--cleanup`. `chain -- <instance>` applies one instance and records every
assertion as `not-run`.

Exit codes for every `lz-live` verb:

| Exit | Meaning |
| --- | --- |
| 0 | pass, also with recorded known deviations |
| 1 | fail |
| 2 | blocked (a missing prerequisite or producer artefact, or a failed baseline) |
| 3 | refused (guard, lock, binding, `project-alias`, retained-resource protection, `consumer-applied`, a drifted or unusable admin under `--fresh-account`) |

## Destroy

```sh
task live:destroy -- --reviewed-sha "$S" demo-dev-gra11-runtime
task live:destroy -- --reviewed-sha "$S" demo-dev-gra11-network
```

`destroy` takes one ephemeral instance. It is a saved `plan -destroy`, checked by the guard
before that file is applied. It refuses (exit 3):

- a retained instance, and `all`;
- a producer while a stack that consumes it is applied (`consumer-applied`).

**No verb tears down the retained instances.** The state buckets and the project carry
`prevent_destroy`, and every verb refuses to delete them. Removing them is a manual Control
Panel task outside this guide. A state bucket's object versions are locked in governance mode for
its retention (7 days by default): until then only a credential holding
`s3:BypassGovernanceRetention`, sending the `x-amz-bypass-governance-retention` header, can delete
them; the state users cannot. No such credential is provisioned for the state buckets (only the
probe's janitor holds one, for its own bucket): create a one-off S3 user with that permission on
the bucket, and remove it afterwards, or wait for the retention to end.

## Read a run record

Every `live:*` run writes `.local/live/<run-id>/` in the clone (gitignored). The run id has the
form `YYYYMMDDThhmmssZ-<4 hex>` and is printed first (`LZ-LIVE run <run-id> start …`). Field by
field, the record is described in the
[data-model](../../specs/005-first-landing-zone-slice/data-model.md#live-run-record-gitignored-locallive).
What a run writes depends on the verb:

- `chain` writes every file below but `run.json`.
- `plan`, `apply` and `destroy` write the rendered plans, the inputs and `run.json`; `apply`
  also `inventory.jsonl`. No summary.json or leftover report.
- `probe` writes its own set (see the
  [probe run sheet](../../tests/live/probes/README.md#before-a-session)).
- `bootstrap:account` writes no run directory.

| File | Read it for |
| --- | --- |
| `run.json` | `plan`, `apply`, `destroy` only: `run_id`, `verb`, `instances`, `deadline_seconds`, `outcome` (`pass`, `fail`, `blocked`), `reviewed_sha` |
| `summary.json` | `outcome` (`pass`, `fail`, `blocked`), `deadline`, `known_deviations` (e.g. `["KD-1"]`), `assertions` (one `{assertion, outcome, deviation, detail}` each, `not-run` with a reason when the chain did not judge it) |
| `leftovers.json` | the leftover check: every leftover and every listing error; `before_run` marks what the baseline already listed |
| `baseline.json`, `listings-before/<kind>.json` | the chain's listings before its first apply |
| `listings/<kind>.json` | the final listings, one file per kind |
| `observations.json` | the raw L7 observations and the subjects the manifest requires (`chain -- all` only) |
| `inventory.jsonl` | one line per created resource, appended as it was created |
| `plan-<id>.txt` | the rendered plan per stack (no raw plan JSON) |
| `inputs/<id>/*.tfvars.json` | the producer outputs and resolved references each consumer received |

`.local/live/records/<id>.json` holds one record per applied stack: `applied_at`,
`source_revision`, `code_digest`, the consumed producer digests and `resolved`. The next run's
selection reads these records. Removing one makes that stack look as if it had never been applied.

Records may hold ids and the account e-mail. Copy the run id, the outcome, the assertion
outcomes and the known deviations into the evidence or PR, never the record itself.

## Clean up and check for leftovers

- The chain cleans up after itself: ephemeral destroy, trap, then the leftover check. A
  leftover or listing error makes the run `fail`. A listing request (an array page, a
  single-object read or the token request) that answers 429 or 5xx, or fails in transport, is
  retried at most twice, 5 s and 15 s later, at the page it failed on, each retry printing one
  `LZ-LIVE retry listing <path> in <wait>: <status or class> (attempt <n> of 3)` line; any other
  failure, and every 4xx of the token endpoint (429 included), is tried once, and a listing that
  still fails is a listing error. The token request counts its attempts once for the whole check. Every failed attempt
  is in `leftovers.json` under `attempts` (spec 007 FR-001, FR-002). The baseline listing before
  the first apply is not retried. The read of a retained instance's state is retried too: after
  a 5xx, a reset or a timed-out connection, at most twice, 10 s and 30 s later, each retry
  printing one `LZ-LIVE retry state read <instance>` line; a 4xx or a read that still fails is a
  state error in `leftovers.json` (T110). The retained reads and the final listings share one
  retry budget, the destroy-on-exit's time: a wait is taken only if the time for a read and the
  final listing (at least 30 s, more after a slower read, listing request or baseline listing) is
  still left after it.
  A refused wait records `blocked waiting-for=<instance>` in `leftovers.json`; when refused waits
  are its only errors the outcome is `blocked`, otherwise `fail`, and the run does not pass
  (spec 006 FR-008). A `blocked` report can still list real leftovers next to the unread
  instance's objects: check each one by hand before you re-run anything. No verb runs the leftover check on its
  own. Before you conclude that something was left behind, read `leftovers.json` and look by
  hand (below).
- If an ephemeral destroy failed, run `task live:destroy` for that instance in the same session.
- If a probe's destroy failed, run
  `task live:probe -- --reviewed-sha "$S" --cleanup <run-id>`. Its state and passphrase under
  `accounts/<account>/state/probes/<run-id>/` are deleted only when the destroy and the
  leftover check both pass. Never delete them by hand while a resource may exist.
- A two-stage probe (`storage-iam`) whose companion could not be destroyed keeps its root, and
  with it the identity the companion's resources need; the run's error names the kept root and
  the `--cleanup <run-id>` to run, and the leftover check reports both as leftovers until then
  ([probe run sheet](../../tests/live/probes/README.md#two-stage-runs-t010)). A failed tofu call's
  output is in the run record's `errors.txt`.
- The `state-backend` probe's Deny check writes `observations.json` to the run record and empties
  its bucket (locked for a day, emptied with the governance bypass) before the destroy; if it cannot, the root is kept and the error names the
  `--cleanup <run-id>` that empties the bucket and destroys the root (row 7b of the
  [probe run sheet](../../tests/live/probes/README.md#runs)).
- The `storage-iam` probe's check (T100) writes `observations.json` with five observations (P25
  reads, `GET /me`, an IAM write, the companion's state object read back) and fails the run on any
  answer other than the expected one, naming the step and the refused IAM actions; before the
  destroy its undo deletes the IAM write's policy should OVHcloud have accepted it, and keeps the
  root if it cannot (rows 8b–8d of the
  [probe run sheet](../../tests/live/probes/README.md#two-stage-runs-t010)).
- To look by hand (read only), use the API console or the `ovhcloud` subcommand of the kind,
  e.g. `ovhcloud cloud project list` (AGENTS.md *Tools*). Per-kind CLI coverage is UNVERIFIED,
  and there is no generic `ovhcloud api get` (P18 refuted, evidence T009). The kinds and API
  paths are listed in the
  [probe run sheet](../../tests/live/probes/README.md#leftover-listing-per-matrix-kind).
- The KD-1 canary is registered with the trap before its create call (spec 006 FR-009), so a
  create that answers an error after making the bucket still ends with the trap removing it. If
  that removal fails, the run fails naming the canary and the leftover check lists it (`lz-`
  prefix); delete it by hand. The run reads the name first and removes only a bucket the read
  found absent. A name the read found is never adopted or removed: `kd1-canary` is `blocked` or
  `fail`, and the run fails naming the bucket while it exists; check it by hand. A create
  answered 409 means the name existed before: nothing is removed and `kd1-canary` is `blocked`.
  A `blocked` assertion still fails the run (exit 1), as any outcome other than `pass` or
  `known-deviation` does; only the assertion is `blocked`. Two narrow windows remain (evidence T025): another creator between the read and a create that
  answers 5xx would have its bucket removed, and a trap that runs while the create is in flight
  (an interrupt) can miss a bucket the create then makes; the leftover check lists it.
- One kind of debris is known not to be cleaned up (evidence T062, gap 7): a delete marker and a
  noncurrent version of `<key>.tflock` in the versioned state bucket, left by every L7 run.
  Whether OVHcloud keeps them is UNVERIFIED.
- The leftover check does not catch everything:
  - A resource created during a run without the `lz-` prefix, and missed by the inventory, is
    not found (evidence T047, gap 4).
  - An IAM policy still bound to a deleted admin client is not reported (evidence T043, gap 3).
  - The rule that skips regions without a bucket service is qualified on one project's regions
    only (evidence T087, gap 1).

## Keep the cost down

The trial credit is one-off (AGENTS.md *Sandbox budget*). Spend it only on what a live run can
show.

- Prefer `--plan-only` probes and `task live:plan` over applies.
- Use `chain` rather than a bare `apply` for anything ephemeral: it destroys on exit; a bare
  `apply` stopped by its deadline keeps what it created.
- Run the full chain before a release, and after a change to IAM, state or credentials. Do not
  run it on every PR.
- Leave `budget_alert` and `quota_guard` off in the manifest unless you want them. Switching
  them on applies live resources. P10 (alerting) and P11 (quota) only planned cleanly; their
  applies are UNVERIFIED.
- After each session, record the approximate cost from the Control Panel billing view (exact
  menu path UNVERIFIED) or `ovhcloud`, next to the run id in the PR.
- A private network, a subnet and empty buckets are believed to cost close to nothing. That is
  UNVERIFIED: P12 (`ovhcloud-docs/.../network-services/vrack.mdx:196,201` covers only the
  vRack) and P13. Check the bill after the first chain.

## What is verified, deviations and limits

What has and has not been observed on OVHcloud, the known deviations (KD-n) and what this slice
leaves out are in the [verification status](../reference/verification-status.md).
