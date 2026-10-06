# Tasks: 001-offline-foundation
Status: planned · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

T001 and T002 are closed with task-scoped preparation and test-authoring evidence. Later acceptance targets remain **planned**. Test tasks
record a valid control plus behavioural red with compiling stubs; missing tools, compile
errors and outages are not red evidence. Implementation/run tasks need green positive
and rejection controls. Map every task to the spec V checks by its requirement IDs.
Task-level Evidence retains red/green controls; each V command also writes its spec
acceptance-table evidence path and references the task packets that support it.
All Go test code lives under the single tools Go module, including tools/internal/probes/.
Root tests/ holds HCL fixtures, captured outputs and qualification data consumed by that
module; no second Go module or package outside tools/ is assumed.
All Task/Go check invocations below are isolated-child commands entered through the
separately installed, independently approved `lz-offline` (creator T003). They are not
host shortcuts. T002 tests the approved boundary stub/entry from an external test driver;
its deliberately untrusted fixture checkout is data, never the host test driver or build source.
T002 alone may use a separately approved compile-then-run proof procedure: build only
the frozen allowlisted stub and test packages in private network-isolated children,
record their resulting binary digests, then execute those exact test binaries as
explicitly reviewed external host orchestration. Host trust is limited to that frozen
driver/test/build/input closure; ordinary candidate source, Task configuration and
arbitrary test binaries remain excluded. The permissive subject and its literal hostile
fixtures execute only inside disposable containment, with calibration in a separate
network-isolated sibling. This exception grants no production entry or capture authority.
Before T003 exists, only the separately reviewed T001 preparation control driver and T002
boundary proof driver/stub may execute, within their approved closures below. Neither admits
tool-output fixture capture. Version/provenance metadata is preparation evidence, not a
tool-output test fixture. T004 capture waits for T003's evidenced capture/isolation gate.

## Authorized run boundary

The current goal (2026-10-02, local decision D61) authorizes all locally implementable
portions of specs 001–004 whose dependencies and required decisions are satisfied, with
TDD, predefined evidence and independent reviews. Publish coherent increments as scoped
PRs, including stacks with explicit parent branches and review order; continue eligible
work without waiting for operator review. A PR merges after an independent review and green
applicable checks. Missing CI is not a green gate, and publication does not
establish merge readiness. T001–T009 remain minimum safety before dependent work, with
conditional T023 after their evidenced completion. D29 still defers hardened persistence/
export until real profile schemas exist.
On 2026-10-01 the operator accepted ADR-0002, ratified constitution
1.2.0 and chose committed reusable workflow scaffolding. Those earlier external gates
are resolved. Add directories with their first real artifact. Exact tool/image pins,
prepared local runtime/mirror and P1/P2 qualification remain implementation premises,
not passed observations. T010–T022 are eligible only when their own dependencies and
required decisions are satisfied; completing the minimum subset does not qualify them.

The 2026-10-02 operator priority exception (local decision D60) accepts C2 (C010.5/P4)
as **DEFERRED**, nonblocking technical debt [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md) for current private
maintainer development, publication, review PR creation and merge. Frozen workflows,
a no-bypass ruleset and the disposable source-admission experiment are later obligations,
not immediate T023 prerequisites. Review PR creation does not establish merge readiness:
tests/evidence, exact pins, read-only authority, T003 isolation, exact-head CI and
independent review remain required. Whole-feature hardened guarantees remain unqualified;
the constitution and ADRs are unchanged.

## Setup and foundation

