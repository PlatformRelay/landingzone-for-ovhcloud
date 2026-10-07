# Outputs of the account-governance stage (data-model *Per-stage values* `account-governance`,
# *Sensitive outputs*; schemas/outputs/account-governance.schema.json). STUB placeholders of the
# right types for T023; T024 reads every value from the component.

output "platform_deployer" {
  description = "Platform deployer OAuth2 client: `client_id`, `identity_urn`."
  value = {
    client_id    = "not-implemented"
    identity_urn = "not-implemented"
  }
}

output "tenants" {
  description = "Per tenant: `deployer_client_id`, `deployer_identity_urn`, `group_urn`."
  value = { for t in keys(var.tenants) : t => {
    deployer_client_id    = "not-implemented"
    deployer_identity_urn = "not-implemented"
    group_urn             = "not-implemented"
  } }
}

output "unlabelled" {
  description = "Addresses of the resources whose API carries no tags (research R14), from the stage root."
  value       = []
}

output "platform_deployer_secret" {
  description = "Client secret of the platform deployer; sensitive, never published."
  value       = "not-implemented"
  sensitive   = true
}

output "tenant_deployer_secrets" {
  description = "Per tenant: client secret of its deployer; sensitive, never published."
  value       = { for t in keys(var.tenants) : t => "not-implemented" }
  sensitive   = true
}
