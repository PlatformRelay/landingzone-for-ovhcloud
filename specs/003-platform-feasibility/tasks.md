# Tasks: 003-platform-feasibility
Status: planned, re-scoped 2026-10-14 to one project (D113) · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

**Re-scope (D113).** See spec.md *Re-scope to one project*. T002–T008 are closed as SUPERSEDED by
spec 005's run core. T009–T010 stay postponed with OKMS and escrow (D85). T011–T024 and the probes
T025–T028 are open; every live part runs as a probe root under `tests/live/probes/<root>` through
`task live:probe` (`lz-live probe`), as an owner session. The probe root names are planned.
EUR amounts are estimates, not gates. Real tenant isolation waits for a second project.

T001 is the only closed task. Acceptance targets are **planned** and do not exist yet. Test tasks
record a valid control plus behavioural red with compiling stubs; missing tools, compile
errors and outages are not red evidence. Implementation/run tasks need green positive
and rejection controls. Map every task to the spec V checks by its requirement IDs.
All Go test code lives under the single tools Go module, including tools/internal/probes/.
Root tests/ holds HCL fixtures, captured outputs and qualification data consumed by that
module; no second Go module or package outside tools/ is assumed.

## Setup and foundation

- [x] T001 Write cost/sandbox operations ADR at docs/adr/0024-cost-and-sandbox-operations.md — authoring closed 2026-10-02; evidence: [T001](evidence/T001.md). ADR remains Proposed; owner/numerical/setup/live prerequisites remain separate.
  - Requirements: FR-001; ADRs: 0008. Depends on: none. Drafting records unresolved numeric/setup decisions; operator approval gates implementation, not this docs-only task.
  - Verify: exempt — docs-only; content review and operator disposition cover budget units, external inventory, prices/freshness, runtime, concurrency, per-spike timeboxes/spend caps/refutation stops, cleanup authority and health bootstrap before live implementation.
  - Evidence: `.local/evidence/003/t001-cost-adr-review.json`; independent documentation review APPROVE, 2026-10-02; no runtime/live qualification.

- [ ] T002 SUPERSEDED by spec 005's run core (D113; postponed by D85): cost admission, ledger and reaper are hygiene in 005 (FR-011), not gates. Pin remaining helper/provider versions and write RunConfig/admission tests in tools/internal/sandbox/admission_test.go and tests/fixtures/sandbox/
  - Requirements: FR-001, FR-002; ADRs: 0008, 0011. Depends on: T001, 001/T009; approved cost/sandbox ADR.
  - Verify: `go -C tools test ./internal/sandbox -run TestAdmission -count=1`: exact approved valid config accepted; missing/wrong project, numeric limit, D49 maintainer custody or independently usable sealed offline copy, cleanup permission, unknown cost, stale billing, exhausted aggregate or unhealthy reaper rejected; missing/over-ceiling per-spike deadline/spend or absent refutation rule rejected; permissive stub yields behavioural red. The development valid control uses one maintainer plus a separately encrypted sealed offline copy, not a fabricated second human.
  - Evidence: `.local/evidence/003/t002-admission-red.json`; initial status `not-run`.

- [ ] T003 SUPERSEDED by spec 005's run core (D113; postponed by D85): cost admission is hygiene in 005, not a gate. Implement approved config schema, external lease/exposure ledger and admission in tools/internal/sandbox/ and tools/harness/live-checks.yaml
  - Requirements: FR-001, FR-002; ADRs: 0008, 0021. Depends on: T002.
  - Verify: `task test:sandbox-admission`: each valid/invalid/duplicate/concurrent reservation case and timeout/spend/refutation controls green as a suite; guard clause mutation fails; cleanup remains enabled despite admission refusal. Creates target and schema-checked preflight entry.
  - Evidence: `.local/evidence/003/t003-admission.json`; initial status `not-run`.

