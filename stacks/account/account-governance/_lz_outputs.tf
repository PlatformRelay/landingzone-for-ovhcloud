// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

output "platform_deployer" {
  description = "Output platform_deployer of the account-governance stage, re-exported for the outputs envelope."
  sensitive   = false
  value       = module.account_governance.platform_deployer
}
output "platform_deployer_secret" {
  description = "Output platform_deployer_secret of the account-governance stage, re-exported for the outputs envelope."
  sensitive   = true
  value       = module.account_governance.platform_deployer_secret
}
output "tenant_deployer_secrets" {
  description = "Output tenant_deployer_secrets of the account-governance stage, re-exported for the outputs envelope."
  sensitive   = true
  value       = module.account_governance.tenant_deployer_secrets
}
output "tenants" {
  description = "Output tenants of the account-governance stage, re-exported for the outputs envelope."
  sensitive   = false
  value       = module.account_governance.tenants
}
output "unlabelled" {
  description = "Output unlabelled of the account-governance stage, re-exported for the outputs envelope."
  sensitive   = false
  value       = module.account_governance.unlabelled
}
