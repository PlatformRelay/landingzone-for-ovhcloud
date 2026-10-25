# Specs: the delivery record

This folder is the delivery record, written with [Spec Kit](https://github.com/github/spec-kit):
one folder per feature with its spec, research, plan, tasks and evidence. To use or understand the
landing zone, start at the [README](../README.md) and the [ADR index](../docs/adr/README.md).

## Specs and their state (2026-10-15)

| Spec | Outcome | State |
| --- | --- | --- |
| [001-offline-foundation](001-offline-foundation/spec.md) | Pinned offline checks, reporting, naming/labels and traceability | Minimum safety T001–T009 is done; the GitHub CI (T023) is closed with gaps (a CI run of that code and registry digest coverage are owed). T010–T021 are postponed by owner gates (D85, D87) or superseded by spec 005; the exit check T022 is open. |
| [002-transaction-rehearsal](002-transaction-rehearsal/spec.md) | Local two-tenant graph, publication, binding and reservation rehearsals; separate forge qualification | Retired 2026-10-14 (D113); no task done. Spec 005 delivered about half of it; each remaining item is postponed with its trigger in [spec 005's out-of-scope table](005-first-landing-zone-slice/spec.md#out-of-scope--postponed-with-trigger). |
| [003-platform-feasibility](003-platform-feasibility/spec.md) | Protected recovery, isolation, floor, credential, sandbox and locking probes | Re-scoped to one project (D113). ADR-0024 written (T001); T002–T008 are superseded by spec 005's run core; T009–T010 stay postponed with OKMS and escrow (D85); T011–T028 are open, live parts as probes on spec 005's runner. Real tenant isolation waits for a second project. |
| [004-guided-preconfiguration](004-guided-preconfiguration/spec.md) | Friendly terminal journey, defaults, progress and save/resume proposal | Amended and held (D113). The written journey is done (T014); D29's schema gate is met by `schemas/deployments.schema.json`; the wizard stays postponed (D85). |
| [005-first-landing-zone-slice](005-first-landing-zone-slice/spec.md) | First real OpenTofu slice: naming and labels, six stage stacks generated from `deployments.yaml`, a scripted account bootstrap, a tested live run core and an owner-run live apply→destroy chain | Every task is closed except the owner session T045 (fresh-account bootstrap). The live chain passed in the sandbox (T049, run `20261013T075309Z-443f`, closed with gaps); the exit record is [T051](005-first-landing-zone-slice/evidence/T051.md). The spec stays draft until T045 runs. |
| [006-harden-first-slice](006-harden-first-slice/spec.md) | Harden and tidy the first slice: the recorded live-lane safety gaps, memory and process limits for the offline entry, runtime preparation in Go, an offline-checked update, release and provenance lane, and the docs that drifted | Draft, specified 2026-10-15; no task done. Every task is offline and needs no owner session; the three owner choices (companion allow-list, update bot, release tooling and signing) are decided (D115). |
| [007-cleanup-first-live-slice](007-cleanup-first-live-slice/spec.md) | Cleanup-first live slice: bounded retry of transient errors on leftover listings with the failed attempts kept, resolved-project aliasing refused (A17), a disposable budget-alert run through the landing zone's module (P10) whose sweep removes an unrecorded alert, a second tenant offline (B5), and a live window that closes clean | 2026-10-25: every task is closed except the owner session T023. Nothing of this spec ran live: T019 (the budget alert through the module, P10) was dropped at its cut-off, so FR-005 is not qualified, and the final chain T022 did not run. The last chain, run `20261020T000733Z-773f` on 2026-10-20, belongs to spec 006 and ran before this spec's stage B fixes. The listing retry, the alias refusal and the probe baseline and sweep are tested offline only. T001: A17 refuses two tenants on one project, so B5 stays offline and narrowed locks (A24) stay deferred. What stays in the account is the approved retained infrastructure of [retained.md](007-cleanup-first-live-slice/evidence/retained.md): the trial project, the two state buckets with their S3 users, the governance IAM objects and the sandbox admin (R1–R20, derived from the code, not yet checked against the account); the trial voucher expired on 2026-10-24. T023 is the owner's: clear three pre-baseline probe runs of spec 003, record the spend and check `retained.md` against the account. |

Feature numbers identify documents, not a delivery schedule.

## Delivery governance

This section records how work is admitted and merged. It is process, not product.

### Gates per spec

| Spec | Gate and independent dependencies |
| --- | --- |
| 001 | T001–T009 remain minimum safety; T023 follows their evidenced completion. Naming, forge adapters and later checks are eligible when their own dependencies and decisions are satisfied. Structure, constitution and scaffold dispositions are confirmed. Full exit still needs V001–V009 green and docs review, including the deferred guarantees below; no partial increment claims full qualification. |
| 002 | Retired (D113): no gate; a fired trigger starts a new spec. |
| 003 | Docs-only T001 is done. D85's scope for spec 003, restated by D113: admission, ledger, reaper and cleanup drills (T002–T008) are superseded by spec 005's run core, which covers cleanup as hygiene, not a gate; escrow and clean recovery (T009–T010) stay postponed with OKMS; T011–T028 are open, every live run an owner session through `task live:probe`. The remaining probes keep their task dependencies and operator gates (see [tasks.md](003-platform-feasibility/tasks.md)). Every refutation blocks its named downstream claim; incomplete observations remain blocked. |
| 004 | Bounded renderer prototype and written journey approved; satisfy task and execution/capture prerequisites before dependent work. D29's real profile schema is `schemas/deployments.schema.json` (D113); the D85 wizard gate stays closed. Synthetic qualification is not real export readiness. |
| 005 | Needs 001/T001–T009 and T023. Live tasks are owner sessions from the owner's dedicated clone on a reviewed commit, admitted by a host guard; the shared sandbox project is a recorded state-isolation deviation (KD-1, D88), so tenant state isolation is not claimed; cost guards are hygiene (constitution 1.3.0, D87). Full AgentEx scope is deferred from this slice (D87). Premise refutations block their dependent clauses. |
| 006 | Needs spec 005's closed tasks and the entry r4-e2. Offline only: no credential, network, OVHcloud call or owner session. Its owner choices are decided (D115, 2026-10-15). The entry limits (FR-013) are not pinned in CI until the runner premise P3 is observed; publication is coordinator work under D89. It is not a vertical slice, so the AgentEx trigger does not fire. |
| 007 | Needs spec 006's live-lane tasks (T022–T026) on `main`. Live runs only by the coordinating session under D118, from a reviewed SHA on `main`, within about 5 € of voucher and 0 € card, never on or after 2026-10-23 except verification and cleanup; the retained-resource list precedes the final chain. Nothing a disposable run created may be accepted as retained. It is the second vertical slice, so it fires the AgentEx trigger (D119); AgentEx is its own offline spec from 2026-10-24. |

### Scope and merge rules

The current goal (2026-10-02, local decision D61) covers all locally implementable portions of
specs 001–004 whose dependencies and required decisions are satisfied. Publish independently
reviewed coherent increments, including stacked PRs with parent branches and review order stated;
continue eligible work without waiting for operator review. A PR merges after an independent
review and green applicable checks. Keep 001/T001–T009 minimum safety before dependent
implementation and retain separate live authority and qualification prerequisites. An unmet
dependency blocks that task, not other independent eligible work.
D85 (2026-10-06) postponed spec 002 whole (retired by D113, 2026-10-14), the spec 004 wizard and the cost machinery of
spec 003; D113 restates the latter as T002–T008 superseded and T009–T010 postponed. Spec 005 is the first vertical slice.
Local models do not prove cloud isolation or forge enforcement. Platform probes qualify only
named fixtures. A future adoption slice needs its own spec and the decisions for its areas;
no runtime, network or regulated support is implied.
The operator selected scalar naming with shared context and handwritten independent projections;
spec 005's stages are its first concrete consumers (D85). Organisation-specific projections remain future work.

The [contributing guide](../.github/CONTRIBUTING.md) explains how a task is picked, checked,
evidenced and closed, and when to stop.

### Deferred CI source admission (KI-001)

The 2026-10-02 operator priority exception (local decision D60) accepts C2 (C010.5/P4)
as **DEFERRED**, nonblocking technical debt [KI-001](../docs/known-issues/KI-001-ci-source-admission-unqualified.md)
for current private maintainer development, publication, review PR creation and merge, with a
reassessment trigger recorded there. Frozen workflows, a no-bypass ruleset and the disposable
source-admission experiment are later obligations, not immediate T023 prerequisites. Review PR
creation does not establish merge readiness: tests/evidence, exact pins, read-only authority, T003
isolation, exact-head CI and independent review remain required. Whole-feature hardened
guarantees remain unqualified; the constitution and ADRs are unchanged.

### Activation gates
The operator accepted ADR-0002, ratified constitution 1.2.0 and selected committed reusable
workflow scaffolding on 2026-10-01. Create directories with their first real artifact.
All eligible portions of specs 001–004 have implementation authorization under D61;
observed premises, task dependencies, required decisions, evidence and independent reviews
remain required. D29's persistence/export deferral is unchanged. No whole-feature acceptance
or merge authority is implied. Live work
additionally needs approved scope and the constitution V prerequisites (dedicated sandbox, scoped
cleanup authority, inventory of created ids, bounded runtime, tested destroy-on-exit and leftover
check); cost guards are hygiene, not gates (constitution 1.3.0). A newly created project alone supplies no
account-scoped IAM/federation authority. Later naming consumers and real profile exports
retain their own implementation and qualification prerequisites.

## Spec Kit workflow
The vendored workflow provides generic command documents under `.specify/commands/`.
These documents describe authoring steps; the scripts prepare paths and templates.
Use `/speckit.plan`, `/speckit.tasks` (plural), then `/speckit.analyze` per feature.
Select a feature explicitly:

```sh
SPECIFY_FEATURE_DIRECTORY=specs/001-offline-foundation .specify/scripts/bash/setup-plan.sh --json
SPECIFY_FEATURE_DIRECTORY=specs/001-offline-foundation .specify/scripts/bash/setup-tasks.sh --json
SPECIFY_FEATURE_DIRECTORY=specs/001-offline-foundation .specify/scripts/bash/check-prerequisites.sh --json --require-spec --require-tasks --include-tasks
```

Replace the feature directory for phases 2, 3 or 4. Script success is not a plan, review
or acceptance result. Core templates stay upstream; project overrides and the constitution
require predefined verification for behavioral tasks. Docs-only tasks have no mandatory tests.
Per spec: spec.md → research.md → plan.md + data-model.md + contracts/ + quickstart.md
→ adversarial review → tasks.md → technical review → cross-artifact analysis.
Each review must also inspect the current decision log and open operator decisions before
judging alignment; internal consistency alone is insufficient. Review scope and commissioning
must be stated, and no planning verdict qualifies unrun product commands.
Review records and dispositions are local-only under `agent-context/inbox/`.

Acceptance commands are defined in each spec before implementation, with their creating task in
tasks.md and evidence `not-run` until that task closes; each task's evidence file records the run.
The [pinned upstream reference map](../docs/reference/upstream-reference-map.md) documents scoped
reuse and value without copying or executing upstream implementation.
