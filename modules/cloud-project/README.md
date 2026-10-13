# cloud-project

One existing Public Cloud project, adopted or referenced (FR-002, FR-004; research R7). The module
never orders a project: in `adopt` mode the stack imports the existing one into
`ovh_cloud_project.this[0]`; in `reference` mode the project is only read
(`data.ovh_cloud_project`). In both modes an optional budget alert can be set. The project's IAM
tags are not set here: the `account-governance` stage sets them under the admin through
[`modules/iam-resource-tags`](../iam-resource-tags/README.md), because the platform deployer that
applies the project stage was refused the tag PUT (evidence/T049.md run 3; D109, T105).

`ovh_cloud_project` is created through the order workflow and deleted through termination
(provider docs `cloud_project`), so a replacement would order a new project. The adopted project
therefore carries a literal `lifecycle { prevent_destroy = true }` and `deletion_protection = true`,
and the order arguments (`ovh_subsidiary`, `description`, `plan`) are passed exactly as the import
plan shows them, never invented. Whether the import plans no change is P5, UNVERIFIED until T009.

`prevent_destroy` cannot be asserted by `tofu test` (spec 005 T013); `task test:dependencies`
checks it statically (rule `RETAINED_UNPROTECTED`, contracts/checks.md G7): the package declares
exactly one `ovh_cloud_project`, in its root directory and without `for_each` (the unit tests pin
its `count` to 0 or 1), every block of it carries both literals, and a `removed` block or a
module source that is not a relative path is refused. The budget alert is not retained: switching
it off removes it. An `_override.tf` touching the project must repeat both literals. The unit tests are plan-only for the same reason as the protected bucket.

```hcl
module "project" {
  source         = "../../modules/cloud-project"
  mode           = "adopt"
  project_id     = var.project_id
  ovh_subsidiary = var.ovh_subsidiary # from the import plan
  description    = var.description    # from the import plan
  plan           = var.project_plan   # from the import plan, or null
  budget_alert   = { enabled = true, monthly_threshold = 50, email = var.billing_email }
}
```

## Switching an adopted project to reference mode

Changing `mode` from `adopt` to `reference` plans the destroy of `ovh_cloud_project.this[0]`;
`prevent_destroy` fails that plan, and the live lane refuses a delete or a `forget` of a retained
resource (T064). The switch is therefore an operator runbook step, never code: no `removed` block
is added to this module or the stack (the scan refuses one here).

1. Plan the stack in reference mode: the plan is expected to fail on `prevent_destroy` for
   `…ovh_cloud_project.this[0]` and nothing else. Any other error stops the runbook.
2. Outside the live lane, with the platform deployer's state access, remove the project from the
   stack's state: `tofu state rm '<stack address>.ovh_cloud_project.this[0]'`. The project itself
   is untouched; it is no longer managed.
3. Plan again in reference mode: no destroy, and the data source reads the project (its tags,
   set by `account-governance` on the same URN, are untouched). Then apply through the live lane
   as usual.

After the switch nothing in OpenTofu protects the project any more: `deletion_protection` is the
provider's own refusal of a destroy of the managed resource ("When set to `true`, `terraform
destroy` will fail", provider docs `cloud_project`), held in state (UNVERIFIED that the API keeps an equivalent
setting on the project; do not rely on one); deleting the project then takes a termination outside this repository.

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `mode` | string | required | `adopt` or `reference`; anything else is refused. |
| `project_id` | string | required | Project id (service name); blank refused. |
| `ovh_subsidiary` | string | null | Adopt: the subsidiary from the import plan. |
| `description` | string | null | Adopt: the current description from the import plan. |
| `plan` | object({duration, plan_code, pricing_mode}) | null | Adopt: the order plan from the import plan; null sends none. |
| `budget_alert` | object({enabled, monthly_threshold, email, delay = 3600}) | `{enabled = false}` | Enabled needs a threshold above 0 and a non-blank email; `delay` one of the API's `cloud.AlertingDelayEnum` values (3600 … 604800 s). |

## Outputs

| Name | Meaning |
|---|---|
| `project_id` | The given project id. |
| `urn` | The project's IAM URN (adopt: the resource's `urn`; reference: the data source's `iam.urn`). |

## Resources

`ovh_cloud_project` (adopt only), `data.ovh_cloud_project` (reference only),
`ovh_cloud_project_alerting` (when enabled). Attributes exist in the pinned ovh 2.21.0 schema
(`task lint` validates against it).

Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/cloud-project`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).
