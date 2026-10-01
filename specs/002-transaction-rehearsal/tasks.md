# Tasks: 002-transaction-rehearsal
Status: planned · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

All boxes are open. Acceptance targets are **planned** and do not exist yet. Test tasks
record a valid control plus behavioural red with compiling stubs; missing tools, compile
errors and outages are not red evidence. Implementation/run tasks need green positive
and rejection controls. Map every task to the spec V checks by its requirement IDs.
All Go test code lives under the single tools Go module, including tools/internal/probes/.
Root tests/ holds HCL fixtures, captured outputs and qualification data consumed by that
module; no second Go module or package outside tools/ is assumed.

## Setup and foundation

- [ ] T001 Record exact Terramate/assent CLI pins and capture tool-owned offline fixture shapes in tests/fixtures/{terramate,assent}/
  - Requirements: FR-002, FR-008; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: 001/T022; exact Terramate and assent CLI availability (forge adapters are not a completion gate).
  - Verify: Procedure: execute recorded pinned `terramate version` and candidate fixture list/run commands; capture exact selection/order shape and `assent` version/output; wrong/missing CLI version refuses fixture capture, never substituted by handwritten output. Record forge-adapter availability separately without blocking this offline task; adapters gate T018/T019/V007 only.
  - Evidence: `.local/evidence/002/t001-tool-fixtures.json`; initial status `not-run`.

- [ ] T002 Write strict manifest/effective-document schema tests in tools/internal/instances/decode_test.go
  - Requirements: FR-001; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T001.
  - Verify: `go -C tools test ./internal/instances -run TestDecode -count=1`: valid manifest accepted; duplicate key/id/path/state key, traversal/escaping symlink/unknown field/version reject; permissive stub yields behavioural red.
  - Evidence: `.local/evidence/002/t002-decode-red.json`; initial status `not-run`.

- [ ] T003 Implement strict schema decoding in schemas/{deployments,effective-document}.schema.json and tools/internal/instances/decode.go
  - Requirements: FR-001; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T002.
  - Verify: `task test:instances`: strict-decode fixtures pass; omitted uniqueness/rooting/field clause fails its mutant; creates test:instances entry, extended in T005/T008.
  - Evidence: `.local/evidence/002/t003-decode.json`; initial status `not-run`.

## US1

Independent test: V001–V002.

- [ ] T004 [US1] Write manifest/stack identity and retirement tests in tools/internal/instances/reconcile_test.go
  - Requirements: FR-002; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T003.
  - Verify: `go -C tools test ./internal/instances -run TestReconcile -count=1`: add/repeat valid; missing/orphan stack, rename and removal without retirement rejected; approved retirement tombstone retained; omitted branch yields behavioural red.
  - Evidence: `.local/evidence/002/t004-reconcile-red.json`; initial status `not-run`.

- [ ] T005 [US1] Implement reconciler over terramate create and generation templates in tools/internal/instances/reconcile.go and templates/tenant-repo/
  - Requirements: FR-002, SC-001; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T004.
  - Verify: `task test:instances; task instances:check`: exact correspondence and repeat no-diff; duplicate ids/keys and unapproved removals fail; generated backend/module/provider HCL comes from Terramate, not custom emitter. Creates instances:check.
  - Evidence: `.local/evidence/002/t005-instances.json`; initial status `not-run`.

- [ ] T006 [US1] Write selection/order tests with independent graph oracle in tools/internal/instances/graph_test.go
  - Requirements: FR-003; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T005.
  - Verify: `go -C tools test ./internal/instances -run TestGraph -count=1`: upstream/intermediate/external-generation changes select exact closure, unrelated tenant excluded, cycles rejected; after-only selection stub yields behavioural red.
  - Evidence: `.local/evidence/002/t006-graph-red.json`; initial status `not-run`.

- [ ] T007 [US1] Implement artefact graph selection and wave planning in tools/internal/instances/graph.go and Terramate stack metadata
  - Requirements: FR-003, SC-001; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T006.
  - Verify: `task test:selection`: exact V002 closure/order from actual 0.17.3 output and independent oracle; drop wants or external-current widening and mutant fails. Creates target.
  - Evidence: `.local/evidence/002/t007-selection.json`; initial status `not-run`.

