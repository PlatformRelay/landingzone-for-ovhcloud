# Account-governance stage (spec 005 T023; FR-002, FR-004, FR-010, FR-013; ADR-0006, ADR-0009,
# ADR-0018; research R6, P9, P26; data-model *Stage table*, *Per-stage values* `account-governance`,
# *Sensitive outputs*, *Resolved-reference input*; contracts/checks.md G5; D88). Mocked provider, no
# credential, no API call.
#
# The stage takes the `tenants` map (resolved-reference input, `{<t>: {project_id, project_urn}}`)
# and, through its one components/identity/ovh-native call (`module.identity`, the only edge a stage
# has, ADR-0002), creates the platform deployer and one deployer, policy and group per entry. A
# second entry adds its own resources and leaves the first entry's values and addresses unchanged in
# the plan. The tenant group has role `NONE` and no members; no S3 user is planned here (moved to
# tenant-state). The component's tests pin G5 in detail; this file checks that the stage passes the
# input through unchanged (tenant policy = P9 allowlist on its own project URN, platform policy =
# `publicCloudProject:apiovh:*` on the tenant project URNs) and its own outputs. No backend or
# provider configuration: `task test:dependencies` refuses one in a stage, and any resource in a
# stage (STAGE_RESOURCE).
#
# Outputs (schemas/outputs/account-governance.schema.json): `platform_deployer {client_id,
# identity_urn}`, `tenants {<t>: {deployer_client_id, deployer_identity_urn, group_urn}}` and
# `unlabelled` (the OAuth2 clients, policies and groups, from the stage root) are published, so not
# sensitive; `platform_deployer_secret` and `tenant_deployer_secrets{<t>}` only sensitive (written to
# `platform-deployer.env` and `tenants/<t>/deployer.env` by the live lane). That no other published
# output exists is not observable here: the envelope validator's `additionalProperties: false`
# refuses one (T018).
#
# Tripwires: a non-computed mock default on a group member (`ovh_me_identity_user`) or an S3
# user, credential or S3 policy fails the run if one is planned (observed, tofu 1.13.0).

mock_provider "ovh" {
  mock_resource "ovh_me_api_oauth2_client" {
    defaults = {
      id            = "mock-client-resource-id"
      client_id     = "mock-default-client-id"
      identity      = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-mock-default-client-id"
      client_secret = "mock-default-client-secret"
    }
  }

  mock_resource "ovh_me_identity_group" {
    defaults = {
      id  = "mock-group-resource-id"
      urn = "urn:v1:eu:identity:group:xx0000-ovh/mock-default-group"
    }
  }

  mock_resource "ovh_iam_policy" {
    defaults = {
      id = "mock-policy-id"
    }
  }

  mock_resource "ovh_me_identity_user" {
    defaults = {
      group = "tripwire-the-tenant-group-has-no-members"
    }
  }

  mock_resource "ovh_cloud_project_user" {
    defaults = {
      description = "tripwire-no-s3-user-in-account-governance"
    }
  }

  mock_resource "ovh_cloud_project_user_s3_credential" {
    defaults = {
      user_id = "tripwire-no-s3-credential-in-account-governance"
    }
  }

  mock_resource "ovh_cloud_project_user_s3_policy" {
    defaults = {
      policy = "tripwire-no-s3-policy-in-account-governance"
    }
  }
}

override_resource {
  target = module.identity.module.platform_deployer.ovh_me_api_oauth2_client.this
  values = {
    client_id     = "platform-client-id"
    identity      = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-platform-client-id"
    client_secret = "platform-client-secret"
  }
}

override_resource {
  target = module.identity.module.tenant_deployer["demo"].ovh_me_api_oauth2_client.this
  values = {
    client_id     = "demo-client-id"
    identity      = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"
    client_secret = "demo-client-secret"
  }
}

override_resource {
  target = module.identity.module.tenant_deployer["alpha"].ovh_me_api_oauth2_client.this
  values = {
    client_id     = "alpha-client-id"
    identity      = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-alpha-client-id"
    client_secret = "alpha-client-secret"
  }
}

override_resource {
  target = module.identity.module.tenant_group["demo"].ovh_me_identity_group.this
  values = {
    urn = "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
  }
}

override_resource {
  target = module.identity.module.tenant_group["alpha"].ovh_me_identity_group.this
  values = {
    urn = "urn:v1:eu:identity:group:xx0000-ovh/alpha-group"
  }
}

variables {
  org        = "lz"
  instance   = "account-governance"
  managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/account-governance"
  tenants = {
    demo = {
      project_id  = "0123456789abcdef0123456789abcdef"
      project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    }
  }
}

