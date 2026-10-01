# ADR-0009: State backend, bootstrap and credentials
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0004, ADR-0007, ADR-0011

## Context
OVHcloud documents an S3 backend on Object Storage, warns state is not encrypted at rest, and says
nothing on locking. OpenTofu native S3 lockfile locking needs conditional writes (`If-None-Match`);
whether OVH honours them is **UNVERIFIED**. OpenTofu offers state encryption with pluggable key
providers; an OVH KMS key provider (`ovh/opentofu-kms-ovhcloud`) exists. Credentials for the OVH API are
OAuth2 client credentials or application keys; Public Cloud also needs an OpenStack user (`clouds.yaml`).
Every prior-art accelerator has exactly one manual privileged bootstrap, then CI-only.

## Options considered
State: OVH Object Storage S3 backend; an external backend (other S3, GitLab-managed state, TACO-managed);
local state for bootstrap only.
Locking: native S3 lockfile; DynamoDB-style external table (not on OVH); TACO-level locking; none.

## Decision (proposed)
- **Default backend:** OVH Object Storage S3 backend, one bucket per blueprint, one key per stage,
  bucket versioning on, with **OpenTofu state encryption mandatory** from stage 00 (KMS key provider if
  the spike passes, otherwise a passphrase/PBKDF2 provider with a documented rotation procedure).
- **Locking:** native lockfile if the conditional-write spike passes. If it fails: document the risk,
  rely on **serialised pipelines** (one apply per stage via CI concurrency groups or TACO locks), and
  say plainly that console/manual `tofu` runs are unprotected.
- **Backend is swappable:** stages declare the backend only in `backend.tf` generated from `stacks.yaml`
  (ADR-0007); alternative backends are documented, not special-cased.
- **Bootstrap (stage 00):** one **manual**, human-run stage that creates: the state bucket and its
  versioning, a dedicated "automation" project (if the order model allows), service accounts per stage
  (least privilege, ADR-0004), and the credentials for the pipeline. It then migrates its own state into
  the bucket it created.
- **Credentials:** no long-lived admin credentials in CI. Order of preference: (1) OIDC/federation if
  OVH supports it, (2) short-lived tokens minted by a bootstrap-created service account, (3) application
  keys with rotation documented. All secrets via the platform's secret store; OpenTofu ephemeral
  resources and write-only attributes (1.11+) used so secrets never land in state.
- **OpenStack credentials** for Public Cloud resources are created per project by the factory and
  stored only in the secret store, never in repo or state in cleartext.

## Consequences
- Bootstrap is the single place a human holds powerful credentials; its runbook is a how-to with a
  checklist and a recovery section (lost state bucket, rotated keys).
- Minimum OpenTofu version is raised to one with ephemeral resources (1.11+), reducing Terraform
  compatibility; see ADR-0011.

## Counterpoints
- State encryption plus versioning plus no locking can still corrupt on concurrent manual runs.
- Bootstrapping into the platform it manages is a chicken-and-egg risk: lose the bucket, lose state.
  Mitigation: a documented cross-region backup of the state bucket (spike for OVH replication support).

## Verification
- Spike: S3 conditional writes against OVH Object Storage with OpenTofu `use_lockfile`.
- Spike: OIDC/federation to OVH from GitHub Actions and GitLab CI.
- Spike: OVH KMS key provider works with `tofu` encryption on a state file round trip.

## Review log
_(empty)_
