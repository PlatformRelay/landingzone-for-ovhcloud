# object-storage-protected

One retained Object Storage bucket for OpenTofu state (FR-002, FR-004; research R5 *Protection*):
versioning is always enabled and is not an input, and the bucket carries a literal
`lifecycle { prevent_destroy = true }`. A plan that would destroy or replace it fails.

Spec 005 T099 (owner decision D104): the bucket is created with S3 Object Lock and a default
retention in **governance** mode, `retention_days` days for every new object version. A state user
cannot delete state history (its policy denies `s3:BypassGovernanceRetention`); a holder of that
permission can, with the `x-amz-bypass-governance-retention` header
(docs.ovhcloud.com guide *Object Storage - Managing object immutability with Object Lock (WORM)*,
source `storage-and-backup/object-storage/s3-managing-object-lock.mdx`, *Governance mode* and
*Bypassing Governance mode*). Object Lock is set only at creation and cannot
be turned off; changing it replaces the bucket, which `prevent_destroy` refuses, so a state bucket
applied without the lock (none existed when it was introduced) can no longer be planned through this
module; moving it is an owner decision. Mode and status are
not inputs. Bounds of `retention_days`: at least 1 (the guide's minimum), at most 365 (ours; the
guide sets none). Whether OVHcloud enforces the lock in GRA is the state-backend probe's
observation (T010 row 7, `tests/live/probes/README.md` row 7b).

`prevent_destroy` cannot be asserted by `tofu test` (spec 005 T013); `task test:dependencies` checks
it statically instead: every resource of this package needs the literal setting, and a `removed`
block or a module source that is not a relative path is refused (rule `RETAINED_UNPROTECTED`, contracts/checks.md G7).
`components/state-backend` may create buckets only through this module (rule
`STATE_BUCKET_UNPROTECTED`).
An `_override.tf` touching the bucket must repeat the lifecycle block (the scan judges each block,
not the merged configuration). The unit tests are plan-only
for the same reason (an apply run's cleanup destroy would be refused).

```hcl
module "state_bucket" {
  source     = "../../modules/object-storage-protected"
  project_id = var.state_project_id
  region     = "GRA"
  name       = module.state_bucket_name.name
  tags       = module.state_bucket_name.labels
  retention_days = 7
}
```

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `project_id` | string | required | Public Cloud project id (service name) that holds the bucket. |
| `region` | string | required | Object Storage region name (`GRA`). |
| `name` | string | required | Bucket name from `modules/naming` (kind `bucket`). |
| `tags` | map(string) | required | Bucket tags, exactly as given (the naming `labels`); a null value is refused. |
| `retention_days` | number | required | Days every new object version stays locked (governance); whole, 1–365. |

## Outputs

| Name | Meaning |
|---|---|
| `name` | Bucket name. |
| `region` | Object Storage region name. |
| `project_id` | Project id (service name) that holds the bucket. |
| `tags` | Tags the bucket carries. |
| `object_lock` | The bucket's Object Lock: `status`, default retention `mode` and `period`. |

## Resources

- `ovh_cloud_project_storage` — provider docs:
  [`cloud_project_storage`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/cloud_project_storage).
  Attributes used (`service_name`, `region_name`, `name`, `tags`, `versioning.status`,
  `object_lock.status`, `object_lock.rule.mode`, `object_lock.rule.period`) exist in the
  pinned ovh 2.21.0 schema (`task lint` validates against it).

Removing a state bucket is a deliberate act outside this module (state removal, then deletion).
Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/object-storage-protected`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).
