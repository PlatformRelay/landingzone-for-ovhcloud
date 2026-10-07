# Outputs of the identity/ovh-native component (spec 005 T023 interface). STUB placeholders of the
# right types; T024 reads every value from the module outputs, which read the resources.

output "platform_deployer" {
  description = "Platform deployer OAuth2 client: `client_id`, `identity_urn`."
  value = {
    client_id    = "not-implemented"
    identity_urn = "not-implemented"
  }
}

output "platform_policy" {
  description = "Platform deployer policy as the resource carries it: `name`, `identities`, `resources`, `allow`."
  value = {
    name       = "not-implemented"
    identities = []
    resources  = []
    allow      = []
  }
}

output "tenants" {
  description = "Per tenant: `deployer_client_id`, `deployer_identity_urn`, `group_urn`, `group_name`, `group_role` and the deployer `policy` (`name`, `identities`, `resources`, `allow`)."
  value = { for t in keys(var.tenants) : t => {
    deployer_client_id    = "not-implemented"
    deployer_identity_urn = "not-implemented"
    group_urn             = "not-implemented"
    group_name            = "not-implemented"
    group_role            = "not-implemented"
    policy = {
      name       = "not-implemented"
      identities = []
      resources  = []
      allow      = []
    }
  } }
}

output "unlabelled" {
  description = "Addresses of the component's resources whose API carries no tags (OAuth2 clients, policies, groups; research R14)."
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