- [x] T001 Select exact signed/checksummed tool and image pins in mise.toml, bootstrap tools/go.mod and qualify approved preparation with a bounded external control driver — closed 2026-10-02, evidence: evidence/T001.md
  - Requirements: FR-001; ADRs: 0002, 0011. Depends on: operator structure, ratification and scaffold dispositions confirmed 2026-10-01; independent planning review before implementation.
  - Verify: Procedure: independently approve the explicit preparation/build closure and recorded pins before any execution; install from that approved source outside the candidate (never load candidate mise/Task configuration on the host), run `go -C tools list -m`, `mise exec -- tofu version`, `mise exec -- terramate version`; compare exact identities to pins. Missing/mismatched identity refuses preparation finalization and tool use. Create the bounded preparation control driver specified in contracts/checks.md, independently approve its exact source/input/build closure, then install it externally before running it. Against the unchanged approved preparation source, retain valid preparation plus missing/wrong tool or module identity, tampered manifest/signature/archive/image trust data, hostile ancestor/system/candidate config and false-source/wrong-candidate-path rejection controls; prove refusal timing, no hostile marker execution and failure cleanup. Challenge each guarded clause with a separately identified behavioural mutant and retain the valid control. A driver/runtime/input-delivery gap blocks T001; source inspection or an unavailable tool is not rejection evidence. No capture gate is claimed by T001 and no tool-output fixture may be captured here.
  - Evidence: `evidence/T001.md`; closed after the final r6 review.

- [x] T002 Write pin/isolation tests and minimal compiling boundary stubs in tools/internal/checks/toolchain_test.go and tools/internal/probes/security/offline_test.go — closed 2026-10-02, evidence: evidence/T002.md
  - Requirements: FR-001, FR-002; ADRs: 0008, 0011, 0021. Depends on: T001.
  - Verify: Create the bounded external boundary proof driver and compiling stubs described in contracts/checks.md; freeze and independently approve their exact source/input/build closure before building or executing outside the candidate. Through that driver execute the focused checks and security suites either using the isolated-child `go -C tools test ./internal/checks ./internal/probes/security -run "TestToolchain|TestOfflineBoundary" -count=1` procedure or the separately approved T002 compile-then-run exception: compile each exact package with pinned `go test -c` in a private network-isolated build child, then run each digest-recorded binary as approved host orchestration with `-test.run 'TestToolchain|TestOfflineBoundary' -test.count=1` and the closure's explicit deadlines, sanitized environment and private paths; valid pin/isolation control plus wrong version, absent tool, mismatched artifact/image, unqualified capture/isolation gate, credential/socket mount, outbound subprocess and malicious host-launch cases must expose behavioural red. Author capture-admission tests that observe a capture-attempt marker only in private scratch: missing/wrong identities or absent gate proof must refuse before tool invocation/output publication, while a valid qualified control is admitted. These markers are synthetic test data, not captured tool-output fixtures. Invoke the boundary stub entry from a candidate cwd with malicious Taskfile shell variables, includes, hooks, launcher replacement and local configuration: no host marker/token/network access may precede isolation. Use synthetic credentials only. Retain reachable outbound positive control, actual denial reason and isolation-off mutation; timeout/DNS failure is not denial proof. This closes tests with valid/red evidence, never the production capture gate; T003 repeats them green through the actual entry.
  - Evidence: `.local/evidence/001/t002-boundary-red.json`; initial status `not-run`.

- [x] T003 Implement trusted preparation, external host entry and local isolation in tools/cmd/lz-offline/, tools/internal/checks/offline.go, harness/capabilities.yaml and Taskfile.yml — closed 2026-10-04, evidence: evidence/T003.md
  - Requirements: FR-001, FR-002; ADRs: 0007, 0011, 0021. Depends on: T002.
  - Verify: Planned `<approved-absolute-path>/lz-offline --candidate <checkout> -- task verify:toolchain` and the same entry with `task test:offline-boundary`: valid prepared image succeeds; wrong/missing pin, mismatched artifact/image, credential/socket/cache mount, outbound child or candidate-controlled launch rejects. Implement capture admission in this entry and its toolchain target: matching pinned tool/artifact identities and proven capture/isolation gate are prerequisites to every tool-output fixture capture. Independently approve the preparation/launcher source and build closure before building, then approve binary/image digests and install outside the candidate; repeat all T002 controls green through this actual entry, including pre-invocation/pre-publication capture refusal and gate-off sensitivity. Requalify the T001 preparation controls against any new preparation closure before it is used. Prepared provider mirror/lockfile and explicit mirror-only CLI config must support real `tofu init -backend=false -lockfile=readonly` with network=none; missing provider/hash or network fallback fails. Creates lz-offline, capture admission and both child targets. T023 supplies minimal CI; C2 source admission is deferred to KI-001; full forge wrapper stays T011.
  - Evidence: `evidence/T003.md`; GREEN through the installed entry, with mutation sensitivity.

