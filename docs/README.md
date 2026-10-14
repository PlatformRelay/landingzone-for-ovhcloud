---
references: []
last_verified: 7406a5791afb1ea7049f63c3cae03583d3a3c3c0
---

# Documentation

Every page under `docs/`, sorted by what you need from it. Words you do not know are in the
[glossary](reference/glossary.md). The project's front page is the repository's `README.md`.

## Start here

- **New to the project?** Read the [architecture overview](explanation/architecture-overview.md),
  then ADR [0001](adr/0001-scope-positioning-and-unofficial-status.md) (scope) and
  [0004](adr/0004-staged-roots-and-output-contracts.md) (stacks and state). The README shows how
  to run the checks offline in about five minutes.
- **Running the slice against an account?** Only the maintainer does this today. Follow the
  [runbook](how-to/run-the-first-slice.md), and read
  [ADR-0009](adr/0009-state-backend-bootstrap-and-credentials.md) for state and credentials first.
- **Contributing?** Read the [contributing guide](../.github/CONTRIBUTING.md) for how work is
  picked, checked and merged, [`AGENTS.md`](../AGENTS.md) at the repository root for the
  working rules, and the [repository map](reference/repository-map.md) to find your way.

## Tutorials (learn by doing)

None yet. A tutorial comes when someone other than the maintainer can run a slice.

## How-to guides (do one task)

- [Run the first slice on an OVHcloud account](how-to/run-the-first-slice.md) — bootstrap,
  apply and destroy the stacks, read a run's record, clean up. Maintainer only.

## Reference (look things up)

- [Glossary](reference/glossary.md) — the project's terms and id families (Tnnn, Vnnn, KD-n …).
- [Repository map](reference/repository-map.md) — every top-level entry, with module, component,
  stage and stack counts.
- [Verification status](reference/verification-status.md) — what the first slice has and has not
  shown on OVHcloud, its known deviations (KD-n) and what it leaves out.
- [Patterns catalogue](reference/patterns-catalogue.md) — the small rules (LZ-xxx-nnn) and what
  enforces each.
- [Upstream reference map](reference/upstream-reference-map.md) — what OVHcloud's
  public-cloud-examples offers this project.

## Explanation (understand why)

- [Architecture overview](explanation/architecture-overview.md) — the four layers, the stage
  order and authorities, offline versus live, where state lives.
- [Product direction](explanation/product-direction.md) — what the project aims to be and what is
  not delivered yet.
- [Naming and labelling design](explanation/naming-and-labelling-design.md) — how names and
  labels are built, and the options that were compared.
- [Guided setup walkthrough](explanation/guided-setup-walkthrough.md) — a design walkthrough of
  the planned setup helper; nothing to run yet.

## Decisions

The [ADR index](adr/README.md) lists every architecture decision. Read these five first: 0001
(scope), 0002 (repository structure), 0004 (stacks and state), 0008 (testing) and 0009 (state
and credentials).

## Known issues

The [known issues](known-issues/README.md) page lists verified findings and accepted limitations
(KI-nnn). The design deviations of the first slice (KD-n) are in the
[verification status](reference/verification-status.md#known-deviations) and, as the source of
record, in [spec 005](../specs/005-first-landing-zone-slice/spec.md#known-deviations).