- [ ] T004 SUPERSEDED by spec 005's run core (D113; postponed by D85): reaper replaced by 005's trap destroy and leftover check. Write ownership, no-id partial create, process-death and reaper-failure tests in tools/internal/sandbox/reaper_test.go
  - Requirements: FR-003; ADRs: 0008, 0021. Depends on: T003.
  - Verify: `go -C tools test ./internal/sandbox -run TestReaper -count=1`: known own-id cleanup accepted, foreign/wrong-project mutation refused; missing-id creation reconciles durable intent without duplicate retry; SIGKILL/reaper outage retained external records; tag-only cleanup stub yields behavioural red.
  - Evidence: `.local/evidence/003/t004-reaper-red.json`; initial status `not-run`.

- [ ] T005 SUPERSEDED by spec 005's run core (D113; postponed by D85): inventory lives in 005's live lane. Implement external resource-id/intention inventory and restricted reaper in tools/internal/sandbox/reaper.go
  - Requirements: FR-002, FR-003; ADRs: 0008, 0021. Depends on: T004.
  - Verify: `task test:sandbox-reaper`: crash/outage recovery reconciles once, no foreign deletion; pending ambiguity blocks new admission; cleanup separately executable; remove id/project guard and mutant fails. Creates target and external health implementation.
  - Evidence: `.local/evidence/003/t005-reaper.json`; initial status `not-run`.

- [ ] T006 SUPERSEDED by spec 005's run core (D113; postponed by D85): escrow and RunConfig postponed; 005 bootstrap verifies the sandbox. Validate operator-provided sandbox, runner, cleanup, escrow and approved run-config inventory through tools/internal/probes/live/sandbox/preflight.go
  - Requirements: FR-001; ADRs: 0008, 0009, 0022. Depends on: T005; operator setup of one pre-existing sandbox project, protected runner, scoped read/apply/cleanup identities, backup destination, D49's one maintainer plus a separately encrypted sealed offline copy for initial development, and numerical RunConfig.
  - Verify: `task live:preflight`: protected read-only real inventory matches approved ids/permissions/health and actual D49 custody/package availability with independently usable access; missing maintainer custody, missing/unusable sealed offline copy, scope, price or heartbeat refuses; no orders/carts/writes. Creates live:preflight; unknown real setup stays blocked. Package presence is not clean recovery proof: T009–T010/V003 must qualify restore without the original account/KMS. No second human custodian or maintainer-loss/production two-person claim.
  - Evidence: `.local/evidence/003/t006-preflight.json`; initial status `not-run`.

## US1

Independent test: V001–V002.

- [ ] T007 [US1] SUPERSEDED by spec 005's run core (D113; postponed by D85): cleanup is covered by 005 G8/G9. Write protected live cleanup-canary/fault helpers in tools/internal/probes/live/sandbox/failure_test.go with offline controls in tools/internal/probes/security/sandbox_faults_test.go
  - Requirements: FR-002, FR-003; ADRs: 0008, 0021. Depends on: T005, T006; separately approved minimal fixture/canary experiment.
  - Verify: `task test:sandbox-reaper` and direct helper tests: cleanup canary succeeds in fake valid scope, overbroad/failed ownership logic red; process-kill and no-id fault schedules captured; create planned task spike:sandbox-failure, still gated on live cleanup canary.
  - Evidence: `.local/evidence/003/t007-failure-controls.json`; initial status `not-run`.

- [ ] T008 [US1] SUPERSEDED by spec 005's run core (D113; postponed by D85): cleanup is covered by 005 G8/G9. Run cleanup canary then protected failure drills against known fixtures in tests/live/sandbox/
  - Requirements: FR-002, FR-003, SC-001; ADRs: 0008, 0021. Depends on: T007; explicit canary and crash-test lease/authorization, healthy externally bootstrapped cleanup.
  - Verify: `task spike:sandbox-failure`: prove canary cleanup first, then SIGKILL/partial-state-write/reaper-outage; external ids/intents survive, cleanup converges with no orphan/foreign removal; unhealthy cleanup blocks further admission but permits cleanup.
  - Evidence: `.local/evidence/003/t008-sandbox-failure.json`; initial status `not-run`.

## US2

Independent test: V003–V005.

