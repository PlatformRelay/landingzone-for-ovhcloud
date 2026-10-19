# FR-005 (spec 007): the landing zone's own budget-alert path, `modules/cloud-project` in
# `reference` mode with `budget_alert` enabled, applied and destroyed by the probe core. The plan
# creates no `ovh_cloud_project`: this root declares none, and the module in reference mode only
# reads the run's project. The alert takes no name, so its sweep works from the run's baseline
# (FR-006, FR-007). The contact address is the account's own (data source ovh_me), so no address
# is committed.
#
# The alert's values sit in `local.budget_alert` because a test reads a child module only through
# its outputs, so probe.tftest.hcl checks them there; keep `budget_alert = local.budget_alert`.
# Arguments as the provider schema gives them (ovh 2.21.0, `ovh_cloud_project_alerting`:
# `monthly_threshold`, `email`, `delay` required); delay one of the API's cloud.AlertingDelayEnum
# values, threshold in the account currency. The probe check judges the alert against the same
# values (`planThreshold`, `planDelay` in tools/internal/probes/live/alerting/check.go): change
# both together, or the live read-back and sweep fail.
data "ovh_me" "account" {}

locals {
  budget_alert = {
    enabled           = true
    monthly_threshold = 1
    email             = data.ovh_me.account.email
    delay             = 3600
  }
}

module "project" {
  source = "../../../../modules/cloud-project"

  mode         = "reference"
  project_id   = local.project.service_name
  budget_alert = local.budget_alert
}

output "alert_id" {
  description = "Budget alert id (scalar, public): the module's budget_alert_id."
  value       = module.project.budget_alert_id
}

output "budget_alert_check" {
  description = "Check output (map of strings) that the budget-alert probe check selects (FR-008)."
  value       = { alert_id = module.project.budget_alert_id }
}

output "run_id" {
  description = "Run id this probe ran under."
  value       = local.run_id
}
