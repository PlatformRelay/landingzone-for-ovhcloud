# Feature Specification: Deployment and self-service transaction rehearsals
Created: 2026-10-01 · Status: draft · ADRs: 0004, 0005, 0006, 0007, 0008, 0021

## Why and scope
Prove the most expensive control-plane seams with local disposable state and artefacts,
then qualify the forge adapter protocol in disposable repositories on both forges.
This covers the offline portions of ranked spikes 2 and 4. Exclude OVH deployment,
project orders, production merging and full tenant-repo release readiness.

## User Scenarios & Testing
### User Story 1 — Materialise and select isolated instances (Priority: P1)
Given two tenants with producer/consumer chains, upstream-only changes select their
consumers and preserve distinct roots; another tenant stays untouched. Repeat succeeds;
rename/removal without retirement fails. Independent test: V001–V002.
### User Story 2 — Publish and consume safely (Priority: P1)
Given an applied producer, its consumer waits for publication. Concurrent producer change,
crash after apply or stale approval prevents consumer apply. Independent test: V003–V004.
### User Story 3 — Authorise a routine request (Priority: P2)
Given trusted owners and policy, an allowed tenant field change can pass; request-authored
authority, stale candidate and oversubscribed aggregate requests block on either forge.
Independent test: V005–V007.

## Premises
| ID | Mechanism premise | Probe | Evidence status |
| --- | --- | --- | --- |
| P1 | Pinned Terramate materialisation, wants selection and after ordering match ADR-0007 | Capture exact 0.17.3 config, changed sets and execution order from a disposable Git repo | UNVERIFIED; installed 0.17.1 help is not qualification |
| P2 | Producer publication and consumer apply can share an effective fencing boundary | Concurrent local-store race harness holding locks over fence-to-apply and publication | UNVERIFIED; V003 proves local mechanism only |
| P3 | assent adapters bind decisions and required checks on GitHub and GitLab | Pinned assent output plus live disposable-repo candidate/rebase/check races | UNVERIFIED; missing adapter is an external blocker, never a stub pass |

## Requirements
- **FR-001**: MUST strictly decode manifests and effective tenant data; reject duplicate keys, unknown fields, wrong apiVersion, duplicate instance ids/paths/state keys and authority edits.
- **FR-002**: MUST reconcile deployments.yaml to one Terramate stack per immutable instance with one backend key; repeated reconciliation/generation is idempotent; retirements retain tombstones.
- **FR-003**: MUST derive acyclic wave ordering and the affected set from artefact edges, git changes and external current-generation changes; selection and ordering are separate.
- **FR-004**: MUST publish immutable digest-bound generations with a durable producer status and final publication record; pending/failed producer status blocks consumers even if an older current exists.
- **FR-005**: MUST bind approved saved plans to all current inputs and reject stale candidates; fence from validation through consumer apply so a producer cannot supersede inputs in that interval; record crash outcomes without assuming exactly-once applies.
- **FR-006**: MUST evaluate routine deltas against trusted-base owners, schemas, defaults and policy using the canonical effective-document digest; refuse privilege, identity, waiver and retirement changes.
- **FR-007**: MUST reserve aggregate quota/budget atomically and idempotently across concurrent requests; cancellation, expiration and retry cannot leak or double-count reservations.
- **FR-008**: MUST qualify candidate binding, required checks, invalidation and serialized execution on real GitHub and GitLab repositories; lack of a necessary assent adapter blocks that forge qualification.

## Acceptance and predefined verification
All commands are **planned**, with creating tasks in tasks.md. No implementation or live
check has run. Evidence below is initially `not-run`; paths are under `.local/evidence/`.

| Check | Requirements | Criterion: positive and negative outcomes | Verify (planned) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | two tenants get distinct keys/roots; rerun no diff; malformed/duplicate/rename/unapproved removal rejected | `task test:instances; task instances:check` | `002/instances.json` |
| V002 | FR-003, SC-001 | upstream/intermediate changes and out-of-git generation changes select exact consumers; unrelated tenant untouched; cycles refused | `task test:selection` | `002/selection.json` |
| V003 | FR-004, FR-005, SC-002 | publication fault blocks, lost writer/crash requires reconciliation, delayed older publisher cannot regress current; concurrent superseding producer waits or consumer aborts | `task test:transaction` | `002/transaction.json` |
| V004 | FR-005, SC-002 | plan/base/policy/schema/toolchain/input/target alteration invalidates approval; exact saved plan accepted once, retries reconcile | `task test:plan-binding` | `002/plan-binding.json` |
| V005 | FR-001, FR-006 | valid own-field delta accepted; request-authored owner/policy, forged approval, changed identity/waiver/lifecycle denied; effective defaults consistent | `task test:tenant-authority; task policy` | `002/authority.json` |
| V006 | FR-007 | two individually valid requests exceeding total cannot both reserve; retry, expiration and cancellation reconcile exactly once | `task test:reservations` | `002/reservations.json` |
| V007 | FR-008, SC-003 | real disposable-repo rebase/new-check/candidate races block stale merge/apply on each forge; positive path succeeds without cloud credentials | `task verify:forge-transaction` | `002/forge-transaction.json` |

## Success Criteria
- **SC-001**: Two-tenant graph has one state owner/key per instance and exactly the affected consumers (V001–V002).
- **SC-002**: No fault schedule in publication/fencing/approval cases admits a stale consumer apply (V003–V004).
- **SC-003**: Both forge adapters pass the same protocol cases; unavailable adapter or repository is blocked, not simulated support (V007).

## Edge cases
Simultaneous runs; two publishers out of order; crash after cloud-like apply before publication;
expired lock holder; producer update immediately after fence; orphan stack; path traversal;
removed tenant; empty current; corrupt/digest-mismatched object; unavailable store; check
status changed after approval; request edits evaluator; reservation expires during apply.

## Implementation surface
Go probe/test helpers live in `tools/internal/probes/`; root `tests/` holds their fixtures.
`templates/tenant-repo/`, `schemas/{deployments,effective-document,artifact,decision}.schema.json`,
`tools/internal/{instances,transaction,authority,reservations}/`, `tools/cmd/lz-deploy/`,
`tests/{transaction,security,forge}/`, `policies/{assent,plan}/`, `pipelines/{github,gitlab}/`.

## Dependencies and stop conditions
Requires phase 001 report/offline/traceability exits. Local state is a test double, not
cloud isolation evidence. Real forge tests are protected no-cloud runs with separate
repository-scoped tokens. If assent lacks an adapter, record a dependency on its upstream
release and block V007; do not implement a second forge engine here. The local fencing
backend is not production-qualified. Live S3/state proof is in phase 003; a production
transaction store must later be qualified against the same race suite before deployment.