- [ ] T009 [US2] Owner gate: postponed (D85) with OKMS and escrow; passphrase encryption is probed in 005/T010. Write actual-tool state/plan encryption and fresh-recovery checks in tools/internal/probes/live/recovery/recovery_test.go and the probe root tests/live/probes/recovery/
  - Requirements: FR-004; ADRs: 0009, 0011, 0022. Depends on: 005/T010 (live probe path; the T008 edge is cut, D85); approved disposable key/data fixture and pinned OKMS helper.
  - Verify: Direct helper tests plus actual pinned `tofu plan -out`/state fixtures: correct-key control, wrong/missing key and enforced plaintext refusal; independently remove state and plan enforcement for behavioural red; no raw canary/state/plan in committed fixtures.
  - Evidence: `.local/evidence/003/t009-recovery-controls.json`; initial status `not-run`.

- [ ] T010 [US2] Owner gate: postponed (D85) with OKMS and escrow; re-runnable bootstrap is 005/T044–T045. Execute clean-runner recovery and empty-bootstrap reconciliation on spec 005's runner with tests/live/probes/recovery/
  - Requirements: FR-004; ADRs: 0009, 0022. Depends on: T009; explicit isolated recovery/backend fixture authority and independently usable escrow package.
  - Verify: `task live:probe -- --reviewed-sha <sha> tests/live/probes/recovery`: correct key consumes actual saved plan/state; wrong/missing keys/plaintext input denied and private canary absent; D49's separately encrypted sealed offline backup restores known data in a clean environment with the original account and KMS unavailable; wrong escrow and same-key replica negatives fail; empty-state import reconciles ids and reissues unreadable OAuth secret. This qualifies only the approved development route, not recovery when the sole maintainer is unavailable or production two-person custody.
  - Evidence: `.local/evidence/003/t010-recovery.json`; initial status `not-run`.

- [ ] T011 [US2] Write cross-tenant state and artefact route checks on the S3 plane in tools/internal/probes/live/state/isolation_test.go and the probe root tests/live/probes/state-isolation/
  - Requirements: FR-005 (C005.1, C005.7); ADRs: 0004, 0009, 0021. Depends on: 005/T049 (tenant S3 user and tenant state bucket exist; T010 edge cut, D85); two independently scoped tenant S3 credentials and buckets within the single approved project. Re-scoped by D113: the S3 credential plane only (Keystone/Swift reach is T025); the OVHcloud API plane is spec 005's KD-1 and decrypt (C005.2) is withdrawn; no cross-project isolation claim.
  - Verify: `go -C tools test ./internal/probes/live/state -run TestIsolation -count=1`: own-route control succeeds; A→B and B→A read, write and artefact calls with S3 credentials require an attributable denial; an outage, 404, 5xx or expired token is never counted as a denial; a permissive evaluator is behavioural red. The probe root plans offline (`tofu validate`) before any live run.
  - Evidence: `.local/evidence/003/t011-isolation-controls.json`; initial status `not-run`.

- [ ] T012 [US2] Owner session: run the S3-plane isolation probe on spec 005's runner with tests/live/probes/state-isolation/
  - Requirements: FR-005 (C005.1, C005.7); ADRs: 0004, 0009, 0021. Depends on: T011, T025 (Keystone/Swift reach, the second plane D113 keeps); owner approval of the live run.
  - Verify: `task live:probe -- --reviewed-sha <sha> tests/live/probes/state-isolation`: all V004 own successes and foreign denials recorded with route, identity and error reason; T025's result recorded next to them; an ambiguous denial or unprobed credential path blocks the corresponding isolation claim; leftover check clean. The result says S3 and Keystone/Swift planes inside one project only: real tenant isolation waits for a second project (D113).
  - Evidence: `.local/evidence/003/t012-isolation.json`; initial status `not-run`.

- [ ] T013 [US2] Write native locking/interruption/promotion checks on an Object-Locked bucket in tools/internal/probes/live/state/locking_test.go and the probe root tests/live/probes/state-locking/
  - Requirements: FR-005 (C005.3–C005.6, C005.8); ADRs: 0009, 0022. Depends on: T012, T028 (force-unlock and version restore under Object Lock); approved lag/runtime and writer-revocation mechanism.
  - Verify: `go -C tools test ./internal/probes/live/state -run TestLocking -count=1`: actual expected lock shape/provenance, serialized writers, owner-death before stale-lock recovery, all-old-writer fencing before replica; the bucket is created with Object Lock in governance mode, as the state buckets are (D104), and every `.tflock` version is retained; accepting a cached old credential yields behavioural red.
  - Evidence: `.local/evidence/003/t013-locking-controls.json`; initial status `not-run`.