run "published_outputs_match_the_schema" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
      }
      alpha = {
        project_id  = "fedcba9876543210fedcba9876543210"
        project_urn = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }

  assert {
    condition = try(output.platform_deployer == {
      client_id    = "platform-client-id"
      identity_urn = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-platform-client-id"
    }, false)
    error_message = "platform_deployer is exactly {client_id, identity_urn} of the platform client (schema)"
  }

  assert {
    condition     = try(join(",", sort(keys(output.tenants))) == "alpha,demo", false)
    error_message = "tenants holds one entry per tenant, keyed by tenant name"
  }

  assert {
    condition = try(output.tenants["demo"] == {
      deployer_client_id    = "demo-client-id"
      deployer_identity_urn = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"
      group_urn             = "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
      } && output.tenants["alpha"] == {
      deployer_client_id    = "alpha-client-id"
      deployer_identity_urn = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-alpha-client-id"
      group_urn             = "urn:v1:eu:identity:group:xx0000-ovh/alpha-group"
    }, false)
    error_message = "each tenants entry is exactly {deployer_client_id, deployer_identity_urn, group_urn} of its own deployer and group (schema)"
  }

  assert {
    condition     = startswith(jsonencode(output.unlabelled), "[") && length(output.unlabelled) > 0 && alltrue([for a in output.unlabelled : a != ""])
    error_message = "unlabelled is a list (not a map or object) of non-empty strings"
  }

  assert {
    condition     = !anytrue([issensitive(output.platform_deployer), issensitive(output.tenants), issensitive(output.unlabelled)])
    error_message = "the published outputs are not sensitive (a sensitive one is left out of the envelope and fails the schema's required list)"
  }
}

