# Tasks: [FEATURE]
Status: planned · Input: spec.md, plan.md, research.md, data-model.md, contracts/
All boxes start open. Commands listed here are planned until their creating tasks land.
Tests are mandatory for non-documentation behaviour. Documentation-only work is exempt.
User interface hypotheses stay pending until decided. Independent tasks may proceed.
## Phase 1: Setup
- [ ] T001 [action with exact path]
  - Requirements: FR-001; ADRs: 0008. Depends on: [actual prerequisites and unresolved owner gates].
  - Verify: [command/procedure; expected positive and negative outcome; creator task]
  - Evidence: [sanitised path; initial status not-run]
## Phase 2: Foundation
[Shared prerequisites, tests before implementation]
## Phase 3: User Story 1 (Priority: P1)
Independent test: [check IDs]
[Test tasks then implementation; focused relevant ADRs for each task; every numbered
requirement clause gets an independent expected outcome and defect control. Reject
unrelated blanket ADR sets; justify phase aggregates per requirement.]
[Include user-visible outcomes and failure/recovery checks, not only internal assertions.]
## Final phase: Polish
[Docs-only tasks: Verify: exempt — docs-only]
## Dependencies & execution order
[Explicit edges; no cycle; external gates; MVP boundary]
## Parallel opportunities
[Only independent paths after shared dependencies; [P] only when safe]
## Completion
[Green evidence for all criteria; unresolved live prerequisites stay blocked]