- [ ] T014 [US2] Owner session: run concurrency, writer interruption and replica promotion on spec 005's runner with tests/live/probes/state-locking/
  - Requirements: FR-005 (C005.3–C005.6, C005.8); ADRs: 0009, 0022. Depends on: T013; separate approved interruption/promotion experiment, isolated backend snapshots and cleanup authority.
  - Verify: `task live:probe -- --reviewed-sha <sha> tests/live/probes/state-locking`: concurrent writers serialize; owner-death proof precedes lock removal; old primary and cached credentials demonstrably denied before replica writes; replica lag <=approved numeric bound; unfenced path aborts promotion; the locked fixture is emptied with the admin's governance bypass (005/T099 janitor) or left to its retention, and the run records which and what it cost.
  - Evidence: `.local/evidence/003/t014-locking.json`; initial status `not-run`.

## US3

Independent test: V006–V007.

- [ ] T015 [US3] Write canary floor/route checks in tools/internal/probes/live/identity/deny_floor_test.go and the probe root tests/live/probes/deny-floor/
  - Requirements: FR-006; ADRs: 0006, 0018, 0021. Depends on: 005/T047 (live lane with trap destroy and leftover check; T008 edge cut, D85), T027 (the URN `account:apiovh:iam/*` actions are checked against); approved disposable principal/actions and route matrix. The tag-envelope subcontrols (C006.9–C006.11) are withdrawn (P25 refuted, D113).
  - Verify: `go -C tools test ./internal/probes/live/identity -run TestDenyFloor -count=1`: delete and floor-binding/policy self-removal each have a valid control and an attributable-denial case; `account:apiovh:iam/*` denials bind to the URN T027 observed; a deny on state buckets inside a tenant's project is reported unsupported (bucket tags do not scope, P25), not passed; removing any evaluator clause is behavioural red; unknown subcontrol cannot pass spike 3.
  - Evidence: `.local/evidence/003/t015-floor-controls.json`; initial status `not-run`.

- [ ] T016 [US3] Observe native break-glass recovery before floor binding through tools/internal/probes/live/identity/recovery_test.go
  - Requirements: FR-006; ADRs: 0006, 0009, 0018. Depends on: T015; separately approved native recovery rehearsal; federation route needs disposable account scope.
  - Verify: Procedure: on disposable canary scope verify native issuer/authenticator and recovery action with floor absent and simulated federation unavailable; inaccessible native route blocks binding; retain protected evidence and no ordinary-account federation change.
  - Evidence: `.local/evidence/003/t016-native-recovery.json`; initial status `not-run`.

- [ ] T017 [US3] Owner session: run the approved canary floor probes on spec 005's runner with tests/live/probes/deny-floor/
  - Requirements: FR-006; ADRs: 0006, 0018, 0021. Depends on: T016; explicit reviewed floor-bind/self-removal/deletion probes; applicable federation account setup.
  - Verify: `task live:probe -- --reviewed-sha <sha> tests/live/probes/deny-floor`: per-route positive and attributable deny covers all V006 subcontrols; native recovery still works; bound polling by the run deadline, measure apply-to-effective allow/deny delay with repeated positive/negative observations, and record policy/group/tag counts and published limit provenance without filling accounts to capacity; alternate Keystone/S3 paths independently observed (T025); missing/failing subcontrol blocks full spike-3 pass, no production rollout.
  - Evidence: `.local/evidence/003/t017-deny-floor.json`; initial status `not-run`.

- [ ] T018 [US3] Write issuer/bridge/fallback revocation checks in tools/internal/probes/live/identity/credentials_test.go and the probe root tests/live/probes/credentials/
  - Requirements: FR-007; ADRs: 0009, 0018. Depends on: T017; approved per-issuer residual/renewal windows and actual pins.
  - Verify: `go -C tools test ./internal/probes/live/identity -run TestCredentials -count=1`: valid issue/refresh succeeds, missing authenticator/expiry/failed cleanup and already-issued token beyond approved bound reject; stub testing only fresh login is behavioural red.
  - Evidence: `.local/evidence/003/t018-credential-controls.json`; initial status `not-run`.

