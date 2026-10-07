# Data model: First landing-zone slice
Feature: 005-first-landing-zone-slice · 2026-10-06 · Revised: 2026-10-06 (D88; round-2 review) · Status: draft

## Deployment manifest — `stacks/deployments.yaml`
Syntax: YAML in its JSON-compatible flow form (as `harness/checks.yaml`), decoded strictly as JSON
with `checks.DecodeStrict`; whole-line `#` comments are allowed, a comment after a value on the same
line, block-style YAML and a second document are refused (`MANIFEST_SYNTAX`). Strict decoding:
unknown fields and case variants, duplicate keys, missing or null required fields and unsupported
`apiVersion`/`kind` are errors. The published shape is `schemas/deployments.schema.json`.

```yaml
{
  "apiVersion": "lz.platformrelay.dev/v1alpha1",
  "kind": "Deployments",
  "metadata": {"name": "sandbox"},
  "spec": {
    # platform (owns account stacks) | tenant (tenant-only repository); platform when absent
    "scope": "platform",
    # naming discriminator (D87); bucket names are global, override if taken
    "org": "lz",
    # managed-in prefix
    "forge": "github.com/platformrelay/landingzone-for-ovhcloud",
    # local: relative path to stages/; git: versioned ref (schema seam, refused by generation)
    "stage_source": {"kind": "local"},
    # KD-1 (D88); absent outside the sandbox
    "sandbox": {"shared_state_project": true},
    "state": {
      # resolved from account.env; must differ from tenant projects unless sandbox.shared_state_project
      "project": {"ref": "STATE"},
      # S3 region (GRA, D87)
      "region": "gra",
      "endpoint": "https://s3.gra.io.cloud.ovh.net"
    },
    "tenants": [
      {
        "name": "demo",
        "environments": [
          {
            "name": "dev",
            # adopt | reference (fallback, R7); order postponed
            "project": {"mode": "adopt", "ref": "DEMO_DEV"},
            "regions": [
              {"name": "GRA11", "network": {"cidr": "10.20.0.0/24", "vlan_id": 0}}
            ],
            # optional (P10, P11)
            "budget_alert": {"enabled": false},
            "quota_guard": {"enabled": false}
          }
        ]
      }
    ],
    "instances": [
      {"id": "account-bootstrap",      "stage": "bootstrap"},
      {"id": "account-governance",     "stage": "account-governance"},
      {"id": "demo-state",             "stage": "tenant-state",    "tenant": "demo"},
      {"id": "demo-dev-project",       "stage": "project",         "tenant": "demo", "environment": "dev"},
      {"id": "demo-dev-gra11-network", "stage": "project-network", "tenant": "demo", "environment": "dev", "region": "GRA11"},
      {"id": "demo-dev-gra11-runtime", "stage": "runtime",         "tenant": "demo", "environment": "dev", "region": "GRA11"}
    ]
  }
}
```

A tenant-only manifest (`spec.scope: tenant`, tenant-only fixture) has no account rows and lists the
platform instances it consumes; a platform manifest has no `external` entries:

```yaml
    # read from artifacts/, never planned here
    "external": [
      {"id": "account-governance", "stage": "account-governance"},
      {"id": "demo-state",         "stage": "tenant-state", "tenant": "demo"}
    ],
```

Rules (FR-006):
- `id`: `^[a-z][a-z0-9-]{2,62}$`, unique, immutable (= Terramate stack id).
- `stage`: enum `account-admin | bootstrap | tenant-state | account-governance | account-fabric |
  project | project-network | runtime | observability`; `account-admin` (reserved: the admin is
  created by `bootstrap:account`, D88), `account-fabric` and `observability` are in the enum, and the
  manifest decoder refuses a row or `external` entry naming one with `STAGE_NOT_IMPLEMENTED`, which
  `stacks:reconcile` reports.
- Names: `org`, tenant and environment names `^[a-z][a-z0-9]{0,15}$` (one naming segment: no
  separator, so joined names cannot collide and no name leaves its directory); region names
  `^[A-Z]+[0-9]+$` (3-AZ ids refused until handled); project refs `^[A-Z][A-Z0-9_]{0,62}$`; a
  tenant, an environment of one tenant or a region of one environment named twice, and one project
  ref on two environments (two `project` stacks would own one project), are `DUPLICATE_NAME`. Every other string has a pattern or an enum in the schema; the decoder applies
  the same rules.
