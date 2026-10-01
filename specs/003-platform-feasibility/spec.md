# Feature Specification: Protected platform and recovery feasibility
Created: 2026-10-01 · Status: draft · ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022

## Why and scope
Observe the load-bearing cloud claims before building a landing zone. Cover ranked
spikes 1, 3, 5, 6 and 7 and the cloud isolation portion of spike 2; spike 4 belongs to 002.
No general module/runtime release, automatic purchase, production deny policy or
unattended deployment is included. Unsupported routes stay explicitly blocked.

## User Scenarios & Testing
### User Story 1 — Admit and clean a bounded experiment (Priority: P1)
Given known inventory, prices, cleanup health and an approved lease, a protected runner
may execute in dedicated sandbox projects. Unknown inputs deny admission; cancellation
and process death still leave an independently discoverable cleanup path. Test: V001–V002.
### User Story 2 — Recover and isolate state (Priority: P1)
A fresh operator restores the disposable fixture without its original KMS; two tenant
credentials cannot reach each other's state, and promotion never leaves two writers.
Independent test: V003–V005.
### User Story 3 — Bound identity authority (Priority: P2)
Each supported principal route has an attributable deny test, a working positive control
and a native recovery path. Already-issued credentials meet recorded residual windows;
unsupported issuer routes are blocked. Independent test: V006–V007.
### User Story 4 — Decide whether to build the slice (Priority: P3)
A maintainer sees independently reviewed evidence, actual costs and specific gaps,
with no promotion of an unprobed tuple. Independent test: V008.

## Premises
| ID | Mechanism premise | Probe | Evidence status |
| --- | --- | --- | --- |
| P1 | Dedicated sandbox and backup location, holders and scoped cleanup identities exist | Protected read-only account/project/region/role inventory; sealed recovery-package check | UNVERIFIED; external setup gate |
| P2 | Cloud lockfile, permissions and replica fencing meet our contract | Pinned OpenTofu/S3 conditional write and concurrent-writer probes on disposable fixtures | UNVERIFIED; authoritative docs do not prove this account |
| P3 | OKMS/PBKDF2 backup and clean bootstrap can recover without original dependency | Independent fresh-runner key-loss and empty-state recovery drills | UNVERIFIED; no live drill has run |
| P4 | Deny-floor bindings and OVH-to-OpenStack credential bridge behave as claimed | Canary route matrix and already-issued-token revocation probes | UNVERIFIED; alternate paths must be observed individually |

## Requirements
- **FR-001**: MUST require operator-approved sandbox project ids, region, price/exposure calculation, two escrow holders, scoped runner/cleanup authority and completed cost/sandbox design before live admission.
- **FR-002**: MUST implement an external durable lease/resource-id inventory, atomic exposure reservation, maximum runtime and cleanup reconciliation; unknown billing, stale data, orphan inventory or failed reaper blocks new leases while cleanup remains enabled.
- **FR-003**: MUST prove cancellation, process death, partial creation-before-state-write and reaper outage recovery without relying on tags; run only against independently allowlisted sandbox ids.
- **FR-004**: MUST prove encrypted state and saved-plan handling, bootstrap before KMS, independently decryptable backup and recovery in a clean runner without the original KMS; never claim fallback decrypts a different method.
- **FR-005**: MUST prove one tenant cannot read/write another tenant state/artifacts or use its decrypt authority; locking handles concurrent/interrupted writers; promoted replica fences all old write paths and records bounded lag.
- **FR-006**: MUST canary-test deny-floor routes across native users, service accounts and available federation, including OVH API and alternate Keystone/S3 paths where applicable; never infer a platform denial covers another plane; drill native recovery before wider binding; test floor-binding/policy self-removal, missing/present authorisation tags, cross-tenant retagging and project-tag effects on child operations for every applicable route.
- **FR-007**: MUST qualify issuer authentication, refresh, expiry, failed cleanup and revocation for OVH tokens and OpenStack bridge/fallback; probe already-issued credentials and record explicit maximum residual windows per supported type.
- **FR-008**: MUST inventory observed principal routes, permissions, resource naming/tag limits and provider schema shapes with exact pinned-tool provenance; unknown route/limit remains UNVERIFIED and blocks its dependent claim.
- **FR-009**: MUST publish sanitised independently reviewed spike decisions and full exposure accounting; readiness catalogue entries remain experimental unless their required live evidence passes.