- [ ] T019 [US3] Owner session: run issuer authentication, refresh, expiry, cleanup and offboarding probes on spec 005's runner with tests/live/probes/credentials/
  - Requirements: FR-007; ADRs: 0009, 0018. Depends on: T018; approved scoped issuer/revoke authority and numeric windows.
  - Verify: `task live:probe -- --reviewed-sha <sha> tests/live/probes/credentials`: use already-issued OVH/OpenStack credentials, measure expiry/revoke within approved bound; qualify bridge via exact OpenStack provider or block it; fallback runs same cases; failed cleanup retains inventory and prevents dependent authority. T026's residual window for a deleted OAuth2 client is one of the recorded windows.
  - Evidence: `.local/evidence/003/t019-credentials.json`; initial status `not-run`.

## US4

Independent test: V008.

- [ ] T020 [US4] Capture real provider schemas and naming/tag/route applicability in tools/internal/probes/live/qualification/capture.go and tests/fixtures/ovhcloud/
  - Requirements: FR-008; ADRs: 0006, 0011, 0018. Depends on: T012, T017, T019; scoped read authority and separately approved metadata mutation where needed.
  - Verify: Procedure: run pinned `tofu providers schema -json`, record actual command/version/hash privately and sanitise schema fixture; observed limits/route subcontrols cite primary source and actual probe; wrong-pin/unknown limit refuses qualification and remains UNVERIFIED; capture IAM policy/group/tag limits from primary sources or approved read-only discovery, and bounded measured propagation windows from the floor probes.
  - Evidence: `.local/evidence/003/t020-provider-and-limits.json`; initial status `not-run`.

- [ ] T021 [US4] Write qualification completeness tests in tools/internal/probes/live/qualification/qualification_test.go with a minimal compiling oracle stub
  - Requirements: FR-009, SC-002; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: 001/T009; offline tests need no live capture. Spec 002 edges cut (D85: transaction, binding and reservations postponed); their observations report `blocked`, never pass.
  - Verify: `go -C tools test ./internal/probes/live/qualification -run TestQualification -count=1`: retain a valid complete packet; permissive stub is behaviorally red on omitted encryption/floor/race/cleanup/review observations, invented IAM limits, unmeasured propagation guarantees, missing refutation stops and unsupported readiness. The packet records P25 as refuted (the tag envelope stays withdrawn) and cost as an estimate plus the actual, never as a gate (D113). Test each guarded clause; authoring closes with valid control plus red, never claimed green implementation.
  - Aggregate ADR rationale: 0004 local ownership; 0006 floor; 0008 admission/cleanup; 0009 encryption/issuers; 0011 pins; 0018 principal routes; 0021 isolation; 0022 readiness/recovery.
  - Evidence: `.local/evidence/003/t021-qualification-controls.json`; initial status `not-run`.

- [ ] T022 [US4] Implement the completeness oracle in tools/internal/probes/live/qualification/qualification.go and its fixture/live-packet target
  - Requirements: FR-009, SC-002; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T021. Actual T020 observations gate live qualification, not offline implementation.
  - Verify: `task test:qualification` offline fixture mode: same T021 positive/rejection controls green; each guard mutant fails. Creates target. Fixture acceptance is labelled synthetic, never real readiness. Real-packet mode uses the same oracle: refuted controls return fail, incomplete/timeout/cap-limited inputs blocked, never pass from a completed decision table.
  - Aggregate ADR rationale: 0004 local ownership; 0006 floor; 0008 admission/cleanup; 0009 encryption/issuers; 0011 pins; 0018 routes; 0021 isolation; 0022 readiness.
  - Evidence: `.local/evidence/003/t022-qualification.json`; initial status `not-run`.

