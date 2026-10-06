# ADR-0012: Compliance profiles
- Status: Accepted
- Date: 2026-10-06
- Related: ADR-0001, ADR-0006

## Context
OCI ships CIS-aligned landing-zone modes; no EU provider ships compliance mapping as code for
SecNumCloud, EUCS or NIS2/DORA (market gap, research 2026-10-01, absence of evidence only). OVHcloud's
certifications are **product- and region-scoped**: HDS covers Public Cloud products; SecNumCloud is
qualified for Hosted Private Cloud, Bare Metal Pod and, since 2026-09-01, the separate SNC Cloud
Platform — **not** standard Public Cloud projects (OVH newsroom, verified). Over-claiming compliance
is a legal and reputational risk.

## Options considered
No profiles; a single hardened default; **switchable profiles** with per-control mapping.

## Decision
- A compliance profile is a value of the golden-path profile's `compliance` field (ADR-0016):
  `none` (default) or `hds-aligned` in v1; a CIS-style `hardened` set later. Each is a data file in
  `policies/compliance/` containing (a) the set of guardrail ids it turns on (ADR-0006) and (b) a
  **region and product allowlist**. The allowlist is enforced **at schema-validation time** (a tenant
  requesting a non-listed product or region fails before plan), by `precondition`s in the stages, and
  by the scanner. **Availability and certification scope are separate evidence**: a nightly diff
  against the live capabilities API creates review findings when availability changes; only dated
  primary certification and contractual evidence, reviewed by a qualified person, can alter the
  compliance allowlist. Unresolved required customer actions block profile readiness.
- Wording rule enforced by a docs linter: the words **"compliant"** and **"certified"** are banned in
  product docs; permitted: "aligned with", "supports controls X, Y". Each profile page has three
  columns per control: *implemented in code*, *detective only*, *not covered / customer responsibility*.
- Claims are tied to product and region, using the allowlist the profile references; the
  scanner (ADR-0006) flags resources outside the claimed product/region set.
- No profile ships before its control mapping is reviewed by someone with the relevant expertise; v1
  always ships `none`; `hds-aligned` ships only after its evidence and reviewer gates pass. A
  framework-free **hardening bundle** (the guardrail ids a careful operator wants regardless of any
  framework) may ship earlier under its own name. SecNumCloud is a documented non-goal for v1 (Hosted Private
  Cloud / SNC platform is a different control plane whose qualification scope moves faster than an
  open-source repo can track). Verified 2026-10-01 from OVHcloud's newsroom: SecNumCloud
  qualification for the **SNC Cloud Platform** (France) was announced on 2026-09-01, the third after
  Bare Metal Pod and VMware on OVHcloud; it does not cover ordinary Public Cloud projects.
- `hds-aligned` states the customer-side prerequisites OVH documents for HDS (contractual activation
  and the support-tier requirement) as `pending_actions`; a Terraform apply never implies HDS scope.

## Consequences
- A differentiator with reduced, not zero, legal exposure; a visible "what we don't cover" list.
- Maintenance when OVHcloud's certification scope changes: a quarterly review task.

## Counterpoints
- "Aligned" can still be read as a promise; the three-column table and disclaimer are the mitigation.
- Maintainer lacks formal compliance qualification; hence any framework-named profile waits for a
  qualified reviewer, and the hardening bundle carries no framework name.

## Verification
- Read OVHcloud's current certification pages per product and region; record the scope matrix with
  dates and URLs.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
