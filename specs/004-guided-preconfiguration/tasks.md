# Tasks: Friendly, resumable repository preconfiguration
Status: planned · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md
All boxes are open. Targets are planned until their creating tasks land; evidence starts `not-run`.
Adversarial plan review: CLEAN (2026-10-01); optional provenance/refresh/write-scope notes included.
Real exports require actual schema/catalogue creators and any dependent joint naming decision.

## Setup and bounded renderer experiment

- [ ] T001 Pin the selected renderer after a bounded Huh/Gum comparison in tools/go.mod, mise.toml, harness/configure/limits.yaml and tests/fixtures/configure/terminal/
  - Requirements: FR-003, FR-005, FR-007, SC-001; ADRs: 0002, 0008, 0011, 0023. Depends on: 001/T001; credential-free runtime/package preparation and reviewed supply-chain pins.
  - Verify: bounded prototype on Linux amd64, recording exact tool/library versions and commands: help/cancel/back/plain/narrow-terminal controls produce usable output; unsupported terminal/platform and missing pin refuse qualification. Set concrete session/answer/output size and helper duration/process-cleanup bounds before implementation; absent/unlimited limits reject. Capture actual pinned outputs, not hand-written terminal fixtures. This is a feasibility experiment, not a shipped second renderer.
  - Evidence: `.local/evidence/004/t001-renderer.json`; initial status `not-run`.

## US1 — Understand, choose and edit (P1)
Independent checks: V001 and V005; synthetic choices are algorithm fixtures, not supported profiles.

- [ ] T002 [US1] Write independent use-case/default/progress/controller tests in tools/internal/configure/flow_test.go and tests/fixtures/configure/choices/
  - Requirements: FR-001, FR-002, FR-008, FR-009, FR-010, SC-001; ADRs: 0003, 0008, 0013, 0019, 0023. Depends on: T001.
  - Verify: `go -C tools test ./internal/configure -run TestFlow -count=1`: tests retain valid unusual choices and a compiling controller stub goes behaviorally red for resolved defaults/help/consequences. Exercise explicit X versus suggested X followed by a use-case edit to Y, valid override preservation, invalid dependent answer removal, conditional progress/back/edit and regulated/experimental gates. Unknown/secret fields and unsupported rules reject; remove each guarded clause and confirm red.
  - Evidence: `.local/evidence/004/t002-flow-red.json`; initial status `not-run`.

- [ ] T003 [US1] Implement the shared strict journey controller and versioned choice rules in tools/internal/configure/flow.go, harness/configure/steps.yaml and Taskfile.yml
  - Requirements: FR-001, FR-002, FR-008, FR-009, FR-010, SC-001; ADRs: 0003, 0008, 0013, 0019, 0023. Depends on: T002.
  - Verify: `task test:configure-flow` (creates target): T002 controls pass; exact valid-completed/relevant-required count matches independent vectors after conditional edits; explicit/default provenance and visible reasons survive re-resolution. Unknown/duplicate choice keys, opaque missing help and forged qualification refuse. Zero choice/test discovery fails. UI and noninteractive callers share this controller; no renderer-owned decisions.
  - Evidence: `.local/evidence/004/t003-flow.json`; initial status `not-run`.

## US2 — Leave and return safely (P1)
Independent check: V002; current validity, not a saved percent, decides the resumed step.

- [ ] T004 [US2] Write strict checkpoint/fault/concurrency tests in tools/internal/configure/session_test.go and tests/fixtures/configure/sessions/
  - Requirements: FR-002, FR-003, FR-004, FR-008, FR-010, SC-002; ADRs: 0008, 0019, 0023. Depends on: T003.
  - Verify: `go -C tools test ./internal/configure -run TestSession -count=1`: valid save/restart control works and a compiling unsafe persistence stub is behaviorally red. Exercise kill/disk-full mid-write, two writers, lock-owner crash, permissions, root/path symlink swaps, oversized/duplicate/unknown fields, foreign working tree and stale source/schema/prompt contents. Equal explicit/default values must retain different provenance across resume/use-case edit. Same-user valid edits revalidate without an authenticity claim; an old completion flag cannot pass.
  - Evidence: `.local/evidence/004/t004-session-red.json`; initial status `not-run`.

