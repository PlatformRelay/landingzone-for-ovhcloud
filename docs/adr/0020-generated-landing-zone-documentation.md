# ADR-0020: Generated landing-zone documentation — system map, accounts and permissions with their reasons
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0005, ADR-0006, ADR-0012, ADR-0013, ADR-0018, ADR-0019

## Context
The operator wants the deployed landing zone to **document itself**: a map of the networking and
the project structure, and the artefacts compliance reviewers ask for — the technical accounts,
their permissions, and **why** each account needs each permission — rendered as Markdown and
published (GitHub or GitLab Pages, or any static site). Today such documents are written by hand,
rot within weeks, and cannot be trusted as evidence. The design already holds every input: the tenant
model and profile (desired state, ADR-0005/0016), stage output contracts (actual ids, ADR-0004), the
role catalogue (ADR-0018), the guardrail spec and deployment report (ADR-0006), the identity ledger,
the IPAM ledger (ADR-0017) and the compliance allowlists (ADR-0012). HDS and similar reviews ask for
exactly this: accounts, rights, justification, review dates.

## Options considered
- **A. Hand-written docs per installation** — rots; not evidence.
- **B. Generated Markdown from model + state**, built into a static site per installation, with
  the reasons stored as data next to the permissions.
- **C. A live dashboard service** — a second product with its own identity and hosting.

## Decision (proposed)
Option B. `tools/lz-docs` renders a per-installation documentation set from data only; nothing in it
is typed by hand after setup.

**Inputs** (all already exist or are introduced here):
- desired state: tenant files, profile, identity ledger, IPAM ledger, waivers;
- actual state: stage output contracts (`stage-outputs.json`) and the scanner's inventory
  (read-only API enumeration), never raw state files;
- **permission rationale as data**: every role's action list in `components/identity/roles/<role>.yaml`
  carries, per action family, a `reason` (why the role needs it), a `scope` (what it may touch) and
  `review_every` (an interval); every automation identity in the ledger carries `purpose`, `owner`,
  `rotation` and the stages it may apply. CI fails on an action without a reason.
- the guardrail spec, the deployment report and the compliance allowlist in force.

**Pages rendered** (Diataxis "reference" section of the installation's site):
1. **System map**: domains → tenants → environments → projects, with regions, resilience model and
   profile; Mermaid or D2 diagrams generated from the same data (ADR-0013: diagrams as text).
2. **Network map**: vRacks, VLANs, private networks, subnets, gateways, public entry points, from
   the IPAM ledger and the stage outputs; a desired-versus-actual diff when the scanner disagrees.
3. **Technical accounts and permissions**: one table per account (service accounts, OpenStack
   application credentials, break-glass user, pipeline identities): the policies attached, the
   actions and resource scopes, **the reason for each**, the owner, the last rotation, the next
   review date, the plane it lives in.
4. **Human access**: groups, roles, the identity provider in force, who holds break-glass.
5. **Guardrails in force**: the honesty page (ADR-0006) instantiated for this installation, with the
   latest deployment report per rule and the active waivers with expiry.
6. **Compliance evidence**: for the profile's allowlist, the products and regions in use, the
   customer-side prerequisites still pending (`pending_actions`), and the control-to-evidence table.
7. **Change log**: the dated merge history of the tenant repo with the auto-merge decision records.

**Publishing**: the tenant-repo template (ADR-0005) includes a docs job: render → build (the site
tool chosen in ADR-0013) → publish to GitHub Pages or GitLab Pages, on every merge and nightly after
the conformance run, so the "actual" columns are at most a day old. Each page carries a generated-at
stamp and the commit it was rendered from. The site is private by default (Pages access control
where the forge supports it); the docs never contain secrets, only identifiers.

**Evidence rules**: every "actual" value cites its source (stage output or scanner run id); a value
the scanner could not collect is shown as `UNVERIFIED`, never omitted or guessed (same rule as the
deployment report).

## Consequences
- Compliance questionnaires are answered from a URL that is regenerated nightly; reviewers can be
  given read access to the site instead of a spreadsheet.
- Reasons become part of the code review: a permission without a reason cannot be merged.
- The renderer is one more tool to maintain; it reads only schemas that already exist, so its
  surface grows with the model, not separately.

## Counterpoints (kept even if overruled)
- Generated prose reads mechanically; the pages are reference material and say so; explanations
  stay hand-written in the project docs.
- Publishing the account inventory, even privately, is a disclosure risk; mitigations: identifiers
  only, access-controlled Pages, and a profile switch to render to a local directory instead.

## Verification
- Spike: render the accounts-and-permissions page for the `solo` profile from a sandbox: ledger +
  role data + scanner inventory → Markdown with reasons; a reviewer with no context can say who may
  do what and why.
- Spike: GitHub Pages and GitLab Pages access control for a private site from the tenant-repo template.

## Review log
_(empty)_
