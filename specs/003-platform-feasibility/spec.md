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
may execute in one dedicated sandbox project. Unknown inputs deny admission; cancellation
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
- **FR-001**: MUST satisfy each clause below.
  - **C001.1**: Require one operator-approved sandbox project ID and region.
  - **C001.2**: Require approved price/exposure calculation.
  - **C001.3**: Require two independently usable escrow holders, unless operator amends that requirement.
  - **C001.4**: Require scoped runner and cleanup authority.
  - **C001.5**: Require completed approved cost/sandbox design before live admission.
- **FR-002**: MUST satisfy each clause below.
  - **C002.1**: Persist an external durable lease and resource-ID inventory.
  - **C002.2**: Reserve exposure atomically.
  - **C002.3**: Enforce per-spike maximum runtime and exposure from the bounds table.
  - **C002.4**: Reconcile cleanup.
  - **C002.5**: Block new leases on unknown billing, stale data, orphan inventory or failed reaper.
  - **C002.6**: Keep cleanup enabled when admission is blocked.
- **FR-003**: MUST satisfy each clause below.
  - **C003.1**: Prove cancellation and process-death cleanup discovery.
  - **C003.2**: Prove recovery from partial creation before state write.
  - **C003.3**: Prove recovery after reaper outage.
  - **C003.4**: Use known IDs and approved project allowlist rather than tags as deletion authority.
- **FR-004**: MUST satisfy each clause below.
  - **C004.1**: Prove encrypted state handling.
  - **C004.2**: Prove encrypted saved-plan handling.
  - **C004.3**: Prove bootstrap before KMS.
  - **C004.4**: Prove independently decryptable backup.
  - **C004.5**: Restore known data in a clean runner without original KMS.
  - **C004.6**: Never claim a fallback decrypts data written by another method.
- **FR-005**: MUST satisfy each clause below.
  - **C005.1**: Prove scoped fixture A cannot read/write B state or artifacts, and vice versa.
  - **C005.2**: Prove A/B cannot use foreign decrypt authority.
  - **C005.3**: Prove locking serializes concurrent writers.
  - **C005.4**: Prove interrupted-writer handling.
  - **C005.5**: Fence every old write path before replica promotion.
  - **C005.6**: Record replica lag against its approved bound.
  - **C005.7**: Do not infer cross-project/account isolation from within-project fixtures.
- **FR-006**: MUST satisfy each clause below.
  - **C006.1**: Canary-test native-user routes.
  - **C006.2**: Canary-test service-account routes.
  - **C006.3**: Canary-test federation only in separately approved disposable account scope.
  - **C006.4**: Observe OVH API and applicable alternate Keystone/S3 routes independently.
  - **C006.5**: Never infer denial in one plane proves another plane.
  - **C006.6**: Drill native recovery before floor binding.
  - **C006.7**: Attempt floor-binding self-removal.
  - **C006.8**: Attempt floor-policy self-removal.
  - **C006.9**: Test missing and present authorization tags.
  - **C006.10**: Attempt cross-tenant retagging.
  - **C006.11**: Test project-tag effects on applicable child operations for each route.
  - **C006.12**: Capture source-bound policy/group/tag limits and current counts without saturation tests.
  - **C006.13**: Measure bounded allow/deny propagation with repeated positive/negative controls; unknown windows remain UNVERIFIED.
- **FR-007**: MUST satisfy each clause below.
  - **C007.1**: Qualify issuer authentication.
  - **C007.2**: Qualify refresh and expiry.
  - **C007.3**: Qualify failed cleanup and revocation.
  - **C007.4**: Qualify OVH tokens and each OpenStack bridge/fallback route independently.
  - **C007.5**: Probe already-issued credentials.
  - **C007.6**: Record approved numeric maximum residual windows per supported type.
- **FR-008**: MUST satisfy each clause below.
  - **C008.1**: Inventory observed principal routes and permissions.
  - **C008.2**: Record naming/tag limits and actual provider schemas with exact pinned-tool provenance.
  - **C008.3**: Leave unknown routes/limits UNVERIFIED and block dependent claims.
- **FR-009**: MUST satisfy each clause below.
  - **C009.1**: Publish sanitized spike decisions with independent review.
  - **C009.2**: Publish full exposure accounting.
  - **C009.3**: Keep readiness experimental until required live evidence passes.
  - **C009.4**: Fail qualification on predefined refutations and apply the named downstream stop.
  - **C009.5**: Keep missing/timeout/cap-limited observations blocked rather than passed.

## Predefined spike bounds and refutation stops
These are draft maximums for the first experiment, not permission to run it. An approved
RunConfig may reduce them; increasing a ceiling requires a reviewed operator amendment.
Each timebox is elapsed wall time across admission, setup and probe retries. At deadline
or reserved exposure exhaustion, stop new writes, retain a blocked result and continue
scoped reconciliation/cleanup under its separate authority. Reserve cleanup and recurring
liabilities before starting: a spend cap covers setup, probe and teardown, not only billed
usage so far. Unknown price or inability to bound exposure denies admission. Actual cost
and all outstanding reservations must also fit D8's EUR 200 total ceiling. These caps
sum to EUR 110; that leaves at most EUR 90 for other remaining trial liabilities, never fresh
headroom inferred from delayed billing. Forge execution costs/capacity are separate setup
prerequisites; EUR 0 below is cloud spend, not a promise of free runners.