- [ ] T005 [US2] Implement bounded atomic checkpoint storage/revalidation in tools/internal/configure/session.go, schemas/configure-session.schema.json and Taskfile.yml
  - Requirements: FR-002, FR-003, FR-004, FR-008, FR-010, SC-002; ADRs: 0008, 0019, 0023. Depends on: T004. This task creates the strict local session schema/decoder; no naming module is required for synthetic checkpoint/resume work.
  - Verify: `task test:configure-resume` (creates target): all T004 positive/rejection/fault controls pass on the qualified filesystem; interruptions retain the last valid checkpoint, writer recovery is bounded and foreign roots reject. Persist provenance and actual content/revision bindings; recompute validity/progress. Writes stay inside owned roots; missing bound config refuses. Clause mutants fail and no signing/authentication subsystem is added.
  - Evidence: `.local/evidence/004/t005-session.json`; initial status `not-run`.

## Trusted read-only prerequisites

- [ ] T006 Write helper-boundary/freshness tests in tools/internal/configure/checks_test.go and tests/fixtures/configure/checks/
  - Requirements: FR-004, FR-005, FR-008; ADRs: 0007, 0008, 0019, 0021, 0023. Depends on: T003; 001/T007 shared report/evidence contract.
  - Verify: `go -C tools test ./internal/configure -run TestChecks -count=1`: actual pinned local-tool capture is the valid control; compiling permissive adapter is behaviorally red. Timeout/hanging descendant, missing/mismatched binary, changed tool/content with unchanged Git HEAD, malicious hooks/answers/session commands, secret input and network attempts must refuse without outside-root writes. Refresh on resume/final review/export; remove each deadline, origin or freshness guard and confirm red.
  - Evidence: `.local/evidence/004/t006-checks-red.json`; initial status `not-run`.

- [ ] T007 Implement bounded trusted read-only helpers in tools/internal/configure/checks.go, harness/configure/checks.yaml and Taskfile.yml
  - Requirements: FR-004, FR-005, FR-008; ADRs: 0007, 0008, 0019, 0021, 0023. Depends on: T006.
  - Verify: `task test:configure-checks` (creates target): T006 controls pass, including process-tree cleanup and actual source/tool binding refresh; missing observations report blocked, never pass. Checks come from the trusted pinned implementation, not executable repo/session/answer values. Evidence uses the shared envelope; zero helper discovery and unknown limits reject. No cloud credentials or network are required.
  - Evidence: `.local/evidence/004/t007-checks.json`; initial status `not-run`.

## Real terminal integration

- [ ] T008 [US1] Write selected-renderer PTY/plain/controller-equivalence tests in tools/internal/configure/terminal_test.go and tests/fixtures/configure/terminal/
  - Requirements: FR-001, FR-002, FR-007, FR-009, FR-010, SC-001; ADRs: 0008, 0013, 0019, 0023. Depends on: T005, T007.
  - Verify: `go -C tools test ./internal/configure -run TestTerminal -count=1`: use actual T001 pinned renderer captures and input events; valid keyboard/plain controller output is retained. A compiling renderer that loses cancel/help, skips validation or invents progress is behaviorally red. Exercise no TTY, narrow terminal, back/edit/save/resume and identical noninteractive effective data; unsupported matrix stays unqualified.
  - Evidence: `.local/evidence/004/t008-terminal-red.json`; initial status `not-run`.

- [ ] T009 [US1] Implement the friendly terminal adapter and Task entrypoints in tools/cmd/lz-configure/main.go, tools/internal/configure/terminal.go and Taskfile.yml
  - Requirements: FR-001, FR-002, FR-007, FR-009, FR-010, SC-001; ADRs: 0008, 0013, 0019, 0023. Depends on: T008.
  - Verify: `task test:configure-flow; task test:configure-resume; task test:configure-checks; task verify:configure-journey` (creates configure, configure:resume and verify:configure-journey): T008 and journey controls pass; show explained defaults/provenance/consequences, accurate progress and next actions. An independent human performs the tone/clarity rubric; missing review/capture reports review-required/blocked. At this task, review/export can remain explicitly unavailable until T011; never show a fake completed export.
  - Evidence: `.local/evidence/004/t009-journey.json`; initial status `not-run`.
  - Human rubric source: plan.md's Verification strategy and ADR-0023's Friendly flow and consequences, plus SC-001; T012 expands these into the finished how-to after export integration, without gating this earlier walkthrough.

## US3 — Review and export a draft (P2)
Independent checks: V003–V005; export is not permission to apply the resulting data.

