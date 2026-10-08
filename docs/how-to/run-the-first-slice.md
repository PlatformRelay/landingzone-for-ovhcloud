---
references:
  - path: tools/cmd/lz-live/main.go
    blob: 0860b1daf0dc6ebcc1c98a0d575534fea7ee901a
  - path: tools/cmd/lz-live/lane.go
    blob: ae27bc8fa62bac408849ebe20559f77d71a5605d
  - path: tools/internal/live/apply.go
    blob: c658bea8402ce8b9259db0f2f71e6fcee81c9ba0
  - path: tools/internal/live/bootstrap.go
    blob: 7819586182b0b57b2fecc2af6aa96edc2305f46e
  - path: tools/internal/live/chain.go
    blob: aa54c58f0e04e8bb9d23c2ed7ed91b3c33f63a91
  - path: tools/internal/live/observe.go
    blob: 8aa5fd34a16cecafed7b4c3a0f6047a025158b55
  - path: stacks/deployments.yaml
    blob: a4e4c7b24494733e88b0fb9e4d3baa3fa8b2d132
  - path: mise.toml
    blob: 2d63ee3ca1e7bb809bed04e4575609ffd4656542
  - path: tests/live/probes/README.md
    blob: 01807dbfce2ac559d2573c4735cccec0cea9bf4d
last_verified: 4817d011585ccad9d42c42eac27470a910148111
---

# Run the first slice on an OVHcloud account

This guide is for the maintainer who runs the first landing-zone slice
([spec 005](../../specs/005-first-landing-zone-slice/spec.md)) against a real OVHcloud account:
bootstrap the account, apply and destroy the stacks, read what a run recorded, and clean up.
Everything here acts on the account and spends credit. **Only the owner runs it, on the
maintainer workstation.** Agents and CI never run `task bootstrap:*` or `task live:*`.