- [x] T004 Capture pinned passing/failing/truncated tofu JSON streams and write report tests in tools/internal/report/report_test.go and tests/fixtures/tofu/ — closed 2026-10-04, evidence: evidence/T004.md
  - Requirements: FR-003, SC-002; ADRs: 0008, 0011. Depends on: T003, including evidenced capture admission and isolation.
  - Verify: `go -C tools test ./internal/report -run TestReport -count=1`: record exact generating `tofu test -json` command/version; valid stream preserved, zero tests, required skip, crash, truncated/invalid stream and cleanup fault cause behavioural red before adapter.
  - Evidence: `evidence/T004.md`; behavioural RED recorded against the permissive report stub.

- [x] T005 Implement observation schema and JSON/JUnit adapters in schemas/check-report.schema.json, tools/internal/report/ and tools/cmd/json2junit/ — closed 2026-10-04, evidence: evidence/T005.md
  - Requirements: FR-003, SC-002; ADRs: 0008, 0019. Depends on: T004.
  - Verify: `task test:reports`: all V002 valid/fault cases and each-clause mutants green as a suite; preserve actual upstream diagnostics; creates target.
  - Evidence: `evidence/T005.md`; GREEN with 31/31 guard-site mutants killed; `task test:reports` through the entry.

- [x] T006 Write trace/DoD tests in tools/internal/checks/traceability_test.go, including docs exemption and stale/forged evidence
  - Requirements: FR-004, SC-003; ADRs: 0008, 0019. Depends on: T005.
  - Verify: `go -C tools test ./internal/checks -run "TestTraceability|TestDoD" -count=1`: complete focused trace and justified phase aggregate accepted; an unrelated spec-wide blanket ADR list, unmapped FR/task, missing Verify/evidence and fake/stale pass rejected; valid docs-only exemption accepted; incomplete oracle yields behavioural red.
  - Evidence: `evidence/T006.md`; RED with 36/36 controls failing behaviourally on the compiling stub.

- [x] T007 Implement shared registry, trace and DoD evaluator in harness/checks.yaml, tools/internal/checks/traceability.go and tools/cmd/lz-check/
  - Requirements: FR-004, SC-003; ADRs: 0008, 0019. Depends on: T006.
  - Verify: `task test:traceability; task check:specs; task dod -- modules/naming`: focused trace and justified aggregate fixtures pass, blanket/unmapped/forged cases fail, unimplemented naming checks report not-run until T014/T018; no missing evidence passes. Creates all three targets.
  - Evidence: `evidence/T007.md`; GREEN through the entry with 133/133 guard-site mutants killed; DoD for modules/naming is not-run until its checks exist.

- [x] T008 Write layer/changed-closure and real-tool static tests in tools/internal/checks/{dependencies,static}_test.go and tests/check/fixtures/{dependencies,static}/
  - Requirements: FR-007; ADRs: 0002, 0008, 0011. Depends on: T007.
  - Verify: `go -C tools test ./internal/checks -run "TestDependencies|TestStatic" -count=1`: leaf+consumer and shared-tool changes select exact closure; reverse edge/cycle/unresolved reference fail; unknown path selects full suite. Include separate aliases, generated files, nested subdirectories, external sources and generated-instance fixtures from ADR-0002; classify every directory/edge and retain transitive consumers. An unresolved external/package boundary fails or widens, never silently disappears. Drop a consumer or classification for each fixture class and require behavioural red; no generator is implemented. Also run pinned `tofu fmt -check`, `tofu validate -json` and configured tflint on valid/unformatted/malformed/linter-offence fixture modules; bypass each clause and require behavioural red, not missing-tool failure.
  - Evidence: `evidence/T008.md`; RED recorded for the dependency and static controls before implementation.

