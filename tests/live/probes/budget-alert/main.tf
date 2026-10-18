# FR-005 (spec 007): the landing zone's own budget-alert path, `modules/cloud-project` in
# `reference` mode with `budget_alert` enabled, applied and destroyed by the probe core. The plan
# creates no `ovh_cloud_project`; the alert takes no name, so its sweep works from the run's
# baseline (FR-006, FR-007). The contact address is the account's own (data source ovh_me).
#
# STUB (T010): the alert is off and the outputs are placeholders, so init and plan run offline;
# T016 enables `local.budget_alert` and wires `alert_id` and `budget_alert_check` to the module.
# The alert's values sit in `local.budget_alert` because a test reads a child module only through
# its outputs, so probe.tftest.hcl checks them there.
data "ovh_me" "account" {}

locals {
  budget_alert = { enabled = false }
}

module "project" {
  source = "../../../../modules/cloud-project"

  mode         = "reference"
  project_id   = local.project.service_name
  budget_alert = local.budget_alert
}

output "alert_id" {
  description = "Budget alert id (scalar, public): the module's budget_alert_id."
  value       = null
}

output "budget_alert_check" {
  description = "Check output (map of strings) that the budget-alert probe check selects."
  value       = { alert_id = "" }
}

output "run_id" {
  description = "Run id this probe ran under."
  value       = local.run_id
}
