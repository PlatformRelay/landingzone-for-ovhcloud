# iam-service-account

One OAuth2 service account (FR-004, FR-010; research R6): a client of the `CLIENT_CREDENTIALS`
flow, which needs no callback URL (docs.ovhcloud.com `manage-and-operate/api/manage-service-account`,
*Create a service account*). Its `identity` URN goes into the `identities` of an IAM policy
([`modules/iam-policy`](../iam-policy/README.md)); the module grants nothing itself. The name comes
from [`modules/naming`](../naming/README.md) (kind `service_account`).

```hcl
module "deployer" {
  source      = "../../../modules/iam-service-account"
  name        = module.deployer_name.name
  description = "Tenant deployer"
}
```

## Secret

`client_secret` is a sensitive output and appears in no other output. It stays in the encrypted
state; the credential writer reads it once (R6). It is never published (G2): `task test:dependencies`
refuses a `nonsensitive()` call in the `*.tf` files of a module, component, stage or stack (rule
`NONSENSITIVE_CALL`); a template rendered by `templatefile()` is not scanned.
`discard_client_secret` is not used: it exists from provider 2.22.0 only (P20).

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | required | OAuth2 client name from `modules/naming` (kind `service_account`). |
| `description` | string | required | OAuth2 client description. |

## Outputs

| Name | Meaning |
|---|---|
| `client_id` | Client id of the OAuth2 client. |
| `identity` | Identity URN of the client, `urn:v1:<eu\|ca>:identity:credential:<nic>/oauth2-<client id>` (same guide). |
| `client_secret` | Client secret; sensitive. Never published (G2). |

## Resources

Provider docs (ovh 2.21.0; attributes used exist in the pinned schema, `task lint` validates):
- `ovh_me_api_oauth2_client` — [`me_api_oauth2_client`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/me_api_oauth2_client) (`name`, `description`, `flow`, `client_id`, `client_secret`, `identity`)

Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/iam-service-account`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).
