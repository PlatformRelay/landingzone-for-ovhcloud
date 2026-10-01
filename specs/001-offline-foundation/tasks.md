# Tasks: 001-offline-foundation
Status: planned · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

All boxes are open. Acceptance targets are **planned** and do not exist yet. Test tasks
record a valid control plus behavioural red with compiling stubs; missing tools, compile
errors and outages are not red evidence. Implementation/run tasks need green positive
and rejection controls. Map every task to the spec V checks by its requirement IDs.
Task-level Evidence retains red/green controls; each V command also writes its spec
acceptance-table evidence path and references the task packets that support it.
All Go test code lives under the single tools Go module, including tools/internal/probes/.
Root tests/ holds HCL fixtures, captured outputs and qualification data consumed by that
module; no second Go module or package outside tools/ is assumed.

## Setup and foundation

- [ ] T001 Select exact signed/checksummed tool and image pins in mise.toml and bootstrap tools/go.mod
  - Requirements: FR-001; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: none.
  - Verify: Procedure: install recorded pins, run `go -C tools list -m`, `mise exec -- tofu version`, `mise exec -- terramate version`; compare exact identities to pins; missing/mismatched identity refuses fixture capture.
  - Evidence: `.local/evidence/001/t001-toolchain.json`; initial status `not-run`.

- [ ] T002 Write pin/isolation tests and minimal compiling boundary stubs in tools/internal/checks/toolchain_test.go and tools/internal/probes/security/offline_test.go
  - Requirements: FR-001, FR-002; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T001.
  - Verify: `go -C tools test ./internal/checks ./internal/probes/security -run "TestToolchain|TestOfflineBoundary" -count=1`; valid pin/isolation control plus wrong version, absent tool, credential/socket mount, outbound subprocess and malicious host-launch cases must expose behavioural red.
  - Evidence: `.local/evidence/001/t002-boundary-red.json`; initial status `not-run`.

- [ ] T003 Implement trusted preparation, launcher and local isolation in tools/internal/checks/offline.go, harness/capabilities.yaml and Taskfile.yml
  - Requirements: FR-001, FR-002; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T002.
  - Verify: `task verify:toolchain; task test:offline-boundary`: valid prepared image succeeds; wrong/missing pin, credential/socket/cache mount, outbound child or candidate-controlled launch rejects. Creates both targets; trusted CI wrapper is integrated in T011.
  - Evidence: `.local/evidence/001/t003-boundary-green.json`; initial status `not-run`.

- [ ] T004 Capture pinned passing/failing/truncated tofu JSON streams and write report tests in tools/internal/report/report_test.go and tests/fixtures/tofu/
  - Requirements: FR-003, SC-002; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T003.
  - Verify: `go -C tools test ./internal/report -run TestReport -count=1`: record exact generating `tofu test -json` command/version; valid stream preserved, zero tests, required skip, crash, truncated/invalid stream and cleanup fault cause behavioural red before adapter.
  - Evidence: `.local/evidence/001/t004-report-red.json`; initial status `not-run`.

- [ ] T005 Implement observation schema and JSON/JUnit adapters in schemas/check-report.schema.json, tools/internal/report/ and tools/cmd/json2junit/
  - Requirements: FR-003, SC-002; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T004.
  - Verify: `task test:reports`: all V002 valid/fault cases and each-clause mutants green as a suite; preserve actual upstream diagnostics; creates target.
  - Evidence: `.local/evidence/001/t005-reports.json`; initial status `not-run`.

- [ ] T006 Write trace/DoD tests in tools/internal/checks/traceability_test.go, including docs exemption and stale/forged evidence
  - Requirements: FR-004, SC-003; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T005.
  - Verify: `go -C tools test ./internal/checks -run "TestTraceability|TestDoD" -count=1`: complete trace accepted; unmapped FR/task, missing Verify/evidence and fake/stale pass rejected; valid docs-only exemption accepted; incomplete oracle yields behavioural red.
  - Evidence: `.local/evidence/001/t006-traceability-red.json`; initial status `not-run`.

