# Outputs of the project stage (data-model *Per-stage values* `project`;
# schemas/outputs/project.schema.json). STUB placeholders: T028 reads the project values from the
# component, never recomputes them. All published; none is sensitive.

output "tenant" {
  description = "Tenant of the environment."
  value       = "not-implemented"
}

output "environment" {
  description = "Environment."
  value       = "not-implemented"
}

output "project_id" {
  description = "Project id: the resolved reference the component manages or reads (KD-3)."
  value       = "not-implemented"
}

output "project_urn" {
  description = "The project's own IAM URN as the component reads it (KD-3)."
  value       = "not-implemented"
}

output "regions" {
  description = "Region names of the environment, as given."
  value       = []
}

output "budget_alert_id" {
  description = "Id of the budget alert; null (absent from the published values) when the alert is off."
  value       = "not-implemented"
}

output "unlabelled" {
  description = "Addresses (from the stage root) of the resources whose API carries no tags (research R14)."
  value       = ["not-implemented"]
}
