# Retained infrastructure — spec 007

- Written: 2026-10-19 (T021), before the final chain (T022). Requirement: FR-011; shape: data-model.md
  *Retained-infrastructure list*. Account: the first sandbox account, the one spec 007's runs target
  (window.md *Second account*: no spec 007 run targets the D121 account, which keeps its own list).
- What stays after the trial voucher expires (2026-10-24 12:42 +02:00, window.md *Credit and
  bounds*): only the rows below. After that time any monthly cost falls on the account's other
  credit or the card (believed: the voucher ends then, window.md; how OVHcloud bills after a voucher
  expires is UNVERIFIED).
- Status: `not-run` against the account. The rows are derived from the code and the manifest on
  `main` at `4a7e493` (verified 2026-10-19 by reading the cited lines). No run record was read and no
  API call was made: the *Run record* column and the *Blockers* table are the coordinator's to fill
  from the latest record in the live clone before T022 starts, and T023 checks them against the
  account.

## Blockers

A resource that a probe or a chain's ephemeral stack created is never a row below. While one exists,
T022 does not start and this spec cannot complete, until a clean leftover check shows it gone
(FR-011; window.md L11). A listing error in a record is written here as "not clean", never as an
empty kind.

| # | Blocker | Source | Status | Cleared by |
|---|---|---|---|---|
| B1 | 9 pre-baseline probe run directories (state and `probe.env`, no baseline) in the maintainer's probe state; whether their states still hold resources is UNVERIFIED | loop.md *Triage*, rows "L T014" and "L T015" | **open** | window.md row 0 (owner: `--cleanup <run-id> --accept-lost-baseline` per run), then a clean leftover check recorded here |
| B2 | The latest run record's leftover check has not been read into this file: T019's if it ran, else the latest chain's (`20261016T082140Z-1797`, research.md *Runs planned*) | this file | **closed 2026-10-19 (coordinator)** | latest chain `20261016T202424Z-b207` (reviewed main `3799e31`, a later run than research.md names): `leftovers.json` has no leftovers and no listing errors, so the leftover check passed. The run's only failed assertion was `state-bucket-object-lock` on `lz-bkt-state` (403 until the owner re-applies the bootstrap, T048). Re-read after T019 and T022 |

## Retained instances

The live lane's stage table marks four stages `retained` and two `ephemeral`
(`tools/internal/stacks/stages.go:24-35`). The manifest instantiates each once
(`stacks/deployments.yaml:43-48`):

| Instance | Stage | Chain | State |
|---|---|---|---|
| `account-bootstrap` | `bootstrap` | retained (`stages.go:24`) | local backend, under the account's config directory (not read) |
| `account-governance` | `account-governance` | retained (`stages.go:27-28`) | account bucket `lz-bkt-state` |
| `demo-state` | `tenant-state` | retained (`stages.go:25-26`) | account bucket `lz-bkt-state` |
| `demo-dev-project` | `project` | retained (`stages.go:30-31`) | tenant bucket `lz-demo-bkt-state` |
| `demo-dev-gra11-network` | `project-network` | **ephemeral** (`stages.go:32-33`): never a row | tenant bucket |
| `demo-dev-gra11-runtime` | `runtime` | **ephemeral** (`stages.go:34-35`): never a row | tenant bucket |

No verb destroys a retained instance: `destroy` refuses one (`tools/internal/live/chain.go:575-576`),
and the state buckets carry a literal `prevent_destroy` (`modules/object-storage-protected/main.tf:29-31`).
Every removal path below is therefore manual (docs/how-to/run-the-first-slice.md *Destroy*).

## Rows

One row per managed resource (data-model.md *Retained-infrastructure list*), plus the project. Names
follow the naming module's version-1 template (`modules/naming/variables.tf:55-69`: segments org,
tenant, environment, region, kind, role; `-`; lower case) and match the captured plans in
`tests/fixtures/outputs/captures/` (bootstrap, tenant-state, account-governance). The project
appears by its ref, never its id. Owner of every row: the maintainer.

Costs: IAM objects (OAuth2 clients, policies, groups, resource tags) are free: "with your OVHcloud
account, you get the IAM feature at no cost" (docs.ovhcloud.com public-cloud/cross-functional/
whats-is-landing-zone.mdx:49). Object Storage is billed per GB per month (same docs,
public-cloud/cross-functional/analyze-billing.mdx:153); the S3 Object Storage price per GB is not in
the local mirror: UNVERIFIED. The buckets' size, every retained version included, is not read
here: UNVERIFIED. Believed small (state files and versions of a handful of stacks), so the believed
bucket cost is near 0 € a month; T023 reads the actual usage.