- [ ] T007 Implement shared registry, trace and DoD evaluator in harness/checks.yaml, tools/internal/checks/traceability.go and tools/cmd/lz-check/
  - Requirements: FR-004, SC-003; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T006.
  - Verify: `task test:traceability; task check:specs; task dod -- modules/naming`: trace fixture passes, unmapped/forged cases fail, unimplemented naming checks report not-run until T014/T018; no missing evidence passes. Creates all three targets.
  - Evidence: `.local/evidence/001/t007-traceability.json`; initial status `not-run`.

- [ ] T008 Write layer/changed-closure and real-tool static tests in tools/internal/checks/{dependencies,static}_test.go and tests/check/fixtures/{dependencies,static}/
  - Requirements: FR-007; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T007.
  - Verify: `go -C tools test ./internal/checks -run "TestDependencies|TestStatic" -count=1`: leaf+consumer and shared-tool changes select exact closure; reverse edge/cycle/unresolved reference fail; unknown path selects full suite; omitted selection clause yields behavioural red. Also run pinned `tofu fmt -check`, `tofu validate -json` and configured tflint on valid/unformatted/malformed/linter-offence fixture modules; bypass each clause and require behavioural red, not missing-tool failure.
  - Evidence: `.local/evidence/001/t008-dependencies-red.json`; initial status `not-run`.

- [ ] T009 Implement dependency scanning, applicable L0 runners and full-suite fallback in tools/internal/checks/{dependencies,static}.go, .tflint.hcl and Taskfile.yml
  - Requirements: FR-007; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T008.
  - Verify: `task test:dependencies; task test:static`: V006 valid/dependency/static fixture controls pass; deleting consumer selection or accepting reversed layer edge fails its control. Creates test:dependencies, test:static and lint targets plus changed-path selection for task check. `task lint -- modules/naming` runs real pinned `tofu fmt -check -recursive modules/naming`, prepared-cache `tofu -chdir=modules/naming init -backend=false`, `tofu -chdir=modules/naming validate -json`, and `tflint --chdir=modules/naming --format=json`; actual counted observations only. Schema result joins after T016; resource scanning is not-applicable, docs-only tests exempt.
  - Evidence: `.local/evidence/001/t009-dependencies.json`; initial status `not-run`.

## US1

Independent test: V001–V003, V006–V007.

- [ ] T010 [US1] Write candidate-host-escape, protected-publisher and fork-cache tests in tools/internal/probes/forge/offline_test.go
  - Requirements: FR-002, FR-008; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T009.
  - Verify: `go -C tools test ./internal/probes/forge -run TestOfflineForge -count=1`: candidate Taskfile/workflow/include attacks cannot execute before isolation; missing protected result, changed launcher or foreign publisher rejects; non-isolating stub yields behavioural red.
  - Evidence: `.local/evidence/001/t010-forge-red.json`; initial status `not-run`.

- [ ] T011 [US1] Implement base-revision dispatch verification launcher and forge adapters in pipelines/github/offline.yml, pipelines/gitlab/offline.yml and tools/internal/checks/launcher.go
  - Requirements: FR-002, FR-008; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T010.
  - Verify: `task test:offline-boundary; task verify:forge-offline`: local wrapper controls pass; archive as data only, no candidate code on host, token outside child; real forge absence reports blocked. Creates forge qualification target; required publisher origin setup gate remains external.
  - Evidence: `.local/evidence/001/t011-forge-adapters.json`; initial status `not-run`.

- [ ] T012 [US1] Qualify trusted offline adapters on both disposable forges using tools/internal/probes/forge/offline_test.go
  - Requirements: FR-008; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T011; external disposable repos, protected base workflow, scoped result publisher and runner setup.
  - Verify: `task verify:forge-offline`: valid candidate green, broken behaviour red; malicious Taskfile/workflow/include cannot change launch; parent fork sees no secrets/socket/cache writes; missing publisher-origin enforcement blocks rather than accepting ordinary candidate CI.
  - Evidence: `.local/evidence/001/t012-forge-offline.json`; initial status `not-run`.

