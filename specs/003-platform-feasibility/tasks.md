# Tasks: 003-platform-feasibility
Status: planned · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

All boxes are open. Acceptance targets are **planned** and do not exist yet. Test tasks
record a valid control plus behavioural red with compiling stubs; missing tools, compile
errors and outages are not red evidence. Implementation/run tasks need green positive
and rejection controls. Map every task to the spec V checks by its requirement IDs.
All Go test code lives under the single tools Go module, including tools/internal/probes/.
Root tests/ holds HCL fixtures, captured outputs and qualification data consumed by that
module; no second Go module or package outside tools/ is assumed.

## Setup and foundation

- [ ] T001 Write cost/sandbox operations ADR at docs/adr/0024-cost-and-sandbox-operations.md
  - Requirements: FR-001; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: 001/T022; operator decisions for numeric limits and setup.
  - Verify: exempt — docs-only; content review and operator disposition cover budget units, external inventory, prices/freshness, runtime, concurrency, cleanup authority and health bootstrap before live implementation.
  - Evidence: `.local/evidence/003/t001-cost-adr-review.json`; initial status `not-run` (docs content review only).

- [ ] T002 Pin remaining helper/provider versions and write RunConfig/admission tests in tools/internal/sandbox/admission_test.go and tests/fixtures/sandbox/
  - Requirements: FR-001, FR-002; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T001; approved cost/sandbox ADR.
  - Verify: `go -C tools test ./internal/sandbox -run TestAdmission -count=1`: exact approved valid config accepted; missing/wrong project, numeric limit, holder, cleanup permission, unknown cost, stale billing, exhausted aggregate or unhealthy reaper rejected; permissive stub yields behavioural red.
  - Evidence: `.local/evidence/003/t002-admission-red.json`; initial status `not-run`.

- [ ] T003 Implement approved config schema, external lease/exposure ledger and admission in tools/internal/sandbox/ and harness/live-checks.yaml
  - Requirements: FR-001, FR-002; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T002.
  - Verify: `task test:sandbox-admission`: each valid/invalid/duplicate/concurrent reservation case green as a suite; guard clause mutation fails; cleanup remains enabled despite admission refusal. Creates target and schema-checked preflight entry.
  - Evidence: `.local/evidence/003/t003-admission.json`; initial status `not-run`.

- [ ] T004 Write ownership, no-id partial create, process-death and reaper-failure tests in tools/internal/sandbox/reaper_test.go
  - Requirements: FR-003; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T003.
  - Verify: `go -C tools test ./internal/sandbox -run TestReaper -count=1`: known own-id cleanup accepted, foreign/wrong-project mutation refused; missing-id creation reconciles durable intent without duplicate retry; SIGKILL/reaper outage retained external records; tag-only cleanup stub yields behavioural red.
  - Evidence: `.local/evidence/003/t004-reaper-red.json`; initial status `not-run`.

- [ ] T005 Implement external resource-id/intention inventory and restricted reaper in tools/internal/sandbox/reaper.go
  - Requirements: FR-002, FR-003; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T004.
  - Verify: `task test:sandbox-reaper`: crash/outage recovery reconciles once, no foreign deletion; pending ambiguity blocks new admission; cleanup separately executable; remove id/project guard and mutant fails. Creates target and external health implementation.
  - Evidence: `.local/evidence/003/t005-reaper.json`; initial status `not-run`.

- [ ] T006 Validate operator-provided sandbox, runner, cleanup, escrow and approved run-config inventory through tools/internal/probes/live/sandbox/preflight.go
  - Requirements: FR-001; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T005; operator setup of pre-existing projects, protected runner, scoped read/apply/cleanup identities, backup destination, two holders and numerical RunConfig.
  - Verify: `task live:preflight`: protected read-only real inventory matches approved ids/permissions/health; missing holder, scope, price or heartbeat refuses; no orders/carts/writes. Creates live:preflight; unknown real setup stays blocked.
  - Evidence: `.local/evidence/003/t006-preflight.json`; initial status `not-run`.

## US1

Independent test: V001–V002.

