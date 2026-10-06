# Research: First landing-zone slice
Feature: 005-first-landing-zone-slice · 2026-10-06 · Revised: 2026-10-06 (D88; round-2 review) · Status: draft

Each entry: decision, rationale, alternatives rejected, evidence. KB paths are relative to
`~/.local/share/ovh-lz/kb/mirror/` unless they start with `api/`. Provider doc paths are
`terraform-provider-ovh/docs/resources/<name>.md`; the mirror is at provider commit `00cb8b0`
(2.22.0 era) while the repository pins `ovh/ovh` 2.21.0 (`harness/capabilities.yaml`).

## R1 Layering: keep components between modules and stages
**Decision** (kept, D87): `modules/` (one OVH product concern) → `components/` (ADR-0003 singletons and family
variants) → `stages/` → generated stacks. Components used: `state-backend` (by `bootstrap` and
`tenant-state`), `identity/ovh-native`, `project-factory`, `network/island`, `runtime/managed-only`.
**Rationale**: the dependency checker (`tools/internal/checks/dependencies.go`, `mayUse`) allows
`stage → component` only; `stage → module` and `stage → naming` are `LAYER_VIOLATION` (fixture at
`dependencies_test.go:161`). D85 says "modules → stages → stacks" and "never widen rules silently";
both hold only if components sit between them. Every component name already exists in ADR-0002/0003,
so no new vocabulary. No checker change is needed for the layer matrix.
**Rejected**: add `stage → module` to `mayUse` (a silent widening of an Accepted ADR); put resources
directly in stages (stages become untestable monoliths and break ADR-0016 rule 2's single variant
selection point).
**Cost**: thin components (often one module call plus naming). Accepted; they are where variants go.

## R2 Stack layout
**Decision** (D87): paths are derived from scope, never authored:
- account scope: `stacks/account/<stage>/`
- account-tenant scope: `stacks/account/<stage>/<tenant>/` (`tenant-state`: platform-owned, so it
  stays out of `stacks/tenants/` and never lifts into a tenant repository)
- environment scope: `stacks/tenants/<tenant>/<environment>/<stage>/` (`project`)
- region scope: `stacks/tenants/<tenant>/<environment>/<region>/<stage>/` (`project-network`, `runtime`)
**Rationale**: the path mirrors the state owner's scope. One project per tenant × environment
(ADR-0005) means `project` is not regional; a second region adds only regional stacks.
`stacks/tenants/<tenant>/` lifts into a tenant repo as is (with a `spec.scope: tenant` manifest, R22).
A runtime `slot` suffixes the stage directory (`runtime-<slot>`). The operator's suggested
`stacks/<tenant>/<environment>/<region>/<stage>/` forces a fake region on `project` and a fake tenant on
account stages.
**Rejected**: flat `stacks/<instance_id>/` (no visible ownership, harder Terramate tag filters);
`templates/tenant-repo/` from ADR-0002 (D85 chose a single self-contained repo for now).

## R3 Instance identity
**Decision**: `instance_id` is authored once, `^[a-z][a-z0-9-]{2,62}$`, immutable, unique, and is the
Terramate stack `id`. Convention at creation: `<tenant>-<env>-<region>-<stage>[-<slot>]`,
`<tenant>-state` or `account-<stage>`; after creation the id never follows renames. The path is
derived from dimensions (including the optional runtime `slot`); a dimension change is
`UNSUPPORTED_CHANGE` until the rename workflow exists.
**Rejected**: UUIDs (unreadable in labels and console); ids derived from dimensions (a rename would
change identity, which ADR-0004 forbids).

## R4 Output exchange
**Decision**: after a successful apply, the live lane converts `tofu output -json` into the
envelope (FR-005), drops every entry with `"sensitive": true`, validates against
`schemas/outputs/<stage>.schema.json` and writes it to the instance's state bucket at
`artifacts/<instance_id>/outputs.json` (ADR-0004, reworded 2026-10-06) using the producer's S3
credentials. The local-state root `bootstrap` publishes to the account bucket it has just created
(bootstrap `publish` phase, with `state.env`); every other root publishes to its own state bucket.
Before a consumer plans, the adapter (data-model *Envelope-to-input adapter*) reads each producer's
object with the consumer's S3 credentials, validates it again, checks `instance_id` and `stage`
against the derived edge, writes `{"<producer_stage_snake>": values}` to
`.local/live/<run-id>/inputs/<consumer>/<producer_stage>.tfvars.json` and passes it with `-var-file`;
generated variables are typed from the producer stage's schema. The consumer's last-applied record
(`.local/live/records/<instance_id>.json`, gitignored) holds consumed digests and the code digest
(FR-009). Offline, fixture `outputs.json` files under `tests/fixtures/outputs/` play the producer,
and V012 runs a real producer → publisher → consumer round trip on generated roots under mocks.
**Rationale**: ADR-0004 "outputs artefacts, not state reads", without generations/fencing (postponed).
The bucket keeps the exchange identical when CI replaces the workstation. One object variable per
producer stage keeps the mapping mechanical and the stage's input contract visible.
**Rejected**: Terramate outputs sharing (`sharing_backend` reads producer state through `tofu output`;
ADR-0007 §3 forbids it until spiked); committing `outputs.json` (puts sandbox ids in a public repo
and makes the live lane write to git); `terraform_remote_state` (exposes whole state; FR-003); one
flat variable per value (silent collisions between producers).

