# Tasks: 003-platform-feasibility
Status: planned, re-scoped 2026-10-14 to one project (D113) · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

**Re-scope (D113).** See spec.md *Re-scope to one project*. T002–T008 are closed as SUPERSEDED by
spec 005's run core. T009–T010 stay postponed with OKMS and escrow (D85). T011–T024 and the probes
T025–T038 are open; every live part runs as a probe root under `tests/live/probes/<root>` through
`task live:probe` (`lz-live probe`), as an owner session, except the probe runs T025–T028, which
the coordinator runs under the owner's standing grant (D117). The probe root names are planned.
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

Four probes, each as three tasks in this order: a test task (cases against fakes, closed on a
verified red with the stub data-model.md *Probe checks* fixes), its implementation task (the
check, the probe root under `tests/live/probes/`, the root's registration in lz-live), and a
live run, after one runner change every probe needs (T037, T038: a sweep that cleans up without
the root's outputs). The live runs keep the ids T025–T028, which T012, T013, T015, T019 and T023 cite for
the observation.

Offline: every Verify below runs through the offline entry,
`lz-offline --candidate <checkout> -- task <target>`; the entry admits only a Task target and
never runs `go test` itself (tools/cmd/lz-offline/main.go `parseCommand`). The target is
`task test:live-lane`, which runs the Go tests of `./internal/probes/live/state` and
`./internal/probes/live/storageiam` and `lz-check unit` on each probe root it lists. T031 adds
`./internal/probes/live/identity` to both of its Go lines; each implementation task adds its
root's `lz-check unit` line. No new target. A test task's red is the target's red: its evidence
quotes each new test's failure line from the target's output.

Rules every test task's cases follow:

- **No vacuous red.** Each case asserts the requests the fakes consumed (method, path or
  operation, which credential, in order) and the fakes' answers consumed, and then the expected
  sanitised observation (step, expected, observed, status, code or actions, `as_expected`). A stub
  whose Observe and Undo only return "not implemented" therefore fails each case on its first
  missing request, for the reason the case states, never on the error text alone.
- **Leak guard shown separately.** One test runs the case's redaction assertion against a
  leaking mutant (a test double that writes the published secret into the record and the
  terminal) and shows the assertion fails; only then is the assertion trusted on the check.
- **One owner per resource.** Every probe resource is created either by the root (tofu) or by the
  check, never both; IAM clients and policies belong to the check. The root's resources are
  destroyed by tofu, the check's by its Undo.
- **Cleanup independent of outputs.** A lost create answer can leave no root output (then the
  runner skips Undo, tools/internal/live/runner.go:743-745) and no state entry (then destroy
  does not know the resource). So every check also has a Sweep (T037, T038) that the runner
  calls with only the run id and the project it recorded in probe.env, on every way out and on
  `--cleanup <run-id>`: it lists every kind the root and the check create, by the exact names
  `lzprobe-<root>-<lower(run id)>…` (IAM names `lzprobe-<root>-<run id>…`), deletes them, lists
  again and fails on anything left. It never reads a published output. Each test task has a
  fresh-process Sweep case, with no prior Observe and no output, after every partial creation,
  including a create whose answer was lost (the fake creates the object, then drops the answer).
- **Names.** Validate checks `run_id` against `live.RunIDPattern` and compares every published
  name exactly with the one derived from it (as storageiam's `validate`,
  tools/internal/probes/live/storageiam/storageiam.go:259-264). Bucket names use
  `lower(run id)` (a run id holds `T` and `Z`; tests/live/probes/storage-iam/main.tf), IAM names
  the run id as is.

Live: the coordinator runs T025–T028 under the owner's standing grant (D117), one at a time,
from the clean live clone (tests/live/probes/README.md *Before a session*: `HEAD` detached at the
reviewed SHA on `origin/main`, `git status --porcelain` empty), never from a worktree. lz-live
destroys the root on every way out (the check's Undo first, then `tofu destroy`), then runs the
leftover check, which lists every matrix kind for names `lzprobe-…` and the run's inventory ids
(the run id is in every name). A failed destroy or leftover check is finished in the same
session with `task live:probe -- --reviewed-sha <sha> --cleanup <run-id>`. Each live task's
evidence records the run id, the observation, the cost (Control Panel or `ovhcloud`, marked
UNVERIFIED when it cannot be read) and the leftover check's `outcome: pass`. No live run starts
below 20 EUR of credit or after 2026-10-23 23:00 (D117).

### The runner's sweep (before every probe's implementation)

- [x] T037 [US1] Write the runner's sweep tests in tools/internal/live/sweep_test.go with the stub of tools/internal/live/sweep.go — closed 2026-10-16, evidence: evidence/T037.md
  - Requirements: FR-002, FR-003; ADRs: 0008, 0021. Depends on: 005/T047 (live lane with trap destroy and leftover check), 005/T113 (`Recorded` on `--cleanup`). The runner skips Undo when the root published nothing (tools/internal/live/runner.go:743-745) and, when no state file was ever written, removes the passphrase and the probe record (runner.go:1241-1246), so a `--cleanup` cannot find the run again. Whether tofu writes root outputs after a partial apply failure is UNVERIFIED.
  - Cases (given / when / then → defect exposed), against the runner's fake children, fake API and fake terminal (as `test:live-lane`'s runner tests do):
    - given a probe check with a Sweep and an apply that fails after creating one resource (its answer lost, no state entry, no output) / when the run ends / then Sweep runs with the run id and the project from probe.env, after destroy and before the leftover check, and Undo is not run → exposes an orphan nobody deletes.
    - given the same apply crashes the process (the fake child is killed) / when `--cleanup <run-id>` runs in a new process / then Sweep runs from probe.env alone → exposes cleanup that needs the dead process's memory or the root's outputs.
    - given no state file was written and Sweep fails, or the leftover check fails / then probe.env, the passphrase and the root record are kept, the run ends `fail` naming `--cleanup <run-id>` → exposes run metadata deleted before reconciliation passed.
    - given Sweep and the leftover check pass with no state file / then the run's files are removed as today → exposes metadata kept forever.
    - given a full apply with outputs / then Undo runs before destroy and Sweep after it, each once → exposes a sweep that replaces Undo, or runs twice.
    - given a check without a Sweep (state-backend, storage-iam) / then the run behaves as today → exposes a regression of spec 005's roots.
    - given Sweep's terminal output / then it passes through the run's redactor → exposes a sweep printing unredacted names or ids beyond the run's.
  - Verify: `task test:live-lane`: each case fails on the stub (Sweep never called, files removed) for its stated reason; spec 005's runner tests stay green.
  - Evidence: `specs/003-platform-feasibility/evidence/T037.md`; initial status `not-run`.

- [x] T038 [US1] Implement the runner's sweep in tools/internal/live/sweep.go and tools/internal/live/runner.go — closed 2026-10-16, evidence: evidence/T038.md
  - Requirements: FR-002, FR-003; ADRs: 0008, 0021. Depends on: T037.
  - Scope: the `Sweep` field of `ProbeCheck` (data-model.md *Probe checks*) and its comment; the runner calls it as T037 fixes, keeps probe.env, the passphrase and the root record until Sweep and the leftover check pass, and passes the run's redacted terminal; `--cleanup` reads run id and project from probe.env (`ProbeProject`) only.
  - Verify: `task test:live-lane`: T037's cases green; `task test:traceability` green.
  - Evidence: `specs/003-platform-feasibility/evidence/T038.md`; initial status `not-run`.

### Probe 1: the tenant S3 user's reach through Keystone/Swift

- [x] T029 [US2] Write the swift-reach check's tests in tools/internal/probes/live/storageiam/swiftreach_test.go with the stub of tools/internal/probes/live/storageiam/swiftreach.go — closed 2026-10-16, evidence: evidence/T029.md
  - Requirements: FR-005 (C005.1), FR-006 (C006.4, C006.5); ADRs: 0004, 0009, 0018. Depends on: 005/T049 (tenant S3 user pattern), 005/T100 (storageiam check shape). The S3 user belongs to a cloud-project (OpenStack) user; whether that user's Keystone token reaches another bucket through Swift, bypassing the S3 bucket-ARN policy (005 G6), is not observed anywhere (UNVERIFIED). Whether Swift serves S3-API buckets at all is UNVERIFIED too.
  - Cases (given / when / then → defect exposed), against a fake Keystone, a fake Swift and a fake S3 endpoint (httptest) through `SwiftOptions.HTTP`; every case runs both directions, user a against bucket b and user b against bucket a:
    - given each user's own bucket lists 200 and the other bucket lists, reads or accepts a write 2xx / when Observe runs / then each step of that direction is `reach`, the record carries a finding per step and Observe returns an error naming them → exposes a check that does not try the other bucket, or counts any 2xx as expected.
    - given the root's seed object in each bucket (`lzprobe-swift-reach/seed`, published as `seed_key`) / when the own-bucket control runs / then it reads its own seed, and the other-bucket read step reads the other bucket's seed → exposes a read step that has nothing to read and so always reads `blocked`.
    - given the own bucket's same-route control succeeds, the other bucket answers 401 or 403, and the control repeated with the same token succeeds again / then the step is `denied`, as expected → exposes a judge that never records a denial.
    - given control 200, then the other bucket 401, then the repeated control 401 with the same token / then the step is `blocked` (token lost), never `denied` → exposes a token expiry or revocation counted as isolation.
    - given the own bucket's control is refused, 404 or 5xx, and the other bucket answers 403 / then the step is `blocked`, never `denied` → exposes a 403 counted as isolation without a positive control.
    - given the other bucket answers 404, a 5xx or a transport error / then the step is `blocked` → exposes an outage or a missing container counted as a denial.
    - given direction a→b is observed and user b's Keystone login fails / then a→b keeps its result and every b→a step is `blocked` with the reason → exposes one direction reported as C005.1 in full.
    - given Keystone refuses the token request for both users / then every Swift step is `not-run`, the record says why and Observe fails → exposes a refused login reported as isolation.
    - given the tenant's Swift access is denied everywhere / when Undo runs / then it lists and deletes the objects the check wrote under `lzprobe-swift-reach/` (never the seeds, which the root owns) in both buckets with each bucket's own cleanup credential over S3, lists again and passes → exposes cleanup that depends on the identity under test.
    - given the tenant's Swift write to the other bucket succeeded and its Swift delete is refused / when Undo runs / then the cleanup credential of that bucket deletes the object and the second listing is empty → exposes an object left behind because the tenant cannot delete what it wrote, so the bucket's destroy fails.
    - given the write's answer was lost after the fake stored the object / when Undo runs / then the object is found by the prefix and deleted → exposes cleanup by a remembered object name only.
    - given a bucket or user whose create answer was lost (no output, no state entry) / when Sweep runs with only the run id and project / then it finds `lzprobe-swift-reach-<lower(run id)>-a|b` and the users of that description by listing, deletes them with the sandbox admin, lists again → exposes an orphan bucket or user.
    - given an object is still listed after the delete / then Undo fails and lz-live keeps the root → exposes a silent leftover.
    - given published variables missing a key, a `run_id` that is not a run id, or a bucket other than `lzprobe-swift-reach-<lower(run id)>-a|b`, or a `seed_key` other than `lzprobe-swift-reach/seed` / then Validate refuses before any request → exposes a check that writes to a bucket the run does not own.
    - given a fake Keystone whose error body echoes the password / then neither the record nor the terminal holds it (leak guard shown first on a leaking mutant) → exposes a credential leak through the server's text.
  - Verify: `task test:live-lane`: each case fails on the stub for its stated reason (the stub's Observe and Undo return "not implemented" and make no request); the leaking mutant fails the leak guard; the package's existing tests stay green.
  - Evidence: `specs/003-platform-feasibility/evidence/T029.md`; initial status `not-run`.

- [x] T030 [US2] Implement the swift-reach check in tools/internal/probes/live/storageiam/swiftreach.go and the probe root tests/live/probes/swift-reach/ — closed 2026-10-16, evidence: evidence/T030.md
  - Requirements: FR-005 (C005.1), FR-006 (C006.4, C006.5); ADRs: 0004, 0009, 0018. Depends on: T029, T038.
  - Scope: the check as data-model.md *Probe checks* fixes it; the root (`main.tf`, `probe.tf`, `probe.tftest.hcl`, the committed `.terraform.lock.hcl` of the other roots) with two buckets `lzprobe-swift-reach-<lower(run id)>-a|b`, each seeded with one object `lzprobe-swift-reach/seed` (through the provider's object resource if it has one, UNVERIFIED; otherwise the root does not own the seed and the check's first step writes it with the bucket's cleanup credential, and Undo deletes it); per bucket a tenant user (OpenStack user with an S3 credential and a policy scoped to its own bucket, as `state-backend` does) and a cleanup user whose S3 policy allows list and delete on that bucket only, independent of the tenant users; the sensitive output `swift_check` (data-model.md); the `swift-reach` case in tools/cmd/lz-live/probe.go `probeCheck`; the root's `lz-check unit` line in `test:live-lane` (Taskfile.yml); its row in the tests/live/probes/README.md run sheet. Every kind the root creates is already in the leftover matrix (bucket, cloud project user, S3 credential, S3 policy). The Keystone URL comes from the provider's user resource or docs.ovhcloud.com's OpenStack guide, cited in the root.
  - Verify: `task test:live-lane`: T029's cases green, the root's offline plan tests pass; `task test:traceability` green (the registry still covers every Go package `test:live-lane` and `live:probe` read); `task docs:check` green.
  - Evidence: `specs/003-platform-feasibility/evidence/T030.md`; initial status `not-run`.

- [x] T039 [US2] Fix the swift-reach root's empty `tenant_id` in tests/live/probes/swift-reach/ (found live: run 20261016T145959Z-42fd on main 1fe496e failed before any Swift step with `output swift_check publishes an empty tenant_id`; cleanup complete) — closed 2026-10-16, evidence: evidence/T039.md
  - Requirements: FR-005 (C005.1), FR-006 (C006.4, C006.5); ADRs: 0004, 0009, 0018. Depends on: T030.
  - Scope: the root publishes `tenant_id = local.project.project_id`, an attribute of `data.ovh_cloud_projects` that is empty against the real API while every offline mock supplied one. Take the tenant (Keystone project) id from the attribute that identifies the project (`service_name`), with a cited source or marked UNVERIFIED; the check's Validate keeps refusing an empty or malformed value, so a wrong source fails closed. Check token-residual, iam-urn, locked-recovery, storage-iam and state-backend for the same pattern and fix each the same way, with its own test.
  - Verify: a probe-root test (`task test:unit -- tests/live/probes/swift-reach`) red first with a mock whose `project_id` is empty and whose `service_name` is the project id, then green; `task test:live-lane`, `task docs:check` green.
  - Evidence: `specs/003-platform-feasibility/evidence/T039.md`; initial status `not-run`.

- [x] T025 [US2] Live run: the swift-reach probe on spec 005's runner with tests/live/probes/swift-reach/ — closed 2026-10-16, evidence: evidence/T025.md
  - Requirements: FR-005 (C005.1), FR-006 (C006.4, C006.5); ADRs: 0004, 0009, 0018. Depends on: T030 on `main` at a reviewed SHA.
  - Verify: `mise exec -- task live:probe -- --reviewed-sha <sha> tests/live/probes/swift-reach` from the clean live clone at that SHA: records each Swift call's status and reason in both directions (own-bucket control and its repeat, other-bucket list, read of the seed, write), and each tenant user's role names (for example `objectstore_operator`, `tests/live/probes/state-backend/main.tf`), since a project-wide Swift role explains reach. Observed reach refutes the S3-plane isolation claim and stops T012's claim; a `blocked` control (for example Swift not serving S3-API buckets) is recorded as the observation, with its reason. The run destroys in finally and sweeps its run-id leftovers with the bucket-scoped cleanup credentials; the leftover check is clean.
  - Evidence: `specs/003-platform-feasibility/evidence/T025.md`: run id, observation, cost (or UNVERIFIED), leftover check; initial status `not-run`.

### Probe 2: an access token after its OAuth2 client is deleted

- [x] T031 [US3] Write the token-residual check's tests in tools/internal/probes/live/identity/residual_test.go with the stub of tools/internal/probes/live/identity/residual.go, and add the package to `test:live-lane` in Taskfile.yml — closed 2026-10-16, evidence: evidence/T031.md
  - Requirements: FR-007 (C007.5, C007.6); ADRs: 0009, 0018. Depends on: 005/T010 iam root (OAuth2 client and policy create/delete observed, P7, P8). The residual window of an already-issued access token after its client is deleted is not observed (UNVERIFIED). The token's lifetime is the `expires_in` of its token answer; spec 005's fixtures model 3599 s (tools/internal/live/leftovers_api_test.go), which is UNVERIFIED for the sandbox.
  - Cases (given / when / then → defect exposed), against a fake OVHcloud API (`live.API`), a fake clock and a fake sleep:
    - given a token answer with `expires_in` missing, zero, negative or above 24 h / then Observe stops before the delete with `blocked` and the reason → exposes a window judged against an expiry nobody validated.
    - given a valid token answer / then the record keeps `issued_at` and `expires_in` → exposes a result that cannot be compared with the lifetime later.
    - given the read stays allowed for three polls after the delete and is then refused 401 or 403 before the expiry / then the observation is `refused-before-expiry` with a lower bound (delete to the last allowed poll) and an upper bound (delete to the first refusal) → exposes a window reported as a single point, or as revocation at delete time.
    - given allowed polls up to one interval before the expiry and the first refusal at or after the expiry / then the observation is `valid-until-expiry` → exposes a deleted client's token counted as revoked when it simply expired.
    - given a refusal exactly at the expiry instant, and given one a second before it / then the first is `valid-until-expiry` and the second `refused-before-expiry` → exposes an off-by-one at the boundary.
    - given polls that alternate allowed and 5xx, then a refusal / then only the observed allowed and refused polls bound the window and each outage is recorded as an outage → exposes an outage counted as the refusal (005/T095), or an outage gap counted as validity.
    - given the last allowed poll is followed only by outages until the expiry / then the observation is `blocked` with the lower bound "valid ≥ N min" and no claim about the tail → exposes validity inferred across an unobserved tail.
    - given the run's context deadline passes while the token is still allowed / then Observe stops and records `blocked (deadline)` with the lower bound "valid ≥ N min" → exposes polling past the deadline, or a deadline stop read as a result.
    - given polling continues / then no poll is made later than the expiry plus one interval → exposes unbounded polling.
    - given the fake counts token requests / then exactly one token is issued, before the delete, and every poll uses it → exposes a stub that tests a fresh login instead of the issued token.
    - given the granted read is refused before the delete (the same-route control) twice and then allowed within `Options.Settle` / then the client is deleted only after the control succeeded, and the record keeps each attempt → exposes a run wasted on a policy still propagating.
    - given the control is still refused at the end of `Options.Settle` / then the observation is `blocked (control)`, no client is deleted for the window → exposes a policy not yet in effect read as revocation (IAM propagation delay UNVERIFIED).
    - given the client delete is refused or fails / then Observe fails with no window and Undo still runs → exposes a window measured without a delete.
    - given a fresh-process Sweep with only the run id and project after each partial creation (client created and policy not; policy created and client not; the client's create answer lost after the fake created it; the client already deleted by Observe) / then it lists clients and policies by the exact name `lzprobe-token-residual-<run id>`, deletes each, lists again and fails on anything left → exposes a client or policy surviving a crash.
  - Verify: `task test:live-lane` with the package added to both Go lines of the target: each case fails on the stub for its stated reason; the other packages stay green.
  - Evidence: `specs/003-platform-feasibility/evidence/T031.md`; initial status `not-run`.

- [x] T032 [US3] Implement the token-residual check in tools/internal/probes/live/identity/residual.go and the probe root tests/live/probes/token-residual/ — closed 2026-10-16, evidence: evidence/T032.md
  - Requirements: FR-007 (C007.5, C007.6); ADRs: 0009, 0018. Depends on: T031, T038.
  - Scope: the check as data-model.md *Probe checks* fixes it, and the API operations it needs in tools/internal/live/apicheck.go (create, list and delete an OAuth2 client; one granted read); the root with the sensitive output `residual_check` (data-model.md); the `token-residual` case in tools/cmd/lz-live/probe.go `probeCheck`; `tools/internal/probes/live/identity` in the inputs of every registered check whose Go packages now import it (today `test:bootstrap`, `live:probe`, `bootstrap:account` in tools/harness/checks.yaml); the root's `lz-check unit` line in `test:live-lane`; its run-sheet row. The check, not the root, creates the client and policy (the window needs a delete inside the run), so its Sweep finds them by exact name; the root creates nothing; both kinds are already in the leftover matrix.
  - Verify: `task test:live-lane`: T031's cases green; `task test:traceability` green; `task docs:check` green.
  - Evidence: `specs/003-platform-feasibility/evidence/T032.md`; initial status `not-run`.

- [x] T026 [US3] Live run: the token-residual probe on spec 005's runner with tests/live/probes/token-residual/ — closed 2026-10-16, evidence: evidence/T026.md
  - Requirements: FR-007 (C007.5, C007.6); ADRs: 0009, 0018. Depends on: T032 on `main` at a reviewed SHA.
  - Verify: `mise exec -- task live:probe -- --reviewed-sha <sha> --deadline <D> tests/live/probes/token-residual` from the clean live clone at that SHA, with `<D>` the token lifetime plus the teardown margin (for a 3599 s token, 60 min plus 15 min: `75m`; lz-live's default of 45 min would stop the probe before a token that outlives its client expires, tools/internal/live/deadline.go). A disposable client with a one-read policy takes a token, the client is deleted, the read repeats at bounded intervals; records the token's `expires_in`, each poll's status, and the result (`refused-before-expiry` with its bounds, `valid-until-expiry`, or `blocked` with its lower bound). The run destroys in finally and sweeps its run-id leftovers; the policy and the client are removed on exit and the leftover check is clean. T019 records the window.
  - Evidence: `specs/003-platform-feasibility/evidence/T026.md`: run id, `expires_in`, observation, cost (or UNVERIFIED), leftover check; initial status `not-run`.

### Probe 3: the URN `account:apiovh:iam/*` actions are checked against

- [x] T033 [US3] Write the iam-urn check's tests in tools/internal/probes/live/identity/urn_test.go with the stub of tools/internal/probes/live/identity/urn.go — closed 2026-10-16, evidence: evidence/T033.md
  - Requirements: FR-006 (C006.2, C006.5), FR-008 (C008.1); ADRs: 0006, 0018. Depends on: T031 (the package and its target line), 005/T049 run 3 and D109 (`account:apiovh:iam/resource/edit` granted on a project URN did not authorise tagging that project; the account URN is the likely target, UNVERIFIED).
  - Each identity's policy grants the probed action and a control action on the same URN: a known-working read on that URN (for the account URN `account:apiovh:me/get`, GET /me, which spec 005 observed; for the project URN the project's read, its action name taken from the IAM action reference and cited, UNVERIFIED until then). The probed call is judged only after that identity's control succeeds; the control is retried at bounded intervals up to a bounded stabilisation time.
  - Cases (given / when / then → defect exposed), against a fake OVHcloud API:
    - given both controls succeed, the project-URN identity's probed call is refused 403 naming the action and the account-URN identity's is allowed / when Observe runs / then the observation names `account` → exposes a judge that reads the URN from one identity alone.
    - given both controls succeed and the reverse / then it names `project` → exposes a check hard-wired to D109's expectation.
    - given both controls succeed and both probed calls are allowed / then it records `either`, not a URN → exposes a URN claimed where the action is not URN-bound.
    - given a control refused twice and then allowed within the stabilisation bound / then the probed call is made only after the control succeeds, and the record keeps each control attempt → exposes a verdict judged before the policy was in effect.
    - given a control still refused at the end of the stabilisation bound / then the observation is `blocked (propagation)` and no URN verdict is recorded, whatever the probed calls answered → exposes D109's false `account` from a policy that never propagated.
    - given both probed calls refused after successful controls, or a 5xx or transport error on either / then `blocked` → exposes an outage read as a URN.
    - given a 403 that names another action / then that call is not an attributable denial and the observation is `blocked` → exposes an unrelated refusal counted for the probed action.
    - given each control and probed call / then the record carries the seconds since that identity's policy was created (as storageiam's `policy_age_seconds`) → exposes a result whose propagation context is lost.
    - given the probed actions, a read and the floor's write `account:apiovh:iam/resource/edit` / then each gets its own result, recorded with the action it holds for → exposes one action's URN applied to another.
    - given the write's target is not the probe bucket `lzprobe-iam-urn-<lower(run id)>` the root creates (for example the retained project's URN, or a policy not named for the run) / then Validate refuses before any call → exposes a probe writing to the retained project or the admin's policy.
    - given the probe bucket is not listed as an IAM resource (UNVERIFIED) / then the write is `blocked (no owned target)` and is not attempted on anything else → exposes a fallback to the retained project.
    - given a fresh-process Sweep with only the run id and project after each partial creation (one identity's client created and its policy not; a policy create whose answer was lost; a tag key added to the probe bucket and its policy deleted) / then it lists both identities' clients and policies by the exact names `lzprobe-iam-urn-<run id>-project|account` and the probe bucket's tag key `lzprobe-iam-urn:run-id`, deletes each, lists again and fails on anything left → exposes leftovers after a crash.
  - Verify: `task test:live-lane`: each case fails on the stub for its stated reason.
  - Evidence: `specs/003-platform-feasibility/evidence/T033.md`; initial status `not-run`.

- [x] T034 [US3] Implement the iam-urn check in tools/internal/probes/live/identity/urn.go and the probe root tests/live/probes/iam-urn/ — closed 2026-10-16, evidence: evidence/T034.md
  - Requirements: FR-006 (C006.2, C006.5), FR-008 (C008.1); ADRs: 0006, 0018. Depends on: T033, T032 (the package's registry inputs), T038.
  - Scope: the check as data-model.md *Probe checks* fixes it, probing a read action (the exact `account:apiovh:iam/*` action and its API call taken from the IAM action reference on docs.ovhcloud.com and cited, UNVERIFIED until then) and the floor's write `account:apiovh:iam/resource/edit`, which only adds the tag key `lzprobe-iam-urn:run-id` to the probe bucket the root creates, never to the retained project; the check, not the root, creates the two OAuth2 clients and two policies `lzprobe-iam-urn-<run id>-project|account`, each with the probed and the control actions; the root creates only the probe bucket `lzprobe-iam-urn-<lower(run id)>` and the sensitive output `urn_check`; the `iam-urn` case in `probeCheck`; the root's `lz-check unit` line; its run-sheet row.
  - Verify: `task test:live-lane`: T033's cases green; `task test:traceability` green; `task docs:check` green.
  - Evidence: `specs/003-platform-feasibility/evidence/T034.md`; initial status `not-run`.

- [x] T027 [US3] Live run: the iam-urn probe on spec 005's runner with tests/live/probes/iam-urn/ — closed 2026-10-16, evidence: evidence/T027.md
  - Requirements: FR-006 (C006.2, C006.5), FR-008 (C008.1); ADRs: 0006, 0018. Depends on: T034 on `main` at a reviewed SHA.
  - Verify: `mise exec -- task live:probe -- --reviewed-sha <sha> tests/live/probes/iam-urn` from the clean live clone at that SHA: records each identity's control attempts and probed call, status and the actions a refusal names, and, per probed action, the URN it is checked against (or `either`, or `blocked` with the reason, `propagation` and `no owned target` included), each result recorded with its action. The run destroys in finally and sweeps its run-id leftovers; the policies, clients and the probe bucket's tag key are removed on exit and the leftover check is clean. T015 binds a floor denial only for an action whose result is a URN, and only to that URN.
  - Evidence: `specs/003-platform-feasibility/evidence/T027.md`: run id, observation, cost (or UNVERIFIED), leftover check; initial status `not-run`.

### Probe 4: `force-unlock` and a version restore on an Object-Locked bucket

- [x] T035 [US2] Write the locked-recovery check's tests in tools/internal/probes/live/state/recovery_test.go with the stub of tools/internal/probes/live/state/recovery.go — closed 2026-10-16, evidence: evidence/T035.md
  - Requirements: FR-005 (C005.4, C005.8); ADRs: 0009, 0022. Depends on: 005/T099 and 005/T010 row 7b (probe bucket with Object Lock, governance, one day: versioned delete refused to the state credential, the janitor empties it with the bypass). The unlock is the pinned `tofu force-unlock` (OpenTofu 1.13.0) run against a disposable backend root `tests/live/probes/locked-recovery/backend/` whose S3 backend (`use_lockfile = true`, https://opentofu.org/docs/language/settings/backends/s3/) points at the locked probe bucket with the state credential; lz-live hands the check a seam that runs that pinned tofu (data-model.md). Whether OVHcloud honours a conditional PutObject (`If-None-Match: *`) is UNVERIFIED (research.md). The version restore has no tofu command and stays an S3 copy. What keeps A holding the lock: the lock object `<key>.tflock` A wrote stays in the bucket after A dies, since only A's own unlock removes it (the S3 lockfile has no expiry, OpenTofu docs above; UNVERIFIED for 1.13.0); A runs `apply` without `-auto-approve` and waits at the approval prompt, which it reaches holding the lock, and is killed with SIGKILL (an interrupt makes tofu release the lock). The backend root's state is not encrypted: this probe claims nothing about C004.1. The tofu diagnostics the cases classify are fixtures taken from OpenTofu 1.13.0's source, each citing its file and line in tests/fixtures/tofu-probes/locked-recovery/; T028 records whether the live diagnostics match, and a mismatch is `blocked`, not a verdict.
  - Cases (given / when / then → defect exposed). Primary, against a fake tofu runner (`RecoveryOptions.Tofu`) that records each invocation's arguments and environment:
    - given writer A's `tofu apply` waiting at its prompt, SIGKILLed, and the seam reporting that A exited on that signal / when writer B runs `tofu plan -lock-timeout=0s` / then B's refusal is recorded `refused (locked)` with the lock id read from B's output → exposes a check that never establishes a held lock.
    - given the seam does not report that A exited after the SIGKILL (the kill failed, or A is still running) / then force-unlock and B's second plan are `not-run` and the observation is `blocked (owner alive)` → exposes a lock broken while its writer may still hold it (V005's owner-death proof).
    - given B's first plan is allowed / then the observation is `blocked (lock not established)`, never a refutation, and no force-unlock runs → exposes an A that released or never took the lock read as a backend fault.
    - given B's refusal / when the check runs `tofu force-unlock -force <lock id>` with the state credential and then B's plan again / then the unlock is `allowed` and B's second plan `allowed`, both recorded → exposes an unlock not followed by a real acquisition.
    - given force-unlock exits non-zero with the fixture of an authorisation or retention denial (403 `AccessDenied`) / then it is recorded `refused` with that reason, not retried, and B's second plan is `not-run` → exposes a retry loop or a refusal reported as success.
    - given force-unlock or a plan fails with a transport error, a 404, a 5xx, a configuration error or a diagnostic matching no fixture / then the step is `blocked` with the reason, never `refused` → exposes an outage or a broken backend root counted as a denial.
    - given every tofu invocation / then the seam's child environment holds the state key pair only (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` of the state user): no `OVH_*` variable, no janitor key, no admin credential → exposes an unlock that works only with broader rights, or a credential leaked into a child.
  - Supporting controls, against a fake S3 endpoint (httptest) through `RecoveryOptions.HTTP`:
    - given a held lock object / when a conditional PUT (`If-None-Match: *`) of it answers 412 / then `refused`, as expected → exposes a condition never sent.
    - given a held lock object / when the conditional PUT answers 2xx / then the step is `condition-ignored`, recorded as a refutation of C005.3/C005.4 in the record's findings, and Observe does not fail on it → exposes a check that treats a lost lock guarantee as a test failure instead of an observation.
    - given two state versions v1 and v2 / when the restore copies v1 as current by version id / then a GET of the current object matches v1's digest (compared, never printed) → exposes a restore reported from the copy's status alone.
    - given the restore is refused / then it is recorded with its code and the step after it reads `not-run` → exposes a refused restore counted as recovery.
    - given a 5xx or no answer on any step / then that step is `no-answer` and Observe fails → exposes an outage counted as either outcome.
  - Cleanup:
    - given a fresh-process Undo after each partial step (lock object left by the killed writer; a version written whose PUT answer was lost; a restore copy made) / then it empties the bucket with the janitor's governance bypass, delete markers included, lists again and fails on anything left (state's existing undo) → exposes locked versions left to their retention unrecorded.
    - given published variables with a `run_id` that is not a run id, or a bucket other than `lzprobe-locked-recovery-<lower(run id)>` / then Validate refuses before any request → exposes an Undo emptying a bucket the run does not own.
    - given the bucket or a user whose create answer was lost (no output, no state entry) / when Sweep runs with only the run id and project / then it finds them by exact name, deletes them with the sandbox admin (an empty bucket: the check writes only after the outputs exist), lists again → exposes an orphan locked bucket.
  - Verify: `task test:live-lane`: each case fails on the stub for its stated reason; the state-backend check's tests stay green.
  - Evidence: `specs/003-platform-feasibility/evidence/T035.md`; initial status `not-run`.

- [x] T036 [US2] Implement the locked-recovery check in tools/internal/probes/live/state/recovery.go and the probe root tests/live/probes/locked-recovery/ — closed 2026-10-16, evidence: evidence/T036.md
  - Requirements: FR-005 (C005.4, C005.8); ADRs: 0009, 0022. Depends on: T035, T038.
  - Scope: the check as data-model.md *Probe checks* fixes it; the tofu seam in lz-live (the pinned OpenTofu of `mise.live.toml`, the run's scratch HOME and its own `TF_DATA_DIR`, the state key pair as the only credential in the child's environment, SIGKILL and the exit report, the redactor on its output), and the `ProbeCheck` contract comment in tools/internal/live/runner.go amended to say this seam is the one child that receives a published credential (the state key pair, nothing else); the S3 operations the supporting controls need in tools/internal/live/s3check.go (CopyObject with a source version id; a conditional PutObject); the root, a copy of `state-backend`'s bucket (Object Lock, governance, `P1D`), state user and janitor, with the sensitive output `recovery_check`, and the disposable backend root `backend/` (no provider resources beyond `terraform_data`); the version restore writes and restores an earlier version of a probe key, never the backend's live state key (T035's tests); the `locked-recovery` case in `probeCheck`; the root's and the backend root's `lz-check unit` lines; its run-sheet row.
  - Verify: `task test:live-lane`: T035's cases green; `task test:traceability` green; `task docs:check` green.
  - Evidence: `specs/003-platform-feasibility/evidence/T036.md`; initial status `not-run`.

- [x] T028 [US2] Live run: the locked-recovery probe on spec 005's runner with tests/live/probes/locked-recovery/ — closed 2026-10-16, evidence: evidence/T028.md
  - Requirements: FR-005 (C005.4, C005.8); ADRs: 0009, 0022. Depends on: T036 on `main` at a reviewed SHA.
  - Verify: `mise exec -- task live:probe -- --reviewed-sha <sha> tests/live/probes/locked-recovery` from the clean live clone at that SHA, on a one-day governance-locked probe bucket: records that A exited on SIGKILL, whether the pinned `tofu force-unlock` and the version restore (an earlier version of the probe key `lzprobe-locked-recovery/restore`, never the backend's live state key) succeed with the state credential, whether the second writer then acquires the lock, whether the live diagnostics matched the fixtures, and whether the conditional PUT is honoured (`condition-ignored` is a refutation); the janitor's bypass empties the bucket before the destroy. The run destroys in finally and sweeps its run-id leftovers; the leftover check is clean; the cost records any version left to its retention.
  - Evidence: `specs/003-platform-feasibility/evidence/T028.md`: run id, observation, cost (or UNVERIFIED), leftover check; initial status `not-run`.

## Planned command creators

| Task target | Creating task |
| --- | --- |
| `task test:qualification` | T022 |

Live probes add no target: each task above that writes a probe root under `tests/live/probes/`
(T009, T011, T013, T015, T018, T030, T032, T034, T036) runs it with spec 005's `task live:probe`
(D113). The probe checks T029–T038 add no target either: they extend spec 005's `test:live-lane`
(T031 adds the identity package, each implementation task its root's `lz-check unit` line).

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

Within 003, state T011–T014 and the probes T029/T030/T025 and T035/T036/T028, and identity
T015–T019 and the probes T031/T032/T026 and T033/T034/T027, branch after 005/T047 with distinct
fixtures and no overlapping live writers. T037/T038 come first; then the four probes' offline
tasks can run in parallel worktrees except T033 after T031 (the identity package) and T034 after T032 (its registry
inputs); their live runs go one at a time.
The offline oracle T021–T022 needs only its listed local prerequisites. No parallel
flag bypasses ownership or cleanup gates.

## Completion

Rerun spec acceptance commands and inspect matching evidence; do not close a task using
a missing-binary result, empty discovery, self-authored green assertion or a simulation
standing in for required live proof. Docs-only work remains exempt. Only the operator
activates dependent live experiments/ratifies ADRs. A refuted observed premise gets its
explicit downstream stop decision; an unrun required experiment stays owed and blocked.
