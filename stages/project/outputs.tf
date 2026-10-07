# Outputs of the project stage (data-model *Per-stage values* `project`;
# schemas/outputs/project.schema.json): the project values are read from the component, never
# recomputed. All published; none is sensitive.

output "tenant" {
  description = "Tenant of the environment."
  value       = var.tenant
}

output "environment" {
  description = "Environment."
  value       = var.environment
}

output "project_id" {
  description = "Project id: the resolved reference the component manages or reads (KD-3)."
  value       = module.project_factory.project_id
}

output "project_urn" {
  description = "The project's own IAM URN as the component reads it (KD-3)."
  value       = module.project_factory.project_urn
}

output "regions" {
  description = "Region names of the environment, as given."
  value       = var.regions
}

output "budget_alert_id" {
  description = "Id of the budget alert; null (absent from the published values) when the alert is off."
  value       = module.project_factory.budget_alert_id
}

output "unlabelled" {
  description = "Addresses (from the stage root) of the resources whose API carries no tags (research R14)."
  value       = [for a in module.project_factory.unlabelled : "module.project_factory.${a}"]
}