- [ ] T007 [US1] Write protected live cleanup-canary/fault helpers in tools/internal/probes/live/sandbox/failure_test.go with offline controls in tools/internal/probes/security/sandbox_faults_test.go
  - Requirements: FR-002, FR-003; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T005, T006; separately approved minimal fixture/canary experiment.
  - Verify: `task test:sandbox-reaper` and direct helper tests: cleanup canary succeeds in fake valid scope, overbroad/failed ownership logic red; process-kill and no-id fault schedules captured; create planned task spike:sandbox-failure, still gated on live cleanup canary.
  - Evidence: `.local/evidence/003/t007-failure-controls.json`; initial status `not-run`.

- [ ] T008 [US1] Run cleanup canary then protected failure drills against known fixtures in tests/live/sandbox/
  - Requirements: FR-002, FR-003, SC-001; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T007; explicit canary and crash-test lease/authorization, healthy externally bootstrapped cleanup.
  - Verify: `task spike:sandbox-failure`: prove canary cleanup first, then SIGKILL/partial-state-write/reaper-outage; external ids/intents survive, cleanup converges with no orphan/foreign removal; unhealthy cleanup blocks further admission but permits cleanup.
  - Evidence: `.local/evidence/003/t008-sandbox-failure.json`; initial status `not-run`.

## US2

Independent test: V003–V005.

- [ ] T009 [US2] Write actual-tool state/plan encryption and fresh-recovery helpers in tools/internal/probes/live/recovery/recovery_test.go and tests/recovery/
  - Requirements: FR-004; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T008; approved disposable key/data fixture and pinned OKMS helper.
  - Verify: Direct helper tests plus actual pinned `tofu plan -out`/state fixtures: correct-key control, wrong/missing key and enforced plaintext refusal; independently remove state and plan enforcement for behavioural red; no raw canary/state/plan in committed fixtures. Creates task spike:recovery.
  - Evidence: `.local/evidence/003/t009-recovery-controls.json`; initial status `not-run`.

- [ ] T010 [US2] Execute clean-runner recovery and empty-bootstrap reconciliation in tests/live/recovery/
  - Requirements: FR-004; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T009; explicit isolated recovery/backend fixture authority and independently usable escrow package.
  - Verify: `task spike:recovery`: correct key consumes actual saved plan/state; wrong/missing keys/plaintext input denied and private canary absent; independent backup restores known data with original KMS unavailable; same-key replica negative fails; empty-state import reconciles ids and reissues unreadable OAuth secret.
  - Evidence: `.local/evidence/003/t010-recovery.json`; initial status `not-run`.

- [ ] T011 [US2] Write cross-tenant state/artifact/decrypt route helpers in tools/internal/probes/live/state/isolation_test.go
  - Requirements: FR-005; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T010; two independently scoped tenant credentials/fixtures.
  - Verify: Direct helper tests: own-route control succeeds, A→B/B→A read/write/decrypt/artifact calls require attributable denial; outage/404/expired token cannot be counted as deny; permissive evaluator is behavioural red. Creates task spike:state-isolation.
  - Evidence: `.local/evidence/003/t011-isolation-controls.json`; initial status `not-run`.

- [ ] T012 [US2] Execute per-tenant state and decrypt isolation probes in tests/live/state/
  - Requirements: FR-005; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T011; approved bidirectional state/artifact/decrypt probe leases.
  - Verify: `task spike:state-isolation`: all V004 own successes and foreign denials recorded with route/identity/error reason; ambiguous denial or unprobed credential path blocks corresponding isolation claim.
  - Evidence: `.local/evidence/003/t012-isolation.json`; initial status `not-run`.

- [ ] T013 [US2] Write native locking/interruption/promotion helpers in tools/internal/probes/live/state/locking_test.go
  - Requirements: FR-005; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T012; approved lag/runtime and writer-revocation mechanism.
  - Verify: Direct helper tests: actual expected lock shape/provenance, serialized writers, owner-death before stale-lock recovery, all-old-writer fencing before replica; accepting cached old credential yields behavioural red. Creates task spike:locking-promotion.
  - Evidence: `.local/evidence/003/t013-locking-controls.json`; initial status `not-run`.

