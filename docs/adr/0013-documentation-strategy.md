# ADR-0013: Documentation strategy
- Status: Accepted
- Date: 2026-10-06
- Related: ADR-0002, ADR-0008, ADR-0012, ADR-0020, ADR-0027

## Amendment (2026-11-02): a designed SVG may accompany a diagram

Owner decision. Diagrams stay text first. A designed SVG may sit beside a diagram for the docs site.
- **The Mermaid block is the source of truth.** It stays in the page, it is what GitHub shows, and
  it is what review diffs. A diagram without an SVG is complete.
- **The SVG is optional.** It is stored as `docs/assets/diagrams/<name>.svg`. Its design source,
  the diagram-design HTML, sits beside it as `docs/assets/diagrams/src/<name>.html`. The SVG is
  exported from that source, never edited by hand.
- **Same facts, no more.** The SVG shows what the Mermaid block shows: the same nodes, edges and
  labels. Layout, colour and icons may differ; facts may not. A fact the SVG needs goes into the
  Mermaid block and its text equivalent first.
- **The page places it** directly after the Mermaid block, as an image with meaningful alt text,
  and the text equivalent follows both
  ([ADR-0027](0027-documentation-accessibility.md#diagrams-and-images)). Which of the two a reader
  sees on each surface is settled with the site tool; the text equivalent shows on every surface.
- **Accessible and self-contained.** Contrast meets ADR-0027 (WCAG 2.2: 4.5:1 for text, 3:1 for
  lines and shapes that carry meaning), and colour is never the only cue. The committed SVG
  fetches nothing: no script, no stylesheet import, no external URL, no web fonts (Google Fonts
  included). Text uses a system font stack. The design source uses the same stack, so the export
  matches it.
- **Regenerated with the Mermaid block.** A change to a Mermaid block that has an SVG re-exports
  the SVG in the same change, or deletes it.
- **Conceptual only.** Hand-designed diagrams explain the design. A per-installation map is
  generated ([ADR-0020](0020-generated-landing-zone-documentation.md)), never drawn.
- **Checked where it can be.** `docs:check` refuses an SVG a page shows that references anything
  external (`DIAGRAM_EXTERNAL_REF`), a missing one (`LINK_BROKEN`), an image without alt text and
  a diagram or image without a text equivalent after it (ADR-0027). Same facts, contrast and
  regeneration stay with review until the site build can check them.

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
  A designed SVG may accompany a diagram, under the 2026-11-02 amendment above.
- **Writing rules**: first page states "unofficial" and "what OVHcloud can't do", then "Which golden
  path am I?" (a decision tree) and the **negative paths** page (ADR-0016); every page carries
  front-matter `verified_against: { tofu, ovh }` compared with `mise.toml` — pages more than two
  minor versions behind fail CI; every page also carries `references:` (each repository file it
  describes, as `path` plus that file's full git blob id) and `last_verified:` (the commit the reviewer
  checked against, informational only), or `references: []` when it describes no file; a glossary; factual positioning of benefits with implementation
  and verification status; the writing-quality lint (Vale or
  similar) runs in CI.
- **Anti-rot gates** (all in CI): generated reference must be current (`terraform-docs --output-check`);
  every code block is pulled by include-marker from a tested example; a page is stale when any
  `references` blob id differs from that path's blob at HEAD (`git rev-parse HEAD:<path>`) or the path
  is gone, and a stale page fails CI until its text is re-checked and its ids and `last_verified` are
  updated in the same change — blob ids, not commit ids, are compared because rebase-merge rewrites
  commit ids, and a pull request can therefore re-verify its own pages; external links re-resolved
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
- Changing a referenced file makes every page that cites it fail until someone re-reads it; a page
  that cites too many files becomes noisy, so `references` lists the files the page actually explains.

## Counterpoints
- Strict generated docs can read mechanically; the tutorial and explanation sections carry the prose.
- Designed SVGs (2026-11-02) drift. An exported SVG diffs like a binary: review cannot read it, so a
  stale or embellished SVG passes unnoticed. The check catches external references, not wrong facts.
  Keep them few, and prefer no SVG to a stale one.
- Two sources per designed diagram, the Mermaid block and the HTML source, means two edits per change.
  The Mermaid block wins any disagreement; the SVG is regenerated or deleted. If the pairs keep
  drifting, generate the SVG from the Mermaid block instead and drop the HTML source.
- Until the site tool decides which surface shows what, a page with both shows the diagram twice on
  GitHub.
- A freshness stamp creates upkeep. Two stamps, never one: `example_tested_at` is updated by
  automation when the example's tests pass; `claim_verified_at` (IAM, regions, compliance, security
  prose) is updated only by a human review with the evidence and scope recorded, and has an owner and
  a deadline. Automation updates test evidence; it never certifies prose. External link outages create
  bounded, deduplicated findings and do not block unrelated fixes.

## Verification
- Spike: MkDocs Material vs Starlight on a 20-page sample with versioning and search.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
- 2026-11-02: amended by owner decision: a designed SVG may accompany a Mermaid diagram.
