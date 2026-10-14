# cloud-quota

Optional quota guard of one Public Cloud project (spec 005 premise P11, optional and off by
default). With `enabled = true` the module reads the project's current regions and quota profiles
(`data.ovh_cloud_quota`) and sends them back unchanged in `ovh_cloud_quota` with
`prevent_automatic_quota_upgrade = true`, so the only thing it changes is that flag. With
`enabled = false` (the default) it reads nothing and manages nothing.

`ovh_cloud_quota` is a singleton per project and `regions` is a required argument; regions left out
of it are left unchanged upstream (provider docs `cloud_quota`). Sending the regions as read keeps
the guard from choosing a profile of its own. Whether the flag can be set on the sandbox project is
P11, UNVERIFIED until T009 (plan only); what destroying the singleton does is UNVERIFIED, so the
unit tests are plan-only.

```hcl
module "quota" {
  source     = "../../modules/cloud-quota"
  project_id = module.project.project_id
  enabled    = var.quota_guard.enabled
}
```

## Callers

[`components/project-factory`](../../components/project-factory/main.tf) calls it as `module.quota`
with the project from [`modules/cloud-project`](../cloud-project/README.md). The `project` stage
passes its optional input `quota_guard = { enabled = bool }` (default `{ enabled = false }`) through
to it, so the guard is off unless a deployment turns it on.

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `project_id` | string | required | Public Cloud project id (service name) whose quota envelope is set; blank refused. |
| `enabled` | bool | `false` | When true, automatic quota upgrades are disabled; when false, the module manages nothing. |

The flag and the regions are not inputs.

## Outputs

| Name | Meaning |
|---|---|
| `prevent_automatic_quota_upgrade` | Whether the module disables automatic quota upgrades; null when the guard is off. |

## Resources

`data.ovh_cloud_quota` and `ovh_cloud_quota`, each only when `enabled`. Attributes exist in the
pinned ovh 2.21.0 schema (`task lint` validates against it).

## Sources

- Provider docs `cloud_quota` (resource and data source).
- [Spec 005](../../specs/005-first-landing-zone-slice/spec.md) premise P11 and
  [research](../../specs/005-first-landing-zone-slice/research.md) (resource applicability table:
  `ovh_cloud_quota`, optional, retained with the project).

Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/cloud-quota`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).