- [ ] T014 [US2] Execute concurrency, writer interruption and replica promotion in tests/live/state/
  - Requirements: FR-005; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T013; separate approved interruption/promotion experiment, isolated backend snapshots and cleanup authority.
  - Verify: `task spike:locking-promotion`: concurrent writers serialize; owner-death proof precedes lock removal; old primary and cached credentials demonstrably denied before replica writes; replica lag <=approved numeric bound; unfenced path aborts promotion.
  - Evidence: `.local/evidence/003/t014-locking.json`; initial status `not-run`.

## US3

Independent test: V006–V007.

- [ ] T015 [US3] Write canary floor/route/tag-envelope helpers in tools/internal/probes/live/identity/deny_floor_test.go
  - Requirements: FR-006; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T008; approved disposable principal/actions and route matrix.
  - Verify: Direct helper tests: delete/floor-binding+policy self-removal/missing+present tags/cross-tenant retag/applicable child operations each have valid control and attributable-denial case; removing any evaluator clause is behavioural red; unknown subcontrol cannot pass spike 3. Creates task spike:deny-floor.
  - Evidence: `.local/evidence/003/t015-floor-controls.json`; initial status `not-run`.

- [ ] T016 [US3] Observe native break-glass recovery before floor binding through tools/internal/probes/live/identity/recovery_test.go
  - Requirements: FR-006; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T015; separately approved native recovery rehearsal; federation route needs disposable account scope.
  - Verify: Procedure: on disposable canary scope verify native issuer/authenticator and recovery action with floor absent and simulated federation unavailable; inaccessible native route blocks binding; retain protected evidence and no ordinary-account federation change.
  - Evidence: `.local/evidence/003/t016-native-recovery.json`; initial status `not-run`.

- [ ] T017 [US3] Execute approved canary floor and tag-envelope probes in tests/live/identity/
  - Requirements: FR-006; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T016; explicit reviewed floor-bind/self-removal/retag/deletion probes, healthy sandbox lease; applicable federation account setup.
  - Verify: `task spike:deny-floor`: per-route positive and attributable deny covers all V006 subcontrols; native recovery still works; alternate Keystone/S3 paths independently observed; missing/failing subcontrol blocks full spike-3/envelope-authority pass, no production rollout.
  - Evidence: `.local/evidence/003/t017-deny-floor.json`; initial status `not-run`.

- [ ] T018 [US3] Write issuer/bridge/fallback revocation helpers in tools/internal/probes/live/identity/credentials_test.go
  - Requirements: FR-007; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T017; approved per-issuer residual/renewal windows and actual pins.
  - Verify: Direct helper tests: valid issue/refresh succeeds, missing authenticator/expiry/failed cleanup and already-issued token beyond approved bound reject; stub testing only fresh login is behavioural red. Creates task spike:credentials.
  - Evidence: `.local/evidence/003/t018-credential-controls.json`; initial status `not-run`.

- [ ] T019 [US3] Execute issuer authentication, refresh, expiry, cleanup and offboarding probes in tests/live/identity/
  - Requirements: FR-007; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T018; approved scoped issuer/revoke authority and numeric windows.
  - Verify: `task spike:credentials`: use already-issued OVH/OpenStack credentials, measure expiry/revoke within approved bound; qualify bridge via exact OpenStack provider or block it; fallback runs same cases; failed cleanup retains inventory and prevents dependent authority.
  - Evidence: `.local/evidence/003/t019-credentials.json`; initial status `not-run`.

## US4

Independent test: V008.

- [ ] T020 [US4] Capture real provider schemas and naming/tag/route applicability in tools/internal/probes/live/qualification/capture.go and catalog/
  - Requirements: FR-008; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T012, T017, T019; scoped read authority and separately approved metadata mutation where needed.
  - Verify: Procedure: run pinned `tofu providers schema -json`, record actual command/version/hash privately and sanitise schema fixture; observed limits/route subcontrols cite primary source and actual probe; wrong-pin/unknown limit refuses qualification and remains UNVERIFIED.
  - Evidence: `.local/evidence/003/t020-provider-and-limits.json`; initial status `not-run`.

