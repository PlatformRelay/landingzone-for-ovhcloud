# One existing Public Cloud project (spec 005, research R7). `adopt` manages it as
# `ovh_cloud_project.this[0]` (the stack imports it there); `reference` only reads it. A managed
# project without an import would order one, and a replacement would order a new one
# (cloud_project.md: created through the order workflow, deleted through termination), so the
# project carries a literal `prevent_destroy` and `deletion_protection = true`, and the order
# arguments are passed unchanged, never invented. `task test:dependencies` pins both literals and a
# single project address (rule `RETAINED_UNPROTECTED`, contracts/checks.md G7).
resource "ovh_cloud_project" "this" {
  count = var.mode == "adopt" ? 1 : 0

  ovh_subsidiary      = var.ovh_subsidiary
  description         = var.description
  deletion_protection = true

  dynamic "plan" {
    for_each = var.plan == null ? [] : [var.plan]
    content {
      duration     = plan.value.duration
      plan_code    = plan.value.plan_code
      pricing_mode = plan.value.pricing_mode
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

data "ovh_cloud_project" "this" {
  count = var.mode == "reference" ? 1 : 0

  service_name = var.project_id
}

locals {
  # The project's own URN in either mode; never built from the id.
  urn = var.mode == "adopt" ? ovh_cloud_project.this[0].urn : data.ovh_cloud_project.this[0].iam.urn
}

# Labels on the project URN in both modes. Destroying this resource removes only the keys in
# `tags`; the project and its other tags are untouched (iam_resource_tags.md, Notes), so it needs
# no `prevent_destroy`.
resource "ovh_iam_resource_tags" "this" {
  urn  = local.urn
  tags = var.tags
}

# Optional budget alert (P10): a separate API object (`/cloud/project/{p}/alerting`) that a
# configuration change may remove.
resource "ovh_cloud_project_alerting" "this" {
  count = var.budget_alert.enabled ? 1 : 0

  service_name      = var.project_id
  monthly_threshold = var.budget_alert.monthly_threshold
  email             = var.budget_alert.email
  delay             = var.budget_alert.delay
}