## R5 State and encryption
**Decision**: `bootstrap`: local backend at
`~/.config/ovh-lz/accounts/<account>/state/<instance_id>.tfstate` (path via early-evaluated variable,
P4), encrypted. Others: S3 backend in the account state bucket (account and account-tenant stacks)
or the tenant's own state bucket (tenant stacks); the account bucket is created by `bootstrap`, each
tenant bucket by that tenant's `tenant-state`; all live in `spec.state.project`. Key
`<path minus "stacks/">/terraform.tfstate` (data-model *Derived instance fields*; e.g.
`tenants/demo/dev/gra11/runtime/terraform.tfstate`), so key uniqueness follows path uniqueness and a
runtime slot (`runtime-<slot>`) gets its own key; `use_lockfile = true`, OVH endpoint flags (P2).
Every generated root has `terraform { encryption { key_provider "pbkdf2" "main" { passphrase =
var.state_passphrase } … state { method = … enforced = true } plan { … enforced = true } } }`; the
passphrase is `TF_VAR_state_passphrase` from the bound account's `state-passphrase.env` (mode 600,
created once, before any encrypted state exists).
**Evidence**: `use-object-storage-terraform-backend-state.mdx:109-125`;
`snc-cloud-platform-terraform.mdx:302-303`; `s3-conditional-writes.mdx:85,94`; `cloud_project_storage.md`
(`versioning`). All Terraform-flavoured; OpenTofu behaviour is P1–P4.
**Per-tenant buckets (D87, D88)**: one state bucket per tenant from day one, as ADR-0009 says, owned
by a retained account-tenant `tenant-state` stack (state in the account bucket, `bootstrap`
authority). It also creates the tenant S3 user and a platform S3 user, each with a policy covering
only that bucket (bucket-level ARNs, as `cloud_project_user_s3_policy.md` shows); tenant consumers
read `artifacts/` objects from the same bucket. Adding a tenant never re-applies `bootstrap`, so the
most privileged root with local state is touched only by `bootstrap:account`. **Rejected**: one
shared bucket with per-tenant key prefixes (needs prefix-scoped S3 policies, premise P19, and an
ADR-0009 deviation for little saving); tenant buckets inside `bootstrap` (every tenant onboarding
re-applies the most privileged root from the maintainer's local state; blocks a split tenant repo
and CI onboarding). OKMS now — postponed by D85; the passphrase is the bootstrap method ADR-0009
already names.
**Protection**: state buckets use `modules/object-storage-protected`, a copy of the bucket resource
with a literal `lifecycle { prevent_destroy = true }` (a lifecycle argument cannot depend on a
variable, so the shared `modules/object-storage` used by `runtime` cannot carry it).
**Known gap**: losing the passphrase loses every state. Mitigation for the sandbox: state is
disposable and the bootstrap is re-runnable (FR-012); no recovery claim.
**Fallback rule**: if P1–P3 are refuted, the fallback (local encrypted state per stack with a
per-tenant flock) changes FR-008; the spec and V005/V010 are revised before T038/T039 close.

## R6 Authorities and credentials
| Authority | Principal | Created by | Runs | Credential files |
| --- | --- | --- | --- | --- |
| root (fresh account only) | account root via short-lived AK/AS/CK, typed at a no-echo prompt | owner, Control Panel | `bootstrap:account --fresh-account` `admin` phase; revoked in `revoke` | none |
| `bootstrap` | `lz-sandbox-admin` OAuth2 client | `bootstrap:account` through the API (`POST /me/api/oauth2/client`, `POST /iam/policy`, P23), or the existing one-off config | `bootstrap`, `tenant-state`, `account-governance` | `sandbox.env` + `accounts/<a>/state.env` |
| `platform` | platform-deployer OAuth2 client + per-tenant platform S3 user | `account-governance`, `tenant-state` | `project` | `platform-deployer.env` + `tenants/<t>/platform-state.env` |
| `tenant` | `<tenant>`-deployer OAuth2 client + tenant S3 user | `account-governance`, `tenant-state` | `project-network`, `runtime` | `tenants/<t>/deployer.env` + `tenants/<t>/state.env` |

