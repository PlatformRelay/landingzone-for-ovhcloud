# Data model: First landing-zone slice
Feature: 005-first-landing-zone-slice · 2026-10-06 · Status: draft

## Deployment manifest — `stacks/deployments.yaml`
Strict decoding: unknown fields, duplicate keys, unsupported `apiVersion`/`kind` are errors.

```yaml
apiVersion: lz.platformrelay.dev/v1alpha1
kind: Deployments
metadata:
  name: sandbox
spec:
  org: lz                      # naming discriminator (D87); bucket names are global, override if taken
  forge: github.com/platformrelay/landingzone-for-ovhcloud   # managed-in prefix
  state:
    region: gra                # S3 region (GRA, D87)
    endpoint: https://s3.gra.io.cloud.ovh.net
  tenants:
    - name: demo
      environments:
        - name: dev
          project: {mode: adopt}               # adopt | reference (fallback, R7); order postponed
          regions:
            - name: GRA11
              network: {cidr: 10.20.0.0/24, vlan_id: 0}
          budget_alert: {enabled: false}         # optional (P10)
          quota_guard: {enabled: false}          # optional (P11)
  instances:
    - {id: account-admin,            stage: account-admin}
    - {id: account-bootstrap,        stage: bootstrap}
    - {id: account-governance,       stage: account-governance}
    - {id: demo-dev-project,         stage: project,         tenant: demo, environment: dev}
    - {id: demo-dev-gra11-network,   stage: project-network, tenant: demo, environment: dev, region: GRA11}
    - {id: demo-dev-gra11-runtime,   stage: runtime,         tenant: demo, environment: dev, region: GRA11}
```

Rules (FR-006):
- `id`: `^[a-z][a-z0-9-]{2,62}$`, unique, immutable (= Terramate stack id).
- `stage`: enum `account-admin | bootstrap | account-governance | account-fabric | project |
  project-network | runtime | observability`; `account-fabric` and `observability` decode but the
  reconciler refuses them with `STAGE_NOT_IMPLEMENTED`.
- Scope per stage (table below): account stages forbid `tenant/environment/region`; `project` requires
  `tenant, environment`, forbids `region`; regional stages require all three, and the tenant,
  environment and region must exist under `spec.tenants`.
- At most one instance per (stage, tenant, environment, region) — one owner per scope.
- Exactly one `account-admin`, `bootstrap`, `account-governance`.
- Project ids are not in the manifest; they come from `~/.config/ovh-lz/project.env`
  (`LZ_PROJECT_ID_<TENANT>_<ENV>`, uppercased, `-`→`_`). Offline fixtures supply a synthetic id.

## Stage table (fixed in code, `tools/internal/stacks/stages.go`)

| Stage | Scope | Consumes outputs of | Needs authority from | Authority | Backend | Live chain |
| --- | --- | --- | --- | --- | --- | --- |
| account-admin | account | — | — | root-bootstrap | local | retained |
| bootstrap | account | — | account-admin | bootstrap | local | retained |
| account-governance | account | bootstrap | account-admin | bootstrap | s3 | ephemeral |
| project | environment | — | account-governance | platform | s3 | retained |
| project-network | region | project | account-governance | tenant | s3 | ephemeral |
| runtime | region | project | account-governance | tenant | s3 | ephemeral |