- [ ] T008 [US1] Qualify two-tenant local-state fixture roots in tests/transaction/fixtures/ and templates/tenant-repo/
  - Requirements: FR-001, FR-002, FR-003, SC-001; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T007.
  - Verify: `task test:instances; task instances:check; task test:selection`: local plain tofu roots apply/plan without cloud or remote-state reads; unique ownership/state keys and generated freshness hold; foreign/cyclic cases fail; every actual tool output recorded.
  - Evidence: `.local/evidence/002/t008-local-roots.json`; initial status `not-run`.

## US2

Independent test: V003–V004.

- [ ] T009 [US2] Write cross-process publication/fencing/crash tests in tools/internal/transaction/store_test.go and tools/internal/probes/transaction/races_test.go
  - Requirements: FR-004, FR-005; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T008.
  - Verify: `go -C tools test ./internal/transaction ./internal/probes/transaction -run "TestPublication|TestFence|TestCrash" -count=1`: valid publish/reconciled retry succeeds; late publisher, failed+old-current, producer after fence, owner death and no-current cases expose behavioural red; 30s process deadlines.
  - Evidence: `.local/evidence/002/t009-transaction-red.json`; initial status `not-run`.

- [ ] T010 [US2] Implement durable local store, sorted cross-process locks and driver in tools/internal/transaction/ and tools/cmd/lz-deploy/
  - Requirements: FR-004, FR-005, SC-002; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T009.
  - Verify: `task test:transaction`: safety and eventual reconciled retry pass; producer waits or consumer aborts from fence through apply; crashes freeze until reconciled; old publisher cannot regress current. Creates target; production store remains unqualified.
  - Evidence: `.local/evidence/002/t010-transaction.json`; initial status `not-run`.

- [ ] T011 [US2] Write each-field approval tamper tests in tools/internal/transaction/binding_test.go
  - Requirements: FR-005; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T010.
  - Verify: `go -C tools test ./internal/transaction -run TestPlanBinding -count=1`: exact unchanged saved plan valid; individually change candidate/base/policy/schema/toolchain/instance/effective/input/plan digest/check state and reject; omitted field yields behavioural red.
  - Evidence: `.local/evidence/002/t011-binding-red.json`; initial status `not-run`.

- [ ] T012 [US2] Implement exact plan/approval binding in schemas/{artifact,decision}.schema.json and tools/internal/transaction/binding.go
  - Requirements: FR-005, SC-002; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T011.
  - Verify: `task test:plan-binding; task test:transaction`: stale or changed field fails, valid single-use operation succeeds, uncertain retry reconciles before replay; creates test:plan-binding.
  - Evidence: `.local/evidence/002/t012-plan-binding.json`; initial status `not-run`.

## US3

Independent test: V005–V007.

- [ ] T013 [US3] Write trusted-base authorisation and differential fixtures in tools/internal/probes/security/authority_test.go and policies/{assent,plan}/
  - Requirements: FR-001, FR-006; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T012.
  - Verify: `go -C tools test ./internal/probes/security -run TestTenantAuthority -count=1` with pinned policy tests: own allowlisted delta passes; edited owners/evaluator, forged approval and identity/waiver/lifecycle changes deny; request-trusting stub yields behavioural red.
  - Evidence: `.local/evidence/002/t013-authority-red.json`; initial status `not-run`.

- [ ] T014 [US3] Implement effective-document resolution and assent/Rego authority projections in tools/internal/authority/ and policies/{assent,plan}/
  - Requirements: FR-001, FR-006; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T013.
  - Verify: `task test:tenant-authority; task policy`: trusted-base defaults/digests agree and each V005 adversarial case denied; remove any authority clause and mutant fails; creates authority target and extends policy aggregate.
  - Evidence: `.local/evidence/002/t014-authority.json`; initial status `not-run`.

- [ ] T015 [US3] Write aggregate reservation concurrency and expiration tests in tools/internal/reservations/reservations_test.go
  - Requirements: FR-007; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T010, T014.
  - Verify: `go -C tools test ./internal/reservations -run TestReservations -count=1`: under-limit pair succeeds, over-total pair cannot both reserve; cancellation/retry/expiry reconcile once; applying reservation not timer-released; non-atomic stub yields behavioural red.
  - Evidence: `.local/evidence/002/t015-reservations-red.json`; initial status `not-run`.

- [ ] T016 [US3] Implement atomic idempotent reservations in tools/internal/reservations/ and transaction store records
  - Requirements: FR-007; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T015.
  - Verify: `task test:reservations`: V006 concurrency/expiry/retry controls green; remove account-total CAS or release applying reservation early and mutant fails. Creates target.
  - Evidence: `.local/evidence/002/t016-reservations.json`; initial status `not-run`.

