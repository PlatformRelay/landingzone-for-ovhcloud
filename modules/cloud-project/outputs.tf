output "project_id" {
  description = "Public Cloud project id (service name)."
  value       = var.project_id
}

output "urn" {
  description = "IAM URN of the project (the target of its resource tags)."
  value       = local.urn
}

output "budget_alert_id" {
  description = "Id of the budget alert; null when the alert is off."
  value       = one(ovh_cloud_project_alerting.this[*].id)
}

output "tags" {
  description = "Tags the project URN carries, read from the tags resource."
  value       = ovh_iam_resource_tags.this.tags
}
