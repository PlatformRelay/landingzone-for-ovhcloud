# Research and decisions
## Decision: sandbox safety precedes all billed platform experiments
D8 sets one dedicated project. Current repository working contract clarifies one-off
EUR 200 trial funding, no monthly replenishment and selective live tests; not a provider hard cap.
The project may exist before its notes/authority are verified. Account-scoped experiments,
additional projects/regions and backup placement remain explicit external decisions. ADR-0008
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

The per-spike ceilings and refutation stops are in spec.md. Approval must bind these
values before a live target starts; deadline/spend stops admission but keeps cleanup
executable. A complete decision packet is not a qualification pass. Aggregate inputs
are local 002/T008/T012/T016 evidence and explicit per-forge status, not adapter availability.

## Upstream input trace
Pinned upstream inputs in ../../docs/reference/upstream-reference-map.md inform T009–T014 encryption/backend and T015–T020 IAM/issuer probes. Record source paths/revision with actual captures; do not copy broad actions or infer two-project proof from within-project fixtures.
