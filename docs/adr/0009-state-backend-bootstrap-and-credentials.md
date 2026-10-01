# ADR-0009: State backend, bootstrap, recovery and credentials
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0004, ADR-0007, ADR-0011, ADR-0018, ADR-0021

## Context
Verified 2026-10-01: OVH Object Storage supports S3 conditional writes (`If-Match`, `If-None-Match`;
unencrypted or SSE-S3 objects), and OVH's blog confirms native lockfile locking works; OpenTofu
state encryption offers an OVHcloud KMS key provider (external) and a PBKDF2 passphrase provider.
Credential mechanisms: OAuth2 clients (`ovh_me_api_oauth2_client`, long-lived secret, **not
importable** with its secret), identity-user tokens with expiry (`ovh_me_identity_user_token`,
requires a user login), the documented IAM service-account bridge to OpenStack (ADR-0018), Keystone
application credentials (OVH support for access rules UNVERIFIED), no OIDC federation from CI found.
Two facts constrain recovery: OpenTofu's encryption `fallback` is a reader for data encrypted by its
own method during rollover, **not a second wrapping key**, so a passphrase fallback cannot decrypt
state written under the OKMS method and losing that key means losing the state unless a separately
decryptable copy exists; and a root cannot be encrypted with a key that the same root creates, so
bootstrap needs a method that exists before any KMS does. Any lease-based credential scheme must also
say who authenticates to the issuer.

## Options considered
State: OVH Object Storage; another S3; TACO- or GitLab-managed state. Locking: native lockfile;
TACO-level; none. Recovery: none; replica under the same key (false safety); **independently
decryptable backup**. Credentials: long-lived secrets in the forge; TACO-held; issuer with leases.

## Decision

### Backend and locking
OVH Object Storage S3, bucket versioning on, `use_lockfile = true`; one bucket per account-level
root set, one bucket per tenant for its instances; backend and decrypt permissions scoped to the
instance's credential class (ADR-0004), otherwise per-tenant buckets isolate nothing. Stale-lock and
interrupted-writer procedures are documented and drilled; a replica is **never** a second active
writer; promotion is an explicit, logged step; Object Lock never applies to lock objects.

### Encryption and key recovery (replaces the false fallback claim)
- Client-side `encryption {}` is mandatory for every instance; plan files encrypted too.
- **Bootstrap starts with an independently escrowed method**: the bootstrap root encrypts with a
  PBKDF2 passphrase held in offline escrow (two holders, ADR-0022) before any KMS exists; it creates
  the OKMS key and the later stages' access credentials; later instances use the OKMS key provider as
  primary with the passphrase method configured only as the migration reader.
- **Key-loss recovery is a separately decryptable backup**, not a fallback: a scheduled job (its own
  identity, read access to state, the escrowed passphrase from a sealed secret) pulls each state
  snapshot and writes a copy encrypted under the escrow method to a second bucket in a second region.
  The escrow package records key identifiers, authenticators, backend configuration, toolchain
  versions and the recovery runbook; nothing needed to use it lives only inside encrypted state.
- OKMS key rotation enabled; KMS or object-storage unavailability, key loss and state corruption
  are three separate drill cases.

### Recoverable bootstrap
Stage `bootstrap` is run by a human once, from a clean environment, and can be **re-run from empty
state**: an independently retained **resource-id and import manifest** (written by the stage, stored
with the escrow package) supplies provider-assigned ids that names cannot reconstruct; secrets that
cannot be read back (OAuth2 client secrets) are re-issued, never "imported". Drills: (a) restore a
snapshot into an isolated backend and validate against the same disposable fixture with production
writes fenced; (b) rebuild in a new project and expect creation plus data restoration; an empty plan
is never the success criterion for a rebuild.

### Credential matrix ("lease, don't store", made explicit)
Every automation identity is a row in `identities.yaml` with: issuer, principal type, **how the
caller authenticates to the issuer**, scope, lifetime, renewal, revocation, state/backend/KMS access,
emergency recovery.

| Authority | Mechanism (preferred → fallback) | Lifetime |
|---|---|---|
| bootstrap / order | human-held OAuth2 client, offline escrow, used only in the bootstrap runbook | long-lived, rotated after each use |
| account governance (identity pipeline) | dedicated OAuth2 client in the forge/TACO protected store; two-pipeline rule | long-lived; client-swap rotation (two live secrets per client UNVERIFIED) |
| deployment per instance | per-instance service account; OVH plane: identity-user token with `expires_in` minted at run start from a service-account user whose login the pipeline holds; OpenStack plane: the IAM service-account bridge (ADR-0018) → fallback Keystone application credential with expiry | per run; revoked in `finally` |
| observation (scanner, docs renderer) | read-only service account | long-lived, rotated quarterly |
| state / backup | per-bucket S3 credentials scoped to the instance; backup job has read-state + write-backup only | rotated quarterly |
| recovery (break-glass) | sealed native user outside the deny-floor; alerting on use | permanent, drilled |

`solo` may use the fallback of per-instance long-lived clients with a documented rotation task.
OIDC federation from CI is adopted only if a spike finds an authoritative source. A token broker is
optional and, if used, is itself a row in the matrix with its own authenticator; it must not run
only inside the platform it recovers.

### Secrets elsewhere
SOPS + age for repo-side bootstrap values and IdP metadata (age recipients: the two escrow holders
and the identity pipeline); the tenant schema forbids keys matching `*secret*|*password*|*token*`;
ephemeral resources and write-only attributes wherever the provider supports them; otherwise state
encryption is the backstop, not a proof of absence.

## Consequences
- Key loss no longer means state loss; the cost is one backup job and an escrow procedure with two
  holders.
- Bootstrap has an offline prerequisite (the escrow package) and a tested re-run path.
- The matrix makes "who can mint what" reviewable; forge secrets hold exactly the rows marked long-lived.

## Counterpoints (kept even if overruled)
- Two encryption methods in play (OKMS primary, escrow for bootstrap and backups) is more to drill;
  rejected alternative — OKMS everywhere — leaves no path when the key is gone.
- Per-run token minting needs a stored authenticator anyway; the gain is scope and lifetime, not
  "no secrets".

## Verification (ranked by the review)
- Spike 1: a fresh operator environment recovers the disposable fixture **without** the original
  key service, using the escrow backup; demonstrate that the old "PBKDF2 fallback" idea fails.
- Spike 5: who authenticates to each issuer; expiry mid-run, renewal, failed cleanup, eventual
  revocation; bridge qualified with the OpenStack provider; application-credential access rules on OVH.
- Spike 7: two concurrent writers; interrupted writer; stale-lock procedure; promotion cannot create
  two active writers; bounded replica lag; restoration into an isolated backend.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
