# Research: First landing-zone slice
Feature: 005-first-landing-zone-slice · 2026-10-06 · Status: draft

Each entry: decision, rationale, alternatives rejected, evidence. KB paths are relative to
`~/.local/share/ovh-lz/kb/mirror/` unless they start with `api/`. Provider doc paths are
`terraform-provider-ovh/docs/resources/<name>.md`; the mirror is at provider commit `00cb8b0`
(2.22.0 era) while the repository pins `ovh/ovh` 2.21.0 (`harness/capabilities.yaml`).

## R1 Layering: keep components between modules and stages
**Decision** (kept, D87): `modules/` (one OVH product concern) → `components/` (ADR-0003 singletons and family
variants) → `stages/` → generated stacks. Components used: `state-backend`, `account-baseline`,
`identity/ovh-native`, `project-factory`, `network/island`, `runtime/managed-only`.
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
- environment scope: `stacks/tenants/<tenant>/<environment>/<stage>/` (`project`)
- region scope: `stacks/tenants/<tenant>/<environment>/<region>/<stage>/` (`project-network`, `runtime`)
**Rationale**: the path mirrors the state owner's scope. One project per tenant × environment
(ADR-0005) means `project` is not regional; a second region adds only regional stacks.
`stacks/tenants/<tenant>/` lifts into a tenant repo as is. The operator's suggested
`stacks/<tenant>/<environment>/<region>/<stage>/` forces a fake region on `project` and a fake tenant on
account stages.
**Rejected**: flat `stacks/<instance_id>/` (no visible ownership, harder Terramate tag filters);
`templates/tenant-repo/` from ADR-0002 (D85 chose a single self-contained repo for now).

## R3 Instance identity
**Decision**: `instance_id` is authored once, `^[a-z][a-z0-9-]{2,62}$`, immutable, unique, and is the
Terramate stack `id`. Convention at creation: `<tenant>-<env>-<region>-<stage>` or `account-<stage>`;
after creation the id never follows renames. The path is derived from dimensions; a dimension
change is `UNSUPPORTED_CHANGE` until the rename workflow exists.
**Rejected**: UUIDs (unreadable in labels and console); ids derived from dimensions (a rename would
change identity, which ADR-0004 forbids).

## R4 Output exchange
**Decision**: after a successful apply, the live lane converts `tofu output -json` into the
envelope (FR-005), drops every entry with `"sensitive": true`, validates against
`schemas/outputs/<stage>.schema.json` and writes it to the instance's state bucket at
`artifacts/<instance_id>/outputs.json` (ADR-0004, reworded 2026-10-06) using the producer's S3 credentials. Before a consumer plans, the lane
reads the producers' objects with the consumer's S3 credentials, validates them again, writes them
to `.local/live/<run-id>/inputs/<instance_id>/<producer_stage>.json` and passes them with `-var-file`;
generated variables are typed by the stage's input types. The consumer's last-applied record
(`.local/live/records/<instance_id>.json`, gitignored) holds consumed digests; a mismatch or a missing
record re-plans the consumer (FR-009). Offline, fixture `outputs.json` files under
`tests/fixtures/outputs/` play the producer.
**Rationale**: ADR-0004 "outputs artefacts, not state reads", without generations/fencing (postponed).
The bucket keeps the exchange identical when CI replaces the workstation.
**Rejected**: Terramate outputs sharing (`sharing_backend` reads producer state through `tofu output`;
ADR-0007 §3 forbids it until spiked); committing `outputs.json` (puts sandbox ids in a public repo
and makes the live lane write to git); `terraform_remote_state` (exposes whole state; FR-003).

## R5 State and encryption
**Decision**: `account-admin` and `bootstrap`: local backend at
`~/.config/ovh-lz/state/<instance_id>.tfstate` (path via early-evaluated variable, P4), encrypted.
Others: S3 backend in the account state bucket (account stacks) or the tenant's own state bucket
(tenant stacks), both created by `bootstrap`; key `<scope path>/<instance_id>/terraform.tfstate`,
`use_lockfile = true`, OVH endpoint flags (P2). Every generated root has
`terraform { encryption { key_provider "pbkdf2" "main" { passphrase = var.state_passphrase } … state
{ method = … enforced = true } plan { … enforced = true } } }`; the passphrase is
`TF_VAR_state_passphrase` from `~/.config/ovh-lz/state-passphrase.env` (mode 600, created once).
**Evidence**: `use-object-storage-terraform-backend-state.mdx:109-125`;
`snc-cloud-platform-terraform.mdx:302-303`; `s3-conditional-writes.mdx:85,94`; `cloud_project_storage.md`
(`versioning`). All Terraform-flavoured; OpenTofu behaviour is P1–P4.
**Per-tenant buckets (D87)**: one state bucket per tenant from day one, as ADR-0009 says; each
tenant S3 user's policy covers only its tenant's bucket (bucket-level ARNs, as
`cloud_project_user_s3_policy.md` shows), and a tenant's consumers read `artifacts/` objects from
the same bucket. **Rejected**: one shared bucket with per-tenant key prefixes (needs prefix-scoped
S3 policies, premise P19, and an ADR-0009 deviation for little saving). OKMS now — postponed by D85; the passphrase is the bootstrap method ADR-0009 already
names.
**Known gap**: losing the passphrase loses every state. Mitigation for the sandbox: state is
disposable and the bootstrap is re-runnable (FR-012); no recovery claim.

