output "project_id" {
  description = "Public Cloud project id (service name)."
  value       = var.project_id
}

output "urn" {
  description = "IAM URN of the project."
  value       = local.urn
}

output "budget_alert_id" {
  description = "Id of the budget alert; null when the alert is off."
  value       = one(ovh_cloud_project_alerting.this[*].id)
}