- [ ] T021 [US4] Write qualification completeness oracle tests in tools/internal/probes/live/qualification/qualification_test.go
  - Requirements: FR-009, SC-002; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T020; 002/T021 required for aggregate outcome.
  - Verify: `go -C tools test ./internal/probes/live/qualification -run TestQualification -count=1`: compiling permissive oracle stub is behaviourally red when any encryption/floor/tag/race/cost/cleanup/independent-review observation is omitted from an otherwise complete packet or an unsupported tuple claims readiness; remove each guarded clause and confirm red. Authoring closes red-first; implementation follows in T022.
  - Evidence: `.local/evidence/003/t021-qualification-controls.json`; initial status `not-run`.

- [ ] T022 [US4] Implement the qualification completeness oracle in tools/internal/sandbox/qualification.go
  - Requirements: FR-009, SC-002; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T021.
  - Verify: `task spike:qualification` offline fixture mode: all T021 controls green — complete independent packet accepted; omitted encryption/floor/tag/race/cost/cleanup/independent-review observation or unsupported tuple claim rejected. Creates target; live aggregate uses same oracle.
  - Evidence: `.local/evidence/003/t022-qualification.json`; initial status `not-run`.

- [ ] T023 [US4] Independently inspect spike packets and run aggregate decision through harness/live-checks.yaml
  - Requirements: FR-008, FR-009, SC-001, SC-002; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T010, T014, T017, T019, T022; fresh independent reviewer and 002/T021 evidence.
  - Verify: `task spike:qualification`: ranked spikes 1–7 (plan.md "Ranked spike register") have actual scoped results, costs/cleanup, matching source/pins and independent disposition; unsupported/refuted route blocks downstream claim; missing observation remains blocked, cannot close incomplete required experiment.
  - Evidence: `.local/evidence/003/t023-aggregate.json`; initial status `not-run`.

## Polish and exit

- [ ] T024 Update Proposed ADRs and sanitized cost/gap notes in docs/adr/, docs/reference/{test-costs,provider-gaps}.md and specs/003-platform-feasibility/decisions.md
  - Requirements: FR-009; ADRs: 0004, 0006, 0008, 0009, 0011, 0018, 0021, 0022. Depends on: T023.
  - Verify: exempt — docs-only; content review records observations/refutations/owed evidence; operator alone ratifies ADRs; no product tuple marked supported from partial tests.
  - Evidence: `.local/evidence/003/t024-docs-review.json`; initial status `not-run` (docs content review only).

## Planned command creators

| Task target | Creating task |
| --- | --- |
| `task test:sandbox-admission` | T003 |
| `task test:sandbox-reaper` | T005 |
| `task live:preflight` | T006 |
| `task spike:sandbox-failure` | T007 |
| `task spike:recovery` | T009 |
| `task spike:state-isolation` | T011 |
| `task spike:locking-promotion` | T013 |
| `task spike:deny-floor` | T015 |
| `task spike:credentials` | T018 |
| `task spike:qualification` | T022 |

## Dependencies & execution order

The per-task Depends on fields are authoritative; local task references form a DAG.
Test tasks precede their implementation; all external gates remain blocked until
observed. A command may be defined with an explicit blocked result before its live
prerequisites exist; defining it does not close its acceptance check.

MVP: admission/reaper offline. T001→T002→T003→T004→T005→T006→T007→T008
is the safety gate for every later live branch. Recovery/isolation/locking and identity
can branch after T008 with distinct fixtures and aggregate leases; T016 must precede
floor binding, and old-writer fencing precedes replica activation. T023 requires both
branches and 002 evidence. Missing live authorization does not block offline preparation.

## Parallel opportunities

No [P] flags are used across incomplete test/implementation dependencies. Within this
feature, state probes (T009–T014) versus identity probes (T015–T019) can proceed after
T008 only with independent approved leases and no shared live writers.

## Completion

Rerun spec acceptance commands and inspect matching evidence; do not close a task using
a missing-binary result, empty discovery, self-authored green assertion or a simulation
standing in for required live proof. Docs-only work remains exempt. Only the operator
activates dependent live experiments/ratifies ADRs. A refuted observed premise gets its
explicit downstream stop decision; an unrun required experiment stays owed and blocked.
