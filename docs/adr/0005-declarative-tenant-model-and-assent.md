# ADR-0005: Declarative tenant model and `assent` as the self-service gate
- Status: Proposed (revised 2026-10-01 after brainstorm round 2; formerly "YAML project factory")
- Date: 2026-10-01
- Related: ADR-0003, ADR-0004, ADR-0006, ADR-0016, ADR-0017, ADR-0018

## Context
On OVHcloud a Public Cloud project is the unit of isolation, billing and quota, so vending projects is
the central landing-zone act. Project creation is a billed order (latency and billing prerequisites
_UNVERIFIED in detail_). Quotas are manageable as code (`ovh_cloud_quota`: profile per region,
`prevent_automatic_quota_upgrade`) and budget alerts exist (`ovh_cloud_project_alerting`) — verified
2026-10-01. Google FAST, AWS LZA and the STACKIT/meshcloud project factory make data the interface;
AWS LZA's lesson is that a YAML schema is a ceiling and users need an escape hatch.
PlatformRelay's `assent` (Apache-2.0, alpha; GitLab forge Core, GitHub adapter planned) decides merge
requests on self-service config repos by Kyverno-style YAML policies with CEL predicates
(approve / comment / request-changes / block), with lintable, testable policies. All three blind
designs in round 2 independently chose "tenant YAML is the only self-service surface, assent gates it".

## Options considered
- **HCL/tfvars per project** — typed, no tooling, unreviewable at 50 projects, tenants write HCL.
- **One YAML file per project** processed by a factory with JSON-Schema validation (round 1).
- **A declarative tenant model** (Kubernetes-style kinds) in a **separate self-service repo**, gated
  by `assent`, rendered by the platform's stages (round 2).
- **A portal/UI** — a second product.

## Decision (proposed)
The declarative tenant model.

**Kinds** (`apiVersion: lz.platformrelay.dev/v1alpha1`, strict-decode JSON Schemas in `schemas/`):
- `Tenant` — one file per tenant: `metadata { name, domain, owners }`, `spec { profile, environments[]
  { name, regions, resilience, quotas, budget, compliance }, identity.bindings { role → groups },
  runtime.<kind> (oneOf, ADR-0017), features, waivers[] { rule, reason, expires }, lifecycle }`.
- `identities.yaml` — the identity ledger (ADR-0018).
- Values the profile marks `overridable` may be set per tenant; everything else is inherited and
  **materialised into plan output** so reviewers see effective values.

**Virtual hierarchy.** `account → domain → tenant → environment → project` exists in data, names, tags
and IAM resource groups; one Public Cloud project per `tenant × environment` is the only hard
boundary. The nightly conformance run re-derives the tree from live tags and diffs it against the model.

**Where files live.** Tenants edit only their file in the **tenant repo** (skeleton in
`templates/tenant-repo/`); they never see HCL. `stages/30-tenants` reads all tenant files
(`fileset` + `yamldecode`), so a merge is the deployment trigger. Anything outside the schema uses the
escape hatch `extensions/<tenant>.tf` in the tenant repo, which receives the stage outputs.

**`assent` as the gate** (policies and fixtures in `policies/assent/`, shipped into the template):
- in-bounds edit of the author's own tenant file (quota within the profile band, allowed regions,
  budget set) → **approve**;
- new tenant, new environment, profile change → **request-changes** (platform review);
- identity-binding change → **block** unless the author is a platform admin;
- removal of a tenant or environment → **block** unless `lifecycle: decommission` was merged earlier
  (two-step destroy); the stage never destroys on file removal without it;
- new `waiver` → **require review**; expired waivers are scanner findings (ADR-0006).
- GitLab is the first fully worked path (assent Core); on GitHub, CODEOWNERS plus the plan-policy
  job stand in until the adapter lands, and the PR comment says so.

**Order-based steps** (quota profile upgrades that need a ticket, payment prerequisites) are intents:
the stage emits `pending_actions` and the pipeline reports them; nothing is silently skipped.

## Consequences
- Adding a project is a merge request with one file; routine changes merge without a human.
- The schemas are the product's main compatibility promise: versioned, changelogged (ADR-0010).
- Two policy languages exist: CEL (assent, on data) and Rego (plan JSON, ADR-0006). Scope rule:
  assent decides *what may be merged*; plan policy decides *what may be applied*.
- Order latency makes tenant tests partly asynchronous; integration tests use a pre-ordered sandbox
  pool (ADR-0008).

## Counterpoints (kept even if overruled)
- YAML indirection hides OpenTofu from users who know it; the escape hatch and the materialised
  effective values are the mitigation.
- One policy language would be simpler; assent is CEL-only and operates on repo diffs, not plans, so
  a second engine for plan JSON is unavoidable unless plan checks move into `check` blocks (recorded
  as an option for ADR-0006).
- assent is alpha; its schema may change. The template pins an assent version (`mise.toml`).

## Verification
- Spike: schema + three assent policies (quota within band → approve; new tenant → request-changes;
  binding change → block) with fixtures and five mutants; pass if `assent lint` is clean and every
  fixture and mutant decides as specified.
- Spike: order one project via `ovh_cloud_project`; measure latency, billing prerequisites, quota
  defaults, idempotence under retries; apply `ovh_cloud_quota` and `ovh_cloud_project_alerting`.

## Review log
- 2026-10-01 revision: project factory → declarative tenant model in a separate repo; assent gate;
  waivers; virtual hierarchy. Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
