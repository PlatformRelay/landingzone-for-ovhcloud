# ADR-0022: Support, release, upgrade and succession policy
- Status: Accepted
- Date: 2026-10-06
- Related: ADR-0010, ADR-0011, ADR-0012, ADR-0016, ADR-0019

## Context
One human maintainer plus agents cannot support every combination the schemas can express (the
profile dimensions alone admit 384 raw tuples before features and regions). Per-module tags cannot
describe a deployable dependency closure, and an "informational" Terraform job can look like a
support promise. A project with a single maintainer
needs a stated support envelope, an immutable release unit, an upgrade promise and a succession plan,
or every one of them is decided in an incident.

## Decision

### Supported combinations are a catalogue, not a schema
`catalog/supported-combinations.yaml` lists every supported tuple (profile, runtime, identity,
network, resilience, compliance, region class, engine, Terramate and provider versions) with a status:
`supported` (evidence from the live layers), `experimental` (qualified in part, named gaps),
`documented-only`. A profile outside the catalogue fails validation. Five named golden paths are
presets over this catalogue; they do not widen it.

### Release unit
The **release train** is the unit people consume: `releases/<version>/manifest.yaml` records the
exact source commit, the component dependency closure (with relative module sources the whole
closure comes from that commit), schemas, policies, toolchain pins, lockfiles and the tested tuples.
Train versions are unique and patchable (`2026.10.0`, `2026.10.1`), never a reused monthly tag.
Module and component tags remain as metadata for library consumers and never imply independently
resolved dependencies (ADR-0010). Pre-1.0: minor versions may break with a migration note; the
contract checker demands a major bump only after 1.0.

### Engine support
OpenTofu is the supported engine. Terraform compatibility is a per-module, per-version qualification
label generated from a passing qualification job; a failing qualification removes the label before
release. No "informational" job exists.

### Upgrade and adoption promise
Every release documents the supported upgrade path from the previous train: state-address changes
with transfer procedures (ADR-0004), schema migrations (`apiVersion` bumps with notes), provider
version windows, and a tested apply-N → upgrade → read/probe data → N+1 exercise (ADR-0008 L9).
Adoption exceptions (imported resources that differ from the convention) are recorded, not forced.
An **exit kit** exports a tenant's resources as `import` blocks so leaving the accelerator needs no
rewrite.

### Security and support policy
A private vulnerability channel (`SECURITY.md`), a response target, and a rule that a security fix
can ship as a train patch outside the normal cadence. Support promise: the catalogue and the
previous-train upgrade path, nothing more; no SLA.

### Succession
Two escrow holders for the recovery package (ADR-0009); a designated backup maintainer with release
and recovery authority documented in `GOVERNANCE.md`; a closure procedure (archive, final train,
exit-kit docs) if the project is abandoned. Agents never hold release authority.

## Consequences
- "Supported" becomes a word with evidence behind it; the honesty page shows the status per tuple.
- Release engineering is a real deliverable: manifest, provenance, notes written for humans.

## Counterpoints (kept even if overruled)
- A catalogue of tuples can feel bureaucratic for `solo` users; the catalogue is generated into one
  readable table, and `solo` is the first supported row.

## Verification
- Spike (ranked 20 by the review): three-module prototype consumed by git ref; leaf change updates
  the dependent release metadata; immutable second hotfix train works; registry publication tested
  separately.

## Review log
- 2026-10-01: created from the round-2 external adversarial review.