- [ ] T023 [US4] Independently inspect spike packets and run the aggregate decision through the T022 oracle
  - Requirements: FR-008, FR-009, SC-001, SC-002; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T022; fresh reviewer and result records for T014/T017/T019/T020 and T025–T028 (observed pass/refuted or explicit blocked prerequisite; T010 postponed, recorded blocked). Spec 002 is retired (D113). Required live observations gate qualification, not writing a negative decision record.
  - Verify: `task test:qualification`: ranked spikes 1–7 have actual scoped results (spike 4 retired with spec 002, spike 6 superseded; D113), costs/cleanup, matching source/pins and independent disposition; refuted controls fail qualification and withdraw the predefined claim; missing/timeout/cap-limited observation remains blocked. A completed decision record is not a qualification pass and cannot close an incomplete experiment.
  - Aggregate ADR rationale: 0004 local ownership; 0006 floor; 0008 admission/cleanup; 0009 encryption/issuers; 0011 captured pins; 0018 principal routes; 0021 isolation; 0022 readiness/recovery.
  - Evidence: `.local/evidence/003/t023-aggregate.json`; initial status `not-run`.

## Polish and exit

- [ ] T024 Update Proposed ADRs and sanitized cost/gap notes in docs/adr/, docs/reference/verification-status.md and specs/003-platform-feasibility/decisions.md
  - Requirements: FR-009; ADRs: 0006, 0008, 0009, 0018, 0022. Depends on: T023.
  - Verify: exempt — docs-only; content review records observations/refutations/owed evidence; operator alone ratifies ADRs; no product tuple marked supported from partial tests.
  - Evidence: `.local/evidence/003/t024-docs-review.json`; initial status `not-run` (docs content review only).

## Probes from spec 005's live findings (D113)

Each is one probe root on spec 005's runner, run as an owner session, cheap and torn down by the
runner. The checks are written test-first against fakes; the live run records the observation.

- [ ] T025 [US2] Probe the tenant S3 user's reach to other buckets through Keystone/Swift in tools/internal/probes/live/storageiam/ and the probe root tests/live/probes/swift-reach/
  - Requirements: FR-005 (C005.1), FR-006 (C006.4, C006.5); ADRs: 0004, 0009, 0018. Depends on: 005/T049 (tenant S3 user pattern), 005/T010 storage-iam root. The S3 user belongs to a cloud-project (OpenStack) user; whether its credentials can obtain a Keystone token and reach another bucket through Swift, bypassing the S3 bucket-ARN policy (005 G6), is not observed anywhere (UNVERIFIED).
  - Verify: `go -C tools test ./internal/probes/live/storageiam -run TestSwiftReach -count=1`: a listing, read or write of the second bucket through Swift counts as reach; a 401/403 counts as denial only next to a same-route positive control on the probe's own bucket; an outage, 404 or 5xx is `blocked`, never denial. Live: `task live:probe -- --reviewed-sha <sha> tests/live/probes/swift-reach` with two probe buckets, records each call's status and reason; observed reach refutes the S3-plane isolation claim and stops T012's claim; leftover check clean.
  - Evidence: `specs/003-platform-feasibility/evidence/T025.md`; initial status `not-run`.

- [ ] T026 [US3] Probe whether an access token still works after its OAuth2 client is deleted in tools/internal/probes/live/identity/ and the probe root tests/live/probes/token-residual/
  - Requirements: FR-007 (C007.5, C007.6); ADRs: 0009, 0018. Depends on: 005/T010 iam root (OAuth2 client and policy create/delete observed, P7, P8). The residual window of an already-issued access token after its client is deleted is not observed (UNVERIFIED).
  - Verify: `go -C tools test ./internal/probes/live/identity -run TestTokenResidual -count=1`: the check repeats one granted read with the already-issued token at bounded intervals after the delete, until it is refused or the token's own expiry passes; a 5xx is an outage (005/T095), never a refusal; a stub that only tests a fresh login is behavioural red. Live: `task live:probe -- --reviewed-sha <sha> tests/live/probes/token-residual`: create a disposable client with a one-read policy, take a token, delete the client, record the measured window (or "valid until expiry"); the policy is removed on exit and the leftover check is clean.
  - Evidence: `specs/003-platform-feasibility/evidence/T026.md`; initial status `not-run`.

