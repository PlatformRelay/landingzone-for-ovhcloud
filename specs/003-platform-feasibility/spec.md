# Feature Specification: Protected platform and recovery feasibility
Created: 2026-10-01 · Status: draft, re-scoped 2026-10-14 to one project (D113) · ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022

## Why and scope
Observe the load-bearing cloud claims before building a landing zone. Cover ranked
spikes 1, 3, 5, 6 and 7 and the cloud isolation portion of spike 2; spike 4 belongs to 002.
No general module/runtime release, automatic purchase, production deny policy or
unattended deployment is included. Unsupported routes stay explicitly blocked.

## Re-scope to one project (D113, 2026-10-14)
Spec 005's live runs changed what this spec can show. The owner re-scoped it (D113):
- **Tag envelopes are refuted** (P25, D107): a tag condition on a project-URN policy does not scope
  bucket reads. C006.9–C006.11 are withdrawn and ADR-0006's tag-envelope clause is amended.
- **Isolation is tested only on the planes one project can show**: the tenant's S3 credentials and
  its reach through Keystone/Swift. On the OVHcloud API plane a tenant deployer reaches every bucket
  in a shared project (spec 005 KD-1), and one passphrase per account makes C005.2 fail by design.
  **Real tenant isolation waits for a second project** (the KD-1 lift in spec 005); nothing here
  claims it.
- **Spikes run as probes on spec 005's live runner**: probe roots under `tests/live/probes/<root>`,
  run by `task live:probe` (`lz-live probe`: host guard, deadline, inventory, destroy on exit,
  leftover check). T002–T008 are dropped; no `spike:*` targets, `live:preflight`, RunConfig or
  leases.
- **EUR amounts are estimates**, not caps or admission conditions (D85, ADR-0024 amendment). The
  budget rules in AGENTS.md apply, and each run records what it cost.
- **State buckets are Object-Locked** (governance mode, default retention; D104): every state and
  `.tflock` version is retained for the retention period. Locking, recovery and promotion account
  for it (C005.8).
- **Four probes come from spec 005's findings** (T025–T028): the tenant S3 user's reach to other
  buckets through Keystone/Swift; whether an access token still works after its OAuth2 client is
  deleted; which URN `account:apiovh:iam/*` actions are checked against (D109); `force-unlock` and
  restoring an earlier state version on a locked bucket.

## User Scenarios & Testing
### User Story 1 — Admit and clean a bounded experiment (Priority: P1)
**Superseded (D85, D113)** by spec 005's run core; kept as the record of the original design.
Given known inventory, prices, cleanup health and an approved lease, a protected runner
may execute in one dedicated sandbox project. Unknown inputs deny admission; cancellation
and process death still leave an independently discoverable cleanup path. Test: V001–V002.
### User Story 2 — Recover and isolate state (Priority: P1)
The maintainer in a clean environment restores the disposable fixture without its original
account or KMS under the development custody amendment below; two tenant
credentials cannot reach each other's state on the S3 and Keystone/Swift planes (D113), and
promotion never leaves two writers.
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
| P1 | Dedicated sandbox and backup location, D49 maintainer custody and sealed offline copy, and scoped cleanup identities exist | Protected read-only account/project/region/role inventory; sealed recovery-package check | UNVERIFIED; external setup gate |
| P2 | Cloud lockfile, permissions and replica fencing meet our contract | Pinned OpenTofu/S3 conditional write and concurrent-writer probes on disposable fixtures | UNVERIFIED; authoritative docs do not prove this account |
| P3 | OKMS/PBKDF2 backup and clean bootstrap can recover without original dependency | Independent fresh-runner key-loss and empty-state recovery drills | UNVERIFIED; no live drill has run |
| P4 | Deny-floor bindings and OVH-to-OpenStack credential bridge behave as claimed | Canary route matrix and already-issued-token revocation probes | UNVERIFIED; alternate paths must be observed individually |

## Development custody amendment (D49)

**Operator-approved scope, 2026-10-01:** for initial development, one maintainer plus a
separately encrypted sealed offline copy replaces C001.3's two-holder requirement under
its explicit operator-amendment provision. This settles the custody shape; a second human
custodian is not an unanswered prerequisite for this development scope.

The offline package must be independently usable without the original account or KMS;
qualify recovery of known data in a clean environment. Actual custody, package availability
and independently usable access remain T006 preflight evidence; T009–T010/V003 retain
the actual key-loss and recovery controls, including wrong/missing keys, wrong escrow,
same-key replica failure, plaintext refusal and separate state/plan enforcement mutations.
No successful restore is inferred from package presence or the approved amendment.

The copy is not a second human custodian. Recovery while the sole maintainer is unavailable,
production human-loss recovery and production two-person custody remain unqualified.
This amendment supplies no ADR ratification, numerical RunConfig, live authorization or
passed task; absent setup or failed/missing recovery evidence blocks its dependent claim.

