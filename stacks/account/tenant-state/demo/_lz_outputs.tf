// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

output "platform_s3" {
  description = "Output platform_s3 of the tenant-state stage, re-exported for the outputs envelope."
  sensitive   = true
  value       = module.tenant_state.platform_s3
}
output "platform_s3_user_id" {
  description = "Output platform_s3_user_id of the tenant-state stage, re-exported for the outputs envelope."
  sensitive   = false
  value       = module.tenant_state.platform_s3_user_id
}
output "state_bucket" {
  description = "Output state_bucket of the tenant-state stage, re-exported for the outputs envelope."
  sensitive   = false
  value       = module.tenant_state.state_bucket
}
output "tenant" {
  description = "Output tenant of the tenant-state stage, re-exported for the outputs envelope."
  sensitive   = false
  value       = module.tenant_state.tenant
}
output "tenant_s3" {
  description = "Output tenant_s3 of the tenant-state stage, re-exported for the outputs envelope."
  sensitive   = true
  value       = module.tenant_state.tenant_s3
}
output "tenant_s3_user_id" {
  description = "Output tenant_s3_user_id of the tenant-state stage, re-exported for the outputs envelope."
  sensitive   = false
  value       = module.tenant_state.tenant_s3_user_id
}
output "unlabelled" {
  description = "Output unlabelled of the tenant-state stage, re-exported for the outputs envelope."
  sensitive   = false
  value       = module.tenant_state.unlabelled
}
