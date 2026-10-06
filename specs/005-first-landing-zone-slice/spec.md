# Feature Specification: First landing-zone slice
Feature: `005-first-landing-zone-slice` · Created: 2026-10-06 · Revised: 2026-10-06 (round-1 spec-set review, D88; round-2 review) · Status: draft
ADRs: 0002, 0003, 0004 (reworded 2026-10-06, Proposed), 0005, 0006, 0007, 0008, 0009, 0017, 0018, 0024
Input: operator decisions D83/D85/D87/D88 (2026-10-06), the 2026-10-06 cost/bootstrap update and
constitution 1.3.0.

## Why and scope
Start writing real OpenTofu. Deliver a working, testable PoC of the ADR-0004 stage chain in the
maintainer sandbox: names and labels, stage modules, Terramate stacks generated from a minimal
`deployments.yaml`, one state per stack, offline tests, and an owner-run live apply→destroy chain.
This is a step towards large, multi-tenant organisations, not the destination: every choice must
leave room for more tenants, environments, regions, runtimes, identity providers, self-service,
GitLab and TACOs (see *Growth seams*).

In: naming module (default pattern), mandatory labels, `modules/` → `components/` → `stages/` →
generated stacks, the stack set below, strict `deployments.yaml` v1alpha1, a minimal reconciler,
S3 state with lockfile and client-side encryption, platform/tenant deployer service accounts, a
host-only live lane with a tested run core (guard, deadline, inventory, destroy-on-exit, leftover
check) that also runs the live probes, a scripted re-runnable account bootstrap, L0/L1/L2-lite
offline tests, an offline output-exchange integration and one L7 live chain.

## Stack set (D85: more stacks from the beginning; D88: admin by script, `tenant-state` per tenant)

| Stage | In/out | What it holds | Authority (runner) | Live chain | Why |
| --- | --- | --- | --- | --- | --- |
| `account-admin` | **out** (reserved) | — the `lz-sandbox-admin` service account and its IAM policy are created by `task bootstrap:account` through the OVHcloud API, outside OpenTofu state (D88) | — | — | No stack can run as a principal it must first create; the name stays reserved in the schema enum |
| `bootstrap` | in | the versioned account state bucket in the state project (protected) and the platform S3 user scoped to it; local encrypted state | `bootstrap` (`lz-sandbox-admin`) | retained | State home for account stacks; never re-applied to add a tenant |
| `tenant-state` | in, one per tenant (account scope) | the tenant's versioned state bucket (protected), the tenant S3 user and a platform S3 user, each scoped to that bucket only; state in the account bucket | `bootstrap` | retained | Adding a tenant adds a row, not a change to `bootstrap` (D88) |
| `account-governance` | in | platform-deployer and tenant-deployer service accounts, their IAM policies, a tenant IAM group (no members) | `bootstrap` | retained | Two credential classes from the first run; shared by all tenants, so never destroyed by a tenant chain (D88) |
| `account-fabric` | **out** | — | — | — | Nothing real and cheap is needed: new projects ship with a vRack (`ovhcloud-docs/.../network-services/vrack.mdx:196`), audit sinks need paid LDP. Not in `deployments.yaml`; name reserved |
| `project` | in | ADOPT the existing sandbox project (import, `prevent_destroy`, `deletion_protection`), labels via IAM resource tags, optional budget alert, optional quota guard | platform deployer | retained | One project per tenant × environment (ADR-0005) |
| `project-network` | in | one private network and one subnet, no gateway | tenant deployer | ephemeral | Cheapest real network; regional |
| `runtime` | in | `managed-only` runtime: one empty, labelled Object Storage bucket | tenant deployer | ephemeral | Cheapest real runtime (ADR-0017 `managed-only`); consumes `project` only, not `project-network` |
| `observability` | **out** | — | — | — | LDP is billed and no consumer exists. Not in `deployments.yaml`; name reserved |

The sandbox manifest yields six stacks: `bootstrap`, `tenant-state` (demo), `account-governance`,
`project`, `project-network`, `runtime`.

## User Scenarios & Testing

### User Story 1 — Names, labels and the first stage, offline (Priority: P1)
A contributor changes the naming template or the bootstrap stage and gets green or red feedback
with no cloud access. Given the default template and a reordered template, names are stable and
every taggable resource carries the mandatory labels; an over-long name, a forbidden character or
an override of a mandatory label key is rejected.
**Independent test**: V001, V002 on `modules/naming`, `modules/object-storage*`,
`components/state-backend`, `stages/bootstrap`; V003.

### User Story 2 — Every in-scope stage planned offline with typed output contracts (Priority: P1)
A contributor runs the full offline suite and sees each stage plan under mocked providers, publish
schema-valid outputs and refuse unsafe input (an IAM-writing tenant policy, an unprotected adopted
project or state bucket, a secret in a published output).
**Independent test**: V002 for every slice directory, V003, V004.