- [ ] T017 [US3] Write live disposable-forge candidate/check/rebase protocol fixtures in tools/internal/probes/forge/transaction_test.go
  - Requirements: FR-008; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T016.
  - Verify: `go -C tools test ./internal/probes/forge -run TestForgeTransaction -count=1`: valid expected publisher/candidate works; simulated stale/foreign/missing-check case red; capture actual pinned adapter output when available, no cloud credentials.
  - Evidence: `.local/evidence/002/t017-forge-red.json`; initial status `not-run`.

- [ ] T018 [US3] Implement protected protocol qualification adapters in pipelines/{github,gitlab}/transaction.yml and tests/forge/
  - Requirements: FR-008, SC-003; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T017; 001 trusted launcher contract; actual adapters gate only real qualification, not local adapter/test construction.
  - Verify: `task verify:forge-transaction`: local cases pass; absent real repo/assent adapter/publisher enforcement returns blocked, never a simulated qualification; creates target with hard <=10min per-forge deadline.
  - Evidence: `.local/evidence/002/t018-forge-adapters.json`; initial status `not-run`.

- [ ] T019 [US3] Execute protocol races against both disposable forges via tools/internal/probes/forge/transaction_test.go
  - Requirements: FR-008, SC-003; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T018; protected disposable repos/tokens and pinned assent adapters for both forges.
  - Verify: `task verify:forge-transaction`: real positive path succeeds; new commit/rebase/changed check/policy and competing candidate block stale merge/apply; evidence binds actual commit/run/publisher, fake source cannot pass.
  - Evidence: `.local/evidence/002/t019-forge-transaction.json`; initial status `not-run`.

## Polish and exit

- [ ] T020 Document local-store limitations and future remote-store qualification in templates/tenant-repo/README.md
  - Requirements: FR-004, FR-005; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T010, T019.
  - Verify: exempt — docs-only; content review distinguishes local rehearsal from cloud/forge support.
  - Evidence: `.local/evidence/002/t020-docs-review.json`; initial status `not-run` (docs content review only).

- [ ] T021 Run the rehearsal exit and independent race-evidence inspection using harness/checks.yaml
  - Requirements: FR-001, FR-002, FR-003, FR-004, FR-005, FR-006, FR-007, FR-008, SC-001, SC-002, SC-003; ADRs: 0004, 0005, 0006, 0007, 0008, 0021. Depends on: T008, T012, T016, T019, T020.
  - Verify: Run all V001–V007 commands; every binding/fencing/reservation clause has a failing behavioural mutant and valid control; missing real forge evidence or unrun race prevents exit.
  - Evidence: `.local/evidence/002/t021-exit.json`; initial status `not-run`.

## Planned command creators

| Task target | Creating task |
| --- | --- |
| `task test:instances` | T003, extended T005/T008 |
| `task instances:check` | T005 |
| `task test:selection` | T007 |
| `task test:transaction` | T010 |
| `task test:plan-binding` | T012 |
| `task test:tenant-authority` | T014 |
| `task policy` | 001/T016, extended T014 |
| `task test:reservations` | T016 |
| `task verify:forge-transaction` | T018 |

## Dependencies & execution order

The per-task Depends on fields are authoritative; local task references form a DAG.
Test tasks precede their implementation; all external gates remain blocked until
observed. A command may be defined with an explicit blocked result before its live
prerequisites exist; defining it does not close its acceptance check.

MVP: US1 roots/graph plus US2 transaction locally; US3 authority depends on binding
and reservations. Real-forge T019 is blocked on both actual assent adapters and setup.

## Parallel opportunities

No [P] flags are used across incomplete test/implementation dependencies. Within this
feature, binding-fixture authoring (T009–T012) versus authority-fixture authoring
(T013–T016) can proceed on distinct files after the shared interfaces stabilize; real
forge qualification (T018–T019) additionally waits on actual assent adapters for both
forges.

## Completion

Rerun spec acceptance commands and inspect matching evidence; do not close a task using
a missing-binary result, empty discovery, self-authored green assertion or a simulation
standing in for required live proof. Docs-only work remains exempt. Only the operator
activates dependent live experiments/ratifies ADRs. A refuted observed premise gets its
explicit downstream stop decision; an unrun required experiment stays owed and blocked.
