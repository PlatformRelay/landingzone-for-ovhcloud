# Implementation Plan: Deployment and self-service transaction rehearsals
Date: 2026-10-01 · Spec: [spec.md](spec.md) · Status: draft · Branch: codex/first-phases

## Summary
Rehearse instances, output publications and routine-change authority with disposable
local roots and a local durable store. Qualify the same no-cloud protocol on each forge.
The two small ADR-0007 programs remain a manifest reconciler and a transaction driver;
Terramate generates HCL. This phase is an experiment, not an unattended deployment system.

## Technical Context
- Language: Go in tools/go.mod, Terramate HCL, plain OpenTofu fixture child stages,
  YAML/JSON schemas, assent CEL and Conftest Rego, thin Task/forge adapters.
- Pins: phase-001 image/toolchain; Terramate 0.17.3 is qualified using the binary's own
  selection/order output. Record an exact assent release/commit; missing GitHub adapter
  blocks real-forge qualification without narrowing the product's both-forges requirement.
- Storage: local durable test store with atomic records and cross-process locks; synthetic
  encrypted plans/local state in private per-run temp dirs. Store interface is explicit.
- Scale: tenant A and B, each with project→network→runtime fixture instances, plus one
  shared account producer; independent state keys. Providers are not cloud mocks pretending
  to prove IAM. `terraform_data`/local pure outputs suffice for state/graph behaviour.
- Offline fault suite <=120s on the pinned runner; forge protocol runs <=10min each with
  a hard timeout. EUR 0 cloud; forge runner capacity is a prerequisite, not free assumed.

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
1. Setup: extend registry and schemas; capture exact Terramate and assent output with
   fixture-generating commands. Test strict decoding and manifest identity before reconciler.
2. US1: build minimal tenant-repo Terramate configuration and reconciler over `terramate
   create`, not a generator. Test add/repeat/rename/removal/tombstone and generated freshness.
   Independently calculate the expected affected set and waves; test git and external inputs.
3. US2: create the fault oracle before transaction driver. Durable states are pending,
   applying, applied-unpublished, published, failed, needs-reconciliation. Publication writes
   immutable output then current last; CAS/locking rejects late old publication. A producer
   is marked pending before mutation; pending blocks old-current reuse.
   Acquire producer/consumer transaction locks in sorted instance-id order, inspect statuses
   and digests after locking, and hold them over approved consumer apply. Producer update
   needs the same lock. On uncertain/crashed apply, freeze dependencies and reconcile actual
   outcome before retry; no exactly-once promise for external effects.
4. US3: strict effective-document resolution; trusted base authorisation; aggregate
   reservations in the durable store; tests for forged/stale decisions. assent decides
   merge permission; Conftest gates saved-plan effects; neither is reimplemented here.
   Finally execute candidate/rebase/check races on disposable GitHub/GitLab repositories.

## Verification strategy
V001–V007 exercise the exact adapters/reconciler/driver, not a replacement subject.
Race harness deterministically pauses at pending-write, apply, immutable-object write,
publication, fence and approval checkpoints. Assert both safety and eventual successful
retry after reconciliation; deadlock tests have a 30s process deadline and kill stalled runs.
Cases include missing current, failed producer with old current, late generation publication,
update after fence, concurrent graph runs, crash/restart, corrupt digest, unavailable store,
foreign instance paths, simultaneous reservations and invalidated required checks.
Each trust-binding field is mutated separately; the valid unchanged candidate remains green.
Local-store success is expressly not S3/forge/cloud qualification. Real forge tests capture
commit/run identity and required-check observations; simulations cannot close V007.

## Dependencies and live change ordering
001 offline/report/registry → strict data → reconcile → graph → transaction/plan binding
→ authority/reservations → real forge qualification. Both US1 and US2 can be tested against
local fixtures, but US2 consumes US1's graph/instance contract. Security tests depend on the
report oracle and trusted fixture separation, not cloud authority.
There are no live OVH objects. Real forge objects: disposable request branch → protected
base policy/check config → candidate binding → merge condition → observed stale/valid result.
Do not change protection on product repos or use account-wide tokens. Upstream assent
adapter release and disposable repo setup are external gates; V007 stays blocked if absent.

## Complexity tracking
A local durable store makes race assumptions observable without new cloud infrastructure.
It is not a production database choice; each future production backend must pass the same
contract. Store locks cover the fence-to-apply interval, not a check followed by an unlocked
apply. Terramate order alone never selects consumers; wants and artefact-change widening
are tested separately. Do not add Terragrunt, TACOs or a bespoke pipeline engine in this phase.