### User Story 3 — Stacks generated from `deployments.yaml`, planned offline, outputs exchanged (Priority: P2)
A platform maintainer adds a tenant × environment × region row and gets new stack directories with
generated backend, provider, module call and labels, ordered by real dependencies, each planning
offline against fixture inputs; a producer's published outputs reach its consumer as typed inputs.
A stale generated file, an orphan directory or a changed `instance_id` fails the freshness check.
**Independent test**: V005, V006 (including the two-tenant growth and tenant-only fixtures), V012.

### User Story 4 — Scripted, re-runnable account bootstrap (Priority: P2)
The maintainer bootstraps (or re-bootstraps) an OVHcloud account with one command: account binding,
passphrase file, admin service account, account state bucket and local credential files. A second
run changes nothing; a fresh account follows the same command, which makes it the account-migration
path; the previous account's files cannot be picked up by mistake.
**Independent test**: V008 offline; V009 owner session.

### User Story 5 — Live chain apply→destroy in the sandbox (Owner session, Priority: P3)
The maintainer runs the chain from the workstation; it applies in dependency order with the right
credential per stack, refuses any plan that would delete or replace retained infrastructure,
asserts real behaviour, destroys ephemeral stacks in reverse order even on failure or deadline,
checks every created resource kind for leftovers and reports what to record as cost and which known
deviation was observed.
**Independent test**: V007 offline; V010 owner session.

### Edge cases
Empty `deployments.yaml`; duplicate `instance_id`; two rows mapping to one path or state key;
region given for an environment-scoped stage; a stage not implemented in this slice (including the
reserved `account-admin`); two runtimes in one scope without distinct `slot`; producer has no
published outputs yet (consumer plan reports `blocked`, no placeholder); a published artefact that is
missing, malformed or from the wrong producer (refused); consumed outputs changed since the
consumer's last apply; a plan that deletes or replaces a retained resource (refused before apply);
interrupted apply (SIGINT) mid-chain; deadline reached mid-chain; destroy failure of one stack
(others still attempted, exit non-zero); leftover check cannot list a kind, or finds a resource of
the slice that is absent from every state (fail, not pass); `ovhcloud` missing; ambient `OVH_*` or
`AWS_*` variables in the caller's shell (ignored, never inherited); credentials file missing or
world-readable; passphrase file exists (never overwritten); bucket name taken globally; import of the
project would replace it; the run happens inside `lz-offline`, from a linked worktree, a dirty tree
or a commit other than the reviewed SHA (refused); the bound account differs from the credential's
account (refused); a fresh-account run fails or is aborted after the root keys were entered (root
credential revoked, retry resumes); a probe's destroy fails (state and passphrase kept for
`--cleanup`); the script-created admin client and policy, outside every state (exempt by id only); two runs touching account stacks, or one tenant, at once (second refused).

## Premises

Evidence column: *doc* = primary source in the local KB, not observed; *UNVERIFIED* = no source or
not observed. Probes run in T007 (offline captures) and T009/T010 (owner session, through the run
core of T055).

