# Outputs of the identity/ovh-native component (spec 005 T023 interface, T024). Every value comes
# from a module output, which reads the resource: policy contents and group role as planned, not as
# passed.

output "platform_deployer" {
  description = "Platform deployer OAuth2 client: `client_id`, `identity_urn`."
  value = {
    client_id    = module.platform_deployer.client_id
    identity_urn = module.platform_deployer.identity
  }
}

output "platform_policy" {
  description = "Platform deployer policy as the resource carries it: `name`, `identities`, `resources`, `allow`."
  value = {
    name       = module.platform_policy.name
    identities = module.platform_policy.identities
    resources  = module.platform_policy.resources
    allow      = module.platform_policy.allow
  }
}

output "tenants" {
  description = "Per tenant: `deployer_client_id`, `deployer_identity_urn`, `group_urn`, `group_name`, `group_role` and the deployer `policy` (`name`, `identities`, `resources`, `allow`)."
  value = { for t in keys(var.tenants) : t => {
    deployer_client_id    = module.tenant_deployer[t].client_id
    deployer_identity_urn = module.tenant_deployer[t].identity
    group_urn             = module.tenant_group[t].urn
    group_name            = module.tenant_group[t].name
    group_role            = module.tenant_group[t].role
    policy = {
      name       = module.tenant_policy[t].name
      identities = module.tenant_policy[t].identities
      resources  = module.tenant_policy[t].resources
      allow      = module.tenant_policy[t].allow
    }
  } }
}

output "unlabelled" {
  description = "Addresses of the component's resources whose API carries no tags (OAuth2 clients, policies, groups; research R14)."
  value = concat(
    ["module.platform_deployer.ovh_me_api_oauth2_client.this", "module.platform_policy.ovh_iam_policy.this"],
    flatten([for t in sort(keys(var.tenants)) : [
      "module.tenant_deployer[\"${t}\"].ovh_me_api_oauth2_client.this",
      "module.tenant_policy[\"${t}\"].ovh_iam_policy.this",
      "module.tenant_group[\"${t}\"].ovh_me_identity_group.this",
    ]]),
  )
}

output "platform_deployer_secret" {
  description = "Client secret of the platform deployer; sensitive, never published."
  value       = module.platform_deployer.client_secret
  sensitive   = true
}

output "tenant_deployer_secrets" {
  description = "Per tenant: client secret of its deployer; sensitive, never published."
  value       = { for t in keys(var.tenants) : t => module.tenant_deployer[t].client_secret }
  sensitive   = true
}
