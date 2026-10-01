# Tasks technical review — focused delta

```text
REVIEW
Verdict: APPROVE
Scope: corrections to the two P2 findings in tasks-tech-review.md
Branch: codex/first-phases
Base: 116405b98960dcc78b3843985a4a49f35afae08a
Date: 2026-10-01
Outstanding findings in this review: none
```

This is a focused follow-up to the independent technical review. The original
report is retained. No plan-adversarial report or other reviewer's rationale was
read. Approval applies to the draft planning/scaffold lane, not to implementation,
cloud execution, deployment authority or platform support claims.

## Finding dispositions

- **L0 task gap — resolved.** Phase 001 FR-007/V006 now explicitly require real
  format, HCL validation, pinned lint and strict data-schema checks. T008 defines
  valid, unformatted, malformed and linter-offence controls and clause sensitivity;
  T009 creates the static runners, `test:static` and `lint`; T016 creates
  `schema:check` with malformed/duplicate/unknown-field and zero-discovery rejection.
  The plan specifies the actual `tofu fmt`, prepared-cache initialization,
  `tofu validate -json` and configured tflint commands. T020 depends on T009 and
  T016; T021 consumes their actual counted observations. Resource scanning is
  explicitly not-applicable to pure naming, and docs-only work retains its
  exemption. See `001-offline-foundation/tasks.md` T008/T009/T016/T020/T021,
  `spec.md` V006, `plan.md` Verification strategy and `contracts/checks.md`.
- **Forge-adapter dependency blocking local MVP — resolved.** Phase 002 T001 now
  closes on the exact Terramate/assent CLI pins and tool-owned offline fixtures;
  unavailable forge adapters are recorded separately and are explicitly not its
  completion gate. T018 permits local adapter/test construction while returning
  blocked for absent real qualification dependencies; T019 and V007 retain the
  actual both-forges requirements. The local roots/graph/transaction dependency
  chain no longer requires an available forge adapter through T001.

The related changes preserve planned-command creator entries, assign success
criteria to their earlier implementing tasks, and clarify that phase-001 task
evidence packets support a separately indexed V-check acceptance packet. They do
not weaken the red/green controls, evidence requirements or live stop gates.

## Fresh checks run

- Working-tree and staged whitespace gates: `git diff --check` and
  `git diff --cached --check` passed.
- All six Bash scaffold scripts passed `bash -n`; all six current JSON metadata
  files parsed successfully.
- All three template resolvers returned content exactly matching the project's
  spec/plan/tasks overrides.
- In a disposable scaffold copy, all three features passed the required
  spec/task prerequisite gate and `setup-tasks.sh --json` selected the project
  override. Removing each required spec and task separately produced the expected
  nonzero missing-file result. No metadata-writing gate ran in the reviewed tree.
- Reparsed all task metadata, unique sequential unchecked task IDs, explicit
  exemptions, requirement/success-criterion acceptance coverage and target creators.
  Totals remain 66 tasks, four docs-only exemptions, 26 FRs, nine SCs and 23
  acceptance checks. All acceptance commands have creator entries: 18 in phase
  001, nine in phase 002 and ten in phase 003, including semicolon-chain commands.
- Reconstructed all local/cross-feature task dependencies: 66 nodes, no missing
  reference and no cycle. Specifically confirmed the added T009/T016 dependencies
  of 001/T020, preserving static/schema results before final latency verification.

## Limits

The new static runners, schemas, fault controls and forge protocols remain future
implementation. Their real behavioral red/green evidence is still owed to the
named tasks; the review did not execute nonexistent targets or count missing
tools as red controls. No cloud or forge mutation, purchase, credential probe,
destructive experiment or outbound message was performed. Existing unverified
platform premises and external setup/authorization gates remain in force.
