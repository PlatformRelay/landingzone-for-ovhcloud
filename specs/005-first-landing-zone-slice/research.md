# Research: First landing-zone slice
Feature: 005-first-landing-zone-slice · 2026-10-06 · Revised: 2026-10-06 (D88) · Status: draft

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
`<scope path>/<instance_id>/terraform.tfstate`, `use_lockfile = true`, OVH endpoint flags (P2).
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
**Host guard** (runs before any credential is read, for every subcommand including `bootstrap` and
`probe`): refuse inside the offline entry (`/tcb` present or `LZ_OFFLINE=1`); resolve the working
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
probes (`lz-live probe`), `chain` and `bootstrap`. It registers the destroy-on-exit before the first
apply (deferred function plus SIGINT/SIGTERM handling and the deadline context); destroy covers
ephemeral instances (or the probe root) only, reverse order, continuing after a failure and exiting
non-zero. The inventory is appended per resource from `tofu apply -json` `apply_complete` events
(P24) as they arrive, so a crash before the state write still leaves the id recorded; the live lane
also records each run's listings before the first apply.
**Leftover kind matrix** (each listed with pagination; children listed per parent):

| Kind | Listing (P18; fallback API path) | Created by | Match |
| --- | --- | --- | --- |
| Object Storage bucket | per region `/cloud/project/{p}/region/{r}/storage` | bootstrap, tenant-state, runtime, probes | name prefix, `lz:run-id` tag |
| private network | `/cloud/project/{p}/network/private` | project-network, probes | name prefix |
| subnet | `/cloud/project/{p}/network/private/{n}/subnet` (per network) | project-network, probes | parent network or CIDR from inventory |
| cloud project user (S3 user) | `/cloud/project/{p}/user` | bootstrap, tenant-state | description/name prefix |
| S3 credential | `/cloud/project/{p}/user/{u}/s3Credentials` (per user) | bootstrap, tenant-state | parent user |
| OAuth2 client | `/me/api/oauth2/client` | account-governance, probes | name prefix |
| IAM policy | `/iam/policy` (v2) | account-governance, probes | name prefix |
| identity group | `/me/identity/group` | account-governance | name prefix |
| IAM resource tags | `/iam/resource/{urn}/tag` on the project URN | project, probes | `lz:run-id` key |

A resource of a matrix kind that matches the slice prefix or the run id is a leftover unless it is in
a retained instance's state (read with `tofu state list`/`show -json`) or is the retained adopted
project. That catches resources that never reached a state. A listing error, a missing binary, an
unparseable listing or a created resource type not in the matrix is `fail`. Each kind has a seeded
leftover fixture, including one absent from every state.
**Not built** (D85 update): spend admission, cost ledger, reaper service.

## R13 Re-runnable bootstrap (account migration path)
**Decision** (D88): `lz-live bootstrap` phases, each idempotent and reporting `ran`, `unchanged`,
`blocked` or `fail`:
1. `guard` — R11.
2. `identify` — `GET /me` with the available credential (`sandbox.env`, or the root keys under
   `--fresh-account`) gives the account id; create or check `accounts/<account>/account.env`
   (endpoint, account id, `org` from the manifest). A different account than the binding, or a
   manifest `org` that differs from the bound one, is refused. Under `--fresh-account` with an
   existing `sandbox.env` for another account, that file moves to `accounts/<old>/sandbox.env`; no
   file of the old account is read afterwards. Missing `LZ_PROJECT_ID_STATE` → `blocked` (P22).
3. `passphrase` — create `state-passphrase.env` with 32 random bytes if absent; never overwrite.
   Runs before anything writes encrypted state.
4. `admin` — two separate facts: *credential works* (`sandbox.env` authenticates; `GET /me` answers
   for the bound account) and *admin present as expected* (the client exists; its policy grants
   exactly `account:apiovh:iam/*`, `account:apiovh:me/*`, `publicCloudProject:apiovh:*` on the
   account and project URNs). Both true → `unchanged`. Credential fails or admin missing without
   `--fresh-account` → `blocked` with the instruction. With `--fresh-account`: prompt for AK/AS/CK
   without echo, create the client and policy through the API (P23), write `sandbox.env` once.
   Drift on an existing admin → `fail` naming the difference; repair is the same `--fresh-account`
   path with root keys, never the admin editing its own policy.
5. `state` — apply the `bootstrap` root with the bootstrap authority; when the account bucket
   exists but is not in state, import it by name (`service_name/region/name`,
   `cloud_project_storage.md` Import); a name taken by another account fails with the `spec.org`
   override message. Writes `state.env` from sensitive outputs through the credential writer.
6. `publish` — `bootstrap` outputs to `artifacts/account-bootstrap/outputs.json` in the account bucket.
7. `verify` — `tofu init` + `plan` of `account-governance` against the bucket; lock round-trip.
8. `revoke` (fresh account only) — `GET /auth/currentCredential` then
   `DELETE /me/api/credential/{id}`; a failed revocation prints the Control Panel step and exits 1.
**Partial states tested** (V008): each prefix of phases done; `sandbox.env` present but the
credential rejected; credential works but admin policy drifted; passphrase present, `state.env`
missing; `state.env` present, bucket missing; bucket present, not in state; binding for another
account; previous account's files present during `--fresh-account`.
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
**Decision**: default template `[org, tenant, environment, region, kind, role]`, separator `-`,
lowercase, empty segments omitted (account scope), kind abbreviations (`bkt` bucket, `pn` private
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
host-side like `generate:foundation-ci`.

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
record) ∪ transitive data consumers of either. Authority edges and Terramate `after` order the
selected set but never add to it. `live:plan|apply -- all` acts on the selected set; `-- <instance>`
acts on that instance and refuses when a producer it consumes is selected and not applied; a
consumer whose producer has no artefact yet is `blocked`. `stacks:order` prints the order and the
reason for each selected stack.
**Locks**: `account` lock for runs touching account or account-tenant stacks; `tenant-<t>` lock per
tenant; acquired in a fixed order (account, then tenants sorted) so two runs cannot deadlock; a
second holder is refused with exit 3. Account stacks are retained, so a tenant chain never revokes
another tenant's deployer.

## R22 Growth seams in the schema
**Decision**: `slot` on runtime instances (two runtimes in one scope: distinct id, path, key);
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
