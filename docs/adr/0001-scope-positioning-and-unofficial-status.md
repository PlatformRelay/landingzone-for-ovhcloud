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
No EU provider we found ships an open, tested landing-zone codebase (Scaleway: guidance PDF; STACKIT:
partner-led blueprints; OVHcloud: documentation of a landing-zone approach, no accelerator).
OVHcloud's own docs define five pillars: IAM, networking, security, billing, observability.

## Options considered
- **A. CAF-parity port** — promise root-level policy. Not achievable; would mislead.
- **B. Opinionated landing zone on OVH's real primitives, honest about the gaps** — project factory,
  IAM/naming conventions, networking baseline, observability, pipeline-side guardrails.
- **C. Module library only** — AVM-style, no blueprints.

## Decision (proposed)
**B, delivered in the order of C**: modules first, then components, then stages and profiles (ADR-0003), each
independently releasable, so the library hedge is never lost.

- "Landing zone" is defined by OVHcloud's five pillars; every golden path documents how it covers each.
- **Principle (round 2): opinionated at the seams, free in the middle.** The project fixes identity,
  state, pipeline, naming, guardrails and audit; it does not dictate what runs in a project. Several
  **golden paths** (ADR-0016) are supported from one codebase; several base stacks (ADR-0017) and
  IAM providers (ADR-0018) are first-class.
- **v1 scope:** Public Cloud (projects, quotas, budgets, IAM across the OVH, Keystone and Kubernetes
  planes, vRack/private networking, Managed Kubernetes and OpenStack VM runtimes, managed services,
  Object Storage, logging, alerting). **Reserved interface, partially automated in v1:** hybrid
  attachment of Bare Metal / Hosted Private Cloud over vRack (`hybrid-vrack`). **Out of v1:** anything
  inside vSphere, AI products, SecNumCloud paths.
- **v1.x goal:** brownfield adoption — a discovery tool that emits the tenant model plus `import`
  blocks for an existing account, and a tenant "exit kit" (export as `import` blocks) as the
  anti-lock-in guardrail. Early spike, because greenfield OVH accounts are rare.
- **Non-goals:** replacing the OVHcloud Manager; claiming certification (ADR-0012); parity with other
  clouds; a CLI product or portal in v1.
- **Unofficial status** appears in: README first block, docs landing page, every profile page, the
  tenant-repo template, release notes. Wording: independent community project, not endorsed or
  supported by OVHcloud, trademarks belong to their owners.
- The docs open with a "What OVHcloud can't do (and what we do instead)" page, before any tutorial,
  followed by "Which golden path am I?".

## Consequences
- Honest positioning costs some marketing appeal; avoids support burden from false expectations.
- Guardrails are weaker than on hyperscalers (preventive only in the pipeline) — see ADR-0006.
- If OVHcloud ships org-level policy, ADR-0006 and ADR-0012 get superseded, not the project.

## Counterpoints
- OVHcloud may publish its own accelerator, making this redundant. Mitigation: unofficial status is
  explicit; interface decisions (ADR-0005) are portable.
- "Landing zone" may over-promise for a platform without a hierarchy. Alternative name: "platform
  baseline". Kept as an open question for ADR-0015.

## Verification
- Spike: confirm no preventive org-wide deny exists (read IAM policy docs end to end, test `deny` +
  tag conditions across two projects).

## Review log
- 2026-10-01 revision: golden-path principle, multi-stack and multi-IAM scope, hybrid reserved
  interface, brownfield goal. Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