`ovh_me_api_oauth2_client` with `flow = "CLIENT_CREDENTIALS"`; the secret stays in encrypted state and is
written once to the credential file by `lz-live`'s credential writer without echo.
`discard_client_secret` is not available in 2.21.0 (`CHANGELOG.md:15`). Policies: `ovh_iam_policy`
with `identities = [client.identity]` (P8); tenant `resources = ["urn:v1:eu:resource:publicCloudProject:<project_id>"]`.
**Tenant allowlist (narrowest the API allows, D88)**: from `api/v1/cloud.json`:
`publicCloudProject:apiovh:network/private/{create,get,edit,delete,region/create}`,
`…/network/private/subnet/{create,get,delete}`,
`…/region/storage/{create,get,edit,delete,bulkDeleteObjects}`, plus read actions the provider needs,
each added only on an observed denial (T010, T049) and recorded here. Excluded from
`region/storage/*` on purpose: `policy/create` (would let a tenant attach storage policies to S3
users), `presign`, `object/*`, `lifecycle/*`, `multipartUpload/*`, `job/replication/*`. Whether the
provider's destroy needs `object/delete` or `object/version/delete` on a runtime bucket is
UNVERIFIED (P14, T010); it is added only on evidence. The OVH IAM resource for these actions is the
project URN, so no allowlist can exclude the state buckets in a shared project: that is KD-1 (R20).
Group: `ovh_me_identity_group` for future human tenant members, bound by a read-only policy;
service-account membership in groups is not relied on.
**Child environment**: `lz-live` starts every `tofu` and `ovhcloud` child with an environment built
from scratch — `PATH`, `HOME`, `TF_*` it sets itself, and the selected authority's variables — so a
`.envrc` that exports the admin credential into the shell cannot leak into a tenant stack.
**Rejected**: one shared deployer (no isolation evidence); identity-user tokens (needs a user login;
JIT postponed); OIDC from CI (no source, ADR-0009); root keys in a file or argv (they would outlive
the run; D88).

## R7 Project adoption
**Decision**: generated `import { to = module.stage.<…>.ovh_cloud_project.this  id = var.project_id }`
in the `project` stack only when the manifest says `adopt`. `modules/cloud-project` sets
`lifecycle { prevent_destroy = true }` and `deletion_protection = true`. Required `ovh_subsidiary`
and `plan` arguments are filled from the probe's `-generate-config-out` result (P5). The project id is
not secret; no import id in this slice carries a secret.
**Fallback if P5 refutes** (import plans a replacement or an update that re-orders): `reference` mode —
`data "ovh_cloud_project"` read plus `ovh_iam_resource_tags` on the project URN for labels; the
project resource is not managed. Decision recorded in `decisions.md` by the operator.
**Premise gate**: T025–T028 build and test both modes offline under a recorded waiver (no cloud
contact, both modes are needed anyway as the fallback); which mode the sandbox manifest uses is set
in T039 from T009's result.
**Evidence**: `cloud_project.md` (Import, `deletion_protection`, order/termination note).
**Risk**: `ovh_cloud_project` is created by an order and deleted by termination; a replacement would
order a project. `prevent_destroy` makes any plan with a replacement fail; the live lane refuses a
delete or replace of any retained resource before apply and never destroys `project`.

## R8 Runtime
**Decision**: `components/runtime/managed-only` creates one empty Object Storage bucket
(`ovh_cloud_project_storage`, standard class, versioning off, tags) and publishes the runtime
envelope (ADR-0017) with an `object-storage` capability; no `network` capability.
**Rationale**: cheapest real runtime (P13); exercises tenant authority and tag conditions.
**Rejected**: Managed Kubernetes or an instance (billed per hour, quota, longer destroy).

## R9 Network
**Decision**: `ovh_cloud_project_network_private` (regions `[GRA11]`, `vlan_id` from the manifest's IPAM
row, default 0) + `ovh_cloud_project_network_private_subnet` (CIDR from input, no gateway, DHCP on).
**Evidence**: `cloud_project_network_private.md`, `cloud_project_network_private_subnet.md`; IAM
action names in `api/v1/cloud.json`. Neither resource carries tags: listed as `none` in the
applicability table; the name carries the instance (P12 for cost/vRack).
**Rejected**: `ovh_cloud_project_region_network` (example is a Local Zone; less documented import);
gateway (billed).

## R10 Stacks left out
`account-admin` (D88): the admin service account is created by `bootstrap:account` through the API
on a fresh account, outside OpenTofu state, because no stack can run as a principal that the same
chain must first create, and the root keys that could create it are revoked after the bootstrap. The
stage name stays reserved. `account-fabric`: the vRack is auto-delivered (`vrack.mdx:196`) and the
network stage does not need an explicit vRack attachment (P12). Audit sinks need LDP (billed).
`observability`: LDP billed, no consumer. All three are left out of `deployments.yaml` (no
placeholders); their names stay in the schema's stage enum and the reconciler answers
`STAGE_NOT_IMPLEMENTED`.
**Counterpoint kept (D88)**: drift on the admin client or policy is found only by the bootstrap's
`admin` and `verify` phases, not by a plan.

