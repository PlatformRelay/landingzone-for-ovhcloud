# ADR-0012: Compliance profiles
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0001, ADR-0006

## Context
OCI ships CIS-aligned landing-zone modes; no EU provider ships compliance mapping as code for
SecNumCloud, EUCS or NIS2/DORA (market gap, research 2026-10-01, absence of evidence only). OVHcloud's
certifications are **product- and region-scoped**: HDS covers Public Cloud products; SecNumCloud is
qualified for Hosted Private Cloud, **not** standard Public Cloud (one secondary source claimed a new
SNC date in 2026; unverified). Over-claiming compliance is a legal and reputational risk.

## Options considered
No profiles; a single hardened default; **switchable profiles** with per-control mapping.

## Decision (proposed)
- A compliance profile is a value of the golden-path profile's `compliance` field (ADR-0016):
  `none` (default) or `hds-aligned` in v1; a CIS-style `hardened` set later. Each is a data file in
  `policies/compliance/` containing (a) the set of guardrail ids it turns on (ADR-0006) and (b) a
  **region and product allowlist**. The allowlist is enforced **at schema-validation time** (a tenant
  requesting a non-listed product or region fails before plan), by `precondition`s in the stages, and
  by the scanner. A nightly test diffs the allowlist against the live capabilities API so the data
  cannot rot silently.
- Wording rule enforced by a docs linter: the words **"compliant"** and **"certified"** are banned in
  product docs; permitted: "aligned with", "supports controls X, Y". Each profile page has three
  columns per control: *implemented in code*, *detective only*, *not covered / customer responsibility*.
- Claims are tied to product and region, using the allowlist the profile references; the
  scanner (ADR-0006) flags resources outside the claimed product/region set.
- No profile ships before its control mapping is reviewed by someone with the relevant expertise; v1
  ships `none` and `hds-aligned` only. SecNumCloud is a documented non-goal for v1 (Hosted Private
  Cloud / SNC platform is a different control plane whose qualification scope moves faster than an
  open-source repo can track; one secondary source claims an SNC Cloud Platform qualification dated
  2026-09-01, UNVERIFIED).

## Consequences
- Differentiator without legal exposure; a visible "what we don't cover" list.
- Maintenance when OVHcloud's certification scope changes: a quarterly review task.

## Counterpoints
- "Aligned" can still be read as a promise; the three-column table and disclaimer are the mitigation.
- Maintainer lacks formal compliance qualification; hence v1 limited to non-framework profiles.

## Verification
- Read OVHcloud's current certification pages per product and region; record the scope matrix with
  dates and URLs.

## Review log
- 2026-10-01 revision: compliance as a profile field with region/product allowlists validated at
  schema time; SecNumCloud non-goal stated. Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