- [x] T009 Implement dependency scanning, applicable L0 runners and full-suite fallback in tools/internal/checks/{dependencies,static}.go, .tflint.hcl and Taskfile.yml
  - Requirements: FR-007; ADRs: 0002, 0008, 0011. Depends on: T008.
  - Verify: `task test:dependencies; task test:static`: V006 valid/dependency/static controls include every T008/ADR-0002 fixture class; deleting consumer selection, dropping an alias/generated/nested/external/generated-instance edge or accepting reversed layer edge fails its own control. Creates test:dependencies, test:static and lint targets plus changed-path selection for task check. `task lint -- modules/naming` runs real pinned `tofu fmt -check -recursive modules/naming`, mirror-only `tofu -chdir=modules/naming init -backend=false -lockfile=readonly`, `tofu -chdir=modules/naming validate -json`, and `tflint --chdir=modules/naming --format=json`; actual counted observations only. Schema result joins after T016; resource scanning is not-applicable, docs-only tests exempt.
  - Subset outcome: verify the real-tool runner on T008's fixture modules. Before T014 creates modules/naming, its lint/DoD result is not-run with nonzero required-check status; never fabricate a module pass or close V006's later schema duties.
  - Evidence: `evidence/T008.md`; GREEN through the entry with every guard site killed by a mutant; `task lint -- modules/naming` is not-run until T014.

## US1

Independent test: V001–V003, V006–V007.

- [ ] T010 [US1] Owner gate: postponed (D85) until GitLab or a protected publisher is needed; GitHub CI is T023. Write candidate-host-escape, protected-publisher and fork-cache tests in tools/internal/probes/forge/offline_test.go
  - Requirements: FR-002, FR-008; ADRs: 0007, 0021. Depends on: T009.
  - Verify: `go -C tools test ./internal/probes/forge -run TestOfflineForge -count=1`: candidate Taskfile/workflow/include attacks cannot execute before isolation; missing protected result, changed launcher or foreign publisher rejects; non-isolating stub yields behavioural red.
  - Evidence: `.local/evidence/001/t010-forge-red.json`; initial status `not-run`.

- [ ] T011 [US1] Owner gate: postponed (D85) until GitLab or a protected publisher is needed. Implement base-revision dispatch verification launcher and forge adapters in pipelines/github/offline.yml, pipelines/gitlab/offline.yml and tools/internal/checks/launcher.go
  - Requirements: FR-002, FR-008; ADRs: 0007, 0021. Depends on: T010.
  - Verify: `task test:offline-boundary; task verify:forge-offline`: local wrapper controls pass; archive as data only, no candidate code on host, token outside child; real forge absence reports blocked. Creates forge qualification target; required publisher origin setup gate remains external.
  - Evidence: `.local/evidence/001/t011-forge-adapters.json`; initial status `not-run`.

- [ ] T012 [US1] Owner gate: postponed (D85) until GitLab is in scope. Qualify trusted offline adapters on both disposable forges using tools/internal/probes/forge/offline_test.go
  - Requirements: FR-008; ADRs: 0007, 0021. Depends on: T011; external disposable repos, protected base workflow, scoped result publisher and runner setup.
  - Verify: `task verify:forge-offline`: valid candidate green, broken behaviour red; malicious Taskfile/workflow/include cannot change launch; parent fork sees no secrets/socket/cache writes; missing publisher-origin enforcement blocks rather than accepting ordinary candidate CI.
  - Evidence: `.local/evidence/001/t012-forge-offline.json`; initial status `not-run`.

## US2

Independent test: V004–V005.

