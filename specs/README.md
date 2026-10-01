# Initial project phases
Planning baseline: 116405b · 2026-10-01. All specs are **draft**; no cloud work has run.
ADR-0002 is Accepted; other ADRs retain their recorded status. This planning set implements no landing-zone code.
The [product direction](../docs/explanation/product-direction.md) preserves the reference-baseline
ambition, useful differentiators and learning through bounded experiments.
The operator selected scalar naming with shared context and handwritten independent projections;
later naming consumers still need module-split diagnosis and implementation evidence.

| Spec | Outcome | Gate and independent dependencies |
| --- | --- | --- |
| [001-offline-foundation](001-offline-foundation/spec.md) | Pinned offline checks, reporting, naming/labels and traceability | T001–T009 plus conditional GitHub CI bootstrap T023 authorized with TDD and independent review; merge permission granted subject to actual checks, PR-head CI and mergeability. Structure, constitution and scaffold dispositions are confirmed. Full exit still needs V001–V009 green and docs review; naming, full forge qualification and latency remain outside this subset. |
| [002-transaction-rehearsal](002-transaction-rehearsal/spec.md) | Local two-tenant graph, publication, binding and reservation rehearsals; separate forge qualification | Local exit: V001–V006 green plus V007 pass or explicit blocked record per unavailable forge. Blocked V007 cannot qualify that forge or satisfy SC-003. |
| [003-platform-feasibility](003-platform-feasibility/spec.md) | Protected recovery, isolation, floor, credential, sandbox and locking probes | Docs-only T001 can start independently. Code/probes require 001/T009 plus their admission/reaper and operator gates. Aggregate needs local 002/T008, T012, T016, not live forge adapters. Every refutation blocks its named downstream claim; incomplete observations remain blocked. |
| [004-guided-preconfiguration](004-guided-preconfiguration/spec.md) | Draft friendly terminal journey, defaults, progress and save/resume proposal | Bounded renderer prototype and written journey approved; hardened persistence/export deferred until real profile schemas exist. Its draft needs follow-up alignment; no 004 implementation is part of the 001 foundation run. Synthetic qualification is not real export readiness. |

Feature numbers identify documents, not a complete delivery schedule. The approved next
increment is 001/T001–T009 plus CI bootstrap T023, followed by preparation of a bounded
IAM/state feasibility slice. That sequence does not authorize live actions or every task
in the later drafts. The recorded downstream choices still need their own alignment,
implementation and qualification.
Local models do not prove cloud isolation or forge enforcement. Platform probes qualify only
named fixtures. A future adoption slice needs its own spec and the decisions for its areas;
no runtime, network or regulated support is implied.

## Activation gates
The operator accepted ADR-0002, ratified constitution 1.2.0 and selected committed reusable
workflow scaffolding on 2026-10-01. Create directories with their first real artifact.
Only 001/T001–T009 plus conditional T023 have implementation authorization here; observed premises, task evidence
and independent reviews remain required. No whole-feature acceptance is implied. Live work
additionally needs approved scope, budget, cleanup
health, recovery setup and numerical RunConfig. A newly created project alone supplies no
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