## R11 Live lane implementation and host guard
**Decision**: Go command `tools/cmd/lz-live` (packages `tools/internal/live`, `tools/internal/stacks`),
Taskfile targets `live:plan|apply|destroy|chain|probe` and `bootstrap:account` that run on the host
only.
**Rationale**: the guards (credential selection, child environment, redaction, trap ordering,
deadline, leftover parsing) need offline tests with fake `tofu`/`ovhcloud`/`git` binaries and mutation
proof; ADR-0002 chose Go for tools.
**Host guard** (runs before any credential is read, for every `lz-live` subcommand: `bootstrap`,
`probe`, `plan`, `apply`, `destroy`, `chain`; not for `stacks:reconcile`/`stacks:generate`, which
read no credential and must run in authoring worktrees, R16): refuse inside the offline entry (`/tcb` present or `LZ_OFFLINE=1`); resolve the working
directory with symlinks evaluated and require it to equal `LZ_OWNER_CHECKOUT` from
`~/.config/ovh-lz/live.env`; require `git rev-parse --git-dir` and `--git-common-dir` to resolve to the
same directory (a linked worktree has a distinct git dir wherever it lives, so no path pattern is
involved); require `git status --porcelain` to be empty (ignored files excepted); require `HEAD` to
equal the `--reviewed-sha` the owner passes and to be an ancestor of `origin/main`. Each refusal
exits 3 with the failed condition named.
**Rejected**: shell scripts (traps and redaction are hard to test and mutate); a `worktrees/` path
test (any worktree outside that directory, or a symlink, passes it).

## R12 Run core, cleanup and leftover check
**Decision**: one run core (`tools/internal/live/{runner,inventory,leftovers,deadline}.go`) serves
probes (`lz-live probe`), `chain` and `bootstrap`. Every apply it performs is of a saved plan file
that has passed the shared retained-resource guard (`protect.go`: refuses a delete, either
replacement order or a `forget` of any resource of a retained instance, including one whose block
was removed from configuration, where `prevent_destroy` no longer applies), and every child stream
passes through `redact.go`. It registers the destroy-on-exit before the first
apply (deferred function plus SIGINT/SIGTERM handling and the deadline context); destroy covers
ephemeral instances (or the probe root) only, reverse order, continuing after a failure and exiting
non-zero. The inventory is appended per resource from `tofu apply -json` `apply_complete` events
(P24) as they arrive, so a crash before the state write still leaves the id recorded; the live lane
also records each run's listings before the first apply.
**Leftover kind matrix** (each listed with pagination; children listed per parent):

Every resource type the slice or its probes create has a row; a type outside this table fails the
check (G9). *Retained* rows are exempt when the resource is in a retained instance's state;
*ephemeral* rows must be gone after the run.

| Provider type | Kind | Listing (P18; fallback API path) | Created by | Treatment | Match |
| --- | --- | --- | --- | --- | --- |
| `ovh_cloud_project_storage` | Object Storage bucket | per region `/cloud/project/{p}/region/{r}/storage` | bootstrap, tenant-state (retained); runtime, probes, KD-1 canary (ephemeral) | per instance | name prefix, `lz:run-id` tag |
| `ovh_cloud_project_network_private` | private network | `/cloud/project/{p}/network/private` | project-network, probes | ephemeral | name prefix |
| `ovh_cloud_project_network_private_subnet` | subnet | `/cloud/project/{p}/network/private/{n}/subnet` (per network) | project-network, probes | ephemeral | parent network or CIDR from inventory |
| `ovh_cloud_project_user` | cloud project user (S3 user) | `/cloud/project/{p}/user` | bootstrap, tenant-state | retained | description/name prefix |
| `ovh_cloud_project_user_s3_credential` | S3 credential | `/cloud/project/{p}/user/{u}/s3Credentials` (per user) | bootstrap, tenant-state | retained (with its user) | parent user |
| `ovh_cloud_project_user_s3_policy` | S3 policy (a property of its user, one per user) | `/cloud/project/{p}/user/{u}/policy` (per user) | bootstrap, tenant-state, probes | retained (with its user); a policy under a user absent from every state is a leftover | parent user |
| `ovh_me_api_oauth2_client` | OAuth2 client | `/me/api/oauth2/client` | account-governance (retained), probes (ephemeral) | per instance | name prefix |
| `ovh_iam_policy` | IAM policy | `/iam/policy` (v2) | account-governance (retained), probes incl. P25 (ephemeral) | per instance | name prefix |
| `ovh_me_identity_group` | identity group | `/me/identity/group` | account-governance | retained | name prefix |
| `ovh_iam_resource_tags` | IAM resource tags (a property of the project URN) | `GET /iam/resource/{urn}` (tags in the response) on the project URN; `/iam/resource/{urn}/tag` has no GET in kb/api | project (retained), probes (ephemeral `lzprobe-` keys) | per instance | `lz:run-id` key |
| `ovh_cloud_project_alerting` (optional, P10) | project alert | `/cloud/project/{p}/alerting` | project when `budget_alert.enabled`, alerting probe | retained (project); probe ephemeral | id from inventory, name prefix |
| `ovh_cloud_quota` (optional, P11) | quota setting (a property of the project, no object to leave behind) | read through the project's quota (`api/v2/publicCloud.json:5530`) | project when `quota_guard.enabled`, quota probe (plan only) | retained with the project; nothing to list as a leftover | — |
| `ovh_cloud_project` | the adopted project | — | project (adopt; `reference` mode creates none) | retained, never destroyed | exempt by id |