# G5 through the stage: the stage passes `tenants` unchanged, so the tenant policy is the P9
# allowlist on the tenant's own project URN and the platform policy covers that URN.
run "deployer_policies_scoped_through_the_stage" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
      }
      alpha = {
        project_id  = "fedcba9876543210fedcba9876543210"
        project_urn = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }

  assert {
    condition = try(toset(module.identity.tenants["demo"].policy.allow) == toset(module.identity.tenants["alpha"].policy.allow) && toset(module.identity.tenants["demo"].policy.allow) == toset([
      "publicCloudProject:apiovh:network/private/create",
      "publicCloudProject:apiovh:network/private/get",
      "publicCloudProject:apiovh:network/private/edit",
      "publicCloudProject:apiovh:network/private/delete",
      "publicCloudProject:apiovh:network/private/region/create",
      "publicCloudProject:apiovh:network/private/subnet/create",
      "publicCloudProject:apiovh:network/private/subnet/get",
      "publicCloudProject:apiovh:network/private/subnet/delete",
      "publicCloudProject:apiovh:region/storage/create",
      "publicCloudProject:apiovh:region/storage/get",
      "publicCloudProject:apiovh:region/storage/edit",
      "publicCloudProject:apiovh:region/storage/delete",
      "publicCloudProject:apiovh:region/storage/bulkDeleteObjects",
    ]), false)
    error_message = "G5: the tenant deployer policy allows exactly the P9 allowlist"
  }

  assert {
    condition = try(toset(module.identity.tenants["demo"].policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"])
    && toset(module.identity.tenants["alpha"].policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"]), false)
    error_message = "G5: each tenant deployer policy covers only that tenant's project URN given in tenants"
  }

  assert {
    condition = try(toset(module.identity.tenants["demo"].policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"])
    && toset(module.identity.tenants["alpha"].policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-alpha-client-id"]), false)
    error_message = "each tenant deployer policy applies to its own deployer only"
  }

  assert {
    condition = try(toset(module.identity.platform_policy.allow) == toset(["publicCloudProject:apiovh:*"])
      && toset(module.identity.platform_policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef", "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"])
    && toset(module.identity.platform_policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-platform-client-id"]), false)
    error_message = "the platform deployer policy covers publicCloudProject:apiovh:* on the tenant project URNs"
  }
}

run "tenant_group_role_none_without_members" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
      }
      alpha = {
        project_id  = "fedcba9876543210fedcba9876543210"
        project_urn = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }

  assert {
    condition     = try(length(module.identity.tenants) == 2 && alltrue([for t in ["demo", "alpha"] : module.identity.tenants[t].group_role == "NONE"]), false)
    error_message = "every tenant identity group has role NONE"
  }

  assert {
    condition     = try(output.tenants["demo"].group_urn == "urn:v1:eu:identity:group:xx0000-ovh/demo-group", false)
    error_message = "the tenant group is created and published"
  }
}

# A second entry (`alpha`, sorting before `demo`) adds its resources and leaves the first entry's
# published values and addresses unchanged: both runs pin the same `demo` values.
run "first_tenant_alone" {
  command = plan

  assert {
    condition = try(output.tenants == {
      demo = {
        deployer_client_id    = "demo-client-id"
        deployer_identity_urn = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"
        group_urn             = "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
      }
    }, false)
    error_message = "one tenant: tenants holds that tenant's deployer and group only"
  }

  assert {
    condition = try(toset(output.unlabelled) == toset([
      "module.identity.module.platform_deployer.ovh_me_api_oauth2_client.this",
      "module.identity.module.platform_policy.ovh_iam_policy.this",
      "module.identity.module.tenant_deployer[\"demo\"].ovh_me_api_oauth2_client.this",
      "module.identity.module.tenant_policy[\"demo\"].ovh_iam_policy.this",
      "module.identity.module.tenant_group[\"demo\"].ovh_me_identity_group.this",
    ]) && length(output.unlabelled) == 5, false)
    error_message = "one tenant: the platform client and policy and the tenant's client, policy and group, keyed by tenant name"
  }
}

run "second_tenant_leaves_the_first_unchanged" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
      }
      alpha = {
        project_id  = "fedcba9876543210fedcba9876543210"
        project_urn = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }

  assert {
    condition = try(output.tenants["demo"] == {
      deployer_client_id    = "demo-client-id"
      deployer_identity_urn = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"
      group_urn             = "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
    } && output.platform_deployer.client_id == "platform-client-id", false)
    error_message = "two tenants: the first tenant's deployer and group and the platform deployer are unchanged"
  }

  assert {
    condition = try(toset([for a in output.unlabelled : a if strcontains(a, "[\"demo\"]") || strcontains(a, "platform_")]) == toset([
      "module.identity.module.platform_deployer.ovh_me_api_oauth2_client.this",
      "module.identity.module.platform_policy.ovh_iam_policy.this",
      "module.identity.module.tenant_deployer[\"demo\"].ovh_me_api_oauth2_client.this",
      "module.identity.module.tenant_policy[\"demo\"].ovh_iam_policy.this",
      "module.identity.module.tenant_group[\"demo\"].ovh_me_identity_group.this",
    ]), false)
    error_message = "two tenants: the first tenant's and the platform's addresses are unchanged"
  }

  assert {
    condition = try(toset([for a in output.unlabelled : a if strcontains(a, "[\"alpha\"]")]) == toset([
      "module.identity.module.tenant_deployer[\"alpha\"].ovh_me_api_oauth2_client.this",
      "module.identity.module.tenant_policy[\"alpha\"].ovh_iam_policy.this",
      "module.identity.module.tenant_group[\"alpha\"].ovh_me_identity_group.this",
    ]) && length(output.unlabelled) == 8 && output.tenants["alpha"].deployer_client_id == "alpha-client-id", false)
    error_message = "two tenants: the second tenant adds its own client, policy and group"
  }
}

run "unlabelled_from_the_stage_root" {
  command = plan

  assert {
    condition     = try(startswith(jsonencode(output.unlabelled), "[") && length(output.unlabelled) == length(distinct(output.unlabelled)), false)
    error_message = "unlabelled is a list, every address once"
  }

  assert {
    condition     = length(output.unlabelled) > 0 && alltrue([for a in output.unlabelled : startswith(a, "module.identity.module.")])
    error_message = "unlabelled holds resource addresses from the stage root"
  }

  assert {
    condition = try(toset([for a in output.unlabelled : regex("(ovh_[a-z0-9_]+)\\.this$", a)[0]]) == toset([
      "ovh_me_api_oauth2_client", "ovh_iam_policy", "ovh_me_identity_group",
    ]), false)
    error_message = "unlabelled lists the OAuth2 clients, the policies and the group"
  }
}

run "deployer_secrets_only_sensitive" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
      }
      alpha = {
        project_id  = "fedcba9876543210fedcba9876543210"
        project_urn = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }

  assert {
    condition     = issensitive(output.platform_deployer_secret) && issensitive(output.tenant_deployer_secrets)
    error_message = "deployer secrets are sensitive outputs"
  }

  assert {
    condition = try(nonsensitive(output.platform_deployer_secret) == "platform-client-secret"
    && nonsensitive(output.tenant_deployer_secrets) == { demo = "demo-client-secret", alpha = "alpha-client-secret" }, false)
    error_message = "each deployer secret is its own client's secret, one per tenant"
  }

  assert {
    condition = alltrue([for s in [jsonencode(output.platform_deployer), jsonencode(output.tenants), jsonencode(output.unlabelled)] :
    !strcontains(s, "client-secret")])
    error_message = "no deployer secret in a published output"
  }
}
