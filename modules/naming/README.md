# naming

Pure-function module: one call gives the name and the labels of one logical resource
([ADR-0003](../../docs/adr/0003-layered-taxonomy-and-module-naming.md), FR-001, FR-002). No provider,
no data source, no resource; modules, components and stages may call it (ADR-0002).

```hcl
module "state_bucket_name" {
  source     = "../../modules/naming"
  org        = "lz"
  tenant     = "demo"
  kind       = "bucket"
  role       = "state"
  instance   = "demo-state"
  managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/state"
}
# module.state_bucket_name.name   == "lz-demo-bkt-state"
# module.state_bucket_name.labels == { "lz:managed-by" = "opentofu", "lz:managed-in" = "…",
#                                     "lz:instance" = "demo-state", "lz:tenant" = "demo",
#                                     "lz:release" = "unreleased" }
```

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `org` | string | required | Organisation discriminator, first segment of the default template (`lz`). |
| `tenant` | string | `null` | Tenant; `null` at account scope (no segment, no `lz:tenant` label). |
| `environment` | string | `null` | Environment; `null` above environment scope. |
| `region` | string | `null` | Region (`GRA11`); `null` above region scope. |
| `kind` | string | required | Logical kind: a key of `template.kinds` with a row in `kinds.yaml`. |
| `role` | string | required | Role of the resource in its scope (`state`, `runtime`). |
| `slot` | string | `null` | Runtime slot; two slots in one scope give two names. |
| `template` | object | default template | Naming template as data (below). |
| `name_override` | string | `null` | Existing name to keep (import): used unchanged after the kind's checks. |
| `instance` | string | required | Deployment instance id, the `lz:instance` label; not empty. |
| `managed_in` | string | required | `<forge>//<stack path>`, the `lz:managed-in` label; not empty. |
| `labels` | map(string) | `{}` | Extra labels; keys of the label set are refused in any case. |

## Outputs

| Name | Meaning |
|---|---|
| `name` | The template applied to the coordinates, or the override; fails on any name error. |
| `labels` | Mandatory labels merged with the extra labels. |
| `algorithm_version` | `1`. A change that alters any produced name is a new version and a migration. |

## Name algorithm (version 1)

The default template, the only one shipped (research R15):

```hcl
template = {
  version   = 1
  segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
  separator = "-"
  case      = "lower"
  kinds     = { bucket = "bkt", private_network = "pn", subnet = "sn", service_account = "sa",
                iam_policy = "pol", identity_group = "grp", s3_user = "s3u" }
}
```

1. Take the coordinates in `segments` order, replace `kind` by its abbreviation, omit `null` ones.
2. Join with `separator` and apply the case rule (`lower` is the only rule implemented).
3. If `name_override` is set, it replaces the result as given: never lowered, cleaned or truncated.
4. Check the name against the kind's row in [`kinds.yaml`](kinds.yaml). Any failure fails
   `output.name` with the reason; a name is never truncated or rewritten to fit.

Refused: a kind without a template abbreviation or without a `kinds.yaml` row; a coordinate given as
`""` (use `null` for an absent one); a name outside the kind's length limits or charset; for kinds
whose row says so, punctuation at either end, doubled punctuation and an IP-address shape.
A label-only change (`labels`, `instance`, `managed_in`) never changes the name.

A template is refused (on `var.template`) when its `version` is not `1`, when a segment is not one of the seven coordinates or
appears twice, when `kind` or `role` is missing from `segments`, when a kind's abbreviation is `null`
or `""` (the kind segment would be dropped silently), when `case` is not `lower`, or when the
separator is empty.

What callers own: a coordinate may itself contain the separator, so `tenant = "a-b"` without an
environment and `tenant = "a"` with `environment = "b"` give the same name; and the case rule lowers
the name but not the `lz:tenant` value, so tenants differing only in case would share names under
different authorisation keys. This module checks neither: cross-call collisions are the caller's
(ADR-0003; the generator's planned-name check refuses them, `NAME_COLLISION`, research R15), and
coordinate values are expected to be validated where they are declared (the manifest).

## Per-kind limits (`kinds.yaml`)

| Kind | Status | Limits |
|---|---|---|
| `bucket` | verified (OVHcloud S3 limitations guide, provider bucket docs) | 3–63 characters, `[a-z0-9.-]`, alphanumeric at both ends, no doubled punctuation, not an IP address |
| `private_network`, `subnet`, `service_account`, `iam_policy`, `identity_group`, `s3_user` | **UNVERIFIED** | provisional: 1–63 characters, `[a-z0-9-]` |

The OVHcloud documentation and the provider docs give no length or character rule for the six
UNVERIFIED kinds (each row cites what was checked). Their provisional limits only narrow what the
default template produces; they are not OVHcloud's limits and may be wider or narrower than the API's.
Widen a row only with a cited source or a recorded live probe, and set `verified: true` then.

## Labels

| Key | Value |
|---|---|
| `lz:managed-by` | `opentofu` |
| `lz:managed-in` | `managed_in` |
| `lz:instance` | `instance` |
| `lz:tenant` | `tenant`; absent at account scope |
| `lz:release` | `unreleased` until the release train of ADR-0010 exists |

`lz:tenant` comes only from the `tenant` coordinate: it is an authorisation key (guard G4), so an
extra label can neither set nor override it. `lz:run-id` belongs to the label set but is added by
the live lane, not by this module; it is refused as an extra label like the other five. Which
resources carry the labels is the calling module's concern (research R14).

## Tests

`tests/unit.tftest.hcl` (names, limits, refusals), `tests/contract.tftest.hcl` (labels, guard
G4) and `tests/template.tftest.hcl` (template and empty-coordinate refusals), plan only: `task test:unit -- modules/naming`, `task lint -- modules/naming`.
