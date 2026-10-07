# Outputs of the project-factory component (spec 005 T027 interface). STUB placeholders: T028
# reads each value from the module calls, never recomputes it.

output "project_id" {
  description = "Project id: the one passed to modules/cloud-project (the bound reference, KD-3)."
  value       = "not-implemented"
}

output "project_urn" {
  description = "The project's own IAM URN as modules/cloud-project reads it (adopted resource or read project); refused unless it is urn:v1:<eu|ca>:resource:publicCloudProject:<project_id> (KD-3)."
  value       = "not-implemented"
}

output "labels" {
  description = "Tags the project URN carries: the modules/naming label set of this scope."
  value       = {}
}

output "budget_alert_id" {
  description = "Id of the budget alert; null when the alert is off."
  value       = "not-implemented"
}

output "prevent_automatic_quota_upgrade" {
  description = "modules/cloud-quota's flag: true when the quota guard is on, null when off."
  value       = false
}

output "unlabelled" {
  description = "Addresses (from the component root) of the managed resources whose API carries no tags: the alert and the quota setting when on (research R14)."
  value       = ["not-implemented"]
}
