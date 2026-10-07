# Outputs of the account-governance stage (data-model *Per-stage values* `account-governance`,
# *Sensitive outputs*; schemas/outputs/account-governance.schema.json), read from the component.

output "platform_deployer" {
  description = "Platform deployer OAuth2 client: `client_id`, `identity_urn`."
  value       = module.identity.platform_deployer
}

output "tenants" {
  description = "Per tenant: `deployer_client_id`, `deployer_identity_urn`, `group_urn`."
  value = { for t, v in module.identity.tenants : t => {
    deployer_client_id    = v.deployer_client_id
    deployer_identity_urn = v.deployer_identity_urn
    group_urn             = v.group_urn
  } }
}

output "unlabelled" {
  description = "Addresses of the resources whose API carries no tags (research R14), from the stage root."
  value       = [for a in module.identity.unlabelled : "module.identity.${a}"]
}

output "platform_deployer_secret" {
  description = "Client secret of the platform deployer; sensitive, never published."
  value       = module.identity.platform_deployer_secret
  sensitive   = true
}

output "tenant_deployer_secrets" {
  description = "Per tenant: client secret of its deployer; sensitive, never published."
  value       = module.identity.tenant_deployer_secrets
  sensitive   = true
}