| Ranked spike | Checks/tasks | Max elapsed | Max exposure EUR | If refuted: withdraw/gate before any next implementation |
| --- | --- | --- | --- | --- |
| 1 Independent decryption and clean bootstrap | V003; T009–T010 | 8 h | 30 | Withdraw ADR-0009 recoverable-bootstrap/key-loss claim and ADR-0022 recovery readiness for the affected route. No deployment on that recovery mechanism. |
| 2 State ownership and root ordering | V004; T011–T012 + 002/V001–V002 | 4 h | 20 | Gate ADR-0004 ownership/artifact isolation and ADR-0009 affected state route. Local roots alone cannot rescue cloud isolation. |
| 3 Floor coverage and native lockout recovery | V006; T015–T017 | 4 h | 10 | Withdraw ADR-0006 preventive/floor or tag-envelope claim for each escaping route, and gate dependent ADR-0018/0021 authority. Failed native recovery forbids floor binding. |
| 4 Merge authorization and races | 002/V003–V007; local T008/T012/T016; live T019 | 8 h total; 10 min per forge run | 0 | Gate ADR-0005/0007 unattended merge/apply on a stale/forged/racy path. Missing adapter is blocked, not refuted. |
| 5 Credential issuance and revocation | V007; T018–T019 | 4 h | 10 | Withdraw ADR-0009/0018 JIT/bridge claim for issuer failure or excessive residual window. Fallback gains authority only after its own passing suite. |
| 6 Sandbox safety under failure | V001–V002; T002–T008 | 4 h | 20 | Stop all new live admission under ADR-0008 when ownership/reaper safety fails or orphan remains; only reconciliation/cleanup continues. |
| 7 Locking, interruption and promotion | V005; T013–T014 | 8 h | 20 | Withdraw ADR-0009/0022 locking/promotion readiness on concurrent writes, unfenced old writer or excessive lag. No replica activation. |

A decision packet can be complete while qualification fails. `pass` needs every required
positive, negative, cleanup and review observation. `refuted` produces a failing check
and the named downstream stop; missing evidence, timeout or spend exhaustion is `blocked`
unless actual observation already refuted the premise. Recording all outcomes never turns
a failed/blocked spike into green. Do not execute a gated follow-up merely to finish the
table: record its blocked prerequisite. Replacement architecture is a new operator decision.

Each numbered clause inherits its parent requirement’s V-check and creating tasks.
For every guarded clause, implementation records a distinct expected outcome and
valid/defect control; parent coverage alone cannot satisfy an untested child clause.

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
| V006 | FR-006 | per-route positive/denial controls cover delete, floor/policy self-removal, missing/present tags, cross-tenant retagging and applicable child operations; native recovery works during federation failure; IAM limits/counts have source-bound or explicit unknown status and allow/deny propagation is measured within the deadline; absent subcontrol blocks spike-3/envelope-authority pass | `task spike:deny-floor` | `003/deny-floor.json` |
| V007 | FR-007 | already-issued credential expires/revokes within its approved window; issuer/bridge failure blocks dependent use; fallback only after same suite passes | `task spike:credentials` | `003/credentials.json` |
| V008 | FR-008, FR-009, SC-002 | schema/limits/route inventory cites source and actual capture; invented limit or unmeasured timing guarantee refuses readiness; unsupported tuple refuses readiness; sanitised packet has versions, revisions, cost, cleanup result and independent disposition | `task spike:qualification` | `003/qualification.json` |

## Success Criteria
- **SC-001**: Admission and every failure drill leave no unaccounted owned resource and no new admission above the EUR 200 total exposure ceiling (V001–V002).
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
Docs-only T001 has no 001 dependency. Behavioral preparation starts after 001/T009,
the minimum local safety subset T001–T009, plus its own cost/sandbox approval and
admission/reaper prerequisites. Naming, both-forge qualification and latency are not
on that critical path. Local 002/T008, T012 and T016 gate the aggregate only; a missing
forge adapter blocks forge readiness, not project-scoped platform observations.
No live work is authorized by this planning request. Baseline is one pre-existing
sandbox project under D8, with two separately scoped state/artifact fixtures inside it;
this tests those routes, not cross-project or account isolation. Region and backup
location must be explicitly approved; no pool or second region is assumed. Account-scoped
IAM/federation, backup placement, state-project ownership and second escrow holder remain
operator decisions. If required recovery/account scope is absent, the affected probe
remains blocked; a project's existence does not authorize account-wide changes.
Protected scoped runners, cleanup identity and numerical runtime/lag/credential windows
are required. Cost/sandbox ADR precedes admission implementation. No automatic orders,
production floor binding or ordinary-account singleton changes are authorized.