- Scope per stage (table below): account stages forbid `tenant/environment/region`; account-tenant
  (`tenant-state`) requires `tenant` and forbids the others; `project` requires `tenant, environment`,
  forbids `region`; regional stages require all three. Named tenants, environments and regions must
  exist under `spec.tenants`.
- `slot`: optional, `^[a-z][a-z0-9]{0,15}$`, allowed only on `runtime`. At most one instance per
  (stage, tenant, environment, region, slot) — one owner per scope and slot.
- `spec.scope: platform`: exactly one `bootstrap` and one `account-governance`, at most one
  `tenant-state` per tenant, every tenant with a tenant-scoped row has a `tenant-state` row, and no
  `external` entries.
  `spec.scope: tenant`: no account or account-tenant rows; every producer a row consumes is a row or
  an `external` entry.
- `spec.state.project.ref` differs from every `tenants[].environments[].project.ref`, unless
  `spec.sandbox.shared_state_project: true`. The live lane repeats the check on the resolved ids.
- Project ids are not in the manifest; they come from the bound account file
  `~/.config/ovh-lz/accounts/<account>/account.env` (`LZ_PROJECT_ID_<REF>`). Offline fixtures supply
  synthetic ids.

## Stage table (fixed in code, `tools/internal/stacks/stages.go`)

| Stage | Scope | Consumes outputs of (data) | Needs principal or state access from (authority) | Authority | Backend | Live chain |
| --- | --- | --- | --- | --- | --- | --- |
| bootstrap | account | — | — (admin created by `bootstrap:account`) | bootstrap | local | retained |
| tenant-state | account-tenant | — | bootstrap | bootstrap | s3 (account bucket) | retained |
| account-governance | account | — | bootstrap | bootstrap | s3 (account bucket) | retained |
| project | environment | — | account-governance, tenant-state | platform | s3 (tenant bucket; KD-3) | retained |
| project-network | region | project | account-governance, tenant-state | tenant | s3 (tenant bucket) | ephemeral |
| runtime | region (+ slot) | project | account-governance, tenant-state | tenant | s3 (tenant bucket) | ephemeral |

Two edge kinds, both real: *data* edges (the consumer reads the producer's `outputs.json` as typed
variables; a changed digest re-plans the consumer) and *authority* edges (the consumer runs as a
principal, or reads/writes state with credentials, that the producer creates; no data flows). Both
become Terramate `after`; only data edges and code changes select a stack for re-planning (FR-009).
`account-governance` needs the tenant project URNs, which come from the bound account file, not from
another stack (see *Resolved-reference input*). `runtime` does not depend on `project-network` (managed-only runtime has no network).

Retained instances are protected twice: `prevent_destroy` in code (state buckets, adopted project)
and the live lane's refusal of any plan that deletes or replaces one of their resources, on every
verb.

## Derived instance fields
| Field | Derivation | Example |
| --- | --- | --- |
| path | scope rule (research R2), region lowercased, `-<slot>` suffix on the stage directory when set | `stacks/tenants/demo/dev/gra11/runtime`, `stacks/account/tenant-state/demo` |
| state bucket | account and account-tenant scope: account state bucket; tenant scope: the tenant's state bucket (one per tenant, ADR-0009, D87) | `lz-demo-bkt-state` |
| state key | `<path minus "stacks/">/terraform.tfstate` — the only derivation (research R5); asserted exactly in T033/T037 | `tenants/demo/dev/gra11/runtime/terraform.tfstate`, `account/tenant-state/demo/terraform.tfstate`, `tenants/demo/dev/gra11/runtime-blue/terraform.tfstate` |
| local state | `~/.config/ovh-lz/accounts/<account>/state/<id>.tfstate` (local backend, `bootstrap` only) | |
| outputs object | `artifacts/<id>/outputs.json` in the instance's state bucket; `bootstrap` publishes to the account bucket (ADR-0004 `artifacts/` prefix) | |
| Terramate tags | `lz-stage-*`, `lz-scope-*`, `lz-tenant-*`, `lz-env-*`, `lz-region-*`, `lz-slot-*` | |
| after | tag filters of the instance's data producers and authority producers in the same tenant/env (and region) | |
| module source | `spec.stage_source`: `local` → relative path to `stages/<stage>`; `git` → versioned ref (seam) | |
| managed-in | `<spec.forge>//<path>` | |
| bucket name (account state) | naming(org, kind=bkt, role=state) | `lz-bkt-state` |
| bucket name (tenant state) | naming(org, tenant, kind=bkt, role=state) | `lz-demo-bkt-state` |
| bucket name (runtime) | naming(org, tenant, environment, region, kind=bkt, role=runtime, slot) | `lz-demo-dev-gra11-bkt-runtime`, `lz-demo-dev-gra11-bkt-runtime-blue` |