## R6 Authorities and credentials
| Authority | Principal | Created by | Runs | Credential file |
| --- | --- | --- | --- | --- |
| `root-bootstrap` | account root via short-lived AK/AS/CK | owner, Control Panel/API | `account-admin` on a fresh account only | `~/.config/ovh-lz/root-bootstrap.env` (deleted after revocation) |
| `bootstrap` | `lz-sandbox-admin` OAuth2 client | `account-admin` (or the existing one-off config, imported) | `bootstrap`, `account-governance` | `sandbox.env` (existing) |
| `platform` | platform-deployer OAuth2 client | `account-governance` | `project` | `platform-deployer.env` + `state.env` (platform S3 user) |
| `tenant` | `<tenant>`-deployer OAuth2 client + tenant S3 user | `account-governance` | `project-network`, `runtime` | `tenants/<tenant>.env` |
`ovh_me_api_oauth2_client` with `flow = "CLIENT_CREDENTIALS"`; the secret stays in encrypted state and is
written once to the credential file by the live lane without echo. `discard_client_secret` is not
available in 2.21.0 (`CHANGELOG.md:15`). Policies: `ovh_iam_policy` with `identities =
[client.identity]` (P8); tenant `resources = ["urn:v1:eu:resource:publicCloudProject:<project_id>"]`
and the allowlist from `api/v1/cloud.json` (P9). Group: `ovh_me_identity_group` for future human
tenant members, bound by a read-only policy; service-account membership in groups is not relied on.
**Rejected**: one shared deployer (no isolation evidence); identity-user tokens (needs a user login;
JIT postponed); OIDC from CI (no source, ADR-0009).

## R7 Project adoption
**Decision**: generated `import { to = module.stage.<…>.ovh_cloud_project.this  id = var.project_id }`
in the `project` stack only when the manifest says `adopt`. `modules/cloud-project` sets
`lifecycle { prevent_destroy = true }` and `deletion_protection = true`. Required `ovh_subsidiary`
and `plan` arguments are filled from the probe's `-generate-config-out` result (P5).
**Fallback if P5 refutes** (import plans a replacement or an update that re-orders): `reference` mode —
`data "ovh_cloud_project"` read plus `ovh_iam_resource_tags` on the project URN for labels; the
project resource is not managed. Decision recorded in `decisions.md` by the operator.
**Evidence**: `cloud_project.md` (Import, `deletion_protection`, order/termination note).
**Risk**: `ovh_cloud_project` is created by an order and deleted by termination; a replacement would
order a project. `prevent_destroy` makes any plan with a replacement fail; the live lane never
destroys `project`.

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
`account-fabric`: the vRack is auto-delivered (`vrack.mdx:196`) and the network stage does not need
an explicit vRack attachment (P12). Audit sinks need LDP (billed). `observability`: LDP billed, no
consumer. Both are left out of `deployments.yaml` (no placeholders); their names stay in the schema's
stage enum and the reconciler answers `STAGE_NOT_IMPLEMENTED`.

## R11 Live lane implementation
**Decision**: Go command `tools/cmd/lz-live` (packages `tools/internal/live`, `tools/internal/stacks`),
Taskfile targets `live:plan|apply|destroy|chain` and `bootstrap:account` that run on the host only.
**Rationale**: the guards (credential selection, redaction, trap ordering, leftover parsing) need
offline tests with fake `tofu`/`ovhcloud` binaries and mutation proof; ADR-0002 chose Go for tools.
**Host execution**: the Taskfile header says every target runs through `lz-offline`; live targets are
the deliberate exception (network and credentials by design). `lz-live` refuses to run when it
detects the offline entry (`/tcb` present or `LZ_OFFLINE=1`) and when the working tree is not the
owner's main checkout on a reviewed commit (D87; no candidate worktree under `worktrees/`).
**Rejected**: shell scripts (traps and redaction are hard to test and mutate).

