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
- Images need alt text that says what the image shows; a decorative image has empty alt text.
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
it, and whether the text carries the same facts stays with review), `HEADING_SKIP`, `LINK_TEXT_VAGUE` and `TABLE_HEADER_MISSING`. Prose rules (sentence length,
banned words such as LZ-DOC-004's) belong to the writing lint of ADR-0013, not to `docs:check`.
Contrast and the full WCAG audit run on the built site once it exists.

### What the repository already does
- `docs:check` holds every page under `docs/`, ADRs included, to resolving links and anchors and
  no 32-hex ids. Pages outside the ADRs also need front matter with fresh references and may name
  only existing `task` targets. The version gate, the superseded-ADR scan and the repository map
  come on top (`tools/cmd/lz-check/docs.go:23-41`; spec 006 FR-018, FR-019).
- One diagram exists (`docs/explanation/architecture-overview.md:32-41`); it is followed by a
  one-line reading of the arrows and a table of the layers (`:43-50`), which together are its text
  equivalent. It has no `accTitle` or `accDescr` yet.
- No page under `docs/` embeds an image.
- A glossary exists (`docs/reference/glossary.md`); terms such as Pn, KD-n and Tnnn are defined
  there.

### Postponed, with triggers
| Part | Trigger |
| --- | --- |
| `accTitle` and `accDescr` on the existing diagram | The next change to `architecture-overview.md` |
| `DIAGRAM_ACC_MISSING`, `DIAGRAM_TEXT_MISSING` | A second Mermaid diagram under `docs/` |
| `IMAGE_ALT_MISSING` | The first image under `docs/` |
| `HEADING_SKIP`, `LINK_TEXT_VAGUE`, `TABLE_HEADER_MISSING` | A review finds the fault twice, or owner acceptance of this ADR |
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