Two edge kinds, both real: *data* edges (the consumer reads the producer's `outputs.json` as typed
variables; a changed digest re-plans the consumer) and *authority* edges (the consumer runs as a
principal the producer creates; no data flows). Both become Terramate `after`; only data edges drive
re-planning. `account-governance` reads `bootstrap` for the tenant state bucket names (each tenant
S3 user is scoped to its tenant's bucket).
`runtime` does not depend on `project-network` (managed-only runtime has no network).

## Derived instance fields
| Field | Derivation | Example |
| --- | --- | --- |
| path | scope rule (research R2), region lowercased | `stacks/tenants/demo/dev/gra11/runtime` |
| state bucket | account scope: account state bucket; tenant scope: the tenant's state bucket (one per tenant, ADR-0009, D87) | `lz-demo-bkt-state` |
| state key | `<path minus "stacks/">/terraform.tfstate` | `tenants/demo/dev/gra11/runtime/terraform.tfstate` |
| local state | `~/.config/ovh-lz/state/<id>.tfstate` (local backends) | |
| outputs object | `artifacts/<id>/outputs.json` in the instance's state bucket (ADR-0004 `artifacts/` prefix) | |
| Terramate tags | `lz-stage-*`, `lz-scope-*`, `lz-tenant-*`, `lz-env-*`, `lz-region-*` | |
| after | tag filters of consumed producers in the same tenant/env (and region) | |
| managed-in | `<spec.forge>//<path>` | |
| bucket name (account state) | naming(org, kind=bkt, role=state) | `lz-bkt-state` |
| bucket name (tenant state) | naming(org, tenant, kind=bkt, role=state) | `lz-demo-bkt-state` |

Uniqueness checks: id, path, (state bucket, state key). Adding a tenant re-applies `bootstrap`, which
creates that tenant's state bucket.

## Output envelope — `outputs.json` (`schemas/outputs/envelope.schema.json`)
```json
{
  "apiVersion": "lz.platformrelay.dev/v1alpha1",
  "kind": "StageOutputs",
  "instance_id": "demo-dev-gra11-runtime",
  "stage": "runtime",
  "source_revision": "<git sha of the applied tree>",
  "values": { "...": "stage-specific, schema-checked" }
}
```
Rules: no key matching `(?i)secret|password|token|private_key|access_key` anywhere in `values`; no entry
that `tofu output -json` marked `sensitive`; `additionalProperties: false` per stage schema; no null or
empty-string placeholders for capabilities.

Per-stage `values` (minimum):
| Stage | values |
| --- | --- |
| account-admin | `admin_client_id`, `admin_identity_urn` |
| bootstrap | `state_bucket`, `tenant_state_buckets {<t>: name}`, `state_region`, `state_endpoint`, `platform_s3_user_id` |
| account-governance | `platform_deployer {client_id, identity_urn}`, `tenants {<t>: {deployer_client_id, deployer_identity_urn, group_urn, s3_user_id, state_bucket}}` |
| project | `tenant`, `environment`, `project_id`, `project_urn`, `regions[]`, `budget_alert_id?` |
| project-network | `network_id`, `regions_openstack_ids{}`, `subnet_id`, `cidr` |
| runtime | envelope per ADR-0017: `kind: managed-only`, `scope {instance, project_id, region}`, `readiness`, `pending_actions[]`, `capabilities {object-storage: {bucket, endpoint, region}}` |
All stages: `unlabelled[]` = resource addresses whose API carries no tags (R14).

Sensitive outputs (never published; written to credential files by the live lane):
`account-admin.admin_client_secret`, `bootstrap.platform_s3 {access_key_id, secret_access_key}`,
`account-governance.platform_deployer_secret`, `account-governance.tenant_credentials{<t>}`.

## Label set
| Key | Value | Source |
| --- | --- | --- |
| `lz:managed-by` | `opentofu` | constant |
| `lz:managed-in` | `<forge>//<path>` | generated |
| `lz:instance` | instance id | generated |
| `lz:tenant` | tenant name (absent for account scope) | manifest only (authorisation key) |
| `lz:release` | `unreleased` | constant until ADR-0010's release train exists (D87) |
| `lz:run-id` | live run id; `none` offline | live lane, `ignore_changes` |
Extra labels: map input; keys in the set above are rejected.

## Naming template (data input to `modules/naming`)
```hcl
template = {
  version   = 1
  segments  = ["org", "tenant", "environment", "region", "kind", "role"]
  separator = "-"
  case      = "lower"
  kinds     = { bucket = "bkt", private_network = "pn", subnet = "sn", service_account = "sa",
                iam_policy = "pol", identity_group = "grp", s3_user = "s3u" }
}
```
Output: `name`, `labels` (mandatory + extra), `algorithm_version`. Errors: unknown kind, empty
required segment, over the kind's limit, forbidden characters, doubled punctuation (buckets).

## Local files (never in the repository)
| Path | Content | Writer |
| --- | --- | --- |
| `~/.config/ovh-lz/sandbox.env` | `OVH_ENDPOINT`, `OVH_CLIENT_ID/SECRET` (lz-sandbox-admin) | existing; bootstrap `admin` phase on a fresh account |
| `~/.config/ovh-lz/root-bootstrap.env` | AK/AS/CK, fresh account only | owner; deleted after revocation |
| `~/.config/ovh-lz/project.env` | `LZ_PROJECT_ID_<TENANT>_<ENV>` | owner |
| `~/.config/ovh-lz/state-passphrase.env` | `TF_VAR_state_passphrase` | bootstrap, once |
| `~/.config/ovh-lz/state.env` | platform S3 `AWS_ACCESS_KEY_ID/SECRET_ACCESS_KEY` | bootstrap `state` phase |
| `~/.config/ovh-lz/platform-deployer.env` | platform OAuth2 client | live lane after governance |
| `~/.config/ovh-lz/tenants/<t>.env` | tenant OAuth2 client + tenant S3 keys | live lane after governance |
| `~/.config/ovh-lz/state/<id>.tfstate` | encrypted local state | `account-admin`, `bootstrap` |
All mode 600, directory 700; the lane refuses group/world-readable files.

## Live run record (gitignored, `.local/live/`)
`<run-id>/plan-<id>.txt` (rendered plans, no raw JSON), `<run-id>/inputs/<id>/*.json`,
`<run-id>/inventory.json` (instance → created resource ids), `<run-id>/leftovers.json`,
`records/<id>.json` (`applied_at`, `source_revision`, consumed `{producer: sha256}`).
Run id: `YYYYMMDDThhmmssZ-<4 hex>`.