## R12 Cleanup and leftover check
**Decision**: `chain` registers the destroy before the first apply (deferred function plus SIGINT/SIGTERM
handling). Destroy covers ephemeral instances only, reverse order, continuing after a failure and
exiting non-zero. Before destroy, `tofu state list`/`show -json` per instance writes the created ids to
`.local/live/<run-id>/inventory.json`. The leftover check uses `ovhcloud` (P18) to list buckets, private
networks, OAuth2 clients and IAM policies; anything matching the slice prefix and not in the retained
set is a leftover; for buckets it also reads `lz:run-id`. A listing error is `fail`.
**Not built** (D85 update): spend admission, cost ledger, reaper service.

## R13 Re-runnable bootstrap (account migration path)
**Decision**: `lz-live bootstrap` phases, each idempotent:
1. `admin` — if `sandbox.env` holds a working client, skip. Else, with `--fresh-account` and
   `root-bootstrap.env`, apply `account-admin` (creates client + policy); without the flag, import the
   existing client (`client_id|client_secret`, `me_api_oauth2_client.md` Import) from `sandbox.env`.
   Writes `sandbox.env`; prints the commands to revoke the AK/CK and deletes `root-bootstrap.env` after
   the owner confirms.
2. `passphrase` — create `state-passphrase.env` with 32 random bytes if absent; never overwrite.
3. `state` — apply `bootstrap` (account bucket and one bucket per tenant); write `state.env` from
   sensitive outputs without echo.
4. `verify` — `tofu init` + `plan` of `account-governance` against the bucket; lock round-trip.
The project id comes from `project.env` (`LZ_PROJECT_ID_<TENANT>_<ENV>`), which the owner fills; ordering
a project stays manual (P22).
**Rationale**: the existing one-off config in `~/.config/ovh-lz/bootstrap/` is not reproducible; the
2026-10-06 update makes the migration path a requirement.
**Constraint**: bucket names are global (P21); the default `org` discriminator is `lz` (D87) and
may collide. A taken name fails with a clear error that names `spec.org` as the override; a new
account needs a new `org` value or the old buckets deleted first.

## R14 Labels
**Decision**: canonical keys under `lz:`: `managed-by`, `managed-in`, `instance`, `tenant`, `release`
(constant `unreleased` until ADR-0010's release train exists, D87), `run-id` (live only, set at
creation, `ignore_changes`, P16). Applicability for this slice: `ovh_cloud_project_storage.tags`
(yes); project via `ovh_iam_resource_tags` on its URN (yes, P15); OAuth2 client, IAM policy, identity
group, private network, subnet, cloud project user: no tags in the provider schema → recorded in
`outputs.json` `values.unlabelled[]` and visible by name.
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
`lz-env-<env>`, `lz-region-<region>`, `lz-scope-<scope>`. `after` uses tag filters derived from edges.
`generate_hcl` renders `_lz_backend.tf`, `_lz_providers.tf`, `_lz_main.tf`, `_lz_variables.tf`,
`_lz_import.tf` (adopt only) and `tests/_lz_offline.tftest.hcl`. Generated files are committed; the
dependency checker sees their header and classifies the directory as a generated instance.
Fixture manifests generate into scratch directories, never under the repository (Terramate would treat
committed fixture stacks as real stacks).
**Freshness**: `lz-offline` mounts the candidate read-only, so `stacks:check` copies the tree to scratch,
runs reconcile `--check` and `terramate generate`, and diffs; `stacks:generate`/`stacks:reconcile` are
host-side like `generate:foundation-ci`.

## R17 Test layers for this slice
| Layer | Where | What |
| --- | --- | --- |
| L0 | `task lint -- <dir>` (existing) | fmt, mirror-only init, validate, TFLint |
| L1 | `<dir>/tests/*.tftest.hcl`, `mock_provider "ovh"` | names, labels, inputs/validations, outputs |
| L2-lite | generated `stacks/**/tests/_lz_offline.tftest.hcl` | each stack plans with fixture inputs; outputs match schema; `prevent_destroy` present for adopt |
| L7 | `tools/internal/probes/live/chain/` (build tag `live`; data in `tests/live/`, the protected discovery root; all Go stays in the one tools module) | real chain observations (V010) |
Mutation proof only for FR-013's security guards (list in `contracts/checks.md`).

## R18 Traceability
`lz-check specs` takes a spec directory but the registry (`harness/checks.yaml`) keys requirements
globally (`FR-001` …), so spec 005's ids would collide with spec 001's. T001–T002 make the registry
spec-scoped (recommended: `requirements` keyed `005/FR-001`, the spec dir selecting the prefix) and make
`task check:specs` take the spec directory as `CLI_ARGS`.

## R19 Toolchain additions
`ovhcloud` CLI 0.15.0 is a host-only live tool. Recommended pin: `mise.live.toml` (`MISE_ENV=live`) so
`verify:toolchain` and the offline image stay unchanged. No provider bump: 2.21.0 stays (bumping means
re-approving the `lz-offline` preparation closure).