**Status (2026-10-08).** The commands below exist and are tested offline against fakes. Against a
real account only plan-only probes have run so far
([T009](../../specs/005-first-landing-zone-slice/evidence/T009.md),
[T087](../../specs/005-first-landing-zone-slice/evidence/T087.md)). The bootstrap, apply, chain
and destroy runs are still open (owner tasks T010, T044, T045, T049). Expect the first real runs
to find what the fakes could not. See [What is still unverified](#what-is-still-unverified).

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
`bootstrap:account` also needs `tofu` on `PATH`.

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
needed, and whatever it exports is ignored. The layout
([data-model](../../specs/005-first-landing-zone-slice/data-model.md#account-binding-and-local-files-never-in-the-repository))
has two files at the top level:

| File | Variables | Written by |
| --- | --- | --- |
| `live.env` | `LZ_OWNER_CHECKOUT` (canonical absolute path of the owner clone), `LZ_AGENT_WORKTREE_ROOT` (absolute path of an existing directory that holds the agent worktrees) | you, once |
| `sandbox.env` | `OVH_ENDPOINT`, `OVH_CLIENT_ID`, `OVH_CLIENT_SECRET` of the account's admin service account `lz-sandbox-admin` | existing, or `bootstrap:account --fresh-account` |

The rest sits under `accounts/<account>/`, where `<account>` is the account that
`GET /auth/details` names for the credential:

| File under `accounts/<account>/` | Variables | Written by |
| --- | --- | --- |
| `account.env` | `LZ_ACCOUNT_ID`, `OVH_ENDPOINT`, `LZ_ORG`, `LZ_PROJECT_ID_<REF>` for each project reference in `stacks/deployments.yaml` (today `LZ_PROJECT_ID_STATE`, `LZ_PROJECT_ID_DEMO_DEV`), `LZ_ADMIN_CLIENT_ID`, `LZ_ADMIN_POLICY_ID` | bootstrap `identify` and `admin`; you fill in missing project ids |
| `state-passphrase.env` | `TF_VAR_state_passphrase` | bootstrap `passphrase`, once, never overwritten |
| `state.env` | the platform S3 keys for the account state bucket | bootstrap `state` |
| `state/<id>.tfstate` | the encrypted local state of `account-bootstrap` | bootstrap `state` |
| `platform-deployer.env` | the platform OAuth2 client | the live lane, after `account-governance` |
| `tenants/<t>/deployer.env` | the tenant OAuth2 client | the live lane, after `account-governance` |
| `tenants/<t>/state.env`, `tenants/<t>/platform-state.env` | the tenant and platform S3 keys for the tenant bucket | the live lane, after `tenant-state` |
| `state/probes/<run-id>/` | a probe's encrypted state and per-run passphrase, until its cleanup passes | `lz-live probe` |
| `locks/` | the run locks (`flock`, local to this workstation) | `lz-live` |
| `sandbox.env` | a previous account's admin credential, moved here on migration | bootstrap `identify --fresh-account` |

`<REF>` is a project `ref` from the manifest. In the one-project sandbox, `STATE` and `DEMO_DEV`
hold the same project id. The manifest allows that only with
`spec.sandbox.shared_state_project: true` (KD-1).

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
- `plan`, `apply` and `destroy` have no run deadline and no inventory of what they create, and
  print no summary line (evidence T059 gap 2, T047 gap 10). Use `chain` when you want a bounded
  run that cleans up after itself.

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
4. Collects and judges the ten L7 assertions: `bucket-tags`, `deployer-binding`, `kd1-canary`,
   `outputs-schema`, `project-tags`, `state-bucket-versioned`, `state-lock-contention`,
   `state-object-encrypted`, `tenant-iam-write`, `tenant-s3-read`.
5. Destroys the two ephemeral stacks in reverse order, each through a saved `plan -destroy`.
   The destroy also fires when an apply fails, on SIGINT or SIGTERM, and at the deadline.
6. Runs the trap (the KD-1 canary bucket, the lock object, a wrongly created policy).
7. Runs the leftover check over the full kind matrix.

The run prints one `LZ-LIVE assert <assertion> <outcome> [<deviation>]` line per assertion, then
`LZ-LIVE summary <run-id> <outcome> known-deviations=<ids|none>` and a reminder to record the
cost. In the sandbox, expect `kd1-canary known-deviation KD-1`. That outcome is recorded, not
counted as `pass`, and it does not change the exit code.

The chain's destroy-on-exit and final check get their own time budget, equal to `--deadline`
(evidence T047, gap 7). Do not shorten the deadline below what a destroy needs. `chain -- <instance>` applies one instance and records every
assertion as `not-run`.

Exit codes for every `lz-live` verb:

| Exit | Meaning |
| --- | --- |
| 0 | pass, also with recorded known deviations |
| 1 | fail |
| 2 | blocked (a missing prerequisite or producer artefact, or a failed baseline) |
| 3 | refused (guard, lock, binding, retained-resource protection, `consumer-applied`, a drifted or unusable admin under `--fresh-account`) |

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
Panel task outside this guide.

## Read a run record

Every `live:*` run writes `.local/live/<run-id>/` in the clone (gitignored). The run id has the
form `YYYYMMDDThhmmssZ-<4 hex>` and is printed first (`LZ-LIVE run <run-id> start …`). Field by
field, the record is described in the
[data-model](../../specs/005-first-landing-zone-slice/data-model.md#live-run-record-gitignored-locallive).
What a run writes depends on the verb:

- `chain` writes every file below.
- `plan`, `apply` and `destroy` write only the rendered plans and the inputs: no summary,
  inventory or leftover report.
- `probe` writes its own set (see the
  [probe run sheet](../../tests/live/probes/README.md#before-a-session)).
- `bootstrap:account` writes no run directory.

| File | Read it for |
| --- | --- |
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
  leftover or listing error makes the run `fail`. The check fails closed on transient 5xx and
  transport errors too (evidence T087). No verb runs the leftover check on its own. Before you
  conclude that something was left behind, read `leftovers.json` and look by hand (below).
- If an ephemeral destroy failed, run `task live:destroy` for that instance in the same session.
- If a probe's destroy failed, run
  `task live:probe -- --reviewed-sha "$S" --cleanup <run-id>`. Its state and passphrase under
  `accounts/<account>/state/probes/<run-id>/` are deleted only when the destroy and the
  leftover check both pass. Never delete them by hand while a resource may exist.
- To look by hand (read only), use the API console or the `ovhcloud` subcommand of the kind,
  e.g. `ovhcloud cloud project list` (AGENTS.md *Tools*). Per-kind CLI coverage is UNVERIFIED,
  and there is no generic `ovhcloud api get` (P18 refuted, evidence T009). The kinds and API
  paths are listed in the
  [probe run sheet](../../tests/live/probes/README.md#leftover-listing-per-matrix-kind).
- Two kinds of debris are known not to be cleaned up (evidence T062, gaps 6 and 7):
  - a KD-1 canary whose create answered an error after the bucket was created. The leftover
    check lists it (`lz-` prefix); delete it by hand.
  - a delete marker and a noncurrent version of `<key>.tflock` in the versioned state bucket,
    left by every L7 run. Whether OVHcloud keeps them is UNVERIFIED.
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
- Use `chain` rather than a bare `apply` for anything ephemeral: it has a deadline and
  destroy-on-exit.
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

## What is still unverified

Every behaviour below is coded against fakes, recorded fixtures or provider docs. None has been
observed on OVHcloud. The source of record is the spec's
[premises table](../../specs/005-first-landing-zone-slice/spec.md#premises) and each task's
evidence gaps.

- **P1–P3, the state backend.** These cover the S3 backend with `use_lockfile`, the OVH endpoint
  flags with OpenTofu 1.13, and client-side state encryption on OVHcloud Object Storage. Owed by
  T010. Until then, every S3-backed stack is unproven live, and so is the lock round-trip in
  bootstrap `verify`.
- **T010 premises:** P7, P8 live, P9, P12–P15, the P26 tenant half, and the optional P25.
  - P7: the admin can create clients and policies.
  - P8: the identity URN works in a policy.
  - P9: the tenant allowlist is enough for network and runtime, and denies IAM writes.
  - P14: bucket destroy also removes noncurrent versions.
  - P15: tag keys containing `:` are accepted.
- **Bootstrap (T044, T045; P22, P23):** never run against an account. The fakes alone encode:
  - S3 path-style SigV4 and 404-for-missing on the first publish (T089);
  - the 409 text of a taken bucket name (T057);
  - the `/auth/time` offset, root-key revocation through `GET /auth/currentCredential`, and
    created-client propagation (T043).
- **Chain (T049):** never run. These are UNVERIFIED:
  - the lock object's conditional create (`If-None-Match`);
  - the lock-refusal text;
  - the versioning and tag fields;
  - the IAM routes the collector reads (T062).

  T048 and T062 pin the assertions only against fakes.
- **Leftover listing:** v1 pagination is UNVERIFIED, and so are the kinds that a plan-only run
  did not list (subnets, S3 credentials and policies, alerts). Six kinds were captured live
  (T084).
- **Plan, apply, destroy:** these have no deadline or inventory (T059). The chain's
  destroy-on-exit gets a budget equal to the run deadline (T047, gap 7).

What has been observed live: the host guard, child environment and admin binding through
`GET /auth/details` (P26 admin half); P5 refuted, so the sandbox project is referenced, not
adopted; P10 and P11 planned; and the API-client leftover listing passing on a plan-only run
(T009, T084, T087).

## Known deviations

Listed in [spec *Known deviations*](../../specs/005-first-landing-zone-slice/spec.md#known-deviations).
A run reports them; it does not hide them.

- **KD-1: state shares the tenant's project.** The sandbox has one Public Cloud project. A tenant
  deployer's API authority there includes `region/storage/delete` and `bulkDeleteObjects`
  (`kb/api/v1/cloud.json:58903,59095`), so it can reach the state buckets through the
  management API. Its S3 keys cannot. `kd1-canary` shows this on a disposable canary bucket and
  reports `known-deviation KD-1`. Without `shared_state_project: true`, the same observation is
  a `fail`. **Tenant isolation of state is not demonstrated.** A separate state project lifts it.
- **KD-2: no pipeline lane.** Live runs start from the maintainer workstation, not a protected
  pipeline (ADR-0019; AGENTS.md *Known deviation*). The host guard stands in.
- **KD-3: `project` state is in the tenant bucket.** The platform-owned `project` stack keeps its
  state and outputs in the tenant's bucket, which the tenant S3 user can write. The adapter
  refuses a `project` artefact for another project id, but tampering with the state object is
  not detected.

## Not in this slice

Postponed, each with its trigger, in
[spec *Out of scope / postponed*](../../specs/005-first-landing-zone-slice/spec.md#out-of-scope--postponed-with-trigger):

- no cost ledger, spend gate or reaper (cost guards are hygiene only);
- no separate state project (KD-1);
- no artefact generations or fencing;
- no rename or retirement (`UNSUPPORTED_CHANGE`);
- no OKMS key, escrow or backup;
- no admin service account in OpenTofu state;
- no GitLab, no TACO, no tenant self-service;
- no pipeline-run live lanes;
- no `account-fabric` or `observability` stacks;
- no narrower `onboarding` authority for `tenant-state` and `account-governance`;
- no scanner, generated docs or compliance profiles;
- no guided preconfiguration wizard (spec 004);
- no profiles, golden paths or catalogue;
- not the full ADR-0019 agent-experience scope.

The run locks exclude runs on this workstation only. A second workstation is serialised only by
the S3 state lockfile (P2, UNVERIFIED).