A resource of a matrix kind that matches the slice prefix or the run id is a leftover unless it is in
a retained instance's state (read with `tofu state list`/`show -json`), is the retained adopted
project, or is covered by the **admin exemption**: exactly the client id in `sandbox.env`
(`OVH_CLIENT_ID`) and the policy id recorded in `account.env` (`LZ_ADMIN_POLICY_ID`, written by the
bootstrap `admin` phase). The admin is created by script, outside every state (D88), and its name
`lz-sandbox-admin` matches the slice prefix, so without the exemption every run would report it;
matching by id keeps the exemption to those two objects — another client or policy with the same
name is still a leftover (seeded fixture in T054 and T065). That catches resources that never reached
a state. A listing error, a missing binary, an unparseable listing or a created resource type not in
the matrix is `fail`. Each kind has a seeded leftover fixture, including one absent from every state.
**Qualification**: synthetic listings (T054) only build the parser; T065 qualifies it on T009's real
captures before the first resource-creating probe (T010).
**Probe state**: each probe run keeps its encrypted state and a private per-run passphrase file
(0600, through `files.go`) under `accounts/<account>/state/probes/<run-id>/` until destroy and the
leftover check pass, then deletes both. A failed destroy or a killed process leaves them, and
`lz-live probe --cleanup <run-id>` finishes from a fresh process. No escrow is involved: the
passphrase protects disposable probe state for the run's lifetime only.
**Not built** (D85 update): spend admission, cost ledger, reaper service.

## R13 Re-runnable bootstrap (account migration path)
**Decision** (D88): `lz-live bootstrap` phases, each idempotent and reporting `ran`, `unchanged`,
`blocked` or `fail`:
1. `guard` — R11.
2. `identify` — `GET /auth/details` with the available credential (`sandbox.env`, or the root keys
   under `--fresh-account`) gives the account id; create or check `accounts/<account>/account.env`
   (endpoint, account id, `org` from the manifest). A different account than the binding, or a
   manifest `org` that differs from the bound one, is refused. Under `--fresh-account` with an
   existing `sandbox.env` for another account, that file moves to `accounts/<old>/sandbox.env`; no
   file of the old account is read afterwards. Missing project references (`LZ_PROJECT_ID_STATE`,
   `LZ_PROJECT_ID_<REF>` for each manifest reference): without `--fresh-account` → `blocked` (the
   admin credential exists, so the owner fills them and re-runs); with `--fresh-account` the phase
   lists the account's projects with the root keys (`GET /cloud/project`) and prompts for each
   missing reference (echo on, project ids are not secret), so a fresh run never stops before the
   admin credential exists (P22).
3. `passphrase` — create `state-passphrase.env` with 32 random bytes if absent; never overwrite.
   Runs before anything writes encrypted state.
4. `admin` — two separate facts: *credential works* (`sandbox.env` authenticates; `GET /auth/details` answers
   for the bound account) and *admin present as expected* (the client exists; its policy grants
   exactly `account:apiovh:iam/*`, `account:apiovh:me/*`, `publicCloudProject:apiovh:*` on the
   account and project URNs). Both true → `unchanged`. Credential fails or admin missing without
   `--fresh-account` → `blocked` with the instruction. With `--fresh-account`: prompt for AK/AS/CK
   without echo, create the client and policy through the API (P23), write `sandbox.env` once.
   Every run records the admin client id and policy id in `account.env` (`LZ_ADMIN_CLIENT_ID`,
   `LZ_ADMIN_POLICY_ID`) for the leftover exemption (R12).
   Drift on an existing admin → `fail` naming the difference; repair is the same `--fresh-account`
   path with root keys, never the admin editing its own policy.
5. `state` — plan the `bootstrap` root with the bootstrap authority, pass the plan through the
   retained-resource guard (R12; the account bucket and platform S3 user are retained), then apply
   that plan file; when the account bucket
   exists but is not in state, import it by name (`service_name/region/name`,
   `cloud_project_storage.md` Import); a name taken by another account fails with the `spec.org`
   override message. Writes `state.env` from sensitive outputs through the credential writer.
6. `publish` — `bootstrap` outputs to `artifacts/account-bootstrap/outputs.json` in the account bucket.
7. `verify` — `tofu init` + `plan` of `account-governance` against the bucket; lock round-trip.
8. `revoke` (fresh account only) — `GET /auth/currentCredential` then
   `DELETE /me/api/credential/{id}`; a failed revocation prints the Control Panel step and exits 1.
   Once root keys were entered, `revoke` runs on every exit path (success, `fail`, `blocked`, abort,
   SIGINT), so a failed fresh run never leaves the root credential valid. A retry resumes by phase:
   without `--fresh-account` when `admin` had completed (the new `sandbox.env` exists), with it when
   it had not (new root keys, then completed phases report `unchanged`).
