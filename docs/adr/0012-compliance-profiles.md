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
- Profiles are named sets of policy ids (ADR-0006) plus module default overrides:
  `baseline` (default), `hardened`, later `hds-aligned` and a CIS-style set. Profile files live in
  `policies/profiles/` and are data, tested.
- Wording rule enforced by a docs linter: the words **"compliant"** and **"certified"** are banned in
  product docs; permitted: "aligned with", "supports controls X, Y". Each profile page has three
  columns per control: *implemented in code*, *detective only*, *not covered / customer responsibility*.
- Claims are tied to product and region, using a data file the blueprint manifest references; the
  scanner (ADR-0006) flags resources outside the claimed product/region set.
- No profile ships before its control mapping is reviewed by someone with the relevant expertise; v1
  ships `baseline` and `hardened` only.

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
_(empty)_
