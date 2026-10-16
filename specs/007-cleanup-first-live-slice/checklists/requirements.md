# Specification quality checklist: Cleanup-first live slice
Purpose: check the spec set before the task loop · Created: 2026-10-16 · Revised: 2026-10-16 (spec-set review round 1) · Spec: [spec.md](../spec.md)

## Content
- [x] Scope matches D119 items 1–5 in D119's order, with the coordinator's round-1 decisions (A24
  deferred with its split design, B5 offline)
- [x] Every requirement is observable and has a check (V001–V006)
- [x] Every OVHcloud claim cites `kb/` or evidence, or is marked UNVERIFIED
- [x] Open owner points carry a default (four, confirmed by the coordinator)
- [x] Out-of-scope items each name a trigger
- [x] FR-010's limits are stated as coordinator procedure; no sensor is claimed for them

## Tasks
- [x] Every offline Verify names a Task target the entry admits (`task <target> [-- <path>]`)
- [x] Every behaviour story lists its test tasks first, with given/when/then cases, the defect each
  exposes, *pins* marked, and fixed signatures in data-model.md (the clock interface included)
- [x] Every live task says it is run by the coordinator under D118, from a reviewed SHA on `main`
- [x] Every resource a live task creates is findable from the run id (names and tags carry it; the
  nameless alert through the baseline), and removable from a fresh process (the budget-alert sweep,
  recovery files kept until reconciliation, T013)
- [x] Every file a check reads is in its registry scope or inputs (the probe root, `modules/cloud-project`
  and the new package are added by T010 and T012; the fixture is under `test:stacks`)
- [x] A disposable resource can never be accepted as retained; it blocks later runs and completion
- [x] Cut-offs: T019 not after 2026-10-21 23:00; T022 done by 2026-10-22; 2026-10-23 verification
  and cleanup only
