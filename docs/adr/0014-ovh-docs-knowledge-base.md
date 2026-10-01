# ADR-0014: OVHcloud docs knowledge base
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0013, ADR-0015

## Context
The operator wants OVHcloud's docs as a knowledge base. `github.com/ovh/docs` is the primary source:
maintained (HEAD 2026-09-01 at time of research), **CC BY-NC-SA 4.0** (attribution, non-commercial,
share-alike), 2.2 GB working tree of which ~1.3 GB images; ~19,383 Markdown files (~192 MB) in 15
locales, en-gb ≈ 1,563 files; layout `pages/<domain>/<product>/<guide>/guide.<locale>.md` plus
`meta.yaml`. It already contains OVH's landing-zone guides under
`pages/public_cloud/public_cloud_cross_functional/` (what is a landing zone, migration, securing and
structuring projects, delegating projects, 3-AZ reference architecture). help.ovhcloud.com mirrors the
same content; scraping it adds terms-of-service questions and no new content.

## Options considered
Scrape help.ovhcloud.com; clone `ovh/docs` and commit it; clone locally, gitignored, and cite;
vendor excerpts into our docs.

## Decision (proposed)
- **Local, gitignored, pinned:** `task kb:fetch` sparse/shallow-clones `ovh/docs` into `knowledge-base/`
  (en-gb only, `meta.yaml` included), records the commit SHA in `knowledge-base/SOURCE.md`, strips
  navigation/iframe blocks, and builds a local search index. Not committed, not shipped, not in releases.
- **Cite, don't copy:** project docs link to the canonical OVHcloud URL and name the guide; any quoted
  text is short, attributed, and licence-compliant. The licence is non-commercial/share-alike, so
  redistributing derived text inside this project's published artifacts is **not** assumed to be allowed;
  that needs a legal decision (ADR-0015).
- **Use:** research and review agents consult the index; design claims about OVH behaviour must cite a
  guide path + commit SHA or the live API schema (`api.ovh.com/console`), or be marked UNVERIFIED.
- **Refresh:** a scheduled task diffs the pinned SHA against upstream and lists changed guides relevant
  to our pillars.
- **Optional:** expose the index to agents via a local MCP server (not a project deliverable in v1).
- API schema and provider docs are separate sources, fetched the same way.

## Consequences
- Contributors need ~200 MB (en-gb Markdown only) to use the knowledge base; the task is optional for
  normal development.
- Evidence trail for every OVH claim.

## Counterpoints
- Non-commercial licence may conflict with commercial adopters of the accelerator only if we embed text;
  citing avoids it. If legal review later allows embedding, revisit.
- A clone ages; the pinned SHA and refresh task make staleness visible.

## Verification
- Confirm the licence file and any per-directory exceptions; measure an en-gb-only sparse clone.

## Review log
_(empty)_
