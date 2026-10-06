# Initial project phases
Planning baseline: 116405b · 2026-10-01. All specs are **draft**; no cloud work has run.
ADR-0002 is Accepted; other ADRs retain their recorded status. This planning set implements no landing-zone code.
The [product direction](../docs/explanation/product-direction.md) preserves the reference-baseline
ambition, useful differentiators and learning through bounded experiments.
The operator selected scalar naming with shared context and handwritten independent projections;
later naming consumers still need module-split diagnosis and implementation evidence.

| Spec | Outcome | Gate and independent dependencies |
| --- | --- | --- |
| [001-offline-foundation](001-offline-foundation/spec.md) | Pinned offline checks, reporting, naming/labels and traceability | T001–T009 remain minimum safety; T023 follows their evidenced completion. Naming, forge adapters and later checks are eligible when their own dependencies and decisions are satisfied. Structure, constitution and scaffold dispositions are confirmed. Full exit still needs V001–V009 green and docs review, including the deferred guarantees below; no partial increment claims full qualification. |
| [002-transaction-rehearsal](002-transaction-rehearsal/spec.md) | Local two-tenant graph, publication, binding and reservation rehearsals; separate forge qualification | Local exit: V001–V006 green plus V007 pass or explicit blocked record per unavailable forge. Blocked V007 cannot qualify that forge or satisfy SC-003. |
| [003-platform-feasibility](003-platform-feasibility/spec.md) | Protected recovery, isolation, floor, credential, sandbox and locking probes | Docs-only T001 can start independently. Code/probes require 001/T009 plus their admission/reaper and operator gates. Aggregate needs local 002/T008, T012, T016, not live forge adapters. Every refutation blocks its named downstream claim; incomplete observations remain blocked. |
| [004-guided-preconfiguration](004-guided-preconfiguration/spec.md) | Draft friendly terminal journey, defaults, progress and save/resume proposal | Bounded renderer prototype and written journey approved; satisfy task and execution/capture prerequisites before dependent work. D29 defers hardened persistence/export until real profile schemas exist. Synthetic qualification is not real export readiness. |
| [005-first-landing-zone-slice](005-first-landing-zone-slice/spec.md) | First real OpenTofu slice: naming and labels, six stage stacks generated from `deployments.yaml`, one state bucket per tenant, re-runnable account bootstrap and an owner-run live apply→destroy chain | Needs 001/T001–T009 and T023. Live tasks are owner sessions from the owner's main checkout on a reviewed commit; cost guards are hygiene (constitution 1.3.0, D87). Full AgentEx scope is deferred from this slice (D87). Premise refutations block their dependent clauses. |

Feature numbers identify documents, not a complete delivery schedule. The current goal
(2026-10-02, local decision D61) covers all locally implementable portions of specs 001–004
whose dependencies and required decisions are satisfied. Publish independently reviewed
coherent increments, including stacked PRs with parent branches and review order stated;
continue eligible work without waiting for operator review. A PR merges after an
independent review and green applicable checks. Keep 001/T001–T009 minimum safety before
dependent implementation and retain separate live authority and qualification prerequisites.
An unmet dependency blocks that task, not other independent eligible work.
D85 (2026-10-06) postpones spec 002 whole, the spec 004 wizard and the cost machinery of
spec 003 (T002–T010); spec 005 is the first vertical slice.
Local models do not prove cloud isolation or forge enforcement. Platform probes qualify only
named fixtures. A future adoption slice needs its own spec and the decisions for its areas;
no runtime, network or regulated support is implied.

The 2026-10-02 operator priority exception (local decision D60) accepts C2 (C010.5/P4)
as **DEFERRED**, nonblocking technical debt [KI-001](../docs/known-issues/KI-001-ci-source-admission-unqualified.md) for current private
maintainer development, publication, review PR creation and merge. Frozen workflows,
a no-bypass ruleset and the disposable source-admission experiment are later obligations,
not immediate T023 prerequisites. Review PR creation does not establish merge readiness:
tests/evidence, exact pins, read-only authority, T003 isolation, exact-head CI and
independent review remain required. Whole-feature hardened guarantees remain unqualified;
the constitution and ADRs are unchanged.

## Activation gates
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

## Workflow
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

All acceptance commands are **planned**, with creators in tasks.md and evidence initially
`not-run`. Product implementation and cloud qualification remain unrun. The
[pinned upstream reference map](../docs/reference/upstream-reference-map.md) documents scoped
reuse and value without copying or executing upstream implementation.
