# iam-resource-tags

IAM tags on one existing resource URN (FR-004; research R12, R14, P15). In this slice it tags the
tenant projects from `components/identity/ovh-native`, which the `account-governance` stage applies
under the admin (D109, T105): the platform deployer was refused the tag PUT on the project even with
`account:apiovh:iam/resource/edit` granted on the project URN (evidence/T049.md run 3). The tags
come from [`modules/naming`](../naming/README.md) (`labels`).

```hcl
module "project_tags" {
  source   = "../../../modules/iam-resource-tags"
  for_each = var.tenants
  urn      = each.value.project_urn
  tags     = module.project_labels[each.key].labels
}
```

The resource manages only the keys in `tags`: destroying it removes those keys and leaves the
resource and its other tags untouched (provider docs `iam_resource_tags`, Notes), so it is not
retained and needs no `prevent_destroy`. Tag keys follow the provider's pattern
`^[a-zA-Z0-9_.:/=+@-]{1,128}$`, and the `ovh:` prefix is reserved (same source).

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `urn` | string | required | URN of the resource to tag, exactly as given; blank refused. |
| `tags` | map(string) | required | Tags on the URN, exactly as given; a null value is refused. |

## Outputs

| Name | Meaning |
|---|---|
| `urn` | The tagged URN, read from the resource. |
| `tags` | The tags the URN carries, read from the resource. |

## Resources

Provider docs (ovh 2.21.0; attributes used exist in the pinned schema, `task lint` validates):
- `ovh_iam_resource_tags` — [`iam_resource_tags`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/iam_resource_tags) (`urn`, `tags`)

Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/iam-resource-tags`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).