- [ ] T013 [US2] SUPERSEDED by 005/T011 (D85: default template plus one reordered test template; recipe-upgrade vectors postponed). Author two independent organisation vectors and provider-free module tests in modules/naming/tests/unit.tftest.hcl, modules/naming/tests/contract.tftest.hcl and tests/fixtures/naming/
  - Requirements: FR-005, SC-004; ADRs: 0003, 0008. Depends on: T007, T009; concrete-consumer diagnosis satisfied by spec 005's stages (D85: one scalar call per logical resource, template as data, single module). Eligible under D61 when these prerequisites are satisfied.
  - Verify: `mise exec -- tofu -chdir=modules/naming test`: compiling pure module stub produces behavioural red for concrete expected names/labels; valid unusual template, collision/truncation/import/upgrade cases and expect_failures for validations; record actual CLI output.
  - Evidence: `.local/evidence/001/t013-naming-red.json`; initial status `not-run`.

- [ ] T014 [US2] SUPERSEDED by 005/T012 (D85: limits in modules/naming/kinds.yaml; names.yaml and snapshot target postponed). Implement pure naming module and documented algorithm version/override in modules/naming/{main,variables,outputs}.tf, modules/naming/README.md and names.yaml
  - Requirements: FR-005, SC-004; ADRs: 0003, 0011. Depends on: T013.
  - Verify: `task test:naming; task snap:check -- modules/naming`: two templates and independent vectors pass; impossible/unknown cloud kind fails, import override stable; label/profile changes never rename; omission mutants fail. Creates naming/snapshot targets and baseline from actual pure plan.
  - Evidence: `.local/evidence/001/t014-naming.json`; initial status `not-run`.

- [ ] T015 [US2] Owner gate: postponed (D85) until assent or a plan policy plane exists; labels are checked by 005 module tests. Write naming/label projection differential tests in tools/internal/namingdata/projections_test.go, policies/plan/naming_test.rego and policies/assent/naming.test.yaml
  - Requirements: FR-006; ADRs: 0003, 0008, 0021. Depends on: T014.
  - Verify: `go -C tools test ./internal/namingdata -run TestProjections -count=1` plus pinned Conftest/assent tests: missing each required label, unknown key and tenant auth-key edits reject in applicable planes; wrong projection stub is behavioural red.
  - Evidence: `.local/evidence/001/t015-naming-policy-red.json`; initial status `not-run`.

- [ ] T016 [US2] Owner gate: postponed (D85) until assent or a plan policy plane exists. Implement strict catalogue/org schemas and handwritten independent projections in schemas/{naming,labels}.schema.json, tools/internal/namingdata/ and policies/{plan,assent}/
  - Requirements: FR-006; ADRs: 0003, 0011, 0021. Depends on: T015.
  - Verify: `task test:naming-policy; task generate:check; task schema:check; task policy`: handwritten evaluators and applicable policy projections agree with shared cases whose expected results are independently specified; deliberately wrong projections, duplicate YAML, unknown key, missing required label and stale generated documentation fail. Generate documentation initially; no shared evaluator/test generator. Creates test:naming-policy, generate:check, schema:check and the policy aggregate. `task schema:check` validates names.yaml and both org naming/label fixture sets against the actual strict schemas and rejects malformed/duplicate/unknown fields and zero discovery; no invented cloud fields or unaudited limits.
  - Evidence: `.local/evidence/001/t016-naming-policy.json`; initial status `not-run`.

T013–T016 also cover the detailed V004–V006 matrix in
../../docs/explanation/naming-and-labelling-design.md: repeated same-kind resources, strict
unknown-field rejection before HCL conversion, same-scope normalized/hash collisions, exact
import overrides, pinned name recipe upgrades, metadata-only stability, separate target
projections and stable selector subsets. Each applicable clause needs independent valid/red/green
controls. Scalar cardinality/shared context is selected; detailed call sites/fields and
module splitting remain illustrative pending concrete-consumer diagnosis. These tasks
are eligible under D61 after their dependencies; no naming qualification is claimed here.