**Partial states tested** (V008): each prefix of phases done; `sandbox.env` present but the
credential rejected; credential works but admin policy drifted; passphrase present, `state.env`
missing; `state.env` present, bucket missing; bucket present, not in state; binding for another
account; previous account's files present during `--fresh-account`. **Fresh-account journeys**:
from an empty `~/.config/ovh-lz/` (only `live.env`) and with a previous account's files present,
each to `revoke` without a `blocked` exit, and each failing after `identify` and after `admin`, with
the root credential revoked on exit and the retry resuming as above.
**Account binding** (P26): the account id comes from `GET /auth/details`, which carries no IAM
action in `kb/api/v1/auth.json` and returns `account`; `GET /me` needs `account:apiovh:me/get`
(`kb/api/v1/me.json:23`), which the admin has but neither deployer policy grants (R6). Every
credential class binds the same way. Whether an OAuth2 client token answers `/auth/details` with the
account is UNVERIFIED (P26: T009 admin, T010 probe identity, V010 both deployers); if refuted,
`account:apiovh:me/get` on the account is added to both deployer policies, recorded here, and G5's
negative list is adjusted to allow exactly that read action.
**Existing admin's previous owner**: on the current sandbox, `lz-sandbox-admin` and its policy are in
the state of the one-off config in `~/.config/ovh-lz/bootstrap/`. The first `bootstrap:account` run
leaves them in place and prints the `tofu state rm` commands that retire that config's ownership
(the owner runs them once, T044), so no second owner can destroy or rewrite the admin; the plain-text
secret leaves that state with the removed resource.
**Rationale**: the existing one-off config in `~/.config/ovh-lz/bootstrap/` is not reproducible; the
2026-10-06 update makes the migration path a requirement; D88 takes the admin out of OpenTofu.
**Constraint**: bucket names are global (P21); the default `org` discriminator is `lz` (D87) and
may collide. A new account needs a new `org` value or the old buckets deleted first.
**Rejected**: an `account-admin` stack (round 1: its only valid authority is revoked after a fresh
bootstrap, and importing the client needs `client_id|client_secret` in an import id,
`me_api_oauth2_client.md:70-73`); a `self` authority where the admin manages its own client (the
admin could widen itself; no review gate).

## R14 Labels
**Decision**: canonical keys under `lz:`: `managed-by`, `managed-in`, `instance`, `tenant`, `release`
(constant `unreleased` until ADR-0010's release train exists, D87), `run-id` (live only, set at
creation, `ignore_changes`, P16). Applicability for this slice: `ovh_cloud_project_storage.tags`
(yes); project via `ovh_iam_resource_tags` on its URN (yes, P15); OAuth2 client, IAM policy, identity
group, private network, subnet, cloud project user, S3 credential and S3 policy: no tags in the
provider schema → recorded in `outputs.json` `values.unlabelled[]` of their stage (`bootstrap`,
`tenant-state`, `account-governance`, `project-network`) and visible by name.
**Evidence**: `cloud_project_storage.md` (`tags`), `iam_resource_tags.md` (pattern, `ovh:` reserved).

## R15 Naming
**Decision**: default template `[org, tenant, environment, region, kind, role, slot]`, separator
`-`, lowercase, empty segments omitted (account scope; `slot` outside multi-runtime scopes); `slot`
is the runtime instance's slot, so two runtimes in one scope get two bucket names
(`lz-demo-dev-gra11-bkt-runtime-blue`, `…-green`) and the generator's planned-name check refuses a
collision (`NAME_COLLISION`), kind abbreviations (`bkt` bucket, `pn` private
network, `sn` subnet, `sa` service account, `pol` IAM policy, `grp` identity group, `s3u` S3 user).
Per-kind limits live in `modules/naming/kinds.yaml` (only the kinds above), each row citing its source;
unknown limits are marked and the module refuses names for kinds without a row.
Bucket rules: 3–63 chars, lowercase alphanumerics, `.`/`-`, no doubled punctuation, globally unique
(`s3-limitations.mdx:49-53`). Other kinds' limits: UNVERIFIED (policy name unique, no spaces:
`account-information/iam-policy-ui.mdx:76`).
**Deviation**: ADR-0003 puts limits in a repo-wide `names.yaml`; a module reading a file outside its
directory breaks when published, so the table lives in `modules/naming/kinds.yaml` (D87).

