# Updated specification analysis

Date: 2026-10-01 · Scope: draft features 001–004 after direction/onboarding task review.
Inputs were inspected read-only; this file records the analysis output. Mapping coverage is
planning traceability, not executed product-test coverage. Initial three-phase results are
historical in speckit-analysis.md.

No unresolved critical/high cross-artifact finding remains after the independent reviews and
applied dispositions. The naming interface choice remains an explicit implementation gate.

## Coverage

| Requirement | Tasks | Checks |
| --- | --- | --- |
| 001/FR-001 | T001, T002, T003, T020, T022 | V001 |
| 001/FR-002 | T002, T003, T010, T011, T020, T022 | V001 |
| 001/FR-003 | T004, T005, T022 | V002 |
| 001/FR-004 | T006, T007, T022 | V003 |
| 001/FR-005 | T013, T014, T022 | V004 |
| 001/FR-006 | T015, T016, T022 | V005 |
| 001/FR-007 | T008, T009, T022 | V006 |
| 001/FR-008 | T010, T011, T012, T022 | V007 |
| 001/FR-009 | T017, T018, T019, T022 | V008 |
| 001/SC-001 | T020, T021, T022 | V001 |
| 001/SC-002 | T004, T005, T022 | V002 |
| 001/SC-003 | T006, T007, T022 | V003 |
| 001/SC-004 | T013, T014, T022 | V004 |
| 002/FR-001 | T002, T003, T008, T013, T014, T021 | V001, V005 |
| 002/FR-002 | T001, T004, T005, T008, T021 | V001 |
| 002/FR-003 | T006, T007, T008, T021 | V002 |
| 002/FR-004 | T009, T010, T020, T021 | V003 |
| 002/FR-005 | T009, T010, T011, T012, T020, T021 | V003, V004 |
| 002/FR-006 | T013, T014, T021 | V005 |
| 002/FR-007 | T015, T016, T021 | V006 |
| 002/FR-008 | T001, T017, T018, T019, T021 | V007 |
| 002/SC-001 | T005, T007, T008, T021 | V001, V002 |
| 002/SC-002 | T010, T012, T021 | V003, V004 |
| 002/SC-003 | T018, T019, T021 | V007 |
| 003/FR-001 | T001, T002, T003, T006 | V001 |
| 003/FR-002 | T002, T003, T005, T007, T008 | V001 |
| 003/FR-003 | T004, T005, T007, T008 | V002 |
| 003/FR-004 | T009, T010 | V003 |
| 003/FR-005 | T011, T012, T013, T014 | V004, V005 |
| 003/FR-006 | T015, T016, T017 | V006 |
| 003/FR-007 | T018, T019 | V007 |
| 003/FR-008 | T020, T023 | V008 |
| 003/FR-009 | T021, T022, T023, T024 | V008 |
| 003/SC-001 | T008, T023 | V001, V002 |
| 003/SC-002 | T021, T022, T023 | V008 |
| 004/FR-001 | T002, T003, T008, T009, T012, T013 | V001 |
| 004/FR-002 | T002, T003, T004, T005, T008, T009, T012, T013 | V001 |
| 004/FR-003 | T001, T004, T005, T013 | V002 |
| 004/FR-004 | T004, T005, T006, T007, T010, T011, T013 | V002 |
| 004/FR-005 | T001, T006, T007, T013 | V003 |
| 004/FR-006 | T010, T011, T012, T013 | V004 |
| 004/FR-007 | T001, T008, T009, T013 | V005 |
| 004/FR-008 | T002, T003, T004, T005, T006, T007, T010, T011, T013 | V003, V004 |
| 004/FR-009 | T002, T003, T008, T009, T012, T013 | V001 |
| 004/FR-010 | T002, T003, T004, T005, T008, T009, T012, T013 | V001 |
| 004/SC-001 | T001, T002, T003, T008, T009, T012, T013 | V001, V005 |
| 004/SC-002 | T004, T005, T010, T011, T013 | V002, V004 |

## Metrics and checks performed

- 47 requirements (36 FR, 11 SC), all mapped to tasks and acceptance checks.
- 28 predefined acceptance checks; 80 tasks, including 75 behavioral verification/evidence
  contracts and five explicit docs-only exemptions.
- Amendment 2026-10-01 (external adversarial review): 003/T021 merged test-authoring and
  implementation; it is now test-authoring T021 plus implementation T022, and the later 003
  ids shifted (inspect T023, docs T024). Counts and the 003 mapping rows above reflect that.
- 43 acceptance Task targets have named creators; unavailable targets remain planned.
- Both fixture and real-integration dependency branches are acyclic; all named task edges resolve.
- Generic 004 checkpoint/terminal work does not depend on the pending naming interface choice.
  Real export retains 001/T016 plus actual configuration/profile/catalogue creator gates.
- 23 authored ADR identities are unique; 0024 is reserved for future cost/sandbox operations.
- Bash syntax, tracked JSON parsing, exact three override compositions, four feature prerequisites
  and diff whitespace pass. A real missing-tasks prerequisite control exits nonzero.
- Reference folder ignore was checked in the main checkout and worktree; the published map is
  tracked, while the main-checkout clone is ignored and pinned to 0ab581a39afbe61a2daa55b39238530f38665cc2.

## Semantic consistency and decisions

The constitution, prompts and overrides retain predefined checks, test-first implementation,
actual pinned fixtures, per-clause behavioral-red controls, honest discovery/status and docs-only
exemption. Reference-baseline ambition does not require market evidence before bounded experiments;
unobserved platform mechanisms still gate downstream claims. Proposed ADRs remain Proposed.

Naming flexibility is independent of call cardinality. Logical resource identity, protected metadata
provenance, exact imports, frozen recipes, scoped collision checks and per-target projections have
concrete controls in 001 V004–V006/T013–T016. The pending scalar/batch choice was not ratified.

004 uses one controller and one selected renderer, versioned explained use-case defaults, valid
conditional progress, explicit/default provenance across resume, refreshed source/tool checks,
bounded untrusted checkpoints and atomic reviewed draft bundles. Only narrow local state/staging/
evidence roots may change; synthetic fixtures do not qualify a real profile or export.

The upstream comparison credits existing network, IAM, encryption/backend and adoption guides.
Additional value remains conditional on delivering qualified operating workflows. Reuse entries
carry source/provenance and required checks; no upstream implementation was copied or executed.

## Outstanding gates and limits

All 28 product acceptance checks remain planned/not-run. No module, Taskfile, wizard, real terminal
capture, toolchain qualification, cloud/forge probe, deployment, purchase or supported tuple was
produced. Installed tool versions still differ from proposed pins. Human journey validation is a
future implementation check; independent design review does not substitute for it.

Naming choice, exact renderer/pins, trusted runtime preparation, real schema/catalogue creators,
protected forge setup and numeric sandbox/cleanup/recovery readiness remain gated. Activate only
the bounded task scope whose prerequisites have been met; incomplete evidence cannot close a phase.