Uniqueness checks: id, path, (state bucket, state key), and planned bucket names across all stacks
(`NAME_COLLISION`, checked on the generated stacks' plans in `test:stack-plans`). Adding a tenant adds a `tenant-state` row
(retained, `bootstrap` authority, state in the account bucket); `bootstrap` is not re-applied.

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
empty-string value anywhere in `values` (an absent capability or optional value is left out, never a
placeholder); object keys match exactly (no case variants) and appear once. The validator in
`tools/internal/stacks` enforces these with strict typed decoding; the schemas under `schemas/outputs/`
are the published contract and a test keeps them and the Go types in agreement.

Per-stage `values` (minimum):
| Stage | values |
| --- | --- |
| bootstrap | `state_bucket`, `state_project_id`, `state_region`, `state_endpoint`, `platform_s3_user_id` |
| tenant-state | `tenant`, `state_bucket`, `tenant_s3_user_id`, `platform_s3_user_id` |
| account-governance | `platform_deployer {client_id, identity_urn}`, `tenants {<t>: {deployer_client_id, deployer_identity_urn, group_urn}}` |
| project | `tenant`, `environment`, `project_id`, `project_urn`, `regions[]`, `budget_alert_id?` |
| project-network | `network_id`, `regions_openstack_ids{}`, `subnet_id`, `cidr` |
| runtime | envelope per ADR-0017: `kind: managed-only`, `slot?`, `scope {instance, project_id, region}`, `readiness`, `pending_actions[]`, `capabilities {object-storage: {bucket, endpoint, region}}` |
All stages: `unlabelled[]` = resource addresses whose API carries no tags (R14).

Sensitive outputs (never published; written to credential files by `lz-live`'s credential writer):
`bootstrap.platform_s3 {access_key_id, secret_access_key}`,
`tenant-state.tenant_s3`, `tenant-state.platform_s3`,
`account-governance.platform_deployer_secret`, `account-governance.tenant_deployer_secrets{<t>}`.

## Envelope-to-input adapter (FR-005, V012)
For a consumer instance, for each derived data edge to producer stage `S` (instance `P`):
1. read `artifacts/<P>/outputs.json` from `P`'s state bucket with the consumer's state credentials
   (offline: from the fixture or scratch publisher directory);
2. refuse when missing (`blocked`), malformed or schema-invalid (`fail`), or when `instance_id ≠ P`
   or `stage ≠ S` (`fail: wrong producer`); for `S = project`, refuse when `project_id` or
   `project_urn` differs from the resolved reference of that tenant and environment
   (`fail: unbound project`, KD-3);
3. write `{"<S with - → _>": <values>}` to `.local/live/<run-id>/inputs/<consumer>/<S>.tfvars.json`;
4. the generated `_lz_variables.tf` declares `variable "<S_snake>"` typed from `S`'s schema; the plan
   gets `-var-file` per edge; the record stores `sha256` of each consumed file.

## Resolved-reference input
Project ids are not in the manifest, so the lane writes one more input per run from `account.env`,
next to the producer inputs: `.local/live/<run-id>/inputs/<id>/resolved.tfvars.json`.
- `account-governance`: `tenants = {<t>: {project_id, project_urn}}`. The tenant names come from a
  generated, committed `_lz_tenants.auto.tfvars.json` in its stack (so a new tenant row changes its
  code digest); the ids are resolved from `LZ_PROJECT_ID_<REF>`.
- `project`: `project_id` from its environment's reference.
Its `sha256` is recorded with the consumed digests (FR-009), so a changed reference selects the
stack. The same resolved ids check every `project` artefact the adapter reads (KD-3 mitigation).

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
  segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]   # slot: runtime only
  separator = "-"
  case      = "lower"
  kinds     = { bucket = "bkt", private_network = "pn", subnet = "sn", service_account = "sa",
                iam_policy = "pol", identity_group = "grp", s3_user = "s3u" }
}
```
Output: `name`, `labels` (mandatory + extra), `algorithm_version`. Errors: unknown kind, empty
required segment, over the kind's limit, forbidden characters, doubled punctuation (buckets).

## Account binding and local files (never in the repository)
The active admin credential is `~/.config/ovh-lz/sandbox.env` (AGENTS.md). Everything else is bound to
one account under `~/.config/ovh-lz/accounts/<account>/`, where `<account>` is the account id that
`GET /auth/details` returns for the credential in use (no IAM action needed, so the admin and both
deployer classes bind the same way; `GET /me` needs `account:apiovh:me/get`, research R13, P26). Every `lz-live` run resolves the account from the
credential, loads only that directory, and refuses when `account.env`'s account id, endpoint or `org`
disagrees with the credential or the manifest.

| Path | Content | Writer |
| --- | --- | --- |
| `~/.config/ovh-lz/live.env` | `LZ_OWNER_CHECKOUT` (canonical absolute path of the owner's dedicated live clone, D92); `LZ_AGENT_WORKTREE_ROOT` (absolute path of the existing directory under which agent worktrees and clones live; no live run starts at or below it; missing, relative, absent on disk or not a directory → `live-env` refusal) | owner, once |
| `~/.config/ovh-lz/sandbox.env` | `OVH_ENDPOINT`, `OVH_CLIENT_ID/SECRET` (lz-sandbox-admin) | existing; `bootstrap:account --fresh-account` (`admin` phase) |
| `accounts/<account>/account.env` | `LZ_ACCOUNT_ID`, `OVH_ENDPOINT`, `LZ_ORG`, `LZ_PROJECT_ID_<REF>`, `LZ_ADMIN_CLIENT_ID`, `LZ_ADMIN_POLICY_ID` (leftover exemption, research R12) | bootstrap `identify` (binding; project ids prompted under `--fresh-account`, else filled by the owner) and `admin` (admin ids) |
| `accounts/<account>/sandbox.env` | a previous account's admin credential, moved there on migration | bootstrap `identify` on `--fresh-account` |
| `accounts/<account>/state-passphrase.env` | `TF_VAR_state_passphrase` | bootstrap `passphrase`, once |
| `accounts/<account>/state.env` | platform S3 keys for the account bucket | bootstrap `state` |
| `accounts/<account>/platform-deployer.env` | platform OAuth2 client | live lane after `account-governance` apply |
| `accounts/<account>/tenants/<t>/deployer.env` | tenant OAuth2 client | live lane after `account-governance` apply |
| `accounts/<account>/tenants/<t>/state.env` | tenant S3 keys for its bucket | live lane after `tenant-state` apply |
| `accounts/<account>/tenants/<t>/platform-state.env` | platform S3 keys for that tenant's bucket | live lane after `tenant-state` apply |
| `accounts/<account>/state/<id>.tfstate` | encrypted local state (`bootstrap`) | `bootstrap` |
| `accounts/<account>/state/probes/<run-id>/<probe>.tfstate` and `…/<run-id>/passphrase.env` | encrypted probe state and its per-run passphrase, kept until destroy and leftover check pass (`lz-live probe --cleanup <run-id>` resumes) | `lz-live probe` |
| `accounts/<account>/locks/{account,tenant-<t>}.lock` | run locks | `lz-live` |
Root AK/AS/CK have no file: typed at a no-echo prompt during `--fresh-account` and revoked before the
run ends. All files mode 600, directories 700; the lane refuses group/world-readable files. Only
`tools/internal/live/files.go` writes credential files.

## Live run record (gitignored, `.local/live/`)
`<run-id>/plan-<id>.txt` (rendered plans, no raw JSON), `<run-id>/inputs/<id>/*.tfvars.json`,
`<run-id>/inventory.jsonl` (one line per created resource, appended as `apply_complete` arrives),
`<run-id>/listings/<kind>.json`, `<run-id>/leftovers.json`, `<run-id>/observations.json`,
`<run-id>/summary.json` (outcome, deadline, known deviations observed), `records/<id>.json`
(`applied_at`, `source_revision`, `code_digest`, consumed `{producer: sha256}`).
Run id: `YYYYMMDDThhmmssZ-<4 hex>`.
