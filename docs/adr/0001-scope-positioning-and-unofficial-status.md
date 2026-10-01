# ADR-0001: Scope, positioning and unofficial status
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0006, ADR-0012, ADR-0015

## Context
Azure (CAF/ALZ), Google (FAST, example-foundation), AWS (LZA, Control Tower) and Oracle (OCI Landing
Zones) ship landing zones whose value rests on **organisation-level** primitives: management groups,
service-control policies, org policy. OVHcloud, per our research, has none of these: the account is the
top level, a Public Cloud project is the unit of isolation, billing and quota, and IAM policies are
allow/deny on actions with tags as conditions, but nothing every project inherits automatically.
OVHcloud already publishes deployable network landing-zone examples in
[`ovh/public-cloud-examples`](../reference/upstream-reference-map.md): hub/spoke with multi-vRack
IPsec, mono-vRack LAN transit, and HA firewall adoption in an existing project. At the inspected
revision the repository labels its code demonstration-only; no landing-zone test suite or CI
workflow was found. This is a scoped source observation, not proof of absence across OVHcloud.
The opportunity is a qualified cross-pillar baseline and its operating workflows, building on
existing examples rather than claiming the first OVHcloud landing-zone codebase.
OVHcloud's own docs define five pillars: IAM, networking, security, billing, observability.

## Options considered
- **A. CAF-parity port** — promise root-level policy. Not achievable; would mislead.
- **B. Opinionated landing zone on OVH's real primitives, honest about the gaps** — project factory,
  IAM/naming conventions, networking baseline, observability, pipeline-side guardrails.
- **C. Module library only** — AVM-style, no blueprints.
- **D. Extend the published network examples only** — lowest duplication; suitable for generally
  useful resource fixes, tests and guides. Their network scope and demo operating model do not yet
  supply the proposed tenant/authority/evidence workflows. Reassess a separate accelerator as those
  workflows are exercised; upstream contributions remain a valid outcome.

## Decision
**B, delivered in the order of C**: modules first, then components, then stages and profiles (ADR-0003), each
independently releasable, so the library hedge is never lost.

- "Landing zone" is defined by OVHcloud's five pillars; every golden path documents how it covers each.
- **Principle: opinionated at the seams, free in the middle.** The project fixes identity,
  state, pipeline, naming, guardrails and audit; it does not dictate what runs in a project. Several
  **golden paths** (ADR-0016) are supported from one codebase; several base stacks (ADR-0017) and
  IAM providers (ADR-0018) are first-class.
- **v1 scope:** Public Cloud (projects, quotas, budgets, IAM across the OVH, Keystone and Kubernetes
  planes, vRack/private networking, Managed Kubernetes and OpenStack VM runtimes, managed services,
  Object Storage, logging, alerting). **Reserved interface, partially automated in v1:** hybrid
  attachment of Bare Metal / Hosted Private Cloud over vRack (`hybrid-vrack`). **Out of v1:** anything
  inside vSphere, AI products, SecNumCloud paths.
- **Adoption is v1:** adopting one existing project with an explained no-replacement plan is the
  default first experience and a v1 acceptance gate (ADR-0005). **v1.x:** account-wide discovery that
  emits the tenant model plus `import` blocks, and a tenant "exit kit" (ADR-0022). The prevalence of
  brownfield accounts is an assumption, not a measured fact.
- **Non-goals:** replacing the OVHcloud Manager; claiming certification (ADR-0012); parity with other
  clouds; a general cloud management CLI or portal in v1. The guided repository-preconfiguration
  helper (ADR-0023), requested explicitly by the operator, is in scope.
- **Unofficial status** appears in: README first block, docs landing page, every profile page, the
  tenant-repo template, release notes. Wording: independent community project, not endorsed or
  supported by OVHcloud, trademarks belong to their owners.
- The docs open with a "What OVHcloud can't do (and what we do instead)" page, before any tutorial,
  followed by "Which golden path am I?".
- Inspect published examples before writing equivalent resource code. The
  [pinned reference map](../reference/upstream-reference-map.md) identifies reuse candidates,
  qualification checks and provenance obligations. No entire network topology becomes a default
  solely because it is available; the existing-project path is the smallest adoption reference.

## Consequences
- Honest positioning costs some marketing appeal; avoids support burden from false expectations.
- Guardrail coverage varies by operation and authority: IAM prevents documented actions, topology
  constrains connectivity, pipeline policy gates managed changes, and scans detect the rest. The
  release catalogue (ADR-0022) lists the combinations for which these claims have evidence.
- If OVHcloud ships org-level policy, ADR-0006 and ADR-0012 get superseded, not the project.

## Counterpoints
- Published examples already overlap the network foundation. Rebuilding those recipes alone adds
  little value. Demonstrate safer adoption, reproducible checks and useful workflows; reuse or
  contribute general fixes upstream instead of maintaining duplicates without a reason.
- "Landing zone" may over-promise for a platform without a hierarchy. Alternative name: "platform
  baseline". Kept as an open question for ADR-0015.
- Differentiators add state, authority and recovery work. Extensive testing is essential; stable
  interfaces, progressive detail and visible operational states also bound that complexity.

## Verification
- Spike: confirm no preventive org-wide deny exists (read IAM policy docs end to end, test `deny` +
  tag conditions across two projects).

## Review log
- 2026-10-01: round-2 external adversarial review applied.