## Requirements
- **FR-001**: MUST satisfy each clause below. FR-001–FR-003 are superseded as gates by spec 005's run core (D85, D113); no remaining task checks them.
  - **C001.1**: Require one operator-approved sandbox project ID and region.
  - **C001.2**: Require approved price/exposure calculation.
  - **C001.3**: Require two independently usable escrow holders, unless operator amends that requirement. For initial development, the approved [D49 amendment](#development-custody-amendment-d49) requires one maintainer plus a separately encrypted sealed offline copy, with independent usability and actual recovery evidence.
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
  - **C004.5**: Restore known data in a clean runner without the original account or KMS.
  - **C004.6**: Never claim a fallback decrypts data written by another method.
- **FR-005**: MUST satisfy each clause below.
  - **C005.1**: Prove scoped fixture A cannot read/write B state or artifacts, and vice versa, with A's S3 credentials and through Keystone/Swift, inside one project (D113). The OVHcloud API plane is spec 005's KD-1 and waits for a second project.
  - **C005.2**: Withdrawn until per-tenant keys or OKMS exist (D113): spec 005 uses one passphrase per account, so A/B share decrypt authority by design.
  - **C005.3**: Prove locking serializes concurrent writers.
  - **C005.4**: Prove interrupted-writer handling.
  - **C005.5**: Fence every old write path before replica promotion.
  - **C005.6**: Record replica lag against its approved bound.
  - **C005.7**: Do not infer cross-project/account isolation from within-project fixtures.
  - **C005.8**: Account for Object Lock on the state buckets (D104): observe stale-lock recovery (`force-unlock`), restoring an earlier state version and teardown of a locked fixture (admin bypass or retention expiry) rather than assume them.
- **FR-006**: MUST satisfy each clause below.
  - **C006.1**: Canary-test native-user routes.
  - **C006.2**: Canary-test service-account routes.
  - **C006.3**: Canary-test federation only in separately approved disposable account scope.
  - **C006.4**: Observe OVH API and applicable alternate Keystone/S3 routes independently.
  - **C006.5**: Never infer denial in one plane proves another plane.
  - **C006.6**: Drill native recovery before floor binding.
  - **C006.7**: Attempt floor-binding self-removal.
  - **C006.8**: Attempt floor-policy self-removal.
  - **C006.9**: Withdrawn (P25 refuted, D107, D113): test missing and present authorization tags.
  - **C006.10**: Withdrawn (P25 refuted, D107, D113): attempt cross-tenant retagging.
  - **C006.11**: Withdrawn (P25 refuted, D107, D113): test project-tag effects on applicable child operations for each route.
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
These are draft bounds for the first experiment, not permission to run it. Each timebox is
elapsed wall time for one probe run; `lz-live probe` enforces a deadline, then destroys and
checks for leftovers. The EUR amounts are **estimates** (D113): cost is hygiene, not an
admission condition (D85, ADR-0024 amendment). Each run records its actual cost, as AGENTS.md
asks. Forge execution costs/capacity are separate setup prerequisites; EUR 0 below is cloud
spend, not a promise of free runners.

| Ranked spike | Checks/tasks | Max elapsed | Estimated cost EUR | If refuted: withdraw/gate before any next implementation |
| --- | --- | --- | --- | --- |
| 1 Independent decryption and clean bootstrap | V003; T009–T010 | 8 h | 30 | Withdraw ADR-0009 recoverable-bootstrap/key-loss claim and ADR-0022 recovery readiness for the affected route. No deployment on that recovery mechanism. |
| 2 State ownership and root ordering | V004; T011–T012, T025 (S3 and Keystone/Swift planes, D113) | 4 h | 20 | Gate ADR-0004 ownership/artifact isolation and ADR-0009 affected state route. Local roots alone cannot rescue cloud isolation. |
| 3 Floor coverage and native lockout recovery | V006; T015–T017, T027 | 4 h | 10 | Withdraw ADR-0006 preventive/floor claim for each escaping route, and gate dependent ADR-0018/0021 authority. Failed native recovery forbids floor binding. The tag-envelope claim is already withdrawn (P25, D113). |
| 4 Merge authorization and races | retired spec 002 (D113); postponed in spec 005's out-of-scope table | — | 0 | Gate ADR-0005/0007 unattended merge/apply on a stale/forged/racy path. Missing adapter is blocked, not refuted. |
| 5 Credential issuance and revocation | V007; T018–T019, T026 | 4 h | 10 | Withdraw ADR-0009/0018 JIT/bridge claim for issuer failure or excessive residual window. Fallback gains authority only after its own passing suite. |
| 6 Sandbox safety under failure | superseded by spec 005's run core (D85, D113) | — | — | Spec 005 stops a run that leaves a leftover; only cleanup continues. |
| 7 Locking, interruption and promotion | V005; T013–T014, T028 | 8 h | 20 | Withdraw ADR-0009/0022 locking/promotion readiness on concurrent writes, unfenced old writer or excessive lag. No replica activation. |

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
Live checks run as probe roots on spec 005's runner: `task live:probe -- --reviewed-sha <sha>
tests/live/probes/<root>` (D113); the root names below are planned.

| Check | Requirements | Criterion: positive and negative outcomes | Verify (planned) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | superseded by spec 005's run core (D85, D113); not run | — | — |
| V002 | FR-003, SC-001 | superseded by spec 005's run core (D85, D113); not run | — | — |
| V003 | FR-004 | actual pinned-tool state and saved plans decrypt/consume with the correct key, reject missing/wrong keys and plaintext input; separate state/plan-enforcement mutations fail; private canary scan finds no plaintext canary; fresh runner restores known data without the original account or KMS, wrong escrow and same-key replica fail, empty bootstrap imports ids/reissues secrets | `task live:probe` on `tests/live/probes/recovery` | `003/recovery.json` |
| V004 | FR-005 | A control reads/writes own state and artefact; A→B and B→A read/write/artefact probes with S3 credentials and through Keystone/Swift deny with attributable reasons; the API plane is KD-1 and decrypt is withdrawn (D113) | `task live:probe` on `tests/live/probes/state-isolation`, `tests/live/probes/swift-reach` | `003/isolation.json` |
| V005 | FR-005 | two writers serialize; interrupted lock recovered only after owner-death proof, `force-unlock` and an earlier-version restore observed on an Object-Locked bucket; promotion disables primary writes, replica lag within approved bound; old writer is denied | `task live:probe` on `tests/live/probes/state-locking`, `tests/live/probes/locked-recovery` | `003/locking.json` |
| V006 | FR-006 | per-route positive/denial controls cover delete and floor/policy self-removal, with `account:apiovh:iam/*` denials bound to the URN T027 observes; native recovery works during federation failure; IAM limits/counts have source-bound or explicit unknown status and allow/deny propagation is measured within the deadline; absent subcontrol blocks spike-3 pass | `task live:probe` on `tests/live/probes/deny-floor`, `tests/live/probes/iam-urn` | `003/deny-floor.json` |
| V007 | FR-007 | already-issued credential expires/revokes within its approved window; issuer/bridge failure blocks dependent use; fallback only after same suite passes | `task live:probe` on `tests/live/probes/credentials`, `tests/live/probes/token-residual` | `003/credentials.json` |
| V008 | FR-008, FR-009, SC-002 | schema/limits/route inventory cites source and actual capture; invented limit or unmeasured timing guarantee refuses readiness; unsupported tuple refuses readiness; sanitised packet has versions, revisions, cost, cleanup result and independent disposition | `task test:qualification` | `003/qualification.json` |

## Success Criteria
- **SC-001**: Superseded with V001–V002 (D113): spec 005's leftover check covers every probe run.
- **SC-002**: Each of the top-seven spikes has observed evidence or a specific refuted/blocked result with a downstream stop gate; no UNVERIFIED control is called supported (V008 and phase 002).

## Edge cases
Delayed billing; unknown recurring charges; two leases exceeding headroom; reservation retry;
SIGKILL between create and state persistence; cleanup credential revoked; KMS down; corrupt
backup; lost original key; unreadable OAuth secret; stale lock; old writer with cached key;
unknown federation account; duplicate issuer; expired token mid-apply; denial caused by outage.

## Implementation surface
Probe roots live in `tests/live/probes/<root>/` and run through `task live:probe` (spec 005's
runner); their Go checks and helpers live in `tools/internal/probes/live/` packages such as
`state`, `storageiam` and `identity` (D113). Root
`tests/fixtures/` holds captured outputs. Decisions go to `docs/adr/`; observations to
`docs/reference/verification-status.md`.

## Dependencies and stop conditions
Docs-only T001 has no 001 dependency. Behavioral preparation starts after 001/T009,
the minimum local safety subset T001–T009, and live work after spec 005's live lane
(005/T047), which replaces the admission/reaper prerequisites (D85, D113). Naming,
both-forge qualification and latency are not on that critical path. Spec 002 is retired
(D113); its items report `blocked` in the aggregate, never pass.
No live work is authorized by this planning request. Baseline is one pre-existing
sandbox project under D8, with two separately scoped state/artifact fixtures inside it;
this tests the S3 and Keystone/Swift routes, not cross-project or account isolation. Real
tenant isolation waits for a second project (D113). Region and backup
location must be explicitly approved; no pool or second region is assumed. Account-scoped
IAM/federation, backup placement and state-project ownership retain their separately
recorded operator dispositions and actual setup gates. D49 settles initial-development
custody as one maintainer plus a separately encrypted sealed offline copy; actual package
availability and independent recovery remain unverified. No second human is required for
that limited scope, and maintainer-loss/production two-person recovery is not qualified.
If required recovery/account scope is absent, the affected probe
remains blocked; a project's existence does not authorize account-wide changes.
Every live run goes through spec 005's runner (`lz-live probe`) as an owner session;
numerical lag/credential windows are required where a check names them. No automatic orders,
production floor binding or ordinary-account singleton changes are authorized.
