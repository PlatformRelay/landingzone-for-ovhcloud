---
references:
  - path: specs/005-first-landing-zone-slice/spec.md
    blob: 65c077ab0062b7ec8fe0bb8fba842f0eed28c22c
  - path: tools/cmd/lz-offline/limits.go
    blob: 90ec35fdb1b1fd535e669e29280b92717d1debfb
  - path: tools/internal/live/apply.go
    blob: 8960463e6b9aa9b86917f237aa969ab2cfda9ae3
  - path: tools/internal/live/bootstrap.go
    blob: 60d561756d64e3d1b45349988d4856f5406b7b2f
  - path: tools/internal/live/chain.go
    blob: c10e5cb1cbd24317bf1508e70bffeb990a7dc92a
  - path: tools/internal/live/deadline.go
    blob: e0c5da9228a55023f61fecec87842e5d994425d0
  - path: tools/internal/live/observe.go
    blob: 7febdec79596d9781a5bec211fc7ca9840b35fc9
last_verified: 34dff5ea7e0946853752335b99c4f59137f309f9
---

# Verification status of the first slice

What the first landing-zone slice
([spec 005](../../specs/005-first-landing-zone-slice/spec.md)) has and has not shown on a real
OVHcloud account, what [spec 006](../../specs/006-harden-first-slice/spec.md) hardened since, the
design deviations the slice accepts, and what it leaves out. To run the slice, follow the
[runbook](../how-to/run-the-first-slice.md). Terms such as Pn, KD-n and Tnnn are in the
[glossary](glossary.md). Task ids below are spec 005's unless a spec is named.

## What has been observed live