| ID | Premise | Source | Probe | Status |
| --- | --- | --- | --- | --- |
| P1 | OVH Object Storage supports the OpenTofu 1.13 S3 backend with `use_lockfile = true` (conditional writes) | `ovhcloud-docs/.../storage-and-backup/object-storage/s3-conditional-writes.mdx:11,85,94`; `.../hosted-private-cloud/cloud-platform/snc-cloud-platform-terraform.mdx:302-303` (Terraform, not OpenTofu) | T010: the second `tofu plan` on one key starts only after the first one's lock object is observed; it is refused with a lock error; lock object gone after | UNVERIFIED |
| P2 | Backend options needed for OVH (`endpoints.s3`, `skip_credentials_validation`, `skip_region_validation`, `skip_requesting_account_id`, `skip_s3_checksum`) work with OpenTofu 1.13 | `.../public-cloud/compute/use-object-storage-terraform-backend-state.mdx:109-125` (Terraform) | T010 init/plan/apply on a probe key | UNVERIFIED |
| P3 | Client-side state encryption (PBKDF2 passphrase, `encryption {}`) works with the S3 backend and lockfile on OVH; conditional writes accept the encrypted object | ADR-0009 context (2026-10-01 note); conditional writes need unencrypted or SSE-S3 objects server-side (`s3-conditional-writes.mdx:85`), client-side encryption is opaque to the server | T010: downloaded state object is an encryption envelope, not plaintext JSON; lock still works | UNVERIFIED |
| P4 | OpenTofu 1.13 evaluates variables in `encryption` and `backend` blocks early (passphrase from `TF_VAR_`, local state path outside the repo) | none in KB | T007 offline capture with local backend | observed offline 2026-10-06 (T007, `tests/fixtures/tofu-probes/captures/p4-encryption.txt`): backend path and passphrase from `TF_VAR_`, state at the variable path is an encryption envelope, a wrong passphrase is refused |
| P5 | Importing the existing sandbox project into `ovh_cloud_project` (required `ovh_subsidiary`, `plan`) yields no replacement; `prevent_destroy` + `deletion_protection` hold | `terraform-provider-ovh/docs/resources/cloud_project.md` (Import, `deletion_protection`) | T009: plan-only with an import block and `-generate-config-out`; no apply | UNVERIFIED; fallback: data source + `ovh_iam_resource_tags` (research R7) |
| P6 | An `import` block in a generated root can target a nested module address | none in KB | T007 offline (mock) and T009 plan | observed offline 2026-10-06 with `terraform_data`, single and keyed module (T007, `captures/p6-import.json`); the mock method is refuted: `tofu test` crashes on an import into a resource of the mocked provider, for `plan` and `apply` (exit 11, `captures/p6-import-mock.txt`; research R23); `ovh_cloud_project` UNVERIFIED until T009 |
| P7 | `lz-sandbox-admin` (policy `account:apiovh:iam/*`, `account:apiovh:me/*`, `publicCloudProject:apiovh:*`, AGENTS.md) may create OAuth2 clients and IAM policies | `me_api_oauth2_client.md`, `iam_policy.md` | T010: create and delete a probe client and policy | UNVERIFIED |
| P8 | An OAuth2 service account is usable as an IAM policy identity via its `identity` URN | `me_api_oauth2_client.md` ("Identity URN … to be used inside an IAM policy") | T010 probe policy grants one read; call succeeds, a non-granted call is denied | doc; UNVERIFIED live |
| P9 | The narrowed tenant-deployer allowlist (research R6: `network/private/{create,get,edit,delete,region/create}`, `network/private/subnet/{create,get,delete}`, `region/storage/{create,get,edit,delete,bulkDeleteObjects}`, plus reads the provider needs, each added only on observed evidence) is sufficient for `project-network` and `runtime` apply and destroy, and denies IAM writes | `kb/api/v1/cloud.json` action names (verified 2026-10-06; `region/storage/*` also holds `policy/create`, `presign`, `object/*`, `lifecycle/*`, `job/replication/*`, which are excluded) | T010 positive create→destroy as a probe tenant identity; V010 chain (positive) and negative IAM write | UNVERIFIED |
| P10 | `ovh_cloud_project_alerting` works on the trial project | `cloud_project_alerting.md` | T009 plan; T010 apply+destroy | UNVERIFIED; optional |
| P11 | `ovh_cloud_quota` `prevent_automatic_quota_upgrade` can be set on the sandbox project | `cloud_quota.md`; `api/v2/publicCloud.json:5530` | T009 plan only | UNVERIFIED; optional, default off |
| P12 | A private network + subnet without gateway or instances creates without explicit vRack management and costs nothing measurable | vRack free and auto-delivered with new projects (`.../network-services/vrack.mdx:196,201`); network price: none | T010 create/destroy in GRA11 | UNVERIFIED (cost, vRack precondition) |
| P13 | An empty or KB-sized Object Storage bucket costs ~0 (order of 0.01 EUR/GB/month) | indirect: `.../ai-machine-learning/ai-notebooks-billing.mdx:116` | Control Panel after V010 | UNVERIFIED |
| P14 | Destroying `ovh_cloud_project_storage` removes objects first; noncurrent versions too | `cloud_project_storage.md` (warning: "will try to remove all objects") | T010 versioned probe bucket with two versions | UNVERIFIED (versions) |
| P15 | Tag keys with `:` (e.g. `lz:tenant`) are accepted on IAM resource tags and S3 bucket tags | IAM: `iam_resource_tags.md` key pattern `^[a-zA-Z0-9_.:/=+@-]{1,128}$`; bucket tags: none | T010 | doc (IAM); UNVERIFIED (bucket) |
| P16 | `lifecycle { ignore_changes = [tags["lz:run-id"]] }` keeps the creating run id stable on later applies | OpenTofu semantics, none in KB | T007 offline (tofu test apply against mock twice) | observed offline 2026-10-06 under `mock_provider "ovh"` (T007, `captures/p16-ignore-changes.jsonl`); another tag still follows the configuration; live provider UNVERIFIED |
| P17 | Terramate 0.17.3: `terramate create --id --tags --after`, `generate_hcl`, stack `after` with tag filters, `terramate list --run-order` | none in KB | T007 offline captures from the pinned binary | observed offline 2026-10-06 (T007, `tests/fixtures/terramate/captures/p17-terramate.txt`), including AND tag filters (`tag:a:b`; an OR reading would have made a cycle); outside git the root config needs `required_version` (research R23) |
| P18 | `ovhcloud` CLI 0.15.0 lists, in machine-readable form and with pagination handled, every kind of the leftover matrix (research R12): buckets, private networks, subnets per network, OAuth2 clients, IAM policies, identity groups, cloud project users, S3 credentials per user | AGENTS.md (install line only); API paths exist in `kb/api/v1/cloud.json`, `me.json`, `api/v2/iam.json` | T009 capture with read-only calls, one per kind | UNVERIFIED; fallback per kind: API listing through the Go client, decision recorded |
| P19 | (withdrawn) An S3 user policy can scope access to a key prefix of one bucket | `cloud_project_user_s3_policy.md` (bucket-level ARNs shown) | — | not needed (D87: one state bucket per tenant; tenant S3 users are scoped by bucket ARN) |
| P20 | Features used exist in the pinned provider `ovh/ovh` 2.21.0, though the KB docs are 2.22.0-era | `terraform-provider-ovh/CHANGELOG.md:1,31` (`discard_client_secret` is 2.22.0, line 15) | L0 `tofu validate` against the mirrored 2.21.0 provider | doc; `discard_client_secret` excluded |
| P21 | Bucket names are unique across OVHcloud | `.../object-storage/s3-limitations.mdx:49-53` | — | doc; template carries an org discriminator |
| P22 | A fresh trial account has, or lets the owner create in the Control Panel, one Public Cloud project | none | T045 | UNVERIFIED; project ordering stays manual |
| P23 | Root application credentials (AK/AS/CK) can create an OAuth2 client (`POST /me/api/oauth2/client`), an IAM policy (`POST /iam/policy`, API v2), read the caller's account (`GET /auth/details`, P26) and revoke their own credential (`GET /auth/currentCredential`, `DELETE /me/api/credential/{credentialId}`) | `kb/api/v1/me.json`, `kb/api/v2/iam.json`, `kb/api/v1/auth.json` (paths and methods present) | T045 fresh-account run; the current sandbox (T044) exercises only `GET /auth/details` and the admin client/policy reads | doc; UNVERIFIED live |
| P24 | `tofu apply -json` (1.13) emits one `apply_complete` event per resource with its address and id, early enough to append it to the run inventory before the next resource | OpenTofu machine-readable UI, none in KB | T007 offline capture on a provider-free root with `terraform_data` resources | observed offline 2026-10-06 (T007, `captures/p24-apply-json.jsonl`, read by `TestInventoryCapturedP24`): a dependant starts after its dependency's `apply_complete`, independent resources interleave; fallback kept: `tofu state list` after each stack plus external listing reconciliation (inventory then lags by one stack) |
| P25 | IAM policy `conditions` on resource tags narrow `region/storage/*` actions to buckets carrying a tenant tag | `iam_policy.md:95` ("Conditions restrict permissions based on resource tags …"); evaluation of bucket tags for actions on the project URN: none | T010 optional probe on two probe buckets | UNVERIFIED; if observed, narrows KD-1 but does not close it |
| P26 | `GET /auth/details` returns the caller's account for every credential class (root keys, admin, an OAuth2 client holding only project actions), with no IAM action; `GET /me` needs `account:apiovh:me/get`, which neither deployer policy grants | `kb/api/v1/auth.json` (`/auth/details`: no `iamActions`, `auth.Details.account`); `kb/api/v1/me.json:23` (`account:apiovh:me/get` required) | T009 (admin), T010 (P9-allowlist probe identity, `/me` denied), V010 (both deployers) | doc; UNVERIFIED live; fallback: grant `account:apiovh:me/get` on the account to both deployer policies (research R13) |

