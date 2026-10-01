# ADR-0005: Declarative tenant model, policy-driven auto-merge and the self-service transaction
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0003, ADR-0004, ADR-0006, ADR-0016, ADR-0017, ADR-0018, ADR-0021

## Context
On OVHcloud a Public Cloud project is the unit of isolation, billing and quota, so vending and
adopting projects is the central landing-zone act. Quotas (`ovh_cloud_quota`) and budget alerts
(`ovh_cloud_project_alerting`) are code; project creation is a billed order with prerequisites and
latency still to be measured. Google FAST, AWS LZA and the STACKIT/meshcloud project factory make
data the interface; LZA's lesson is that a schema is a ceiling and users need an escape hatch.
**Policy-driven auto-merge of routine tenant changes is a core feature of this project on every
forge** (operator decision D7). PlatformRelay's `assent` is the engine: deterministic decisions
(approve / comment / request-changes / block) from lintable, testable YAML policies with CEL
predicates. Its current limits (GitHub adapter unbuilt) are tracked in the assent repository and do
not shape this design.
Auto-merge makes repository content part of an authorisation protocol, so three things must hold: a
request can never edit the data that authorises it (owners, effective defaults, waivers); tenant HCL
never executes under platform credentials; and a stale flag in a file never authorises a deletion.

## Options considered
- HCL/tfvars per project; a portal; one YAML file per project rendered by the platform.
- **A declarative tenant model in a separate self-service repo, with an auto-merge gate whose
  authority comes from trusted data, and a defined transaction lifecycle** (chosen).

## Decision

### Model
Kinds under `apiVersion: lz.platformrelay.dev/v1alpha1`, strict-decode schemas in `schemas/`
(duplicate keys, unknown fields and unsupported versions are rejected):
- `Tenant` — one file per tenant: `metadata { name, domain }`, `spec { profile, environments[]
  { name, regions, resilience, quotas, budget, compliance }, identity.bindings { role → groups },
  runtime capabilities (ADR-0017), features, waivers[] (see below), lifecycle }`.
- Platform-controlled files, **outside the routine lane**: `owners.yaml` (who may change what),
  `deployments.yaml` (instances, ADR-0004), `identities.yaml` (ADR-0018), `ipam.yaml` (ADR-0017),
  `profiles/`, `.assent/` policies, CI definitions and schemas.
- The **canonical effective deployment document**: before any decision, the platform resolves the
  tenant file against the profile defaults into one document with a digest; the merge gate, the plan
  and the plan policy all evaluate that document, never the raw file (same resolved defaults, same
  target identifiers). Effective values are materialised in the plan output.

### Virtual hierarchy
`account → domain → tenant → environment → project` lives in data, names, tags and IAM resource
groups; one project per tenant × environment is the only hard boundary; a nightly conformance run
re-derives the tree from live tags and diffs it against the model.

### Auto-merge authority
- **Allowlisted field deltas against the trusted base.** A request may auto-merge only if it changes
  nothing but allowlisted fields of the author's own tenant file, within the profile band (quota,
  budget, allowed regions, features marked `overridable`), judged against the **base revision's**
  ownership data, not the request's. Changes to owners, policy, schemas, CI, `deployments.yaml`,
  profile, identity bindings, waivers or lifecycle never auto-merge.
- **Decision binding.** Every decision is bound to the exact candidate commit, base commit, policy
  and schema versions, effective-document digest, target instance, the **digests of every consumed
  input artefact** (ADR-0004) and required checks; a rebase, merge-queue change, new commit or a
  changed upstream artefact generation invalidates it and re-evaluation runs on the merged candidate.
- **Aggregate reservations.** Quota and budget changes reserve against the account's totals
  atomically, so two individually in-bounds requests cannot combine into an over-budget state.
- **Review paths.** New tenant, new environment, profile change → platform review. Identity-binding
  change → a verified platform approval (a tenant may author the request; a platform admin must
  approve it; the approval is bound as above). New waiver → review by the control owner.
- **Waivers** bind rule id and revision, immutable scope, approver (resolved from platform data,
  never free text), reason and expiry. Expiry blocks new violating changes and notifies owners; it
  never destroys running infrastructure.

### Transaction lifecycle (merge is not deployment)
States per instance: `queued → planning → policy → ready → applying → applied | blocked | failed`,
with `pending-actions` when manual steps remain (federation, orders, HDS prerequisites). Idempotent
re-runs; partial completion recorded; rebase invalidation; a merged tenant may be `applied` yet
`not-ready-for-workloads` until pending actions close. The state is written back to the request and
to the generated documentation (ADR-0020).

### Extensions (escape hatch)
Anything outside the schema is an **extension root**: a separately reviewed deployment instance in
the tenant repo that consumes only that tenant's published output artefacts (ADR-0004) and runs with
that tenant's scoped credentials. **Tenant-supplied HCL never executes in the factory, identity or
shared-state runners.**

### Adopt first, order second
The default first experience adopts an existing project (`import` blocks, expected no-change plan,
name overrides preserve existing names). Ordering is `lifecycle: order`, run only with the
bootstrap/order identity: preflight (payment prerequisites, project-count eligibility,
region/product availability, quota, estimated cost) → human-reviewed preview → execute → **reconcile
before any retry** → record. Data sources that create carts are not treated as read-only until tested.

### Retirement
Removal requires a **retirement record** approved by the platform, bound to immutable project ids
and a manifest revision, with a completed freeze, dependency check and backup/restore evidence.
File absence never authorises destruction. The retirement runbook (freeze, inspect, preserve,
revoke, remove billable resources, verify inventory and billing, close) is a tested task; US-partition
deletion differs and is documented separately.

## Consequences
- Routine changes merge and deploy without a human; everything that confers authority goes through
  a review path with bound approvals.
- Two policy languages exist by choice, not necessity: CEL (assent) decides merge authority on data;
  Rego (Conftest) decides deployment effects on plan JSON; both consume the same effective document
  and share control ids and fixtures (ADR-0006). Differential fixtures prove they agree on boundary
  cases.
- The schemas and the effective-document format are the product's main compatibility promise.

## Counterpoints (kept even if overruled)
- YAML indirection hides OpenTofu from users who know it; extension roots and materialised effective
  values are the mitigation.
- A platform admin with cloud rights can still bypass forge policy; the cloud permission boundary
  (ADR-0006) and the honesty page state the limit.
- assent is alpha and its GitHub adapter is unbuilt; recorded, not designed around (D7).

## Verification
- Spike (ranked 4 by the review): lint and the intended fixtures pass, **and** the gate rejects owner
  and policy edits, forged approvals, stale base and merge candidates, waiver scope changes and stale
  retirement authorisations; aggregate reservation holds under simultaneous requests on both forges.
- Spike (ranked 13): adoption yields an explained no-replacement plan; an approved disposable order
  is reconciled after a timeout without a duplicate purchase; retirement proves restore and residual
  billing checks.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