## US3

Independent test: V003, V008.

- [ ] T017 [US3] Owner gate: postponed (D87) with the full AgentEx scope until the second vertical slice or agents repeatedly misread check output. Write diagnostic/evidence/decision-map tests in tools/internal/checks/agentex_test.go
  - Requirements: FR-009; ADRs: 0008, 0019. Depends on: T007, T014, T016.
  - Verify: `go -C tools test ./internal/checks -run TestAgentEx -count=1`: seeded defect needs stable rule id/location/observed/expected/fix; absent observation stays not-run, expert duty review-required; stale ADR mapping and a blanket mapping without decision rationale fail, incomplete implementation is behavioural red.
  - Evidence: `.local/evidence/001/t017-agentex-red.json`; initial status `not-run`.

- [ ] T018 [US3] Owner gate: postponed (D87) with the full AgentEx scope until the second vertical slice or agents repeatedly misread check output. Implement structured diagnostics and decision-map rendering in tools/internal/checks/agentex.go and docs/reference/decision-map.md
  - Requirements: FR-009; ADRs: 0019. Depends on: T017.
  - Verify: `task test:agentex; task decision-map:check`: V008 controls and unknown/stale ADR references pass/fail correctly; remove any required diagnostic/evidence clause and its mutant fails. Creates both targets.
  - Evidence: `.local/evidence/001/t018-agentex.json`; initial status `not-run`.

- [ ] T019 [US3] Owner gate: postponed (D87) with the full AgentEx scope until the second vertical slice or agents repeatedly misread check output. Write progressive router and implemented-kind guides in AGENTS.md and harness/guides/{checks,naming}.md
  - Requirements: FR-009; ADRs: 0019. Depends on: T018.
  - Verify: exempt — docs-only; content review confirms commands, trusted boundaries and deferred generators are accurately described.
  - Evidence: `.local/evidence/001/t019-docs-review.json`; initial status `not-run` (docs content review only).

## Polish and exit

- [ ] T020 Owner gate: postponed (D87); no exclusive self-hosted runner. Write full-suite latency/discovery tests in tools/internal/checks/latency_test.go
  - Requirements: FR-001, FR-002, SC-001; ADRs: 0008, 0019. Depends on: T009, T012, T016, T018.
  - Verify: `go -C tools test ./internal/checks -run TestLatency -count=1`: complete applicable naming suite on the declared exclusive runner accepted; unknown/overlapping runner blocks, 120s overrun or omitted layer/count rejects; disabling deadline/discovery clause yields behavioural red.
  - Evidence: `.local/evidence/001/t020-latency-red.json`; initial status `not-run`.

- [ ] T021 Owner gate: postponed (D87); no exclusive self-hosted runner. Implement dedicated-runner latency gate in tools/internal/checks/latency.go and Taskfile.yml
  - Requirements: SC-001; ADRs: 0008, 0019. Depends on: T020.
  - Verify: `task verify:latency`: entire applicable naming check suite <=120s after preparation on an exclusive self-hosted runner with fixed CPU/memory, digest-pinned OS/image, cache preparation and no overlapping jobs recorded; versions, counts and each layer are recorded, including actual T009 L0 and T016 schema results; >120s/missing layer fails; unknown runner allocation or overlapping jobs blocks measurement. Creates target; no filtering to meet budget.
  - Evidence: `.local/evidence/001/t021-latency.json`; initial status `not-run`.