## R16 Terramate
**Decision**: `terramate.tm.hcl` at the repository root (Terramate needs the project root), all
landing-zone config under `stacks/_lz/*.tm.hcl` imported by `stacks/`. Stack metadata from
`terramate create --id --name --tags --after` (P17). Tags: `lz-stage-<stage>`, `lz-tenant-<tenant>`,
`lz-env-<env>`, `lz-region-<region>`, `lz-scope-<scope>`, `lz-slot-<slot>`. `after` uses tag filters
derived from data and authority edges. `generate_hcl` renders `_lz_backend.tf`, `_lz_providers.tf`,
`_lz_main.tf`, `_lz_variables.tf`, `_lz_import.tf` (adopt only) and `tests/_lz_offline.tftest.hcl`.
Generated files are committed; the dependency checker sees their header and classifies the
directory as a generated instance. Fixture manifests generate into scratch directories, never under
the repository (Terramate would treat committed fixture stacks as real stacks).
**Freshness**: `lz-offline` mounts the candidate read-only, so `stacks:check` copies the tree to scratch,
runs reconcile `--check` and `terramate generate`, and diffs; `stacks:generate`/`stacks:reconcile` are
host-side like `generate:foundation-ci`: credential-free, no host guard, allowed in an authoring
worktree with a dirty tree (the manifest edit must precede them). The host guard protects
credentials, not file generation; review of the committed generated files is what `stacks:check`
and the PR provide. Tested as edit → reconcile → generate → check in a scratch linked worktree
(T037).

## R17 Test layers for this slice
| Layer | Where | What |
| --- | --- | --- |
| L0 | `task lint -- <dir>` (existing) | fmt, mirror-only init, validate, TFLint |
| L1 | `<dir>/tests/*.tftest.hcl`, `mock_provider "ovh"` | names, labels, inputs/validations, outputs, `prevent_destroy` on protected modules |
| L2-lite | generated `stacks/**/tests/_lz_offline.tftest.hcl` | each stack plans with fixture inputs; outputs match schema; `prevent_destroy` present for adopt and state buckets |
| L2-exchange | `task test:exchange` (V012) | producer root → envelope → publisher → adapter → consumer plan, on generated roots under mocks |
| L7 | `tools/internal/probes/live/chain/` (build tag `live`; data in `tests/live/`, the protected discovery root; all Go stays in the one tools module) | real chain observations (V010), collected by `tools/internal/live/observe.go` |
Mutation proof only for FR-013's security guards (list in `contracts/checks.md`).

## R18 Traceability
`lz-check specs` takes a spec directory but the registry (`harness/checks.yaml`) keys requirements
globally (`FR-001` …), so spec 005's ids would collide with spec 001's. T001–T002 make the registry
spec-scoped (recommended: `requirements` keyed `005/FR-001`, the spec dir selecting the prefix) and make
`task check:specs` take the spec directory as `CLI_ARGS`.

## R19 Toolchain additions
`ovhcloud` CLI 0.15.0 is a host-only live tool. Recommended pin: `mise.live.toml` (`MISE_ENV=live`) so
`verify:toolchain` and the offline image stay unchanged. No provider bump: 2.21.0 stays (bumping means
re-approving the `lz-offline` preparation closure). The bootstrap's root-key API calls (P23) use an
in-process signed client in Go, so the root keys never reach a child process.

## R20 Sandbox state-isolation deviation (KD-1, D88)
**Decision**: the sandbox keeps one project for tenant resources and state
(`spec.sandbox.shared_state_project: true`). `spec.state.project` is an input; the schema and the
live lane refuse a state project equal to a tenant project without that flag. The tenant allowlist
is the narrowest the API allows (R6). V010 demonstrates the gap: the lane creates a disposable
canary bucket (`<org>-bkt-canary-<run>`, one object, bootstrap authority) in the state project; the
tenant deployer then calls `DELETE …/region/{r}/storage/{canary}` and `bulkDeleteObjects` through
the API. Outcomes: denied → `pass` and a note that KD-1 did not reproduce; allowed with the flag set
→ `known-deviation KD-1` recorded in `summary.json`, printed, and listed in the PR; allowed without
the flag → `fail`. A `known-deviation` does not change the chain's exit code (0 when all other
assertions pass), and is never folded into `pass`. The trap deletes the canary if it still exists.
Real state buckets are never the target.
**Rationale**: OVH IAM actions for buckets apply to the project URN (`cloud.json` action names), so
only a separate project, or tag conditions if P25 holds, can keep tenant authority off state.
**Rejected**: claiming isolation from the S3 policy alone (G6 is necessary but not sufficient);
testing against the real account bucket (destructive if the gap exists).

## R21 Selection (ADR-0007)
**Decision**: selected set = code-changed stacks (record `code_digest` ≠ digest of the stack directory
plus its stage, component and module closure) ∪ input-changed stacks (consumed digest differs or no
record) ∪ transitive data consumers of either. Consumed inputs include the *resolved-reference
input* the lane writes from `account.env` (data-model): the `tenants` map of `account-governance`
(tenant names from the committed generated file, project ids from `account.env`) and the
`project_id` of each `project`. A new tenant row changes the generated map, so `account-governance`
is code-changed and selected with the new `tenant-state`; a changed `LZ_PROJECT_ID_<REF>` changes
the resolved digest and selects `account-governance` and that `project`. Authority edges and Terramate `after` order the
selected set but never add to it. `live:plan|apply -- all` acts on the selected set; `-- <instance>`
acts on that instance and refuses when a producer it consumes is selected and not applied; a
consumer whose producer has no artefact yet is `blocked`. `stacks:order` prints the order and the
reason for each selected stack.
**Locks**: `account` lock for runs touching account or account-tenant stacks; `tenant-<t>` lock per
tenant; acquired in a fixed order (account, then tenants sorted) so two runs cannot deadlock; a
second holder is refused with exit 3. Account stacks are retained, so a tenant chain never revokes
another tenant's deployer.