## US2

Independent test: V004–V005.

- [ ] T013 [US2] Author two independent organisation vectors and provider-free module tests in modules/naming/tests/unit.tftest.hcl, modules/naming/tests/contract.tftest.hcl and tests/fixtures/naming/
  - Requirements: FR-005, SC-004; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T007, T009.
  - Verify: `mise exec -- tofu -chdir=modules/naming test`: compiling pure module stub produces behavioural red for concrete expected names/labels; valid unusual template, collision/truncation/import/upgrade cases and expect_failures for validations; record actual CLI output.
  - Evidence: `.local/evidence/001/t013-naming-red.json`; initial status `not-run`.

- [ ] T014 [US2] Implement pure naming module and documented algorithm version/override in modules/naming/{main,variables,outputs}.tf, modules/naming/README.md and names.yaml
  - Requirements: FR-005, SC-004; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T013.
  - Verify: `task test:naming; task snap:check -- modules/naming`: two templates and independent vectors pass; impossible/unknown cloud kind fails, import override stable; label/profile changes never rename; omission mutants fail. Creates naming/snapshot targets and baseline from actual pure plan.
  - Evidence: `.local/evidence/001/t014-naming.json`; initial status `not-run`.

- [ ] T015 [US2] Write naming/label projection differential tests in tools/internal/namingdata/projections_test.go, policies/plan/naming_test.rego and policies/assent/naming.test.yaml
  - Requirements: FR-006; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T014.
  - Verify: `go -C tools test ./internal/namingdata -run TestProjections -count=1` plus pinned Conftest/assent tests: missing each required label, unknown key and tenant auth-key edits reject in applicable planes; wrong projection stub is behavioural red.
  - Evidence: `.local/evidence/001/t015-naming-policy-red.json`; initial status `not-run`.

- [ ] T016 [US2] Implement strict catalogue/org schemas and shared projection data in schemas/{naming,labels}.schema.json, tools/internal/namingdata/ and policies/{plan,assent}/
  - Requirements: FR-006; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T015.
  - Verify: `task test:naming-policy; task generate:check; task schema:check; task policy`: valid independent vectors and applicable projections agree; duplicate YAML, unknown key, missing required label and stale generated data fail. Creates test:naming-policy, generate:check, schema:check and the policy aggregate. `task schema:check` validates names.yaml and both org naming/label fixture sets against the actual strict schemas and rejects malformed/duplicate/unknown fields and zero discovery; no invented cloud fields or unaudited limits.
  - Evidence: `.local/evidence/001/t016-naming-policy.json`; initial status `not-run`.

## US3

Independent test: V003, V008.

- [ ] T017 [US3] Write diagnostic/evidence/decision-map tests in tools/internal/checks/agentex_test.go
  - Requirements: FR-009; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T007, T014, T016.
  - Verify: `go -C tools test ./internal/checks -run TestAgentEx -count=1`: seeded defect needs stable rule id/location/observed/expected/fix; absent observation stays not-run, expert duty review-required; stale ADR mapping fails, incomplete implementation is behavioural red.
  - Evidence: `.local/evidence/001/t017-agentex-red.json`; initial status `not-run`.

- [ ] T018 [US3] Implement structured diagnostics and decision-map rendering in tools/internal/checks/agentex.go and docs/reference/decision-map.md
  - Requirements: FR-009; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T017.
  - Verify: `task test:agentex; task decision-map:check`: V008 controls and unknown/stale ADR references pass/fail correctly; remove any required diagnostic/evidence clause and its mutant fails. Creates both targets.
  - Evidence: `.local/evidence/001/t018-agentex.json`; initial status `not-run`.

