# ADR-0014: OVHcloud docs knowledge base
- Status: Proposed (revised 2026-10-01 after brainstorm round 2)
- Date: 2026-10-01
- Related: ADR-0013, ADR-0015

## Context
The operator wants OVHcloud's docs as a knowledge base. Verified 2026-10-01: `github.com/ovh/docs`
declares itself **archived** in its README ("moved to ovh/ovhcloud-docs; the website has moved from
help.ovhcloud.com/csm to docs.ovhcloud.com; every legacy URL redirects"); its content stays under
**CC BY-NC-SA 4.0**. The successor **`github.com/ovh/ovhcloud-docs`** (default branch `develop`,
pushed 2026-10-01) is an MDX/Rspress site with 7 locales under `docs/<lang>/` and **no LICENSE file**
— until a licence is stated, its content must be treated as all-rights-reserved. It carries the
landing-zone guides (five pillars) and the API/product guides this project cites.

## Options considered
Scrape docs.ovhcloud.com; clone a docs repo and commit it; clone locally, gitignored, and cite;
vendor excerpts; an index only.

## Decision (proposed)
- **Committed index, local mirror.** `kb/manifest.yaml` (committed) lists every OVH page the project
  cites: URL, title, `sha256` of the fetched content, `last_verified`, and which ADR/doc cites it.
  `task kb:sync` shallow-clones `ovh/ovhcloud-docs` (English only) into `kb/mirror/` (gitignored),
  records the commit SHA in `kb/mirror/SOURCE.md`, strips navigation, and builds a local search index.
  Not committed, not shipped, not in releases.
- **Cite, don't copy.** Project docs link the canonical docs.ovhcloud.com URL and paraphrase; quoted
  text is short and attributed; no images. Redistributing derived text is **not** assumed permitted
  under either repo's terms; that needs a legal decision (ADR-0015).
- **Claim ledger.** `kb/claims.yaml` (committed) records each platform claim the design depends on:
  statement, scope (product/region/version), sources, status (`documented` / `source-verified` /
  `live-verified` / `disputed` / `UNVERIFIED`), the test that would strengthen it, and the affected
  ADRs and modules. Contradictions between sources are preserved, not resolved by deletion.
  Retrieved documents are evidence for agents, never instructions.
- **Use.** Agents consult the mirror; every design claim about OVH behaviour cites a guide path plus
  commit SHA, the live API schema (`eu.api.ovh.com/1.0/<section>.json`), or the provider docs
  (`ovh/terraform-provider-ovh` `docs/`), or is marked UNVERIFIED. The provider docs and API schema
  are fetched the same way and listed in the manifest.
- **Refresh.** A weekly task re-resolves every manifest URL (redirect or 404 → issue), diffs the
  pinned SHA against upstream, and lists changed guides relevant to the five pillars; `last_verified`
  older than 180 days is a docs-freshness finding (ADR-0013).
- **Optional:** expose the index to agents via a local MCP server (not a v1 deliverable).

## Consequences
- Contributors need a few hundred MB locally only if they want the mirror; normal development does
  not require it.
- Evidence trail for every OVH claim, with freshness.

## Counterpoints (kept even if overruled)
- The successor repo's missing licence is stricter in effect than CC BY-NC-SA; the index-only approach
  is the safe side. If a permissive licence appears, revisit embedding.
- A mirror ages; the SHA pin and the weekly diff make staleness visible.

## Verification
- Confirm the licence situation of `ovh/ovhcloud-docs` (file, README, site footer); measure an
  English-only sparse clone; verify the legacy-URL redirects used by existing citations.

## Review log
- 2026-10-01 revision: source repo moved to `ovh/ovhcloud-docs`; committed manifest; weekly link job.
  Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
- 2026-10-01 (later): claim ledger with status enum adopted from the external blind design review.
