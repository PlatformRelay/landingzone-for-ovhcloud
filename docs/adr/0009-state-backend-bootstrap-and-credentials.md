# ADR-0009: State backend, bootstrap and credentials
- Status: Proposed (revised 2026-10-01 after brainstorm round 2)
- Date: 2026-10-01
- Related: ADR-0004, ADR-0007, ADR-0011, ADR-0018

## Context
Verified 2026-10-01: OVH Object Storage supports S3 conditional writes (`If-Match`, `If-None-Match` on
`PutObject` and `CompleteMultipartUpload`; "unencrypted objects and objects encrypted with SSE-S3"),
and OVH's blog (2026-06-15) confirms native Terraform/OpenTofu S3 lockfile locking works on it.
OpenTofu state encryption lists an OVHcloud KMS key provider (external, maintained by OVHcloud) and a
PBKDF2 passphrase provider; plan files can be encrypted too. OVH docs warn the S3 backend is not
encrypted at rest by default. Credentials: OAuth2 clients (`ovh_me_api_oauth2_client`, long-lived
secrets), identity-user tokens with `expires_in` (`ovh_me_identity_user_token`), access-token auth in
go-ovh (`OVH_ACCESS_TOKEN`); Keystone application credentials with expiry and access rules for the
OpenStack plane. No OIDC federation from CI providers to the OVH API was found (absence unproven).

## Options considered
State: OVH Object Storage S3 backend; an external S3; TACO-managed state; local state for bootstrap only.
Locking: native lockfile (now confirmed viable); TACO-level locking; none.
Credentials: long-lived client secret in forge secrets; TACO-held secret; "lease, don't store".

## Decision (proposed)
- **Backend:** OVH Object Storage S3, bucket versioning on, **`use_lockfile = true`**. Granularity:
  one bucket per platform stage set, one key per stage; **one bucket per tenant** for tenant and
  runtime state (blast radius of a leaked tenant credential is one tenant's state).
- **Encryption mandatory** from stage 00: OpenTofu client-side `encryption {}` with the OVHcloud KMS
  key provider as primary and a PBKDF2 fallback whose passphrase is sealed offline; `plan {}`
  encrypted; key rotation enabled. Client-side encryption yields opaque objects, so the SSE-C caveat
  on conditional writes does not apply.
- **Disaster recovery of state:** scheduled copy of state buckets to a second region (native
  replication: spike); a **quarterly restore drill** (ADR-0008 L10) restores from the replica into a
  scratch project and asserts an empty plan; the state bucket is covered by the deny-floor
  (ADR-0006).
- **Recoverable bootstrap (stage 00):** one manual, human-run stage creates the state bucket, the
  OKMS key, the automation project (if the order model allows), the identity pipeline's service
  account and the deny-floor, then migrates its own state into the bucket. Resource names come from
  the naming module, so the stage can be **re-run from empty state with `import` blocks** if state is
  lost.
- **Two-pipeline rule:** the pipeline that changes identity (stage 10) uses its own service account,
  protected branch and review rule; tenant and runtime pipelines cannot change IAM.
- **Credentials — "lease, don't store":**
  1. the only long-lived secrets are the identity pipeline's OAuth2 client and the age key for SOPS,
     held in the forge or TACO secret store;
  2. every other run mints short-lived credentials: an identity-user token with `expires_in` for the
     OVH plane (service-account user per stage, least privilege), and a Keystone application
     credential with `expires_at` and access rules for the OpenStack plane; revoked in a `finally` step;
  3. P0 fallback for `solo`: per-stage OAuth2 clients with a documented rotation task;
  4. OIDC federation from CI is used if a spike ever finds it; a token-broker component is optional,
     never required.
- **Repo-side secrets:** SOPS + age for bootstrap values and IdP metadata; the tenant schema forbids
  keys named `*secret*`/`*password*`.
- **Nothing in state in cleartext:** ephemeral resources and write-only attributes (OpenTofu 1.11+)
  wherever the provider supports them; otherwise state encryption is the backstop.

## Consequences
- The conditional-write spike from round 1 is closed; locking is a configuration, not a risk.
- Bootstrap is the single place a human holds powerful credentials; its runbook ends in a tested
  task and has a recovery section.
- Minimum OpenTofu is 1.13 (ADR-0008); Terraform cannot read encrypted state.

## Counterpoints (kept even if overruled)
- Encryption keyed by OKMS makes every plan depend on an OVH service; the PBKDF2 fallback is the
  mitigation and is drilled.
- Per-tenant buckets multiply backend configs; generated from `stacks.yaml` (ADR-0007).
- "Lease, don't store" adds a broker step per run; `solo` keeps the simple fallback.

## Verification
- Spike: two concurrent `tofu apply` with `use_lockfile` on an OVH bucket; second is refused.
- Spike: OKMS key provider round trip; rotate; delete the key; restore with the PBKDF2 fallback.
- Spike: mint an identity-user token with `expires_in = 3600` and a Keystone app credential with
  access rules; allowed path works, disallowed path 403, unusable after expiry.
- Spike: bucket replication or scheduled copy across OVH regions; restore drill script.

## Review log
- 2026-10-01 revision: locking confirmed; per-tenant buckets; DR replica and restore drill; import-based
  re-bootstrap; two-pipeline rule; lease-don't-store. Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
