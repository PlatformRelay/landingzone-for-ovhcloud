# Implementation Plan: Protected platform and recovery feasibility
Date: 2026-10-01 · Spec: [spec.md](spec.md) · Status: draft · Branch: codex/first-phases

## Summary
Prepare protected experiments before applying broad cloud guardrails. Sandbox admission and
cleanup qualification comes first; then recovery, state and identity probes on disposable
fixtures. A failed premise stops its dependent implementation and revises the Proposed ADR.

## Technical Context
- Go probe helpers and Task targets in the existing tools module; pinned OpenTofu 1.13.0,
  ovh/ovh 2.21.0, OpenStack provider/version and OKMS key helper chosen/pinned only after
  primary-source review. Test suites separate tests/live from offline discovery.
- Execution: protected trusted runner, explicit approved sandbox ids and credential class;
  no production authority. At least one existing sandbox project, second disposable tenant
  scope for isolation, separate regional backup storage, two independent escrow holders.
- Storage: external durable lease/resource-id inventory not hosted solely inside the
  fixtures it cleans; private encrypted snapshots/plans/logs; sanitised evidence envelope.
- Cost: EUR 200/month exposure ceiling from operator decision D8. Include preparation,
  idle/recurring charges, replica/storage, test, teardown and already-reserved future costs.
  No spend hard cap claim; budget alert alone cannot admit a run.
- Before each live target an approved run configuration defines numeric max_seconds,
  max_exposure_eur, price/billing freshness, reservation limits, replica-lag seconds and
  residual credential windows. Missing values block; no invented platform timing values.

## Constitution Check
| Principle | Before research / after design |
| --- | --- |
| I Evidence | Premises listed; only bounded proof tasks can start with UNVERIFIED mechanisms; dependent implementation blocked. |
| II Traceability | Spec acceptance V checks predefined; task generation must map creators, requirements, outcomes and evidence. Docs-only exemption retained. |
| III Test first | Behaviour controls precede implementation; every sensor clause challenged; actual pinned output fixtures required. |
| IV Authority | One state owner, trusted evaluator and protected execution boundaries explicit; no authoring cloud authority. |
| V Recovery | Missing observations fail closed; private evidence and separate cleanup/recovery paths required. Live readiness remains blocked on setup. |
| VI Scope | Bounded first increment; no supported-tuple claim, broad scaffolding or runtime rollout. |
| VII Useful outcomes | Preserve the reference baseline and differentiators; existing acceptance cases cover visible reports/status and failure paths; no new portal/CLI implied. |
| VIII Decisions | Approved direction is distinct from pending interfaces; phase 001 naming T013 gates the joint choice; dependent work respects that gate. |


Draft design conforms; this table is a review of the design, not evidence that future gates passed.


## Project Structure
Implementation paths: see spec.md's Implementation surface; no generic src/ tree.
Feature artefacts: research.md, data-model.md, contracts/checks.md, quickstart.md, tasks.md.

## Delivery phases
1. Setup (offline): write cost/sandbox operating ADR with approved units/ceilings and
   external inventory/cleanup authority, lease protocol and failure ordering. Design the
   approved run-config schema; test every missing/invalid admission input first.
2. US1: implement ledger, exposure reservations and reaper against local fakes. Retain
   resource ids outside state before/around API creation; if API returns no id, reconcile by
   durable request identity and bounded inventory scan before any retry. Reaper is restricted
   by independent sandbox project allowlist plus known ids, not tenant-supplied labels.
   External setup gate: existing sandbox, scoped read/apply/cleanup identities, protected
   runner, alert, holders and budget config. Canary cleanup then fault drill; failed health
   forbids subsequent admission while the cleanup lane remains executable.
3. US2: create disposable state/plan fixture, snapshot and sealed id/import manifest;
   demonstrate clean bootstrap without KMS and independent escrow backup. Fresh runner
   has no original key/helper service; restore known data into isolated backend and fence
   production writes. Empty-state reconciliation imports stable ids; unreadable OAuth secret
   is reissued, never claimed recoverable. Use actual pinned OpenTofu state and saved-plan
   files: correct key decrypts/consumes, wrong/missing key and plaintext inputs reject;
   private sensitive-canary scan and separate state/plan-enforcement mutants must fail.
   Then test cross-tenant state/artifact/decrypt
   access, native locking and interruption. Promotion revokes/fences all old write paths
   before activating replica; blocked revocation aborts promotion.
4. US3: establish native recovery before attaching a floor to a disposable canary. Test
   every supported route with positive control and attributable denial; alternate Keystone
   and S3 routes are independent tests. Attempt floor/policy self-removal, missing/present
   authorisation tags, cross-tenant retagging and applicable child operations. Unrun or
   failed subcontrols block full spike-3 and dependent envelope-authority qualification.
   No production deny rollout. Probe credential bridge,
   fallback, expiry/refresh, offboarding with already-issued credentials and cleanup failure.
5. US4: capture exact provider schema and applicable naming/tag limits; independently
   review packets, unresolved routes, measured cleanup and total costs. Update Proposed
   ADRs and catalogue honestly; gate first vertical slice rather than implement it here.

## Verification strategy
V001–V008 have offline positive/negative controls before protected live targets exist.
Every live observation records exact source/config digest, tool pin, principal route,
project/resource id, method, expected denial/success, actual status/reason and cleanup.
An outage, 404 or expired token does not prove authorisation: run a same-route positive
control and require the platform's attributable denial; if ambiguous report blocked.
Defect-specific mutations cover every admission clause, ownership fence and report rule.
Credentials/raw state/plan/API logs never enter tracked evidence. Per-target deadlines,
bounded pagination/retries and reconciliation after partial writes are mandatory.
Targets consult the external lease health check; collection failure never admits new runs.

## Dependencies and live change ordering
001 harness exit → offline admission/reaper tests + approved cost/sandbox ADR → operator
setup → read-only preflight → live cleanup canary → sandbox failure drill → recovery and
isolation/locking → native recovery gate → deny-floor canary → credential probes → aggregate
qualification (also requires 002 protocol results). Identity/state branches can run separately
only after sandbox exit, with no overlapping writers and within reserved aggregate exposure.
For every live step: owned fixture/project → approved lease+cleanup gate → probe outcome
+ inventory reconciliation. Key-loss destructive simulation uses only disposable keys/data.
Cleanup/revocation remains authorized by the approved experiment even when admission fails.
Revocation-before-replica-promotion is a hard edge. Native recovery-before-floor-binding
is a hard edge. Unsupported/revocation-unproven routes cannot gain deployment authority.

## Complexity tracking
Start with the external inventory and scoped reaper before buying compute or deploying KMS.
A tested explicit fallback is cheaper than a broker; do not build one unless issuer probes
prove the need. Federation may remain a separately gated route if the sandbox cannot isolate
an account-wide singleton. Do not change the maintainer's live federation for a probe.
Replay, seven TACOs, full network topology, monthly rotations and profile e2e are deferred.
