# ADR-0027: Documentation accessibility
- Status: Accepted (2026-10-20, D128)
- Date: 2026-10-20
- Related: ADR-0013, ADR-0019, ADR-0020, ADR-0023; spec 006 FR-018, FR-019, E4, E5

## Context
[ADR-0013](0013-documentation-strategy.md) asks for "amazing documentation and readability",
diagrams as text and a writing-quality lint, but says nothing about readers who use a screen
reader, zoom, high contrast or a translation tool, or who read English as a second language.
[ADR-0020](0020-generated-landing-zone-documentation.md) adds generated pages that compliance
reviewers read, with network diagrams and permission tables. The guided setup already promises a
"plain/accessible terminal mode" ([ADR-0023](0023-guided-repository-preconfiguration.md)); the
written docs have no matching rule. The ADR index has listed this record as unwritten since the
first slice ([spec 006, postponed row E5](../../specs/006-harden-first-slice/spec.md#out-of-scope--postponed-with-trigger)).

**Decision drivers**
- Approachable operations: simple entry points, progressive detail, explained changes
  (constitution VII; ADR-0013 *Useful polish*).
- One source for humans and checks: a rule that matters should become a `docs:check` finding,
  added test first (constitution III), never by widening an exclusion.
- Small increments: no site exists yet (the ADR-0013 site-tool spike has not run), so rules that
  need a built site wait for it (constitution VI).
- Generated pages are evidence; a status shown only by colour is lost to some readers and to
  plain-text exports (ADR-0020 *Evidence rules*).

## Options considered
| Option | Assessment |
| --- | --- |
| **A. A writing standard only**, checked in review | Cheap, but review misses the mechanical faults (an image without alt text, a skipped heading level) that a check finds every time. |
| **B. WCAG 2.2 level AA as the reference, a short writing standard, and mechanical rules added to `docs:check` one at a time** | Recommended. Uses a public standard instead of inventing one; each rule lands when its first real case exists. |
| **C. A full automated audit now** (a browser-based checker over the built site) | Premature: there is no site build to audit. Becomes part of option B once the site tool is chosen. |

## Decision
Option B.

### Writing standard
- Lead with the point. Short sentences, one idea per paragraph, active voice.
- Define a term at first use or link the [glossary](../reference/glossary.md); spell out an
  abbreviation once per page.
- Link text says where it goes ("the runbook's cleanup section"), never "here" or "this link".
- Headings in order, no skipped level; one H1 per page.
- Tables have a header row and are for data, not layout.
- Meaning never rests on colour, position or an emoji alone: write the status word (`pass`,
  `fail`, `blocked`, `UNVERIFIED`).
- Code blocks name their language; commands a reader types are separate from their output.

### Diagrams and images
- Diagrams stay text (Mermaid or D2, ADR-0013). Each diagram is followed by a **text equivalent**
  that carries the same facts: a sentence for a simple one, a table for anything with more than a
  few edges. A Mermaid diagram also sets `accTitle` and `accDescr`. That metadata is a separate
  requirement and never stands in for the text equivalent.
- Colour: keep the renderer's default theme. A diagram that sets its own colours meets WCAG 2.2
  contrast (4.5:1 for text, 3:1 for lines and shapes that carry meaning) and still does not use
  colour as the only cue.
- A designed SVG may accompany a Mermaid diagram (ADR-0013 amendment of 2026-11-02). It sits
  directly after the Mermaid block, carries the same facts, and the text equivalent follows both.
- Images need alt text that says what the image shows. A decorative image is marked, not left
  blank: `<img src="…" alt="" role="presentation">`. A Markdown image cannot be marked, so
  `![](…)` always lacks alt text. A decorative image needs no text equivalent.
  Screenshots are avoided where text or a table can say the same.

### Generated documentation (ADR-0020)
The renderer applies the same rules to what it emits, and its tests assert them: each generated
diagram has a generated table beside it (the network map as a table of networks, VLANs,
ranges, connections, gateways and public entry points; the system map as a table of tenants, environments and projects), every status cell
holds a word, and every table has a header row. The generated-at stamp and the source revision
are text on the page.

### How `docs:check` can enforce it later
Each rule is a finding with its own name, a failing fixture first, and the trigger below:
`IMAGE_ALT_MISSING`, `DIAGRAM_ACC_MISSING` (a Mermaid block without `accTitle` and `accDescr`),
`DIAGRAM_TEXT_MISSING` (a diagram with no text equivalent after it; the metadata does not satisfy
it, and whether the text carries the same facts stays with review), `HEADING_SKIP`, `LINK_TEXT_VAGUE` and `TABLE_HEADER_MISSING`.
All six are in force; *What the repository already does* defines them. Prose rules (sentence length,
banned words such as LZ-DOC-004's) belong to the writing lint of ADR-0013, not to `docs:check`.
Contrast and the full WCAG audit run on the built site once it exists.

### What the repository already does
- `docs:check` holds every page under `docs/`, ADRs included, to resolving links and anchors and
  no 32-hex ids. Pages outside the ADRs also need front matter with fresh references and may name
  only existing `task` targets. The version gate, the superseded-ADR scan and the repository map
  come on top (`tools/cmd/lz-check/docs.go:23-41`; spec 006 FR-018, FR-019).
- `HEADING_SKIP`, `LINK_TEXT_VAGUE` and `TABLE_HEADER_MISSING` hold on the same pages, ADRs
  included, outside front matter and fenced code blocks: a heading may rise one level at a time
  and a page has one H1, a link's text is not one of a fixed list of vague words, and a table's
  first row is followed by a delimiter row (`tools/cmd/lz-check/docs.go:471-705`; passing and
  failing fixtures in `tools/cmd/lz-check/docs_test.go:799-884`). Triggered by this ADR's
  acceptance (D128).
- `DIAGRAM_ACC_MISSING`, `DIAGRAM_TEXT_MISSING`, `IMAGE_ALT_MISSING` and, for the designed SVGs
  of the ADR-0013 amendment, `DIAGRAM_EXTERNAL_REF` hold on the same pages, outside front matter
  and blocks nested in another fence. Triggered by the owner decision of 2026-11-02, ahead of the
  second diagram. A line is read on its own, as for the link rules, and an image inside an HTML
  comment is still judged. Only Mermaid blocks are diagrams to these rules: no page uses D2, and
  a D2 block is held to them when the first one arrives.
  - `DIAGRAM_ACC_MISSING`: a block fenced as `mermaid`, in any case, has an `accTitle:` line with
    a value and an `accDescr:` line with a value or a multi-line `accDescr {`. A `%%` comment
    does not count.
  - `DIAGRAM_TEXT_MISSING`: after a Mermaid block, or after a line that holds only images (links
    around them aside), the first non-blank line that is not another image line is prose: a
    paragraph, list, table or quote. A heading, a fence, HTML (a comment included), a thematic
    break, a reference definition or the end of the page is not. An SVG twin can therefore sit
    between a Mermaid block and its text. An image inside a sentence and a decorative image need
    no text after them.
  - `IMAGE_ALT_MISSING`: a Markdown image, inline or by a full, collapsed or shortcut reference
    (a shortcut only when its label is defined), has alt text that is not blank; a code span in
    it is text. An `<img>` has a non-blank `alt` unless it is marked decorative (`alt=""` and
    `role="presentation"`). Attributes are read with their quotes, so a name inside another
    attribute's value does not count.
  - `DIAGRAM_EXTERNAL_REF`: an SVG a page shows, by a Markdown image, a reference or an `<img>`,
    holds no `<script`, no event-handler attribute (`onload=`), no `javascript:` URL, no
    `@import`, and no absolute or protocol-relative URL in an `href`, a `src` or a CSS `url()`,
    after character references are decoded. A reference resolves to its first definition, and a
    symbolic link that stays in the repository is followed. An `<img>` source is held to the
    link rules too, so a missing SVG is `LINK_BROKEN`.

  Code: `tools/cmd/lz-check/docs.go:707-991`; passing and failing fixtures and the SVG cases in
  `tools/cmd/lz-check/docs_test.go:886-1063`. 40 hand mutants of the rules all turn the tests red.
- One diagram exists (`docs/explanation/architecture-overview.md:32-43`). It sets `accTitle` and
  `accDescr`, and is followed by a one-line reading of the arrows and a table of the layers
  (`:45-52`), which together are its text equivalent.
- No page under `docs/` embeds an image.
- A glossary exists (`docs/reference/glossary.md`); terms such as Pn, KD-n and Tnnn are defined
  there.

### Postponed, with triggers
| Part | Trigger |
| --- | --- |
| Writing lint (Vale or similar, ADR-0013; spec 006 E4) | E4's trigger: the tool arrives in the offline entry |
| Contrast and WCAG audit of the built site | The site tool is chosen (ADR-0013 spike) |
| Renderer rules and their tests | `tools/lz-docs` exists (ADR-0020) |

## Consequences
- Authors write one more paragraph per diagram; that paragraph is also what a reader on a slow
  connection or a terminal sees.
- Checks grow one named rule at a time, each with its own fixture, so a red `docs:check` always
  names the fault.
- Pointing at WCAG 2.2 AA sets the bar for the published site; the repository does not claim
  conformance until the site audit passes.

## Counterpoints (kept even if overruled)
- WCAG targets web content; much of this documentation is read as Markdown on a forge. The rules
  above are chosen because they also help there; the site audit only applies to the site.
- An explicit decorative marker is stricter than WCAG, which accepts `alt=""` alone. Without the
  marker a check cannot tell a decorative image from a forgotten alt text.
- `DIAGRAM_TEXT_MISSING` judges position, not content: one sentence satisfies it for a large
  diagram. Review still owns whether the text carries the same facts.
- A text equivalent beside every diagram duplicates content and can drift from it. The mitigation
  is the same as for generated pages: generate both from one source where the renderer exists, and
  keep hand-drawn diagrams few.

## Verification (how we will know / spikes)
- Each `docs:check` rule ships with a passing and a failing fixture under `tests/check/`.
- Spike, with the ADR-0013 site-tool spike: run a browser-based accessibility checker over the
  20-page sample and record what it finds.
- A reader walkthrough: someone using a screen reader follows the
  [runbook](../how-to/run-the-first-slice.md) to the first plan and notes where it fails them.

## Review log
- 2026-10-20: drafted; one independent review round, findings applied.
- 2026-10-20: accepted (D128) after two further independent reviews; their changes applied.
- 2026-11-02: `DIAGRAM_ACC_MISSING`, `DIAGRAM_TEXT_MISSING`, `IMAGE_ALT_MISSING` and
  `DIAGRAM_EXTERNAL_REF` added ahead of their triggers (owner decision on designed SVGs, ADR-0013
  amendment); the decorative-image marker set; the existing diagram given `accTitle` and `accDescr`.
  Two independent reviews; their bypasses (shortcut references, repeated definitions, attribute
  decoys, encoded URLs, linked SVGs, event handlers) closed with a regression row each.
