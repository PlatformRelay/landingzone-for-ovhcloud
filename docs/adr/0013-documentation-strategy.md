# ADR-0013: Documentation strategy
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0002, ADR-0008, ADR-0012

## Context
Requirement: amazing documentation and readability. Prior art: AVM generated references, OCI and IBM
manifests, Diataxis as a structure that separates learning, doing, looking up and understanding.
Docs rot when examples and reference are written by hand.
The published [OrbitalEdge example](../reference/upstream-reference-map.md) already supplies a
use-case story, architecture, governance, day-1/day-2 and operations guides. Use that progression
as prior art; a worked example alone is not this project's differentiator. Generated installation
facts and tested recovery journeys are the additional work.

## Options considered
Hand-written README per module; generated reference only; Diataxis site with generated reference and
executed examples.

## Decision
- **Useful polish:** documentation is an entry point to the differentiators in ADR-0001. Start with
  a short adoption journey and resolved choices, then reveal contracts, authority and recovery
  detail on demand. Installation diagrams, permission explanations and next actions should be
  understandable without reconstructing the composition graph. Search, navigation and readable
  examples support this; an extra application/portal is not required.
- **Diataxis** under `docs/`: `tutorials/` (guided first landing zone on a sandbox), `how-to/` (bootstrap,
  add a project, rotate credentials, migrate between versions, add a policy), `reference/` (generated:
  module inputs/outputs via terraform-docs, schemas, policies, provider gaps, repo map), `explanation/`
  (why no org hierarchy, guardrail classes, state strategy, prior-art comparison).
- **Generated where possible, with CI drift gates**: module READMEs, policy pages, schema docs, repository
  map, CLI help.
- **Examples are tests**: runnable examples are included from tested `examples/` directories;
  illustrative snippets and expected-output fragments are labelled as such and checked according to
  their kind; no unlabelled hand-pasted HCL.
- **Diagrams as text** (Mermaid or D2, checked in); a C4 context + container view per golden path.
- **Writing rules**: first page states "unofficial" and "what OVHcloud can't do", then "Which golden
  path am I?" (a decision tree) and the **negative paths** page (ADR-0016); every page carries
  front-matter `verified_against: { tofu, ovh }` compared with `mise.toml` — pages more than two
  minor versions behind fail CI; a glossary; factual positioning of benefits with implementation
  and verification status; the writing-quality lint (Vale or
  similar) runs in CI.
- **Anti-rot gates** (all in CI): generated reference must be current (`terraform-docs --output-check`);
  every code block is pulled by include-marker from a tested example; external links re-resolved
  weekly (OVH moved its docs site in 2026, so this is not theoretical); every how-to ends in a `task`
  target that the nightly runs in dry-run or mock mode; an ADR referenced from code that is
  `Superseded` fails lint; the **honesty page** and the enforcement matrix are generated from
  `policies/guardrails.yaml` (ADR-0006).
- **Site tool:** MkDocs Material or Starlight — a time-boxed comparison spike picks one; requirements:
  versioned docs, search, offline build, no server.
- ADRs live in `docs/adr/`, linked from the explanation section.
- Docs are published from `main`; versioned snapshots per release train tag.
- **Docs-only exemption:** prose changes need content review, not invented automated behaviour
  tests. Existing link/format checks can still run. Executable examples, renderers, generators and
  publication/access-control workflows retain predefined tests and evidence under ADR-0008.

## Consequences
- Docs build is part of CI; a broken link or stale generated file blocks a PR.
- Writing the explanation pages is real work, scheduled as deliverables, not an afterthought.

## Counterpoints
- Strict generated docs can read mechanically; the tutorial and explanation sections carry the prose.
- A freshness stamp creates upkeep. Two stamps, never one: `example_tested_at` is updated by
  automation when the example's tests pass; `claim_verified_at` (IAM, regions, compliance, security
  prose) is updated only by a human review with the evidence and scope recorded, and has an owner and
  a deadline. Automation updates test evidence; it never certifies prose. External link outages create
  bounded, deduplicated findings and do not block unrelated fixes.

## Verification
- Spike: MkDocs Material vs Starlight on a 20-page sample with versioning and search.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