- [ ] T022 Run foundation exit checks and independent evidence inspection through harness/checks.yaml
  - Requirements: FR-001, FR-002, FR-003, FR-004, FR-005, FR-006, FR-007, FR-008, FR-009, FR-010, SC-001, SC-002, SC-003, SC-004, SC-005; ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021. Depends on: T019, T021, T023.
  - Verify: Run every V001–V009 command from spec.md and `task check -- modules/naming`; valid cases pass, every guarded clause has a killed behavioural mutant/valid unusual case; absent tool, forge proof or expert review prevents phase exit. Deferred C010.5/P4 (KI-001) also prevent whole-feature hardened acceptance; the current private increment does not close T022.
  - Aggregate ADR rationale: 0002 paths/layers; 0003 names/labels; 0007 forge execution; 0008 checks/evidence; 0011 pins; 0019 diagnostics/DoD; 0021 protected boundary.
  - Evidence: `.local/evidence/001/t022-exit.json`; initial status `not-run`.

## Planned command creators

| Task target | Creating task |
| --- | --- |
| bounded external preparation control driver (not a Task target) | T001, exact closure approval before build/execution |
| bounded external boundary proof driver/stub (not a Task target) | T002, exact closure approval before build/execution |
| `lz-offline` (approved external host entry) | T003 |
| tool-output fixture capture admission | T003, T002 authors valid/red controls |
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
| `task test:runtime-image` | T023 |
| `task test:foundation-ci` | T023 |
| `task ci:foundation` | T023 |

## Conditional CI bootstrap

- [x] T023 Test first, then implement minimal GitHub foundation CI in .github/workflows/foundation.yml, pipelines/github/foundation-source.json, tools/cmd/lz-pack/ (reproducible runtime image), tools/internal/checks/foundation_ci_test.go, tools/internal/checks/foundation_ci.go and Taskfile.yml — closed with gaps 2026-10-06 (CI run of this code and registry digest coverage owed), evidence: evidence/T023.md
  - Requirements: FR-010 active clauses C010.1–C010.4/C010.6, SC-005; ADRs: 0007, 0008, 0011, 0021. Depends on: T009; evidenced T001–T009 completion, independent review of exact workflow/launcher closure and actual GitHub execution available. C010.5/P4 remain DEFERRED under D60, not prerequisites for this task or current private maintainer publication/PR/merge.
  - Verify: Planned child `task test:runtime-image; task test:foundation-ci; task ci:foundation`. Write approved-source/candidate fixtures first; retain behavioral red for each active C010 clause before implementation. Reject unpinned Actions, widened authority, candidate host execution, omitted/zero/skipped checks and stale source/head.
  - Deferred source admission: [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md) retains the frozen-workflow/no-bypass ruleset, disposable source-admission experiment, initial-history/rebase-merge admission, edit/add/rename/delete/second-push denials and setup/cleanup/recovery qualification. These remain owed, not green; minimal T023 does not qualify pre-execution source enforcement.
  - CI proof: execute the T003/T005/T007/T009 aggregate in T003 isolation. Independently reviewed YAML uses full-SHA Actions and digest-pinned approved host launcher/image, owned-branch push triggers, ephemeral hosted runner, contents-read outer fetch and 10-minute timeout; candidate archive is data, never host checkout/build/source/execution. Tokens stay outside the child. Retain tests green, an actual failing behavioral run and valid GitHub run/check on the foundation PR head. Verify executed workflow/launcher/image/publisher identities, exact candidate SHA, counts and evidence. Missing/foreign/stale/cancelled/skipped CI blocks merge, not review PR creation; no cloud/deployment/protected-environment authority. Creates both child targets, source manifest and the active V009 packet; full T010–T012 qualification remains open.
  - Evidence: `.local/evidence/001/t023-foundation-ci.json`; initial status `not-run`; retain pre-implementation red, green controls, independent source review, actual run/check IDs and exact head/source bindings, plus the V009 packet reference. Record C010.5/P4 as DEFERRED to KI-001, never passed or silently omitted.

