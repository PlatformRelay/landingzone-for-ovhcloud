# Research and decisions
## Decision: sandbox safety precedes all billed platform experiments
D8 sets EUR 200/month, not the stale 100 EUR alert note in the local inbox. ADR-0008
requires external run leases, known resource ids and fail-closed cleanup health. The alert
threshold and numeric per-run bounds are approved in cost/sandbox design, not invented here.
Unknown setup is an explicit external gate; planning does not spend the budget.
## Decision: prove independent decryption first after cleanup is qualified
https://opentofu.org/docs/language/state/encryption/ describes method/fallback readers.
The recovery design cannot use PBKDF2 as a second wrapping key for OKMS-written state;
003 must observe a separately encrypted backup and clean restore against the pinned tool.
Account-specific OKMS helper, backend conditional-write and IAM behaviours remain UNVERIFIED.
## Decision: supported principal routes require their own evidence
ADR-0006 and 0018 already distinguish OVH IAM, Keystone and S3, singleton SAML and cached
Kubernetes tokens. We do not promote a denial on one API to global prevention or assume
immediate revocation after group removal. Use per-issuer approved measured residual bounds.
## Decision: exact account read and live tool captures, not web prose alone
Provider resource pages and source (https://github.com/ovh/terraform-provider-ovh),
https://opentofu.org/docs/language/settings/backends/s3/ and the account's read-only
inventory inform experiments. Account probes were not run during planning. Credentials,
scoped projects, holders, cleanup health and forge runners all remain setup gates.
Failed mechanisms revise Proposed ADRs in place; no unsupported branch gets a green label.
