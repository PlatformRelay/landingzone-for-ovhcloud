# ADR-0013: Documentation strategy
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0002, ADR-0008, ADR-0012

## Context
Requirement: amazing documentation and readability. Prior art: AVM generated references, OCI and IBM
manifests, Diataxis as a structure that separates learning, doing, looking up and understanding.
Docs rot when examples and reference are written by hand.

## Options considered
Hand-written README per module; generated reference only; Diataxis site with generated reference and
executed examples.

## Decision (proposed)
- **Diataxis** under `docs/`: `tutorials/` (guided first landing zone on a sandbox), `how-to/` (bootstrap,
  add a project, rotate credentials, migrate between versions, add a policy), `reference/` (generated:
  module inputs/outputs via terraform-docs, schemas, policies, provider gaps, repo map), `explanation/`
  (why no org hierarchy, guardrail classes, state strategy, prior-art comparison).
- **Generated where possible, with CI drift gates**: module READMEs, policy pages, schema docs, repository
  map, CLI help.
- **Examples are tests**: every snippet in docs is extracted from a tested `examples/` directory
  (ADR-0008 layer 5); no hand-pasted HCL.
- **Diagrams as text** (Mermaid or D2, checked in); a C4 context + container view per blueprint.
- **Writing rules**: first page states "unofficial" and "what OVHcloud can't do"; every page has an
  owner-less "last verified against provider vX.Y" stamp, enforced by a check; a glossary; no marketing
  tone; the writing-quality lint (Vale or similar) runs in CI.
- **Site tool:** MkDocs Material or Starlight — a time-boxed comparison spike picks one; requirements:
  versioned docs, search, offline build, no server.
- ADRs live in `docs/adr/`, linked from the explanation section.
- Docs are published from `main`; versioned snapshots per release train tag.

## Consequences
- Docs build is part of CI; a broken link or stale generated file blocks a PR.
- Writing the explanation pages is real work, scheduled as deliverables, not an afterthought.

## Counterpoints
- Strict generated docs can read mechanically; the tutorial and explanation sections carry the prose.
- A "last verified" stamp creates upkeep; made cheap by a scheduled check that bumps it when examples
  still pass.

## Verification
- Spike: MkDocs Material vs Starlight on a 20-page sample with versioning and search.

## Review log
_(empty)_