- [ ] T010 [US3] Write reviewed-bundle/export safety tests in tools/internal/configure/export_test.go and tests/fixtures/configure/bundles/
  - Requirements: FR-004, FR-006, FR-008, SC-002; ADRs: 0003, 0005, 0008, 0021, 0023. Depends on: T005, T007.
  - Verify: `go -C tools test ./internal/configure -run TestExport -count=1`: valid reviewed fixture bundle is a control; compiling overwrite/permissive exporter is behaviorally red. Exercise unreviewed/invalid/unknown data, edit after review, changed tools/dirty source, incomplete/crashed publication, raced/symlinked destination and modified retry bundle. Exact verified retry is idempotent; volatile observation times cannot change config output. Inventory every path outside owned roots, including ignored files; existing configuration and credentials remain byte-for-byte unchanged.
  - Evidence: `.local/evidence/004/t010-export-red.json`; initial status `not-run`.

- [ ] T011 [US3] Implement strict deterministic draft export in tools/internal/configure/export.go, tools/internal/configure/terminal.go and Taskfile.yml
  - Requirements: FR-004, FR-006, FR-008, SC-002; ADRs: 0003, 0005, 0008, 0021, 0023. Depends on: T010, T009 for synthetic export. Real-export branch also depends on 001/T016 and actual configuration/profile schemas and supported catalogue creators in the later adoption spec, plus any dependent joint naming choice. Without them only explicit synthetic-fixture verification may pass; real export reports blocked. These gates do not block generic save/resume.
  - Verify: `task test:configure-export` (creates target): all T010 controls pass; confirmation binds exact effective draft/source digests and invalidates on change. Refresh helpers, show deterministic diff/blockers, atomically publish a complete schema-valid staging bundle with manifest and idempotent verified retry. No writes outside owned roots, cloud calls, secret collection, apply/commit/merge/procurement. Every guarded clause has red mutation proof; partial/missing discovery never passes.
  - Evidence: `.local/evidence/004/t011-export.json`; initial status `not-run`.

## Guide and final independent walkthrough

- [ ] T012 Write the setup/resume/review guide and human clarity rubric in docs/how-to/preconfigure-repository.md
  - Requirements: FR-001, FR-002, FR-006, FR-009, FR-010, SC-001; ADRs: 0013, 0019, 0023. Depends on: T009, T011.
  - Verify: exempt — docs-only prose/rubric authoring; any added executable example remains behavioral work with its own mapped target.
  - Evidence: docs content review; no invented behavioral-test evidence required.

- [ ] T013 Aggregate V001–V005 and inspect independent journey/evidence in tools/internal/configure/qualification_test.go and Taskfile.yml
  - Requirements: FR-001, FR-002, FR-003, FR-004, FR-005, FR-006, FR-007, FR-008, FR-009, FR-010, SC-001, SC-002; ADRs: 0008, 0013, 0019, 0021, 0023. Depends on: T009, T011, T012.
  - Verify: `task verify:configure` (creates target): run all four test:configure-* targets, verify:configure-journey and exact supported-terminal captures; independently perform every V001–V005 positive/rejection/interruption case and human rubric. Seed missing capture/human review/command/dependency evidence and zero discovery: aggregate refuses qualification; behavioral mutants fail. Distinguish fixture qualification from real export readiness and retain actual-tool private evidence. All outside-root inventories remain unchanged.
  - Evidence: `.local/evidence/004/t013-qualification.json`; initial status `not-run`.

## Planned command creators

| Task target | Creating task |
| --- | --- |
| `task test:configure-flow` | T003 |
| `task test:configure-resume` | T005 |
| `task test:configure-checks` | T007 |
| `task configure` | T009 |
| `task configure:resume` | T009 |
| `task verify:configure-journey` | T009 |
| `task test:configure-export` | T011 |
| `task verify:configure` | T013 |

## Dependencies, parallel work and MVP
T001 → T002 → T003. After T003, checkpoint T004→T005 and helper T006→T007 paths can proceed
independently in separate files. Their common exports/controller contracts must stay fixed.
T005+T007 → T008→T009 and T010. T009+T010 → T011 → T012 → T013 (T013 also needs T009).
No task authorizes cloud work. Phase001 cross-feature edges are explicit above and acyclic.

The first useful increment is a credential-free synthetic journey with explained defaults,
progress and save/resume. It is labelled fixture-only. Actual repository-ready configuration is
gated on real schema/catalogue creators; the interface proposal is not silently ratified.
Support only the measured renderer/platform first. No second renderer, portal or broad CLI.

## Completion
All requirements and V001–V005 need evidence, clause red/green proof and independent human review
for the claimed scope. Missing real-data, platform, pin or human evidence stays blocked/not-run.
Documentation authoring is exempt; executable examples, schemas, controllers and checks are not.