| # | Kind | Name | Owning stack | Why kept | Removal path | Monthly cost | Run record |
|---|---|---|---|---|---|---|---|
| R1 | Public Cloud project | ref `DEMO_DEV` (= `STATE` under KD-1, `stacks/deployments.yaml:18-20,32`) | none: referenced, not managed (`project.mode = reference`, `deployments.yaml:32`; a `data` source only, `modules/cloud-project/main.tf:29-30`) | the trial project: holds both state buckets and the S3 users | after R2–R18, before R19–R20: Control Panel, Public Cloud project, *Delete project*; it stays suspended up to 7 days and a final invoice is issued (docs.ovhcloud.com public-cloud/cross-functional/delete-a-project.mdx:11, :31, :42) | UNVERIFIED (believed 0 € with only R2–R12 in it) | `b207`: retained, not a leftover |
| R2 | Object Storage bucket, versioned, Object Lock governance 7 days | `lz-bkt-state` (region `gra`, `deployments.yaml:19-23`) | `account-bootstrap` (`stages/bootstrap/main.tf:5-13`) | account state: `account-governance`, `demo-state`, the bootstrap's published outputs | empty it, then `DELETE /cloud/project/{serviceName}/region/{regionName}/storage/{name}` (API schema cloud.json) or Control Panel. Every version stays locked for 7 days in governance mode (`components/state-backend/variables.tf:47-51`, `modules/object-storage-protected/main.tf:21-27`); only a credential with `s3:BypassGovernanceRetention` sending `--bypass-governance-retention` deletes one early (docs.ovhcloud.com storage-and-backup/object-storage/s3-managing-object-lock.mdx:57-59, :241-255). No state user holds it (`components/state-backend/main.tf:21-33`): create a one-off S3 user with it on this bucket and delete that user after, or wait out the retention (run-the-first-slice.md *Destroy*). Remove `prevent_destroy` only in a throwaway copy, never on `main`. Whether this bucket was created with Object Lock (it is set only at creation, `object-storage-protected/main.tf:18-19`) is UNVERIFIED until the `state-bucket-object-lock` assertion of T022 | storage per GB and size: UNVERIFIED; believed near 0 € | `b207`: retained, not a leftover |
| R3 | S3 user (role `objectstore_operator`) | `lz-s3u-state-platform` | `account-bootstrap` (`stages/bootstrap/main.tf:12`; `modules/object-storage-user/main.tf:12-16`) | the platform's access to `lz-bkt-state` | after R4: `DELETE /cloud/project/{serviceName}/user/{userId}` (API schema cloud.json) | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R4 | S3 credential of R3 | (access key; no name) | `account-bootstrap` (`object-storage-user/main.tf:22-27`) | R3's S3 access | revoke: `DELETE /cloud/project/{serviceName}/user/{userId}/s3Credentials/{access}` (API schema cloud.json) | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R5 | S3 policy of R3 | (inline on R3) | `account-bootstrap` (`object-storage-user/main.tf:29-75`) | confines R3 to `lz-bkt-state` | the API has no DELETE for it (`/cloud/project/{serviceName}/user/{userId}/policy`: GET, POST in cloud.json); that it goes with R3 is UNVERIFIED | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R6 | Object Storage bucket, versioned, Object Lock governance 7 days | `lz-demo-bkt-state` | `demo-state` (`stages/tenant-state/main.tf:6-15`) | tenant `demo`'s state: `demo-dev-project` and the ephemeral stacks' states | as R2 (same module and retention) | as R2 | `b207`: retained, not a leftover |
| R7 | S3 user | `lz-demo-s3u-state-tenant` | `demo-state` (`stages/tenant-state/main.tf:14`) | tenant access to `lz-demo-bkt-state` | as R3, after R8 | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R8 | S3 credential of R7 | (access key) | `demo-state` | R7's S3 access | as R4 | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R9 | S3 policy of R7 | (inline on R7) | `demo-state` | confines R7 to `lz-demo-bkt-state` | as R5 | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R10 | S3 user | `lz-demo-s3u-state-platform` | `demo-state` (`stages/tenant-state/main.tf:14`) | platform access to `lz-demo-bkt-state` | as R3, after R11 | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R11 | S3 credential of R10 | (access key) | `demo-state` | R10's S3 access | as R4 | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R12 | S3 policy of R10 | (inline on R10) | `demo-state` | confines R10 to `lz-demo-bkt-state` | as R5 | UNVERIFIED (believed 0 €) | `b207`: retained, not a leftover |
| R13 | IAM resource tags on the project | the `lz:` labels of tenant `demo` on `DEMO_DEV`'s URN | `account-governance` (`components/identity/ovh-native/main.tf:122-138`) | ownership labels on the project | `DELETE /iam/resource/{resourceURN}/tag/{key}` per key (API schema iam.json v2), or gone with R1 | 0 € (IAM, cited above) | `b207`: retained, not a leftover |
| R14 | OAuth2 service account (client credentials) | `lz-sa-platform-deployer` | `account-governance` (`main.tf:80-84`) | platform deployer | revoke by `DELETE /me/api/oauth2/client/{clientId}` (API schema me.json); its secret sits in that stack's remote state | 0 € (IAM) | `b207`: retained, not a leftover |
| R15 | IAM policy | `lz-pol-platform-deployer` | `account-governance` (`main.tf:86-93`) | binds R14 to the tenant project URNs | `DELETE /iam/policy/{policyId}` (API schema iam.json v2) | 0 € (IAM) | `b207`: retained, not a leftover |
| R16 | OAuth2 service account | `lz-demo-sa-deployer` | `account-governance` (`main.tf:95-100`) | tenant `demo`'s deployer | as R14 | 0 € (IAM) | `b207`: retained, not a leftover |
| R17 | IAM policy | `lz-demo-pol-deployer` | `account-governance` (`main.tf:102-110`) | binds R16 to `DEMO_DEV`'s URN | as R15 | 0 € (IAM) | `b207`: retained, not a leftover |
| R18 | Identity group, role `NONE`, no members | `lz-demo-grp-tenant` | `account-governance` (`main.tf:112-118`) | tenant `demo`'s group | `DELETE /me/identity/group/{group}` (API schema me.json) | 0 € (IAM) | `b207`: retained, not a leftover |
| R19 | OAuth2 service account (the admin) | `lz-sandbox-admin` | bootstrap, `admin` phase (`tools/internal/live/bootstrap.go:102-116`); on this account first created by the one-off config outside the repository (AGENTS.md *Sandbox credentials*) | the admin every live verb runs as; exempt by id from the leftover check (`tools/internal/live/leftovers.go:42-67`) | by the owner in the Control Panel (IAM), not by the admin itself: after R20 the admin holds no right to delete itself; then delete the local `sandbox.env` and the one-off bootstrap state, which holds its secret | 0 € (IAM) | `b207`: retained, not a leftover |
| R20 | IAM policy of R19 | `lz-sandbox-admin` (same name; id `LZ_ADMIN_POLICY_ID` in account.env) | bootstrap, `admin` phase (`bootstrap.go:112-116`) | grants R19 `account:apiovh:iam/*`, `account:apiovh:me/*`, `publicCloudProject:apiovh:*` | by the owner in the Control Panel (IAM), with R19 | 0 € (IAM) | `b207`: retained, not a leftover |