## Acceptance and predefined verification
All commands are **planned**, with creating tasks in tasks.md. No implementation or live
check has run. Evidence below is initially `not-run`; paths are under `.local/evidence/`.

| Check | Requirements | Criterion: positive and negative outcomes | Verify (planned) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | approved admission succeeds; missing/wrong project, unknown cost, stale billing, exhausted cap or unhealthy cleanup blocks; cleanup still admitted | `task test:sandbox-admission; task live:preflight` | `003/admission.json` |
| V002 | FR-003, SC-001 | injected crash/partial create/reaper outage leave recorded ids; recovery empties owned fixture inventory with no foreign deletion | `task test:sandbox-reaper; task spike:sandbox-failure` | `003/sandbox-failure.json` |
| V003 | FR-004 | actual pinned-tool state and saved plans decrypt/consume with the correct key, reject missing/wrong keys and plaintext input; separate state/plan-enforcement mutations fail; private canary scan finds no plaintext canary; fresh runner restores known data without original KMS, wrong escrow and same-key replica fail, empty bootstrap imports ids/reissues secrets | `task spike:recovery` | `003/recovery.json` |
| V004 | FR-005 | A control reads/writes own state and decrypts own fixture; A→B and B→A read/write/decrypt/artifact probes deny with attributable reasons | `task spike:state-isolation` | `003/isolation.json` |
| V005 | FR-005 | two writers serialize; interrupted lock recovered only after owner-death proof; promotion disables primary writes, replica lag within approved bound; old writer is denied | `task spike:locking-promotion` | `003/locking.json` |
| V006 | FR-006 | per-route positive/denial controls cover delete, floor/policy self-removal, missing/present tags, cross-tenant retagging and applicable child operations; native recovery works during federation failure; absent subcontrol blocks spike-3/envelope-authority pass | `task spike:deny-floor` | `003/deny-floor.json` |
| V007 | FR-007 | already-issued credential expires/revokes within its approved window; issuer/bridge failure blocks dependent use; fallback only after same suite passes | `task spike:credentials` | `003/credentials.json` |
| V008 | FR-008, FR-009, SC-002 | schema/limits/route inventory cites source and actual capture; unsupported tuple refuses readiness; sanitised packet has versions, revisions, cost, cleanup result and independent disposition | `task spike:qualification` | `003/qualification.json` |

## Success Criteria
- **SC-001**: Admission and every failure drill leave no unaccounted owned resource and no new admission above the EUR 200 monthly exposure ceiling (V001–V002).
- **SC-002**: Each of the top-seven spikes has observed evidence or a specific refuted/blocked result with a downstream stop gate; no UNVERIFIED control is called supported (V008 and phase 002).

## Edge cases
Delayed billing; unknown recurring charges; two leases exceeding headroom; reservation retry;
SIGKILL between create and state persistence; cleanup credential revoked; KMS down; corrupt
backup; lost original key; unreadable OAuth secret; stale lock; old writer with cached key;
unknown federation account; duplicate issuer; expired token mid-apply; denial caused by outage.

## Implementation surface
Go probe/test helpers live in `tools/internal/probes/`; root `tests/` holds their fixtures.
`tools/internal/sandbox/`, `tests/live/{sandbox,recovery,state,identity}/`,
`tests/{security,recovery}/`, `harness/live-checks.yaml`, `catalog/`,
`docs/adr/` (cost/sandbox decision), `docs/reference/{test-costs,provider-gaps}.md`.

## Dependencies and stop conditions
Offline harness 001 must pass before any live task. Offline rehearsals in 002 gate only
V008's aggregate decision; state and identity probes can proceed after their own gates.
No live work is authorized by this planning request. Need dedicated pre-existing sandbox
projects, separate regional backup destination, two escrow holders, protected read-only
and apply runners, cleanup identity and approved numerical runtime/lag/credential windows.
Write cost/sandbox ADR before implementing admission. Use a pre-ordered project pool;
any project order or intentional destructive probe needs a separate reviewed experiment.
No deny-floor binding on the maintainer's ordinary identity or production account.
All commands are planned; failure/unsupported results revise Proposed ADRs in place.
