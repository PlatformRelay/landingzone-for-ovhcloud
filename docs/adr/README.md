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
| [0005](0005-yaml-project-factory.md) | YAML project factory as the primary interface | Proposed |
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

## Review protocol

1. Author drafts in this directory with status `Proposed`.
2. At least two independent review rounds, each in a fresh session or a different model family, each
   reading only the files — never another reviewer's output in round one.
3. Findings are recorded in the ADR's *Review log* section with the reviewer, date, and disposition
   (accepted / rejected + reason).
4. Status moves to `Accepted` only on the operator's say-so.

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
