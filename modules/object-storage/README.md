# object-storage

One Object Storage bucket with the name and tags it is given (FR-002, FR-004; research R5). The
caller builds both with [`modules/naming`](../naming/README.md) and passes them unchanged. The
bucket is replaceable: use it for ephemeral runtime buckets. State buckets use
[`modules/object-storage-protected`](../object-storage-protected/README.md).

```hcl
module "runtime_bucket" {
  source     = "../../modules/object-storage"
  project_id = var.project_id
  region     = "GRA"
  name       = module.runtime_bucket_name.name
  tags       = module.runtime_bucket_name.labels
}
```

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `project_id` | string | required | Public Cloud project id (service name) that holds the bucket. |
| `region` | string | required | Object Storage region name (`GRA`). |
| `name` | string | required | Bucket name from `modules/naming` (kind `bucket`). |
| `tags` | map(string) | required | Bucket tags, exactly as given (the naming `labels`); a null value is refused. |
| `versioning` | bool | `false` | `true` enables versioning; `false` sends no `versioning` block (the default of a new bucket is UNVERIFIED). Setting `false` on a bucket that has versioning does not suspend it (believed: the attribute is optional and computed); S3 versioning can be suspended, never removed. |

## Outputs

| Name | Meaning |
|---|---|
| `name` | Bucket name. |
| `region` | Object Storage region name. |

## Resources

- `ovh_cloud_project_storage` — provider docs:
  [`cloud_project_storage`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/cloud_project_storage).
  Attributes used (`service_name`, `region_name`, `name`, `tags`, `versioning.status`) exist in the
  pinned ovh 2.21.0 schema (`task lint` validates against it).

Tests: `tests/unit.tftest.hcl` (mocked provider, no credential); `task test:unit -- modules/object-storage`.
The committed `.terraform.lock.hcl` is the test lock file for the offline entry's
`init -lockfile=readonly`; a consuming root owns its own lock (ADR-0011, amendment 2026-10-07).
