# Initial project phases
Planning baseline: 116405b · 2026-10-01. All specs are **draft**; no cloud work has run.
The design remains in Proposed ADRs. This planning run implements no landing-zone code.

| Order | Spec | Outcome | Exit gate |
| --- | --- | --- | --- |
| 1 | [001-offline-foundation](001-offline-foundation/spec.md) | Pinned offline checks, honest reporting, naming/labels and traceability | V001–V008 green; docs review complete |
| 2 | [002-transaction-rehearsal](002-transaction-rehearsal/spec.md) | Two-tenant artefact graph and self-service authority rehearsals; both forge contracts | V001–V007 green, including independent race cases |
| 3 | [003-platform-feasibility](003-platform-feasibility/spec.md) | Protected probes of recovery, isolation, deny floor, credentials, sandbox failure and locking | V001–V008 green or unsupported routes explicitly block the next slice |

This is a risk-reduction sequence, not a release promise. Phase 2's local models do not
prove cloud isolation or forge enforcement. Phase 3 qualifies only the named fixtures.
A first adopt-existing-project vertical slice gets its own spec after these exits; it
must include ADR-0019's remaining generators, evaluation drill, consumers, documentation
renderer and readiness checks. No runtime, network or regulated support is implied.
Before that slice: operator ADR ratification; network/resilience, cost/sandbox,
observability/incident-response and accessibility decisions for the areas being built.

## Workflow
Spec Kit was initialized with installed Specify CLI 1.0.5.dev0, Bash and generic command
files under `.specify/commands/`; no global installation or settings were changed.
The slash-command documents are executed by the authoring assistant, not shell programs.
Use `/speckit.plan`, `/speckit.tasks` (plural), then `/speckit.analyze` per feature.
Select a feature explicitly; branch names are not relied upon by this CLI version:

```sh
SPECIFY_FEATURE_DIRECTORY=specs/001-offline-foundation .specify/scripts/bash/setup-plan.sh --json
SPECIFY_FEATURE_DIRECTORY=specs/001-offline-foundation .specify/scripts/bash/setup-tasks.sh --json
SPECIFY_FEATURE_DIRECTORY=specs/001-offline-foundation .specify/scripts/bash/check-prerequisites.sh --json --require-spec --require-tasks --include-tasks
```

Replace the feature directory for phases 2 or 3. Setup scripts only prepare paths and
templates; their success is not a plan, review or acceptance result. Core templates stay
upstream; project overrides and the constitution remove the optional-test default.
Per spec: spec.md → research.md → plan.md + data-model.md + contracts/ + quickstart.md
→ independent adversarial review → tasks.md → independent technical review → analysis.
Reviews and dispositions are in `reviews/`; commands in acceptance tables are **planned**,
and implementation evidence starts `not-run`. Docs-only tasks have no mandatory tests.

## Current observations
The workstation has OpenTofu 1.10.3, Terramate 0.17.1 and Task 3.53.1 (version commands,
2026-10-01). It does not meet the proposed OpenTofu 1.13.0 / Terramate 0.17.3 pins.
Do not treat these installed versions as qualification evidence. No cloud credentials,
account probes or purchases are part of this planning run.

## Planning review results
- [Adversarial plan re-review](reviews/plan-adversarial-delta.md): CLEAN after the
  original two critical gaps and launcher warning were corrected.
- [Technical task re-review](reviews/tasks-tech-review-delta.md): APPROVE after concrete
  L0 checks were assigned and the offline/forge adapter dependency was separated.
- [Spec Kit analysis](reviews/speckit-analysis.md): all 35 requirements map to checks
  and tasks; 66 tasks, 62 with predefined verification and four docs-only exemptions;
  23 acceptance checks and no dependency cycle.
- [Dispositions](reviews/dispositions.md) retain the original findings and their fixes.

These results approve the planning lane only. Specs remain draft, all product acceptance
is not-run, and live experiments retain their explicit setup and authorization gates.
