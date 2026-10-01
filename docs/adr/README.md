# Architecture decision records

Every non-trivial decision is recorded here before code depends on it. Format: MADR-lite.
An ADR is **Proposed** until it has survived independent review (see below), then **Accepted**.
A decision that is overruled keeps its counterpoints in the record — the reasoning outlives the vote.

## Index

| ADR | Title | Status |
|---|---|---|
| [0001](0001-scope-positioning-and-unofficial-status.md) | Scope, positioning and unofficial status | Proposed |
| [0002](0002-repository-structure.md) | Repository structure (monorepo) | Proposed |
| [0003](0003-layered-taxonomy-and-module-naming.md) | Layered taxonomy and module naming | Proposed |
| [0004](0004-staged-roots-and-output-contracts.md) | Staged roots, separate state, typed output contracts | Proposed |
| [0005](0005-declarative-tenant-model-and-assent.md) | Declarative tenant model and `assent` as the self-service gate | Proposed |
| [0006](0006-guardrails-without-org-level-policy.md) | Guardrails without organisation-level policy | Proposed |
| [0007](0007-pipeline-portability.md) | Pipeline portability: task contract, generated TACO config | Proposed |
| [0008](0008-testing-strategy.md) | Testing strategy | Proposed |
| [0009](0009-state-backend-bootstrap-and-credentials.md) | State backend, bootstrap and credentials | Proposed |
| [0010](0010-versioning-release-and-distribution.md) | Versioning, release and distribution | Proposed |
| [0011](0011-provider-strategy-and-opentofu-first.md) | Provider strategy and OpenTofu-first | Proposed |
| [0012](0012-compliance-profiles.md) | Compliance profiles | Proposed |
| [0013](0013-documentation-strategy.md) | Documentation strategy | Proposed |
| [0014](0014-ovh-docs-knowledge-base.md) | OVHcloud docs knowledge base | Proposed |
| [0015](0015-licence-and-project-name.md) | Licence and project name | Proposed (needs operator) |
| [0016](0016-golden-paths-as-profiles.md) | Golden paths as profiles over one composition graph | Proposed |
| [0017](0017-runtime-families-and-output-contract.md) | Runtime families and the output-only contract (multiple base stacks) | Proposed |
| [0018](0018-identity-planes-roles-and-providers.md) | Identity model: three planes, a role catalogue, pluggable providers | Proposed |
| [0019](0019-agent-experience-sensors-and-guides.md) | Agent experience: sensors and guides for implementing agents | Proposed |

Small patterns, guidelines, tools and techniques live in [`docs/reference/patterns-catalogue.md`](../reference/patterns-catalogue.md).

Revision note: ADRs 0001–0004, 0006–0009 and 0011–0014 were revised on 2026-10-01 after an
independent second brainstorm round (three blind designs merged with round 1); each carries the
revision in its review log. The merge record is kept outside the repo in the maintainers' harness.

## Review protocol

1. Author drafts in this directory with status `Proposed`.
2. At least two independent review rounds, each in a fresh session or a different model family, each
   reading only the files — never another reviewer's output in round one.
3. Findings are recorded in the ADR's *Review log* section with the reviewer, date, and disposition
   (accepted / rejected + reason).
4. A third round is an **operator walkthrough**: a fresh session and the operator walk through first
   setup, a failed apply, a compromised credential, lost state, an upgrade and a retirement using
   only the documented design; gaps become edits.
5. Status moves to `Accepted` only on the operator's say-so.

## Template

```markdown
# ADR-NNNN: Title
- Status: Proposed | Accepted | Superseded by ADR-XXXX
- Date: YYYY-MM-DD
- Depends on / related: ADR-…

## Context
## Options considered
## Decision
## Consequences
## Counterpoints (kept even if overruled)
## Verification (how we will know / spikes)
## Review log
```
