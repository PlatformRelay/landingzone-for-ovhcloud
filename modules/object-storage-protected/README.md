# object-storage-protected

One retained Object Storage bucket for OpenTofu state (FR-002, FR-004; research R5 *Protection*):
versioning is always enabled and is not an input, and the bucket carries a literal
`lifecycle { prevent_destroy = true }`. A plan that would destroy or replace it fails.

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
}
```

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `project_id` | string | required | Public Cloud project id (service name) that holds the bucket. |
| `region` | string | required | Object Storage region name (`GRA`). |
| `name` | string | required | Bucket name from `modules/naming` (kind `bucket`). |
| `tags` | map(string) | required | Bucket tags, exactly as given (the naming `labels`); a null value is refused. |

## Outputs

| Name | Meaning |
|---|---|
| `name` | Bucket name. |
| `region` | Object Storage region name. |
| `project_id` | Project id (service name) that holds the bucket. |
| `tags` | Tags the bucket carries. |

## Resources

- `ovh_cloud_project_storage` — provider docs:
  [`cloud_project_storage`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/cloud_project_storage).
  Attributes used (`service_name`, `region_name`, `name`, `tags`, `versioning.status`) exist in the
  pinned ovh 2.21.0 schema (`task lint` validates against it).

Removing a state bucket is a deliberate act outside this module (state removal, then deletion).
Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/object-storage-protected`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).
