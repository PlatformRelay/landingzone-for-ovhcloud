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

## Decision: S3-API buckets are not on the Swift plane; show it against a Swift-native control (T040–T041)
Sources: kb mirror ovhcloud-docs 5c9f23e, under `docs/en/guides/storage-and-backup/object-storage/`,
and kb `api/v1/cloud.json` (fetched 2026-10-06). Read 2026-10-20.

- OVHcloud sells two Object Storage offers. The S3-compatible classes (High Performance, Standard,
  Infrequent Access, Active Archive, Cold Archive) are listed as `S3-compatible`. Only the two Swift
  classes are listed as `OpenStack Swift` (`s3-choosing-the-right-storage-class-for-your-needs.mdx`
  146-166, `s3-faq.mdx` 21-25, `s3-migration-swift-to-s3.mdx` 9). Moving data from one offer to the
  other is a copy with rclone, not a re-mount (`s3-migration-swift-to-s3.mdx` 119-128).
- Swift's S3 compatibility runs one way only. The legacy Swift Standard class also answers an S3
  API at `https://s3.<region>.cloud.ovh.net` (`s3-location.mdx` 358-362,
  `pcs-object-storage-standard-s3-and-swift-rest-api-compatibility.mdx`). No guide gives a Swift
  endpoint, account or container form for an S3-API bucket.
- The Swift form the T030 check used is the documented one: the catalog's public object-store
  endpoint `https://storage.<region>.cloud.ovh.net/v1/AUTH_<project id>/<container>`
  (`pcs-capabilities-and-limitations.mdx` 41; `public-cloud/cross-functional/managing-tokens.mdx`
  129). The check took that endpoint from the Keystone catalog (T030). So T025's 404s are
  believed not to be an endpoint or account mistake, and they fit the documented split. This is
  an inference: no guide states that an S3-API bucket name answers 404 on Swift.
- The API's Swift container calls are separate from the S3 bucket calls: `POST`, `GET` and
  `DELETE /cloud/project/{serviceName}/storage[/{containerId}]` ("Create/Get/Delete SWIFT
  container", `api/v1/cloud.json` 61978, 62014, 62135; actions
  `publicCloudProject:apiovh:storage/create|get|delete`). The provider's `ovh_cloud_project_storage`
  makes an S3-compatible bucket (terraform-provider-ovh `docs/resources/cloud_project_storage.md` 8).
  The provider has no Swift container resource (`docs/resources/`, listed).
- Swift access is granted by role and by container ACL. In the ACL guide, a user with the
  `ObjectStore operator` role creates containers, and a user with no role gets 403 until a
  container ACL admits it (`pcs-acl.mdx` 42-44, 100-170). Whether a second `objectstore_operator`
  user of the same project reaches another user's container is not stated: UNVERIFIED, believed
  yes.

Decision: on the Keystone/Swift plane, C005.1 is **not applicable** to S3-API buckets on the
documented route (the catalog's `storage.<region>` endpoint and the project's `AUTH_` account).
The docs support this only by omission, and other routes are not excluded (UNVERIFIED), so the
probe still has to show it for that route. T040–T041
add a positive control: per tenant, a Swift-native container that the check creates and deletes
with the sandbox admin through the container calls above. The same token and endpoint that got
404 for the S3 bucket names must succeed on the tenant's own container (list, write, read). Then
the S3-bucket steps read `not-applicable` and cite this decision. Without that control they stay
`blocked`. The cross-container Swift-native steps are recorded as observations, not as a C005.1
verdict, because state never lives in Swift. A `reach` there is reported as a Swift-plane fact for
ADR-0018's route inventory. Rejected: probing other host or account forms (`s3.<region>…`, other
`AUTH_` spellings). No source documents them for S3-API buckets, and each costs a live run.

## Decision: `iam/resource/edit` stays blocked by design: no owned target
Sources: kb `api/v2/iam.json` and `api/v1/cloud.json` (fetched 2026-10-06); kb mirror
ovhcloud-docs 5c9f23e `docs/en/guides/account-and-service-management/account-information/iam-policies-api.mdx`.
Read 2026-10-20.

- `account:apiovh:iam/resource/edit` guards exactly one call: `PUT /iam/resource/{resourceURN}`,
  "Update an existing resource" (`iam.json` 862-919). Its long description adds that adding a
  tag also needs `$resourceType:apiovh:iam/resource/tag/add` on the resource, and removing one
  needs `…/tag/remove` (`iam.json` 916). The dedicated tag calls, `POST /iam/resource/{urn}/tag` and
  `DELETE …/tag/{key}`, check `account:apiovh:iam/resource/tag/add|remove` (`iam.json` 1000, 1043),
  not `iam/resource/edit`. The schema names the tag action with two prefixes, `account:` and
  `$resourceType:`. Which one the PUT checks is UNVERIFIED.
- **An IAM resource group is not a target.** A group's URN type is `resourceGroup`, not `resource`
  (`iam-policies-api.mdx` 100, 123). It is edited with `PUT /iam/resourceGroup/{groupId}`, which
  checks `account:apiovh:iam/resourceGroup/edit` (`iam.json` 1131, 1230). A group the run creates
  would exercise a different action. Whether `/iam/resource/{urn}` accepts a group URN at all is
  UNVERIFIED, and believed not.
- In the project API, only the project model carries IAM resource metadata (`cloud.ProjectWithIAM`,
  `cloud.json` 67494, the one `…WithIAM` model in that file). Buckets, users, Swift containers and
  OAuth2 clients have none, which agrees with T027's empty bucket listing. The IAM-bearing models
  the run could create elsewhere are an OKMS secret or service key (`api/v2/okms.json`) or a new
  project (`api/v2/publicCloud.json`). Both need an order or a KMS that spec 003 postpones (D85).
  Their cost is UNVERIFIED.
- No call answers the question without a write. `POST /iam/resource/{urn}/authorization/check`
  reports what the caller may do on a URN the caller names (`iam.json` 928). The question is which
  URN the real PUT evaluates.

Decision (coordinator, 2026-10-20): `iam/resource/edit` **stays blocked by design: no owned
target**. T015 binds no floor denial for it. Tagging the retained project with a run-named key was
considered and refused, for two reasons:

- it would undo the guard the 2026-10-16 spec-set review added (loop.md *Triage* R2);
- the chain's `project-tags` assertion reads the same project's tags
  (docs/reference/verification-status.md, D109). A probe writing them concurrently could break
  that assertion. The PUT may also replace the whole set (UNVERIFIED; `iam.json` 916 names only
  the tag actions it needs), and the schema offers no conditional PUT.

The question stays open until an owned IAM resource exists in scope, for example an OKMS service
key once OKMS is no longer postponed (D85; cost UNVERIFIED).

## Upstream input trace
Pinned upstream inputs in ../../docs/reference/upstream-reference-map.md inform T009–T014 encryption/backend and T015–T020 IAM/issuer probes. Record source paths/revision with actual captures; do not copy broad actions or infer two-project proof from within-project fixtures.
