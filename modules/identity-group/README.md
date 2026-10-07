# identity-group

One identity group (FR-004, FR-010; research R6). Its `urn` goes into the `identities` of an IAM
policy ([`modules/iam-policy`](../iam-policy/README.md)). The name comes from
[`modules/naming`](../naming/README.md) (kind `identity_group`).

```hcl
module "tenant_group" {
  source      = "../../../modules/identity-group"
  name        = module.tenant_group_name.name
  description = "Tenant members"
}
```

## Role

`role` is one of `ADMIN`, `REGULAR`, `UNPRIVILEGED`, `NONE` (provider docs; API schema `me.json`),
case-sensitive, default `NONE`; a null role is `NONE`, so the module always sets it. What each role
grants is UNVERIFIED offline. The module admits `ADMIN`; the tenant group's `NONE` is held by its
caller (T023).

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | required | Group name from `modules/naming` (kind `identity_group`). |
| `description` | string | required | Group description. |
| `role` | string | `"NONE"` | Account role of the group's members. |

## Outputs

| Name | Meaning |
|---|---|
| `urn` | URN of the group, `urn:v1:eu:identity:group:<nic>/<name>` (docs.ovhcloud.com `account-information/iam-policies-api`). |
| `name` | Name of the group. |
| `role` | The group's role as the resource carries it. |

## Resources

Provider docs (ovh 2.21.0; attributes used exist in the pinned schema, `task lint` validates):
- `ovh_me_identity_group` — [`me_identity_group`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/me_identity_group) (`name`, `description`, `role`, `urn`)

Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/identity-group`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).