## R22 Growth seams in the schema
**Decision**: `slot` on runtime instances (two runtimes in one scope: distinct id, path, key and
resource names, since `slot` is a naming segment, R15);
`spec.scope: tenant` with `external` producers (artefacts only, never planned) for a tenant-only
repository; `spec.stage_source` (`local` relative path now; `git` with a version ref as the seam,
refused by the generator with `STAGE_SOURCE_NOT_IMPLEMENTED`). Fixtures: growth manifest with two
slots, tenant-only manifest generated in a separate scratch repository with stages taken from a
relative source outside it.
**Rejected**: building a second runtime variant or self-service now (not needed to keep the seam).

## R23 Premise gates on implementation
Offline implementation that a premise could invalidate proceeds only under a recorded waiver that
names the premise and the revision a refutation forces: T025–T028 (P5, both modes built); T037/T038
(P1–P4, P6: T038 closes only after T010's result; a refuted P1–P3 revises FR-008 first); T054/T055
(P24: the fallback inventory path is tested too). Live tasks depend on the probe result directly.

**T007 results (2026-10-06, pinned OpenTofu 1.13.0 and Terramate 0.17.3 through `lz-offline`
capture admission; fixtures and sidecars in `tests/fixtures/{tofu-probes,terramate}/captures/`)**:
P4, P16, P17 and P24 observed; P6 observed for the address form, but its offline test method is
refuted. Two findings revise later tasks:
- **Imports into a mocked provider cannot be tested (P6)**: `tofu test` with `mock_provider "ovh"`
  crashes (exit 11, "Importing is not supported in testing context") on an `import` block whose
  target is an `ovh` resource, for `command = plan` as well as `apply` (both captured); the same
  address form into `terraform_data` under `tofu test` without a mock passes (captured), and the T007
  review observed it passing under `mock_provider "ovh"` too (not captured): the crash is the
  import into the mocked provider's resource. So R17's L2-lite test cannot load an adopt-mode `project` root with `_lz_import.tf` as
  generated (T037/T038 stack plans; module and stage tests hold no root `import`). Revision owed
  before T037 closes, one of:
  (A, recommended) the offline test of an adopt root runs on a scratch copy without
  `_lz_import.tf`, and a static check pins that file's `to`/`id` to the stage address; (B) an
  `import` with `for_each` over a generated variable that the offline test sets to `{}`
  (UNVERIFIED: not captured); (C) adopt roots are covered only by T009's live plan. The `to`
  address itself (single and keyed module) is observed under `tofu plan` with `terraform_data`.
- **Terramate root outside git (P17)**: a root `terramate.tm.hcl` with only `config { git {…} }`
  is not taken as the project root outside a git repository; `required_version` is. R16's
  `stacks:check` copies the tree to scratch: that copy needs a git repository or a root config
  with `required_version`. `terramate create --tags` sets the stack's tags although its help lists
  `--tags` as a filter. `after = ["tag:a:b"]` matches stacks carrying both tags (AND): an OR
  reading would have made the captured tree cyclic.
- P4: a wrong passphrase is refused, but the message reads "Error acquiring the state lock …
  decryption failed"; the live lane's error text must not be read as a lock conflict.

## R24 Platform `project` state in the tenant bucket (KD-3)
**Decision**: the platform-owned `project` stack keeps its state and its `artifacts/` object in the
tenant's state bucket, which the tenant S3 user can write, so a tenant credential could overwrite
the `project` state or publish a forged `project` artefact. Recorded as known deviation KD-3 next to
KD-1, not fixed in this slice. **Mitigation built**: the adapter refuses a `project` artefact whose
`project_id`/`project_urn` differs from the bound account's `LZ_PROJECT_ID_<REF>` for that tenant
and environment (`fail: unbound project`, T060), so a forged id cannot point `runtime` or
`project-network` at another project; the `project` stack's own plan is protected by the
retained-resource guard. A tampered `project` state object is not detected.
**Rejected for now**: `project` state in the account bucket — its tenant consumers read the
artefact with the tenant S3 user, which G6 forbids from reading the account bucket, so the
artefact would need a second location and a second writer; a platform-only prefix in the tenant
bucket excluded from the tenant S3 policy — needs prefix-scoped S3 policies (P19, withdrawn).
**Lifted when**: a platform-only bucket per tenant (or a qualified prefix policy) holds `project`
state and artefacts, with the tenant S3 user granted read on the artefact only; trigger: the first
non-sandbox tenant or the first claim of tenant isolation (same trigger as KD-1).
