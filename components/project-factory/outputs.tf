# Outputs of the project-factory component (spec 005 T027 interface), each read from the module
# calls, never recomputed.

output "project_id" {
  description = "Project id: the one passed to modules/cloud-project (the bound reference, KD-3)."
  value       = module.project.project_id
}

output "project_urn" {
  description = "The project's own IAM URN as modules/cloud-project reads it (adopted resource or read project); refused unless it is urn:v1:<eu|ca>:resource:publicCloudProject:<project_id> (KD-3)."
  value       = module.project.urn

  precondition {
    condition     = can(regex("^urn:v1:(eu|ca):resource:publicCloudProject:[0-9a-f]+$", module.project.urn)) && endswith(module.project.urn, ":${module.project.project_id}")
    error_message = "project_urn must be urn:v1:<eu|ca>:resource:publicCloudProject:<project_id> of the bound project (KD-3)."
  }
}

output "labels" {
  description = "Tags the project URN carries: the modules/naming label set of this scope, read from the tags resource."
  value       = module.project.tags
}

output "budget_alert_id" {
  description = "Id of the budget alert; null when the alert is off."
  value       = module.project.budget_alert_id
}

output "prevent_automatic_quota_upgrade" {
  description = "modules/cloud-quota's flag: true when the quota guard is on, null when off."
  value       = module.quota.prevent_automatic_quota_upgrade
}

output "unlabelled" {
  description = "Addresses (from the component root) of the managed resources whose API carries no tags: the alert and the quota setting when on (research R14)."
  # From the toggles the modules receive, so the list is known at plan time (the alert's id is not).
  value = concat(
    var.budget_alert.enabled ? ["module.project.ovh_cloud_project_alerting.this[0]"] : [],
    var.quota_guard.enabled ? ["module.quota.ovh_cloud_quota.this[0]"] : [],
  )
}
