---
references:
  - path: specs/005-first-landing-zone-slice/spec.md
    blob: 88d7443d103e6b517fe9e8b7809eda3f609b534f
  - path: tools/internal/live/bootstrap.go
    blob: 60d561756d64e3d1b45349988d4856f5406b7b2f
  - path: tools/internal/live/chain.go
    blob: 25c706686fc6c9e49961cda7fdd60bb02682fd06
  - path: tools/internal/live/observe.go
    blob: 34d48bfd49a54f3dd62df5e8b45a4309d0ff81f7
last_verified: f39dbf533e4060143d227791fd8a14557fd8cbfa
---

# Verification status of the first slice

What the first landing-zone slice
([spec 005](../../specs/005-first-landing-zone-slice/spec.md)) has and has not shown on a real
OVHcloud account, the design deviations it accepts, and what it leaves out. To run the slice,
follow the [runbook](../how-to/run-the-first-slice.md). Terms such as Pn, KD-n and Tnnn are
in the [glossary](glossary.md).

## What is still unverified

Every behaviour below is coded against fakes, recorded fixtures or provider docs. None has been
observed on OVHcloud. The source of record is the spec's
[premises table](../../specs/005-first-landing-zone-slice/spec.md#premises) and each task's
evidence gaps.

- **P1–P3, the state backend:** observed on a probe bucket (T010 runs 14 and 15): the S3
  backend with `use_lockfile` (a second writer is refused), and a state object that is the
  OpenTofu encryption envelope. Not yet observed on a lane stack's state: the first is T049,
  where the chain's state-object check (T102) runs live for the first time.
- **P25 refuted** (T010, D107): a tag condition on a project-URN policy does not scope bucket
  reads. Per-tenant bucket envelopes rely on one project per tenant, not on tags.
- **Bootstrap, fresh account (T045; P22, P23):** never run. The existing-account path ran twice
  (T044). The fakes alone encode:
  - the 409 text of a taken bucket name (T057);
  - the `/auth/time` offset, root-key revocation through `GET /auth/currentCredential`, and
    created-client propagation (T043);
  - `identify` creating a binding from scratch (T044 found the binding already in place).
- **Project tags (T103, T104, T105):** the `project` stage, applied by the platform deployer, could
  not tag the project. T049 run 1 was refused with 403 `missing IAM permissions:
  account:apiovh:iam/resource/edit`; run 2 with 400 `unknown action:
  "account:apiovh:iam/resource/get"`; run 3 with the same 403 while `iam/resource/edit` was
  granted on the project URN. So `account-governance`, which runs under the admin, now sets each
  tenant project's tags, and the platform deployer is back to `publicCloudProject:apiovh:*` alone
  (D109, T105). The next chain run plans, in the retained `account-governance` stack, one tag
  resource per tenant project and an in-place update of the platform policy; nothing there is
  destroyed or replaced. Whether the admin's tag PUT and the L7 read-back
  (`GET /iam/resource/{urn}`) succeed on the project is UNVERIFIED until that run. If either is
  refused, stop and record it for a decision.
- **Chain (T049):** runs 1 and 3 ended at `demo-dev-project`, run 2 at `account-governance`
  (above).
  These are UNVERIFIED:
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
adopted; the API-client leftover listing (T009, T084, T087); the create→destroy probes for
P1–P3, P7–P10, P12, P15 and the P26 tenant half (T010; P13's cost and P14's destroy of a bucket
that still holds noncurrent versions stay open: the probes emptied the bucket first); and the
account bootstrap on the existing account: state bucket with Object Lock, `state.env`, publish,
and a second run with every phase after `guard` `unchanged` (T044).

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
