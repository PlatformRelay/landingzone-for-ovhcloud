# Architecture decision records

Every non-trivial decision is recorded here before code depends on it. Format: MADR-lite.
An ADR remains **Proposed** through independent review; only the operator's explicit
acceptance makes it **Accepted** (see the review protocol below).
A decision that is overruled keeps its counterpoints in the record — the reasoning outlives the vote.

**Until the repository is public, readability beats history**: ADRs are amended or deleted in place,
review findings are applied rather than logged in detail, and the merge records of each review round
live in the maintainers' harness. Historisation (superseded-by links, dated revision notes) starts
with the first public release.

## Index

| ADR | Title | Status |
|---|---|---|
| [0001](0001-scope-positioning-and-unofficial-status.md) | Scope, positioning and unofficial status | Proposed |
| [0002](0002-repository-structure.md) | Repository structure (monorepo) | Accepted |
| [0003](0003-layered-taxonomy-and-module-naming.md) | Layered taxonomy, naming and labelling | Accepted |
| [0004](0004-staged-roots-and-output-contracts.md) | Stacks, one state each, and outputs passed as files | Proposed |
| [0005](0005-declarative-tenant-model-and-assent.md) | Declarative tenant model and `assent` as the self-service gate | Accepted |
| [0006](0006-guardrails-without-org-level-policy.md) | Guardrails without organisation-level policy | Accepted |
| [0007](0007-pipeline-portability.md) | Pipeline portability and instance orchestration with Terramate | Accepted |
| [0008](0008-testing-strategy.md) | Testing strategy | Accepted |
| [0009](0009-state-backend-bootstrap-and-credentials.md) | State backend, bootstrap and credentials | Accepted |
| [0010](0010-versioning-release-and-distribution.md) | Versioning, release and distribution | Accepted |
| [0011](0011-provider-strategy-and-opentofu-first.md) | Provider strategy and OpenTofu-first | Accepted |
| [0012](0012-compliance-profiles.md) | Compliance profiles | Accepted |
| [0013](0013-documentation-strategy.md) | Documentation strategy | Accepted |
| 0014 | OVHcloud docs knowledge base | Withdrawn: maintainers' local tooling, not a product decision |
| [0015](0015-licence-and-project-name.md) | Licence and project name | Accepted |
| [0016](0016-golden-paths-as-profiles.md) | Golden paths as profiles over one composition graph | Accepted |
| [0017](0017-runtime-families-and-output-contract.md) | Runtime families and the output-only contract (multiple base stacks) | Accepted |
| [0018](0018-identity-planes-roles-and-providers.md) | Identity model: three planes, a role catalogue, pluggable providers | Accepted |
| [0019](0019-agent-experience-sensors-and-guides.md) | Agent experience: sensors and guides for implementing agents | Accepted |
| [0020](0020-generated-landing-zone-documentation.md) | Generated landing-zone documentation: system map, accounts and permissions with reasons | Accepted |
| [0021](0021-threat-model-authorisation-boundaries-and-supply-chain.md) | Threat model, authorisation boundaries and supply chain | Accepted |
| [0022](0022-support-release-upgrade-and-succession-policy.md) | Support, release, upgrade and succession policy | Accepted |
| [0023](0023-guided-repository-preconfiguration.md) | Friendly, resumable repository preconfiguration | Accepted |
| [0024](0024-cost-and-sandbox-operations.md) | Cost and sandbox operations | Accepted |

ADR-0024 records the proposed operating contract; documentation-task closure does not
approve its open numerical/setup choices or authorize live implementation.

Still to be written: network and resilience architecture, observability
and incident response, documentation accessibility.

Small patterns, guidelines, tools and techniques live in [`docs/reference/patterns-catalogue.md`](../reference/patterns-catalogue.md).


## Review protocol

1. Author drafts in this directory with status `Proposed`.
2. At least two independent review rounds, each in a fresh session or a different model family, each
   reading the files plus the current decision log and open operator decisions — never
   another reviewer's output in round one. State the commissioning and scope.
3. Detailed findings/dispositions stay in local-only review records. The ADR *Review log*
   keeps a date and concise disposition without authoring attribution.
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