- [ ] T024 Write snapshot-admission tests for generated stacks in tools/cmd/lz-offline/ tests and fixtures
  - Requirements: FR-001, FR-002; ADRs: 0002, 0008, 0011, 0021. Depends on: T003.
  - Verify: `go -C tools test ./cmd/lz-offline -count=1`: a candidate holding `stacks/` (generated stacks, `stacks/deployments.yaml`, `stacks/_lz/*.tm.hcl`) and a root `terramate.tm.hcl` is snapshotted with those paths present; a hostile file under `stacks/` stays data with no effect on the entry's own targets; a root file outside the allowlist stays absent; symlink, size and depth limits apply under `stacks/` as elsewhere. The current entry omits `stacks/` and `terramate.tm.hcl`: behavioural red. Found 2026-10-06: without this no offline check sees the stacks spec 005 generates (005/T038).
  - Evidence: `evidence/T024.md`; PR; initial status `not-run`.

- [ ] T025 Admit `stacks/` and `terramate.tm.hcl` in tools/cmd/lz-offline/main.go and publish the next runtime revision
  - Requirements: FR-001, FR-002, FR-010; ADRs: 0002, 0008, 0011, 0021. Depends on: T024, T023.
  - Verify: `task test:offline-boundary; task test:runtime-image; task test:foundation-ci`: T024 controls green; the boundary suite unchanged and green; the entry rebuilt for the current runtime manifest; the runtime packed reproducibly by `lz-pack` as the next revision (two packs, identical digest), published to `ghcr.io/platformrelay/landingzone-for-ovhcloud/runtime` (D89), and `pipelines/github/foundation-source.json` plus the rendered workflow pinned to its manifest and layer digests; the previous revision stays published.
  - Evidence: `evidence/T025.md`; PR (image digests, run id); initial status `not-run`.

- [x] T026 Let the foundation CI check resolve spec-scoped creators in tools/internal/checks/foundation_ci.go and foundation_ci_test.go — closed 2026-10-06, evidence: evidence/T026.md
  - Requirements: FR-010; ADRs: 0007, 0008. Depends on: T023.
  - Verify: `task test:foundation-ci`: a test written first shows a target whose registry creator is a spec-scoped key (`005/T055`) of a ticked task in another spec is refused today as `CHECK_NOT_RUNNABLE` (behavioural red), then accepted; an open spec-scoped creator, an unknown spec prefix and a missing task id are still refused; bare spec 001 creators behave as before. Found 2026-10-06 when `test:live-lane` (creator 005/T055, closed) could not join the foundation workflow.
  - Evidence: `evidence/T026.md`; PR; initial status `not-run`.

## Dependencies & execution order

The per-task Depends on fields are authoritative; local task references form a DAG.
Test tasks precede their implementation; all external gates remain blocked until
observed. A command may be defined with an explicit blocked result before its live
prerequisites exist; defining it does not close its acceptance check.

Minimum safety: T001–T009 supplies offline/report/traceability and fixture-based
dependency/static checks without cloud resources. Review the foundation, then run T023
only if those tasks have evidenced completion; C2 source admission remains deferred to
KI-001. Review PR creation may precede merge readiness.
Naming, forge adapters, AgentEx and later checks are eligible when their own prerequisites
are satisfied. T022 still requires real forge evidence, whole-suite latency and the
deferred guarantees identified in its verification contract. Dependent IAM/state
implementation follows minimum safety, retaining its separate live action/account/
recovery/budget authority and task prerequisites. Independent documentation may proceed
earlier; record a blocked task's precise prerequisite and continue other eligible work.

## Parallel opportunities

Within 001, naming T013–T016 and forge T010–T012 can branch after T009 on
distinct files, subject to their own external gates. Diagnostics T017–T019 wait for
T014/T016. No incomplete dependency receives a parallel flag.

## Completion

Rerun spec acceptance commands and inspect matching evidence; do not close a task using
a missing-binary result, empty discovery, self-authored green assertion or a simulation
standing in for required live proof. Docs-only work remains exempt. Only the operator
activates dependent live experiments/ratifies ADRs. A refuted observed premise gets its
explicit downstream stop decision; an unrun required experiment stays owed and blocked
unless explicitly deferred within the scoped D60 exception. C2 stays DEFERRED in KI-001,
not passed, and remains required for whole-feature hardened acceptance.