- [ ] T019 [US3] Write progressive router and implemented-kind guides in AGENTS.md and harness/guides/{checks,naming}.md
  - Requirements: FR-009; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T018.
  - Verify: exempt — docs-only; content review confirms commands, trusted boundaries and deferred generators are accurately described.
  - Evidence: `.local/evidence/001/t019-docs-review.json`; initial status `not-run` (docs content review only).

## Polish and exit

- [ ] T020 Write full-suite latency/discovery tests in tools/internal/checks/latency_test.go
  - Requirements: FR-001, FR-002, SC-001; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T009, T012, T016, T018.
  - Verify: `go -C tools test ./internal/checks -run TestLatency -count=1`: complete applicable naming suite accepted; 120s overrun or omitted layer/count rejects; disabling deadline/discovery clause yields behavioural red.
  - Evidence: `.local/evidence/001/t020-latency-red.json`; initial status `not-run`.

- [ ] T021 Implement recorded-runner latency gate in tools/internal/checks/latency.go and Taskfile.yml
  - Requirements: SC-001; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T020.
  - Verify: `task verify:latency`: entire applicable naming check suite <=120s after preparation with versions, runner identity, counts and each layer recorded, including actual T009 L0 and T016 schema results; >120s/missing layer fails. Creates target; no filtering to meet budget.
  - Evidence: `.local/evidence/001/t021-latency.json`; initial status `not-run`.

- [ ] T022 Run foundation exit checks and independent evidence inspection through harness/checks.yaml
  - Requirements: FR-001, FR-002, FR-003, FR-004, FR-005, FR-006, FR-007, FR-008, FR-009, SC-001, SC-002, SC-003, SC-004; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T019, T021.
  - Verify: Run every V001–V008 command from spec.md and `task check -- modules/naming`; valid cases pass, every guarded clause has a killed behavioural mutant/valid unusual case; absent tool, forge proof or expert review prevents phase exit.
  - Evidence: `.local/evidence/001/t022-exit.json`; initial status `not-run`.

## Planned command creators

| Task target | Creating task |
| --- | --- |
| `task verify:toolchain` | T003 |
| `task test:offline-boundary` | T003 |
| `task test:reports` | T005 |
| `task test:traceability` | T007 |
| `task check:specs` | T007 |
| `task dod` | T007 |
| `task test:dependencies` | T009 |
| `task test:static` | T009 |
| `task lint` | T009 |
| `task schema:check` | T016 |
| `task verify:forge-offline` | T011 |
| `task test:naming` | T014 |
| `task snap:check` | T014 |
| `task test:naming-policy` | T016 |
| `task generate:check` | T016 |
| `task policy` | T016 |
| `task test:agentex` | T018 |
| `task decision-map:check` | T018 |
| `task verify:latency` | T021 |
| `task check` | T009 (selection); T021 (complete naming aggregate) |

## Dependencies & execution order

The per-task Depends on fields are authoritative; local task references form a DAG.
Test tasks precede their implementation; all external gates remain blocked until
observed. A command may be defined with an explicit blocked result before its live
prerequisites exist; defining it does not close its acceptance check.

MVP: T001–T009 supplies offline/report/traceability without cloud resources; US2
can start then, independent of forge setup, followed by US3. Foundation exit T022 still
requires real forge evidence and whole-suite latency.

## Parallel opportunities

No [P] flags are used across incomplete test/implementation dependencies. After common
prerequisites, independent fixture/guide authoring can run on distinct files: 001 naming
versus forge setup after T009; 002 binding-fixture authoring versus authority-fixture
authoring after interfaces stabilize; 003 state versus identity probes after T008 only
with independent approved leases and no shared live writers.

## Completion

Rerun spec acceptance commands and inspect matching evidence; do not close a task using
a missing-binary result, empty discovery, self-authored green assertion or a simulation
standing in for required live proof. Docs-only work remains exempt. Only the operator
activates dependent live experiments/ratifies ADRs. A refuted observed premise gets its
explicit downstream stop decision; an unrun required experiment stays owed and blocked.
