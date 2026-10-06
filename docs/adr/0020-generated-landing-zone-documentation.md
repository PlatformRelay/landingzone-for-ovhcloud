# ADR-0020: Generated landing-zone documentation — system map, accounts and permissions with their reasons
- Status: Accepted
- Date: 2026-10-06
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

## Decision
Option B. `tools/lz-docs` renders a per-installation documentation set from data only; nothing in it
is typed by hand after setup.

Automatic documentation is a substantive differentiator (ADR-0001): an operator should be able
to explain the installation without rebuilding its model mentally. Human explanations and
machine-readable observations are views of the same sources, with progressive detail. The renderer
is behavioural code and requires predefined tests even though its output is documentation.

**Inputs** (all already exist or are introduced here):
- desired state: tenant files, profile, identity ledger, IPAM ledger, waivers;
- actual state: published stage output contracts (`outputs.json`) from validated `current` publication
  records (ADR-0004); verify generation/digest and producer status before reading. Pending,
  failed or unpublished generations never become current actual values; scanner inventory
  comes from read-only API enumeration. Never read raw state files;
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

**Publishing, fail-closed**: the tenant-repo template (ADR-0005) includes a docs job: render → build
(the site tool chosen in ADR-0013) → publish, on every merge and nightly after the conformance run,
so the "actual" columns are at most a day old. Each page carries a generated-at stamp and the commit
it was rendered from. The rendered inventory names break-glass holders, permission scopes and network
structure, so **the default target is a private artefact** (the forge's protected artefact store or a
protected branch readable only by repository members) or a local directory. Publishing to a hosted
site requires **verified access control** before any upload: the job proves that an unauthenticated
request is denied and an intended reader is admitted, and refuses to publish otherwise. GitHub Pages
can restrict visibility only for organisations on Enterprise Cloud; GitLab Pages access control is
available more broadly but is still verified, not assumed. Public publishing is a separate, explicit
choice with a reviewed data scope (no accounts, no network map). The docs never contain secrets,
only identifiers.

**Evidence rules**: every "actual" value cites its source (stage output or scanner run id); a value
the scanner could not collect is shown as `UNVERIFIED`, never omitted or guessed (same rule as the
deployment report).
Generation time is separate from observation time and any human attestation; rendering again
cannot refresh the evidence. A failed/stale scanner shows its last observation, age and failure
status. Permission rationale describes desired intent, not proof of live effective authority or
a completed access review. Each access-review record names last human attestation,
reviewer, next due date and current owner; missing attestation/owner and overdue review
remain visibly unresolved. Scanner health/last success/age remain separate, including an
explicit overdue/failed status; rendering cannot manufacture an attestation. These states
appear in the human view and its underlying data. Scanner failure or observations beyond
the configured freshness bound emit an alert to the explicitly configured accountable
recipient; absence of a recipient is an unresolved operating prerequisite, not healthy
monitoring. Qualify the API observer's account-inventory visibility separately from site
reader/forge membership; a site member is not proof the observer can enumerate IAM.

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
  only, private artefact by default, verified access control before any hosted publication, and a
  reviewed reduced scope for anything public.

## Verification
- Spike: render the accounts-and-permissions page for the `solo` profile from a sandbox: ledger +
  role data + scanner inventory → Markdown with reasons; a reviewer with no context can say who may
  do what and why.
- Spike: GitHub Pages and GitLab Pages access control for a private site from the tenant-repo template.
- Renderer controls: missing/stale inventory and a failed scanner remain visibly unverified;
  re-rendering never changes the observation/attestation date; a valid fresh observation retains
  its source. Private publication and reduced public scope have positive and rejection controls.
- Renderer controls: valid published current/digest/status accepted; unpublished, pending,
  failed, mismatched-digest or late stale generation rejected. Missing owner/attestation,
  overdue access review and stale/failed scanner stay visibly unresolved; generation time
  cannot refresh any observation or human review. A failed/stale scanner produces the
required alert and stale status; missing alert recipient stays unresolved. Independent
API observer and site-reader controls must each prove their own scope.

## Review log
_(none yet)_
