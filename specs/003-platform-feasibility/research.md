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
003 must observe a separately encrypted backup and clean restore without the original
account/KMS against the pinned tool.
Account-specific OKMS helper, backend conditional-write and IAM behaviours remain UNVERIFIED.
## Decision: initial-development custody amendment
On 2026-10-01 the operator approved D49: one maintainer plus a separately encrypted sealed
offline copy for initial development, exercising C001.3's amendment provision. The
[spec amendment](spec.md#development-custody-amendment-d49) records its scope and evidence
requirements. A second human custodian is not a pending development prerequisite; actual
package availability, independent access and clean-environment recovery without the
original account/KMS remain unverified until their respective checks pass. Key-loss,
wrong-escrow, same-key-replica and plaintext-refusal controls remain mandatory. The copy
does not qualify recovery while the maintainer is unavailable or production two-person custody.
## Decision: supported principal routes require their own evidence
ADR-0006 and 0018 already distinguish OVH IAM, Keystone and S3, singleton SAML and cached
Kubernetes tokens. We do not promote a denial on one API to global prevention or assume
immediate revocation after group removal. Use per-issuer approved measured residual bounds.
## Decision: exact account read and live tool captures, not web prose alone
Provider resource pages and source (https://github.com/ovh/terraform-provider-ovh),
https://opentofu.org/docs/language/settings/backends/s3/ and the account's read-only
inventory inform experiments. Account probes were not run during planning. Credentials,
scoped projects, D49 maintainer custody/sealed offline package, cleanup health and forge
runners all remain actual setup gates; the approved custody shape alone passes none of them.
Failed mechanisms revise Proposed ADRs in place; no unsupported branch gets a green label.

The per-spike ceilings and refutation stops are in spec.md. Approval must bind these
values before a live target starts; deadline/spend stops admission but keeps cleanup
executable. A complete decision packet is not a qualification pass. Aggregate inputs
are local 002/T008/T012/T016 evidence and explicit per-forge status, not adapter availability.

## Upstream input trace
Pinned upstream inputs in ../../docs/reference/upstream-reference-map.md inform T009–T014 encryption/backend and T015–T020 IAM/issuer probes. Record source paths/revision with actual captures; do not copy broad actions or infer two-project proof from within-project fixtures.