The project stack (`demo-dev-project`) manages no resource on this manifest: `budget_alert` and
`quota_guard` are off (`stacks/deployments.yaml:36-37`), so the alert and quota resources have count 0
(`modules/cloud-project/main.tf:43-44`, `modules/cloud-quota/main.tf:6,11-12`). If its state lists
any managed resource, that is a row the coordinator adds with a removal path, not a silent extra.

## Credentials to revoke on removal

Never written here, only where they live: the admin's client secret (`sandbox.env`, R19); the
deployers' client secrets (remote state of `account-governance`, R14, R16); the three S3 credentials
(states of `account-bootstrap` and `demo-state`, R4, R8, R11); the state passphrase
(`state-passphrase.env`, `tools/internal/live/bootstrap.go:425-458`), which is useless once R2 and R6
are gone. The bootstrap's temporary root keys are not retained: its `revoke` phase revokes the
consumer key and the application is deleted by hand (docs/how-to/set-up-a-fresh-account.md:78-86;
for this account, AGENTS.md *Sandbox credentials*). That none is left live is UNVERIFIED (T023).

## Removal order

R19 and R20 are what can remove everything else, so they go last, by the owner in the Control
Panel; R1 deletes what is inside it, so it goes just before them. Order: R13–R18 (IAM), then each
S3 credential before its user (R4, R3; R8, R7; R11, R10; the policies R5, R9, R12 with their users),
then R2 and R6 (emptied under the governance bypass, then deleted), then R1, then R20 and R19 and
the local credentials. Not run in spec 007: the account is kept (FR-011 lists what stays).