A refuted premise blocks the clauses that rely on it and is recorded in `research.md` with the
fallback taken; it never turns into a silent pass. A fallback that changes a requirement (for
example local state instead of FR-008's S3 backend) revises this spec and its checks before the
dependent task closes.

## Known deviations

| ID | Deviation | Why accepted | Effect on claims | Lifted when |
| --- | --- | --- | --- | --- |
| KD-1 (D88) | The sandbox has one Public Cloud project, which holds both tenant resources and every state bucket. A tenant deployer's OAuth2 authority on that project includes `region/storage/delete` and `bulkDeleteObjects` (`kb/api/v1/cloud.json:58903,59095`), so it can reach state buckets through the management API even though its S3 credentials cannot. ADR-0009 assumes state lives where tenant authority does not reach | Operator answer "Known sandbox deviation" (D88): one trial project; a second project is a separate order | Tenant isolation of state is **not** demonstrated in this slice; S3-level scope (G6) and IAM-write denial are. `spec.state.project` must differ from every tenant project unless `spec.sandbox.shared_state_project: true`; the tenant allowlist is the narrowest the API allows (P9); V010 runs a negative that shows the gap on a disposable canary bucket and reports it as `known-deviation KD-1`, never as `pass` | A separate state project exists; the flag is removed and the same V010 negative must then pass |
| KD-2 | Live runs start from the maintainer's workstation, not a protected pipeline (ADR-0019) | AGENTS.md known deviation; no pipeline lane yet | Guarded by FR-011's host guard | Protected-branch CI with sandbox credentials |
| KD-3 | The platform-owned `project` stack keeps its state and `artifacts/` object in the tenant's state bucket, which the tenant S3 user can write (research R24). ADR-0009 scopes backend permissions to the instance's credential class | One bucket per tenant (D87) without prefix-scoped S3 policies (P19 withdrawn); moving `project` to the account bucket would cut its tenant consumers off from the artefact (G6) | A tenant credential could overwrite `project` state or publish a forged `project` artefact. The adapter refuses a `project` artefact whose project id differs from the bound account's reference (FR-005, V012), so a forged id cannot redirect consumers; tampering with the state object itself is not detected. Not claimed as isolated | A platform-only bucket (or a qualified prefix policy) per tenant holds `project` state and artefacts; trigger as KD-1 |

## Requirements

### Functional requirements
- **FR-001 Naming** — `modules/naming` is pure (no provider, no data source). Inputs: hierarchy
  coordinates (`org`, `tenant`, `environment`, `region`, `kind`, `role`, and the runtime `slot`) and a
  template object passed as data; two runtime slots in one scope get distinct names. Only the default template ships; a second template exists in tests to prove the template
  is data. Names are deterministic, independent of labels, validated against per-kind limits for
  the kinds this slice uses, and never silently truncated. An explicit name override (import) is
  passed through unchanged after validation. Supersedes 001/T013–T014 for this scope.
- **FR-002 Labels** — every resource whose API carries tags gets `managed-by=opentofu`,
  `managed-in=<forge>/<org>/<repo>//<stack path>`, `instance=<instance_id>`, `tenant=<tenant>` (empty
  key absent for account scope) and `release` (the constant `unreleased` until ADR-0010's release
  train exists, D87) under the `lz:` namespace; extra labels
  may not override these keys; `tenant` comes only from the platform manifest. Resources without
  tags are listed in an applicability table and recorded in every stage's published outputs
  (`values.unlabelled[]`). Live runs add `lz:run-id` at creation and never update it.
- **FR-003 Layering** — `modules/` (provider-thin) → `components/` → `stages/` → generated stacks.
  Modules, components and stages declare no backend, no provider configuration and never use
  `terraform_remote_state`; only generated stacks configure backend and provider. The dependency
  checker classifies every new directory and rejects any other edge; its ADR-0002 matrix is not
  widened.
- **FR-004 Stages** — the in-scope stages in *Stack set* exist as child modules with typed inputs
  and outputs. Retained infrastructure is protected in code: the adopted project and every state
  bucket carry `prevent_destroy` (state buckets through a protected storage module, since
  `prevent_destroy` cannot depend on a variable) and the project carries `deletion_protection`.
  Budget alert and quota guard are optional inputs. Tenant-deployer IAM policies grant only the
  P9 allowlist on the tenant's project URN — no `account:apiovh:iam/*` action, no
  `region/storage/policy/create`, no wildcard.
- **FR-005 Output contracts** — each stack publishes `outputs.json`: an envelope (`apiVersion`,
  `kind: StageOutputs`, `instance_id`, `stage`, `source_revision`, `values`) validated against
  `schemas/outputs/<stage>.schema.json`. Sensitive outputs are never published. A capability that does
  not exist is absent, never a placeholder. Consumers receive producer values only as typed
  variables from these files, through one adapter: each consumed producer stage maps to one object
  variable named after that stage, typed by its outputs schema; the adapter refuses an artefact
  whose `instance_id` or `stage` does not match the derived edge, or that fails validation, and a
  `project` artefact whose project id differs from the bound account's reference (KD-3).
- **FR-006 Manifest** — `stacks/deployments.yaml`, `apiVersion: lz.platformrelay.dev/v1alpha1`,
  `kind: Deployments`, strict decoding (unknown field, duplicate key, unsupported version rejected).
  Dimensions tenant × environment × region plus an optional runtime `slot`; `instance_id` is
  immutable, unique and equals the Terramate stack id; stage scope rules (account / account-tenant /
  environment / region) are enforced; dependency edges are derived from the stage table, not
  authored. `spec.scope: platform` (default) requires exactly one `bootstrap` and one
  `account-governance`; `spec.scope: tenant` forbids account rows and names the platform instances
  it consumes as `external` producers (artefacts only). `spec.state.project` must differ from every
  tenant project unless `spec.sandbox.shared_state_project: true` (KD-1).
- **FR-007 Reconciler and generation** — `task stacks:reconcile` creates a missing stack with
  `terramate create`; any other mismatch (directory without row, changed id, changed dimension) fails
  with `UNSUPPORTED_CHANGE` (retirement, rename and tombstones postponed; the seam is this error);
  a reserved or unimplemented stage fails with `STAGE_NOT_IMPLEMENTED`.
  Generation renders backend, provider, the one stage call (module source derived from
  `spec.stage_source`: a relative path now, a versioned reference as the seam), typed input
  variables, labels and (adopt only) the import block. `task stacks:check` fails on any stale
  generated file, and on two stacks planning the same bucket name (`NAME_COLLISION`).
  `stacks:reconcile` and `stacks:generate` read no credential and are not behind the host guard: they
  run in any checkout, including an authoring worktree with the uncommitted manifest edit.
- **FR-008 State** — `bootstrap` uses local encrypted state outside the repository; every other
  stack uses the S3 backend with `use_lockfile = true`, its own key and `encryption {}`: account and
  account-tenant stacks in the account state bucket, tenant stacks in their tenant's own state bucket
  (one bucket per tenant from day one, ADR-0009, D87; the account bucket created by `bootstrap`, each
  tenant bucket by its `tenant-state`). State buckets live in `spec.state.project` (KD-1 in the
  sandbox). Each stack's `artifacts/<instance_id>/outputs.json` lives in the same bucket as its
  state; a local-state root publishes to the account bucket once it exists. The `encryption {}` block
  uses a PBKDF2 passphrase from the bound account directory under `~/.config/ovh-lz/` (never
  committed), created before any encrypted state is written. Every generated backend carries
  `encryption {}`; OKMS and escrow are postponed.
- **FR-009 Order and runs** — Terramate `after` follows derived edges (data and authority); producers
  run before consumers. The selected set of a run is: stacks whose recorded code digest (stack plus
  its stage/component/module closure) changed, stacks whose consumed `outputs.json` digest — or
  resolved-reference input from the bound account (project ids, data-model) — differs from the one
  recorded at their last apply (or have no record), and the transitive data consumers
  of those; authority edges order but never select. A consumer whose producer has published nothing
  is `blocked`, not planned. Runs that touch an account or account-tenant stack hold the account
  lock; tenant stacks hold their tenant's lock; locks are taken in a fixed order and a second holder
  is refused.
- **FR-010 Credentials** — each instance names its authority (`bootstrap`, `platform`, `tenant`); the
  live lane builds each child process environment from scratch with only that authority's
  credentials (ambient `OVH_*`/`AWS_*` variables are never inherited) and refuses a mismatch. Root
  AK/AS/CK exist only inside `bootstrap:account --fresh-account`: typed at a no-echo prompt, held in
  memory, never in argv, environment of a child, file or log, and revoked at the end of that run.
  Secret values never reach stdout, stderr, logs, argv, generated files, `outputs.json`, the
  repository or a worktree; credential files are mode 600 under `~/.config/ovh-lz/` and have one
  writer (`lz-live`'s credential writer). Every credential is checked against the bound account
  before use, through a call every credential class may make (`GET /auth/details`, P26).
- **FR-011 Live lane** — `task live:plan|apply|destroy|chain -- <instance|all>` and
  `task live:probe -- <probe>` run on the maintainer host only. Before any credential loads, the
  guard requires: not inside `lz-offline`; the canonical (symlink-resolved) checkout equals the
  owner's dedicated clone named in `~/.config/ovh-lz/live.env`, has a private `.git` with no linked
  worktrees, alternates or shared object store, and lies outside `LZ_AGENT_WORKTREE_ROOT` (git
  metadata, not a path pattern; D92); a clean tree; `HEAD` equal to the externally supplied
  `--reviewed-sha` and reachable from `origin/main` (D87). `bootstrap:account` uses the same guard;
  credential-free generation (`stacks:reconcile`, `stacks:generate`) does not.
  Every run has a deadline (default 45 min, then the destroy-on-exit fires), appends each created
  resource id to its inventory as it is created, and refuses any plan that deletes or replaces a
  resource of a retained instance (`bootstrap`, `tenant-state`, `account-governance`, `project`)
  through one shared guard that every applying verb, including the bootstrap `state` phase and the
  probes, runs on a saved plan before applying exactly that plan;
  `destroy` refuses retained instances on every verb. `chain` applies, asserts, then destroys
  ephemeral instances (`runtime`, `project-network`) in reverse dependency order from a trap that
  also fires on failure, interruption and deadline. It reconciles the inventory against
  independent `ovhcloud` listings of every resource type the slice creates (the admin client and
  policy exempt by recorded id only), prints the run id, any observed
  known deviation, and a reminder to record the approximate cost in the PR. Cost guards are hygiene:
  no spend gate, no cost ledger; the budget alert is optional.
- **FR-012 Re-runnable bootstrap** — `task bootstrap:account` brings an account from empty (or any
  partial state) to a bound, bootstrapped account, in this order: `guard`; `identify` (the
  credential's account binds `~/.config/ovh-lz/accounts/<account>/account.env` with endpoint,
  account id, `org` and project references; a mismatch with an existing binding or the manifest is
  refused); `passphrase` (created once, never overwritten); `admin` (checks separately that the
  `sandbox.env` credential works and that the admin client and policy exist as expected; with
  `--fresh-account` it creates them through the API with root keys and writes `sandbox.env`);
  `state` (applies `bootstrap`, importing an existing unmanaged account bucket by name; writes
  `state.env`); `publish`; `verify` (init and plan of `account-governance`, lock round-trip);
  `revoke` (fresh account only). Each phase detects completed work and reports `unchanged`; a
  re-run on a bootstrapped account changes nothing. On a fresh account the previous account's
  `sandbox.env` moves into its own account directory and no file of the previous account is read;
  missing project references are prompted for during `identify`, so the fresh run never stops
  before the admin credential exists, and once root keys were entered they are revoked on every
  exit path.
  Deployer credential files are written by the live lane after `account-governance` and
  `tenant-state` applies, not by the bootstrap. Bucket names are global: a taken name fails with a
  clear error that names `spec.org` (default `lz`, D87) as the override.
- **FR-013 Tests** — L0 (`task lint`) and L1 (`tofu test` with `mock_provider`) for every module,
  component and stage; L2-lite plan assertions per generated stack against fixture inputs; an
  offline producer→publisher→consumer integration; one L7 live chain. Mutation proof is required
  only for security guards: credential selection, child environment, account binding and secret
  redaction; tenant isolation (tenant label, tenant policy scope, tenant state-bucket scope, KD-1
  reporting); authority (retained-infrastructure protection, IAM-write denial); host guard; locks;
  cleanup (trap, deadline, retained set, leftover check).
- **FR-014 Traceability** — `task check:specs -- specs/005-first-landing-zone-slice` traces every
  requirement and task of this spec; requirement ids do not collide with spec 001's.

### Key entities
Deployment manifest, deployment instance, stage, stack, output envelope, label set, naming
template, authority, account binding, credential file, live run record, known deviation. See
`data-model.md`.

## Success criteria
- **SC-001**: From a clean checkout, `task test:slice` passes in `lz-offline` with no network and no
  credentials; every module, component and stage directory has at least one L1 test (V002).
- **SC-002**: The sandbox manifest (1 tenant × 1 environment × 1 region) yields 6 stacks; the growth
  fixture (2 tenants × 2 environments × 2 regions, plus two runtime slots in one scope) yields unique
  ids, paths, state keys and bucket names and one state bucket per tenant; a tenant-only manifest generates in a
  separate scratch repository; all plan offline with no stage code change (V005, V006).
- **SC-003**: One owner session runs `task live:chain -- all` to completion within its deadline
  (< 60 min); six stacks apply, the two ephemeral ones are destroyed; the leftover check over the
  full kind matrix reports zero; KD-1 is reported as `known-deviation`, not hidden; run id and
  approximate cost are in the PR (V010).
- **SC-004**: A second `task bootstrap:account` on the bootstrapped sandbox reports no change; on a
  fresh account the same command yields a working state backend and revoked root keys (V009; fresh
  part blocked until an account exists).
- **SC-005**: A seeded secret never appears in any captured output, log, argv, generated file,
  `outputs.json` or committed file; each security guard of FR-013 has a killed mutant (V004, V007,
  V008).

## Acceptance and predefined verification
All commands are **planned**; creators are in `tasks.md`; evidence starts `not-run`. Offline
targets run through `lz-offline`; `live:*` and `bootstrap:account` run on the host by the owner.
Details and controls: `contracts/checks.md`.

| Check | Requirements | Verify (planned) |
| --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | `task test:unit -- modules/naming; task lint -- modules/naming` |
| V002 | FR-003, FR-004, FR-013, SC-001 | `task test:slice` |
| V003 | FR-003 | `task test:dependencies` |
| V004 | FR-005, SC-005 | `task test:outputs` |
| V005 | FR-006, FR-007, FR-008, SC-002 | `task test:stacks; task stacks:check` |
| V006 | FR-009, FR-013, SC-002 | `task test:stack-plans; task stacks:order -- all` |
| V007 | FR-010, FR-011, FR-013, SC-005 | `task test:live-lane` |
| V008 | FR-010, FR-012, SC-005 | `task test:bootstrap` |
| V009 | FR-008, FR-012, SC-004 | Owner session: `task bootstrap:account` twice; fresh-account procedure |
| V010 | FR-004, FR-008, FR-009, FR-010, FR-011, SC-003 | Owner session: `task live:chain -- all` |
| V011 | FR-014 | `task check:specs -- specs/005-first-landing-zone-slice` |
| V012 | FR-005, FR-009, FR-013 | `task test:exchange` |

## Growth seams (nothing here may block later growth)
- More tenants/environments/regions: rows in `deployments.yaml`; paths and keys are derived; a new
  tenant is a `tenant-state` row, never a `bootstrap` change.
- More runtimes per scope: the optional `slot` is part of instance identity, path, key and resource
  names (ADR-0017 composable runtime instances); the growth fixture holds two.
- Tenant onboarding authority: `tenant-state` and `account-governance` run as the `bootstrap`
  authority (`lz-sandbox-admin`, `account:apiovh:iam/*`); onboarding from CI will need a narrower
  `onboarding` authority (deployer clients, policies and S3 users with the slice prefix only). The
  seam is the stage table's authority column; nothing else changes.
- Tenant repo: a `spec.scope: tenant` manifest with `external` platform producers and a versioned
  `spec.stage_source` generates on its own; the tenant-only fixture proves it offline.
- Identity providers: `components/identity/<plane>-<source>` behind the same stage outputs; the stage
  selects the variant at one point.
- Rename/retirement: `UNSUPPORTED_CHANGE` is where tombstones plug in.
- Artefact transaction: `outputs.json` envelope + recorded consumed digests are the inputs that
  generations, fencing and waves will extend.
- OKMS, escrow: generated backend already carries `encryption {}` and a per-stack key; state buckets
  are per tenant from day one.
- State isolation: `spec.state.project` is already an input; a separate state project lifts KD-1
  without a code change.
- GitLab/TACOs: stacks are vanilla OpenTofu roots; the live lane is a Taskfile contract.

## Out of scope / postponed (with trigger)
| Item | Seam kept | Trigger to start |
| --- | --- | --- |
| Artefact transaction (generations, fencing, waves; spec 002) | output envelope, consumed-digest record | second consumer of one producer in CI, or concurrent tenant runs |
| Tombstones, retirement, rename | `UNSUPPORTED_CHANGE` | first instance removal |
| OKMS state key, escrow, backup | `encryption {}` in every backend | first non-sandbox tenant or a second maintainer |
| Separate state project (lifts KD-1) | `spec.state.project` input, V010 negative | a second project in the sandbox, or the first claim of tenant isolation |
| Narrower `onboarding` authority for `tenant-state`/`account-governance` | stage table authority column | tenant onboarding from CI |
| Admin service account in OpenTofu state | reserved `account-admin` stage name | a self-managing authority decision, or admin drift found by the bootstrap `admin` phase more than once |
| GitLab | Taskfile contract | operator request or a consumer on GitLab |
| TACOs | vanilla roots | first TACO user |
| Assent / self-service | manifest is platform-controlled | first tenant author who is not the maintainer |
| Scanner (`lz-audit`), generated docs, compliance profiles | labels, outputs, applicability table | first reference environment |
| Guided preconfiguration wizard (spec 004) | `deployments.yaml` schema | real profile schemas (D29) |
| `account-fabric`, `observability` stacks | reserved stage names | a consumer that needs vRack/audit sinks or logs |
| Cost ledger, spend admission, reaper (spec 003 T002–T008) | inventory + leftover check | spend outside the trial or shared sandbox |
| Profiles/golden paths, catalogue | stage variant selection point | second runtime variant |
| Pipeline-run live lanes (ADR-0019) | host-only `live:*` | protected-branch CI with sandbox credentials |
| Full ADR-0019 AgentEx scope (deferred, D87; constitution VI; 001/T017–T019) | existing `lz-check` diagnostics | the second vertical slice, or agents repeatedly misreading check output |

## Dependencies and stop conditions
Depends on 001/T001–T009 and T023 (done). Live tasks additionally need the owner and the sandbox
credentials in `~/.config/ovh-lz/`, and start only from the owner's dedicated clone (D92) on a reviewed
commit. Constitution 1.3.0 (D87) settles the earlier Principle V (cost guards are hygiene) and
Principle VI (AgentEx deferred) conflicts. A refuted premise stops its dependent tasks and records
the fallback.