- [ ] T027 [US3] Probe which URN `account:apiovh:iam/*` actions are checked against in tools/internal/probes/live/identity/ and the probe root tests/live/probes/iam-urn/
  - Requirements: FR-006 (C006.2, C006.5), FR-008 (C008.1); ADRs: 0006, 0018. Depends on: 005/T049 run 3 and D109 (`account:apiovh:iam/resource/edit` granted on a project URN did not authorise tagging that project; the account URN is the likely target, UNVERIFIED).
  - Verify: `go -C tools test ./internal/probes/live/identity -run TestIamUrn -count=1`: the same call is made by a disposable identity granted one `account:apiovh:iam/*` action on a project URN, then on the account URN; success under one and an attributable 403 under the other names the URN; both refused or a 5xx is `blocked`. Live: `task live:probe -- --reviewed-sha <sha> tests/live/probes/iam-urn`: prefer a read action; any write targets only a resource the probe created; the policies are removed on exit and the leftover check is clean. T015 binds the floor's IAM denials to the observed URN.
  - Evidence: `specs/003-platform-feasibility/evidence/T027.md`; initial status `not-run`.

- [ ] T028 [US2] Probe `force-unlock` and restoring an earlier state version on an Object-Locked bucket in tools/internal/probes/live/state/ and the probe root tests/live/probes/locked-recovery/
  - Requirements: FR-004 (C004.1), FR-005 (C005.4, C005.8); ADRs: 0009, 0022. Depends on: 005/T099 and 005/T010 row 13 (probe bucket with Object Lock, governance, one day: versioned delete refused to the state credential, the janitor empties it with the bypass).
  - Verify: `go -C tools test ./internal/probes/live/state -run TestLockedRecovery -count=1`: fakes cover a lock held by a killed writer, `tofu force-unlock` with the state credential, a second writer acquiring the lock, and restoring an earlier state version as current by version id; a refused unlock or restore is recorded with its reason, not retried as success. Live: `task live:probe -- --reviewed-sha <sha> tests/live/probes/locked-recovery` on a one-day governance-locked probe bucket: record whether `force-unlock` and the restore succeed with the state credential, then empty the bucket with the janitor's bypass; record the cost of versions retained until the retention ends.
  - Evidence: `specs/003-platform-feasibility/evidence/T028.md`; initial status `not-run`.

## Planned command creators

| Task target | Creating task |
| --- | --- |
| `task test:qualification` | T022 |

Live probes add no target: each task above that writes a probe root under `tests/live/probes/`
(T009, T011, T013, T015, T018, T025–T028) runs it with spec 005's `task live:probe` (D113).

## Dependencies & execution order

The per-task Depends on fields are authoritative; local task references form a DAG.
Test tasks precede their implementation; all external gates remain blocked until
observed. A command may be defined with an explicit blocked result before its live
prerequisites exist; defining it does not close its acceptance check.

MVP (D85, D113): the spec 005 live lane (005/T047) is the safety gate for every later live branch;
T002–T008 are superseded and T009–T010 postponed. Recovery/isolation/locking and identity
can branch after 005/T047 with distinct fixtures; T016 must precede
floor binding, and old-writer fencing precedes replica activation. T023 requires result records for both branches; spec 002 evidence is postponed (D85)
and reported blocked; no live forge adapter gate. T021 tests and T022 implementation are offline. A stopped
branch remains blocked in the decision packet; T023 can record the stop but cannot pass
qualification or close missing live tasks. Missing live authorization does not block offline preparation.

## Parallel opportunities

Within 003, state T011–T014 and T025/T028, and identity T015–T019 and T026/T027, branch after
005/T047 with distinct fixtures and no overlapping live writers.
The offline oracle T021–T022 needs only its listed local prerequisites. No parallel
flag bypasses ownership or cleanup gates.

## Completion

Rerun spec acceptance commands and inspect matching evidence; do not close a task using
a missing-binary result, empty discovery, self-authored green assertion or a simulation
standing in for required live proof. Docs-only work remains exempt. Only the operator
activates dependent live experiments/ratifies ADRs. A refuted observed premise gets its
explicit downstream stop decision; an unrun required experiment stays owed and blocked.
