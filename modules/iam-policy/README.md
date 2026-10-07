# iam-policy

One IAM policy (FR-004, FR-010; research R6): every `allow` action on every `resources` URN for
every `identities` URN, in one statement. The module passes its inputs unchanged and adds nothing
of its own — no `permissions_groups`, `except`, `deny` or `expired_at`. Which actions and resources
a policy may hold is the caller's decision and the caller's guard (G5,
`components/identity/ovh-native`); the module refuses no action list.

```hcl
module "tenant_policy" {
  source      = "../../../modules/iam-policy"
  name        = module.tenant_policy_name.name
  description = "Tenant deployer on its own project"
  identities  = [module.deployer.identity]
  resources   = ["urn:v1:eu:resource:publicCloudProject:${var.project_id}"]
  allow       = local.tenant_actions
}
```

## Conditions

`conditions` is an optional tree of at most three levels, passed as given: `operator` (`AND`, `OR`,
`NOT`, `MATCH`), `values` (a map such as `resource.Tag(environment)`), and nested `condition`
lists. `MATCH` is terminal (provider docs, *Conditions*). How OVHcloud evaluates a condition is
UNVERIFIED offline (P25).

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | required | Policy name from `modules/naming` (kind `iam_policy`). |
| `description` | string | required | Policy description. |
| `identities` | set(string) | required | Identity URNs (OAuth2 client `identity`, group `urn`). |
| `resources` | set(string) | required | Resource URNs. |
| `allow` | set(string) | required | Actions allowed on every resource for every identity. |
| `conditions` | object | `null` | Condition tree, three levels at most; `null` = none. |

## Outputs

| Name | Meaning |
|---|---|
| `id` | Id of the policy. |
| `name` | Name of the policy. |

## Resources

Provider docs (ovh 2.21.0; attributes used exist in the pinned schema, `task lint` validates):
- `ovh_iam_policy` — [`iam_policy`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/iam_policy) (`name`, `description`, `identities`, `resources`, `allow`, `conditions`)

Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/iam-policy`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).
