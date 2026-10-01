# ADR-0006: Guardrails without organisation-level policy
- Status: Proposed (revised 2026-10-01 after brainstorm round 2)
- Date: 2026-10-01
- Related: ADR-0001, ADR-0005, ADR-0008, ADR-0012, ADR-0018

## Context
Azure Policy, AWS SCPs and GCP org policy let a root enforce rules every child inherits. OVHcloud has
no hierarchy, but — verified 2026-10-01 in the provider docs — `ovh_iam_policy` supports `allow`,
`except`, **`deny` ("always denied even if also allowed by this policy or another one")**,
`permissions_groups`, **`conditions`** (nested `AND|OR|NOT|MATCH`, depth 3, keys `resource.Tag(<name>)`,
`date(<tz>).WeekDay[.In]`, `request.IP`) and **`expired_at`**. So a real preventive platform layer
exists: a deny that every principal inherits through group membership. What does not exist is an
inherited *policy engine* with compliance evaluation. A guardrail that lives only in the pipeline is
bypassed by anyone with console rights, so every rule must say which planes enforce it.
Cost controls (verified): `ovh_cloud_quota` with `prevent_automatic_quota_upgrade`,
`ovh_cloud_project_alerting` budget alerts; no hard spend cap.

## Options considered
1. Pipeline-only policy checks.
2. Pipeline checks plus IAM least privilege.
3. **One guardrail spec with three enforcement planes** (platform IAM, pipeline, detective), each rule
   declaring which planes enforce it, generated into docs, tested by mutation.
4. Wait for the platform.

## Decision (proposed)
Option 3.

**Guardrail spec** `policies/guardrails.yaml`: one entry per rule — id, intent, severity, profile
membership (ADR-0012), and for each plane whether it enforces the rule and how:

| Plane | Mechanism | Strength |
|---|---|---|
| **Preventive (platform)** | **Deny-floor group**: every principal, including admins, is in `lz-all`, whose policy carries `deny` for catastrophic actions (delete project, modify IAM policy or group, delete `*-state-*`/`*-audit-*` buckets). Tenant envelopes: `allow` on tenant URNs with `conditions` on `resource.Tag(lz:tenant)`. Only the break-glass user and the identity pipeline's service account are outside the floor. | Real; bounded by what IAM actions exist |
| **Preventive (pipeline)** | Rego on `tofu show -json` plan output (Conftest); OpenTofu `check`/`precondition` blocks; assent on tenant files (ADR-0005). Apply blocked on violation; plan hash pinned so apply re-plans and aborts on drift. | Bypassable outside the pipeline |
| **Preventive (architecture)** | The deployed topology makes the violation impossible: islands-first networking (no east-west path exists), no public gateway unless requested, per-tenant state buckets. | Real; holds until someone changes the topology |
| **Detective** | `tools/lz-audit` on a schedule: enumerates projects, principals, policies, OpenStack users, NIC-handle delegations, public buckets, effective security-group rules (including OpenStack's unmanaged default allow-all egress, which `ovh_cloud_security_group` never shows as drift), expired waivers, federation settings; reconciles against the tenant model, ledger and spec; emits SARIF (forge code-scanning tab) and JSON (Logs Data Platform). Report-only; never deletes. | Catches console changes a day late |

- **Deployment report**: every conformance run (and every e2e test) emits a report with one of five
  outcomes per rule — `passed`, `failed`, `not-applicable`, `exception` (waiver id), `UNVERIFIED`
  (no evidence collected). A mandatory rule in `regulated` with no effective implementation fails
  enrolment; an unsupported control never silently becomes `passed`.
- **Commercial-safety rules** are guardrails too (`LZ-POL-006`): no automatic retry of orders or
  destroys; reconcile before retrying; a data source that creates carts is not read-only.

- **Honesty page** generated from the spec: per rule and per profile, which planes enforce it. The
  words "enforced" and "prevented" may appear only where the platform plane is `yes`.
- **Generation**: v1 ships hand-written IAM policies, Rego and scanner checks that *reference* the rule
  id; a CI test fails if a rule has zero enforcing planes or if any artefact references an unknown id.
  Compiling all three from the spec is a spike, not a v1 promise (risk: the compiler becomes a policy
  engine of its own).
- **Mutation tests**: for each rule, a synthetic violation is injected per enforcing plane
  (fixture plan, tenant file, scanner input) and the test asserts detection (ADR-0008).
- **Break-glass**: sealed native user outside the floor; planned elevated work uses policies with
  `expired_at`; both alert through the audit sink (source: spike, ADR-0018).
- **Cost and quota guardrails**: every project has a budget alert (plan policy refuses otherwise);
  quota profiles are declared per environment and `prevent_automatic_quota_upgrade` is `true` by
  default in `regulated`; a **cost preview** (plan → estimate from the OVH public catalogue API) is a
  spike; the test sandbox has a fail-closed budget guard (ADR-0008).
- Policies are Rego in `policies/plan/` with a metadata header (id, severity, class, profiles) and a
  test file each; a violation names the rule id and a fix; the docs have one page per rule.
- Auto-remediation is **off**; the scanner never deletes (ADR-0004 safe-by-default).
- Docs state the bypass: "console users with project admin rights can step outside the pipeline; the
  deny-floor stops the catastrophic actions, the scanner reports the rest".

## Consequences
- Stronger and more honest than round 1: a platform-enforced floor plus an explicit matrix.
- Three artefacts per rule to keep in sync until the compiler spike lands; the id-reference test is
  the guard.
- The scanner is a second product to maintain; it needs a read-only service account.

## Counterpoints (kept even if overruled)
- The deny-floor can lock out the operator; the sealed break-glass user and a tested
  "remove-from-floor" runbook are mandatory before the floor ships.
- Detective-only coverage of most rules may disappoint compliance-driven adopters; the honesty page
  says exactly what is covered.
- Moving plan checks into `check` blocks would remove Rego; rejected for v1 because TACOs plug OPA
  bundles natively and `check` blocks cannot block `apply` by themselves.

## Verification
- Spike (first): deny on project deletion via `lz-all`, attempted by an otherwise-allowed user → 403;
  tag-conditioned envelope: call on a resource lacking the tag → denied, with the tag → allowed.
- Spike: API list calls the scanner needs and their rate limits (`api_rate_limits` guide).
- Spike: cost preview from the public catalogue API for three flavours and one managed cluster.

## Review log
- 2026-10-01 revision: deny-floor and tag conditions (verified facts), enforcement matrix as data,
  mutation tests, cost/quota guardrails. Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
- 2026-10-01 (later): architecture plane, five-outcome deployment report, delegation and default-egress
  findings, commercial-safety rules adopted from the external blind design review.
