# Specification quality checklist: Cleanup-first live slice
Purpose: check the spec set before the task loop · Created: 2026-10-16 · Spec: [spec.md](../spec.md)

## Content
- [x] Scope matches D119 items 1–5, in D119's order
- [x] Every requirement is observable and has a check (V001–V006)
- [x] Every OVHcloud claim cites `kb/` or evidence, or is marked UNVERIFIED
- [x] Open owner points carry a default (four, spec *Clarifications*)
- [x] Out-of-scope items each name a trigger

## Tasks
- [x] Every offline Verify names a Task target the entry admits (`task <target> [-- <path>]`)
- [x] Every behaviour story lists its test tasks first, with given/when/then cases, the defect each
  exposes, and fixed signatures in data-model.md
- [x] Every live task says it is run by the coordinator under D118, from a reviewed SHA on `main`
- [x] Every resource a live task creates is findable from the run id (names and tags carry the run
  id; the nameless alert through the pre-apply baseline, FR-006)
- [x] Every file a check reads is in its registry scope or inputs (new probe root and
  `modules/cloud-project` added to `test:live-lane` by T010; the fixture is under `test:stacks`)
- [x] Live-window guards in the tasks: voucher ceiling, 0 € card, 20 € stop, retained list before the
  final chain, 2026-10-23 for verification and cleanup only, no run whose recovery crosses 23:00