- **The chain, end to end (spec 005 T049 run 4, 2026-10-13, `pass known-deviations=KD-1`):**
  `account-governance`, `demo-dev-project`, `demo-dev-gra11-network` and
  `demo-dev-gra11-runtime` applied in run order, each with its own authority; each passed the
  state-object check (T102). Every assertion passed (`bucket-tags`, `deployer-binding`,
  `outputs-schema`, `project-tags`, `state-bucket-versioned`, `state-lock-contention`,
  `state-object-encrypted`, `tenant-iam-write`, `tenant-s3-read`) except `kd1-canary`, which
  reported `known-deviation KD-1` as expected. The ephemeral stacks were destroyed in reverse
  order, the leftover check found nothing and no listing failed, all in about 3.5 minutes. Source:
  the run table in [T049's evidence](../../specs/005-first-landing-zone-slice/evidence/T049.md).
- **P1–P3, the state backend:** first on a probe bucket (T010 runs 14 and 15), then on lane
  stacks' state in run 4: a second writer is refused with `Error acquiring the state lock`, and
  the state object is the OpenTofu encryption envelope.
- **Project tags (T103, T104, T105):** runs 1–3 showed that the platform deployer cannot tag a
  project (403 `missing IAM permissions: account:apiovh:iam/resource/edit`, also when that action
  was granted on the project URN; 400 `unknown action: "account:apiovh:iam/resource/get"`). So
  `account-governance`, under the admin, sets each tenant project's tags, and the platform
  deployer is back to `publicCloudProject:apiovh:*` alone (D109). In run 4 the admin's tag create
  and the read-back (`project-tags`) passed, and the platform policy was narrowed in place.
- **The IAM and bucket reads the collector makes:** the binding, IAM write, versioning and tag
  reads behind the run-4 assertions above (T048, T062) answered as the fakes assume.
- Earlier: the host guard, child environment and admin binding through `GET /auth/details` (P26
  admin half); P5 refuted, so the sandbox project is referenced, not adopted; the API-client
  leftover listing (T009, T084, T087); the create→destroy probes for P1–P3, P7–P10, P12, P15 and
  the P26 tenant half (T010); and the account bootstrap on the existing account: state bucket with
  Object Lock, `state.env`, publish, and a second run with every phase after `guard` `unchanged`
  (T044).
- **P25 refuted** (T010, D107): a tag condition on a project-URN policy does not scope bucket
  reads. Per-tenant bucket envelopes rely on one project per tenant, not on tags.

## What is still unverified

Coded against fakes, recorded fixtures or provider docs, and not observed on OVHcloud:

- **Bootstrap, fresh account (T045; P22, P23):** never run. The existing-account path ran twice
  (T044). The fakes alone encode:
  - the 409 text of a taken bucket name (T057);
  - the `/auth/time` offset, root-key revocation through `GET /auth/currentCredential`, and
    created-client propagation (T043);
  - `identify` creating a binding from scratch (T044 found the binding already in place).
- **The chain, beyond run 4:**
  - all five chain stacks in one run: `demo-state` was applied in run 1 and left unchanged in
    run 4, so a run applying all five needs a fresh account (T045);
  - destroy after a failed assertion: no live run has had one fail (T047 and T048 cover it
    offline);
  - the lock's conditional create (`If-None-Match`): its effect, the refused second writer, was
    observed; the request itself was not captured;
  - the Object Lock read the chain added in spec 006 (`state-bucket-object-lock`, T026:
    `GetObjectLockConfiguration`, its answer and not-found code) and whether the state buckets'
    S3 users may send it: no chain has run since.
- **P13 and P14:** a bucket's cost, and the destroy of a bucket that still holds noncurrent
  versions, stay open: the T010 probes emptied the bucket first.
- **Leftover listing:** v1 pagination is UNVERIFIED, and so are the kinds that a plan-only run
  did not list (subnets, S3 credentials and policies, alerts). Six kinds were captured live
  (T084).
- **Everything spec 006 changed in the live lane** (below) runs against fakes only; no live run
  has used it yet.

## What spec 006 hardened

Each item is shown by offline tests through the entry (spec 006
[V001–V010](../../specs/006-harden-first-slice/spec.md#acceptance-and-predefined-verification)),
not by a live run, unless it says otherwise.

- **Secrets stay out of errors and records (US1):** account-binding errors name the call, the
  status class and the next action, never a header, body, redirect URL or credential (FR-003);
  the leftover check's bearer token is registered with the run's redactor before the first
  listing (FR-004); deployer credential files are written as one set into a fresh generation and
  made current by one pointer switch (FR-005); a probe's companion variables pass an allow-list
  (FR-006); no child of `lz-live` inherits the caller's `HOME` (FR-007).
- **Bounded live verbs (US2):** `lz-live plan`, `apply` and `destroy` run under
  `DefaultDeadline` (45 min; they take no `--deadline`), print the summary line, and `apply`
  appends to the run's inventory as each `apply_complete` event arrives (FR-001,
  `tools/internal/live/apply.go`). A state-store request of these verbs ends at the run's deadline,
  and no new write or record starts after it. Destroy-on-exit, in the chain and the probe core, gets
  max(`DefaultDeadline`, the run's deadline), detached from the run's cancellation; a destroy that
  runs out reports `fail` and leaves what it did not reach for `--cleanup` (FR-002,
  `tools/internal/live/deadline.go`). One leftover check shares one retry budget (FR-008); the
  KD-1 canary is registered for removal before its create (FR-009); the chain asserts Object Lock
  on each state bucket (FR-010).
- **Entry limits, Go preparation and provenance (US3):** `lz-offline` runs each child in a
  systemd user scope with `MemoryMax` 4 GiB, `TasksMax` 1024 and swap off, and refuses a host
  where they do not apply (FR-013, `tools/cmd/lz-offline/limits.go`). The runtime bundle is
  prepared in Go by `lz-pack prepare` from one pin table, with the OpenTofu and Terramate binaries taken from their
  verified release archives (FR-014); `lz-pack` writes an unsigned provenance statement beside
  each image (FR-017). Runtime r5 carries all three and is pinned in CI (PR 135). Observed on
  GitHub's hosted runner: a probe (run 39249394208) stopped an allocation past the memory limit
  and a fork storm past the task limit, and the first foundation run on r5 (run 39252705025) was
  green on all 18 targets under the limits. The recorded foundation runs were refreshed for r5
  (PR 140). Behaviour on a host with free swap is not observed
  ([KI-002](../known-issues/KI-002-offline-entry-memory-process-limits.md)).
- **Stage locks and registry reads (US4):** `stacks:generate` refuses unreconciled rows
  (`UNRECONCILED_ROW`) and copies the stage lock into new stacks; `stacks:check` reports stages
  whose locks differ (`STAGE_LOCK_MISMATCH`) (FR-011). Each registered check's inputs cover the
  Go packages it depends on (FR-012).
- **Update lane and release manifest (US5):** `.github/renovate.json` covers every pin source,
  with an offline check and a committed provider schema snapshot (FR-015); whether Renovate reads
  it as the check assumes is UNVERIFIED until the app is installed (owner, C5). `lz-release
  manifest` renders the release-train manifest byte-identically and against
  `schemas/release-manifest.schema.json`; it creates no tag (FR-016).
- **Docs gates (US6):** `docs:check` fails a page whose `verified_against` names a tool more than
  two minor versions behind its pin, a how-to or reference page that names a tool command without
  that stamp, a link to a superseded ADR (FR-018), and a repository map that names a missing path
  or omits an admitted top-level directory (FR-019).

## Known deviations

Listed in [spec *Known deviations*](../../specs/005-first-landing-zone-slice/spec.md#known-deviations).
A run reports them; it does not hide them.

- **KD-1: state shares the tenant's project.** The sandbox has one Public Cloud project. A tenant
  deployer's API authority there includes `region/storage/delete` and `bulkDeleteObjects`
  (`kb/api/v1/cloud.json:58903,59095`), so it can reach the state buckets through the
  management API. Its S3 keys cannot. `kd1-canary` shows this on a disposable canary bucket and
  reports `known-deviation KD-1` (observed in run 4). Without `shared_state_project: true`, the
  same observation is a `fail`. **Tenant isolation of state is not demonstrated.** A separate
  state project lifts it.
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
the S3 state lockfile (P2): run 4 showed it refusing a second writer, but no run has come from
two workstations.
