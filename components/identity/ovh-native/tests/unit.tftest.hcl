# Identity/ovh-native component (spec 005 T023; FR-002, FR-004, FR-010, FR-013; ADR-0006, ADR-0009,
# ADR-0018; research R6 *Authorities and credentials* and *Tenant allowlist*, P9, P26; data-model
# *Resolved-reference input*; contracts/checks.md G5). Mocked provider, no credential, no API call.
#
# The platform deployer (one OAuth2 client of the `CLIENT_CREDENTIALS` flow) gets one policy:
# `publicCloudProject:apiovh:*` on every tenant project URN. Per tenant (`for_each` over `tenants`,
# keyed by tenant name, so a second tenant leaves the first one's addresses and values unchanged):
# one deployer, one policy holding exactly the P9 allowlist on that tenant's own project URN, and
# one identity group with role `NONE` and no members. Guard G5: no tenant resource but its project
# URN (never `*`), no `account:apiovh:iam/` action, no `region/storage/*` or
# `region/storage/policy/create`, no wildcard action, and no `account:apiovh:me/get` (the account
# binding uses `GET /auth/details`, P26; the action is added to both deployer policies only if P26
# is refuted, research R6). P9 action names: `kb/api/v1/cloud.json` (research R6, verified
# 2026-10-06); whether they suffice live is UNVERIFIED (T010).
#
# Policy contents are read through the component outputs, which T024 reads from the module outputs
# of modules/iam-policy and modules/identity-group (read from the resources, T016 precedent);
# `override_resource` cannot set configured attributes (observed, tofu 1.13.0), so that binding is
# reviewed, not machine-checked here (evidence/T023.md *Harness gaps*).
#
# Tripwires: the mocked provider gives a non-computed default to the resource types this component
# must never plan — a group member (`ovh_me_identity_user`), an S3 user, credential or S3 policy
# (moved to tenant-state). Any planned instance of them fails the run with `Non-computed field … is
# not allowed to be overridden` (observed, tofu 1.13.0; probe in evidence/T023.md).
#
# Mock values: every computed value the tests compare is distinct per resource (`override_resource`
# on the keyed instance), and the mock defaults differ from all of them (T019 learning), so a
# swapped or shared client, group or secret is visible.

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
  target = module.platform_deployer.ovh_me_api_oauth2_client.this
  values = {
    client_id     = "platform-client-id"
    identity      = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-platform-client-id"
    client_secret = "platform-client-secret"
  }
}

override_resource {
  target = module.tenant_deployer["demo"].ovh_me_api_oauth2_client.this
  values = {
    client_id     = "demo-client-id"
    identity      = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"
    client_secret = "demo-client-secret"
  }
}

override_resource {
  target = module.tenant_deployer["alpha"].ovh_me_api_oauth2_client.this
  values = {
    client_id     = "alpha-client-id"
    identity      = "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-alpha-client-id"
    client_secret = "alpha-client-secret"
  }
}

override_resource {
  target = module.tenant_group["demo"].ovh_me_identity_group.this
  values = {
    urn = "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
  }
}

override_resource {
  target = module.tenant_group["alpha"].ovh_me_identity_group.this
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

# G5: the tenant deployer's policy is exactly the P9 allowlist on its own project URN, for its own
# deployer only; each forbidden family is asserted on its own as well, so a mutant is named.
run "tenant_policy_is_the_p9_allowlist_on_its_project" {
  command = plan

  assert {
    condition = try(toset(output.tenants["demo"].policy.allow) == toset([
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
    error_message = "G5: the tenant deployer policy allows exactly the P9 allowlist (research R6), nothing added, nothing dropped"
  }

  assert {
    condition     = try(toset(output.tenants["demo"].policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"]), false)
    error_message = "G5: the tenant deployer policy covers only its tenant's project URN (never `*`)"
  }

  assert {
    condition     = try(toset(output.tenants["demo"].policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"]), false)
    error_message = "the tenant deployer policy applies to that tenant's deployer only"
  }

  assert {
    condition     = try(length(output.tenants["demo"].policy.allow) > 0 && alltrue([for a in output.tenants["demo"].policy.allow : !startswith(lower(a), "account:apiovh:iam/")]), false)
    error_message = "G5: no `account:apiovh:iam/` action in the tenant deployer policy"
  }

  assert {
    condition     = try(length(output.tenants["demo"].policy.allow) > 0 && alltrue([for a in output.tenants["demo"].policy.allow : !strcontains(a, "*")]), false)
    error_message = "G5: no wildcard action (`*`, `region/storage/*`) in the tenant deployer policy"
  }

  assert {
    condition     = try(length(output.tenants["demo"].policy.allow) > 0 && !contains([for a in output.tenants["demo"].policy.allow : lower(a)], "publiccloudproject:apiovh:region/storage/policy/create"), false)
    error_message = "G5: no `region/storage/policy/create` in the tenant deployer policy"
  }

  assert {
    condition     = try(length(output.tenants["demo"].policy.allow) > 0 && !contains([for a in output.tenants["demo"].policy.allow : lower(a)], "account:apiovh:me/get"), false)
    error_message = "P26: no `account:apiovh:me/get` in the tenant deployer policy (binding uses GET /auth/details)"
  }

  assert {
    condition = try(toset(output.platform_policy.allow) == toset(["publicCloudProject:apiovh:*"])
      && toset(output.platform_policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"])
    && toset(output.platform_policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-platform-client-id"]), false)
    error_message = "one tenant: the platform deployer policy is exactly `publicCloudProject:apiovh:*` on that project URN, for the platform deployer only"
  }
}

# A second tenant: each policy keeps its own project URN and its own deployer, both hold the P9
# allowlist (a fixture-conditioned implementation fails one of them).
run "each_tenant_policy_confined_to_its_own_project" {
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
    condition = try(alltrue([for t in ["demo", "alpha"] : toset(output.tenants[t].policy.allow) == toset([
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
    ])]), false)
    error_message = "G5: every tenant deployer policy allows exactly the P9 allowlist"
  }

  assert {
    condition = try(toset(output.tenants["demo"].policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"])
    && toset(output.tenants["alpha"].policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"]), false)
    error_message = "G5: each tenant deployer policy covers only its own tenant's project URN"
  }

  assert {
    condition = try(toset(output.tenants["demo"].policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"])
    && toset(output.tenants["alpha"].policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-alpha-client-id"]), false)
    error_message = "each tenant deployer policy applies to its own deployer only"
  }
}

# The platform deployer covers `publicCloudProject:apiovh:*` on every tenant project URN, for its
# own identity only; no IAM action, no `account:apiovh:me/get` (P26).
run "platform_policy_covers_the_tenant_projects" {
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
    condition     = try(toset(output.platform_policy.allow) == toset(["publicCloudProject:apiovh:*"]), false)
    error_message = "the platform deployer policy allows exactly `publicCloudProject:apiovh:*` (no IAM action, no `account:apiovh:me/get`)"
  }

  assert {
    condition = try(toset(output.platform_policy.resources) == toset([
      "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef",
      "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210",
    ]), false)
    error_message = "the platform deployer policy covers exactly the tenant project URNs"
  }

  assert {
    condition     = try(toset(output.platform_policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-platform-client-id"]), false)
    error_message = "the platform deployer policy applies to the platform deployer only"
  }

  assert {
    condition = try(output.platform_deployer.client_id == "platform-client-id"
    && output.platform_deployer.identity_urn == "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-platform-client-id", false)
    error_message = "platform_deployer carries the platform client's own client id and identity URN"
  }
}

# One deployer, one policy and one group per tenant entry, each its own (per-instance mock values).
run "one_deployer_policy_and_group_per_tenant" {
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
    condition     = try(join(",", sort(keys(output.tenants))) == "alpha,demo", false)
    error_message = "one entry per tenant, keyed by tenant name"
  }

  assert {
    condition = try(output.tenants["demo"].deployer_client_id == "demo-client-id"
      && output.tenants["demo"].deployer_identity_urn == "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"
      && output.tenants["alpha"].deployer_client_id == "alpha-client-id"
    && output.tenants["alpha"].deployer_identity_urn == "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-alpha-client-id", false)
    error_message = "each tenant has its own deployer (client id and identity URN of its own client)"
  }

  assert {
    condition = try(output.tenants["demo"].group_urn == "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
    && output.tenants["alpha"].group_urn == "urn:v1:eu:identity:group:xx0000-ovh/alpha-group", false)
    error_message = "each tenant has its own group"
  }

  assert {
    condition = try(output.tenants["demo"].policy.name == "lz-demo-pol-deployer" && output.tenants["alpha"].policy.name == "lz-alpha-pol-deployer"
    && output.platform_policy.name == "lz-pol-platform-deployer", false)
    error_message = "policies named by modules/naming: org, tenant, kind iam_policy, role deployer; platform: org, role platform-deployer"
  }

  assert {
    condition     = try(output.tenants["demo"].group_name == "lz-demo-grp-tenant" && output.tenants["alpha"].group_name == "lz-alpha-grp-tenant", false)
    error_message = "groups named by modules/naming: org, tenant, kind identity_group, role tenant"
  }
}

# Names follow `org` (not constants).
run "names_follow_the_org" {
  command = plan

  variables {
    org = "acme"
  }

  assert {
    condition = try(output.tenants["demo"].policy.name == "acme-demo-pol-deployer" && output.platform_policy.name == "acme-pol-platform-deployer"
    && output.tenants["demo"].group_name == "acme-demo-grp-tenant", false)
    error_message = "policy and group names follow org"
  }
}

# The tenant group: role `NONE` (T019 review decision), its own URN; no members (tripwire above).
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
    condition     = try(length(output.tenants) == 2 && alltrue([for t in ["demo", "alpha"] : output.tenants[t].group_role == "NONE"]), false)
    error_message = "every tenant identity group has role NONE (never ADMIN, REGULAR or UNPRIVILEGED)"
  }

  assert {
    condition = try(output.tenants["demo"].group_urn == "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
    && output.tenants["alpha"].group_urn == "urn:v1:eu:identity:group:xx0000-ovh/alpha-group", false)
    error_message = "each tenant group is created (its own URN)"
  }
}

# A second entry (`alpha`, sorting before `demo`) adds its own resources and leaves the first
# entry's values and addresses unchanged: both runs pin the same `demo` values and keyed addresses
# (`run.<name>` outputs are not readable in a plan-only assertion on tofu 1.13.0, observed).
run "first_tenant_alone" {
  command = plan

  assert {
    condition = try(output.tenants["demo"].deployer_client_id == "demo-client-id"
      && output.tenants["demo"].deployer_identity_urn == "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"
      && output.tenants["demo"].group_urn == "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
      && output.tenants["demo"].group_name == "lz-demo-grp-tenant" && output.tenants["demo"].group_role == "NONE"
      && output.tenants["demo"].policy.name == "lz-demo-pol-deployer"
      && toset(output.tenants["demo"].policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"])
      && toset(output.tenants["demo"].policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"])
      && toset(output.tenants["demo"].policy.allow) == toset([
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
    error_message = "one tenant: the first tenant's deployer, policy and group carry their one-tenant values"
  }

  assert {
    condition     = try(output.platform_deployer.client_id == "platform-client-id", false)
    error_message = "one tenant: the platform deployer is the same client"
  }

  assert {
    condition = try(toset([for a in output.unlabelled : a if strcontains(a, "[\"demo\"]")]) == toset([
      "module.tenant_deployer[\"demo\"].ovh_me_api_oauth2_client.this",
      "module.tenant_policy[\"demo\"].ovh_iam_policy.this",
      "module.tenant_group[\"demo\"].ovh_me_identity_group.this",
    ]), false)
    error_message = "one tenant: the first tenant's resource addresses are keyed by its name"
  }

  assert {
    condition     = try(length(output.unlabelled) == 5 && join(",", sort(keys(output.tenants))) == "demo", false)
    error_message = "one tenant: five resources (platform client and policy, the tenant's client, policy and group)"
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
    condition = try(output.tenants["demo"].deployer_client_id == "demo-client-id"
      && output.tenants["demo"].deployer_identity_urn == "urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"
      && output.tenants["demo"].group_urn == "urn:v1:eu:identity:group:xx0000-ovh/demo-group"
      && output.tenants["demo"].group_name == "lz-demo-grp-tenant" && output.tenants["demo"].group_role == "NONE"
      && output.tenants["demo"].policy.name == "lz-demo-pol-deployer"
      && toset(output.tenants["demo"].policy.identities) == toset(["urn:v1:eu:identity:credential:xx0000-ovh/oauth2-demo-client-id"])
      && toset(output.tenants["demo"].policy.resources) == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"])
      && toset(output.tenants["demo"].policy.allow) == toset([
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
    error_message = "two tenants: the first tenant's deployer, policy and group carry their one-tenant values"
  }

  assert {
    condition     = try(output.platform_deployer.client_id == "platform-client-id", false)
    error_message = "two tenants: the platform deployer is the same client"
  }

  assert {
    condition = try(toset([for a in output.unlabelled : a if strcontains(a, "[\"demo\"]")]) == toset([
      "module.tenant_deployer[\"demo\"].ovh_me_api_oauth2_client.this",
      "module.tenant_policy[\"demo\"].ovh_iam_policy.this",
      "module.tenant_group[\"demo\"].ovh_me_identity_group.this",
    ]), false)
    error_message = "two tenants: the first tenant's resource addresses are keyed by its name"
  }

  assert {
    condition     = try(length(output.unlabelled) == 8 && output.tenants["alpha"].deployer_client_id == "alpha-client-id" && output.tenants["alpha"].policy.name == "lz-alpha-pol-deployer", false)
    error_message = "two tenants: the second tenant adds its own client, policy and group"
  }
}

# `unlabelled` lists the OAuth2 clients, the policies and the groups (no tags on these APIs,
# research R14): a list, every address once, exactly these.
run "unlabelled_clients_policies_and_groups" {
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
    condition     = startswith(jsonencode(output.unlabelled), "[")
    error_message = "unlabelled is a list"
  }

  assert {
    condition     = length(output.unlabelled) == length(distinct(output.unlabelled))
    error_message = "unlabelled lists every address once"
  }

  assert {
    condition = toset(output.unlabelled) == toset([
      "module.platform_deployer.ovh_me_api_oauth2_client.this",
      "module.platform_policy.ovh_iam_policy.this",
      "module.tenant_deployer[\"alpha\"].ovh_me_api_oauth2_client.this",
      "module.tenant_policy[\"alpha\"].ovh_iam_policy.this",
      "module.tenant_group[\"alpha\"].ovh_me_identity_group.this",
      "module.tenant_deployer[\"demo\"].ovh_me_api_oauth2_client.this",
      "module.tenant_policy[\"demo\"].ovh_iam_policy.this",
      "module.tenant_group[\"demo\"].ovh_me_identity_group.this",
    ])
    error_message = "unlabelled is exactly the OAuth2 clients, policies and groups"
  }
}

# Secrets only sensitive: each deployer's own secret, in no published output.
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
      && nonsensitive(output.tenant_deployer_secrets["demo"]) == "demo-client-secret"
      && nonsensitive(output.tenant_deployer_secrets["alpha"]) == "alpha-client-secret"
    && length(output.tenant_deployer_secrets) == 2, false)
    error_message = "each deployer secret is its own client's secret, one per tenant"
  }

  assert {
    condition = !anytrue([
      issensitive(output.platform_deployer), issensitive(output.platform_policy), issensitive(output.tenants), issensitive(output.unlabelled),
    ])
    error_message = "the outputs the stage publishes from are not sensitive"
  }

  assert {
    condition = alltrue([for s in [jsonencode(output.platform_deployer), jsonencode(output.platform_policy), jsonencode(output.tenants), jsonencode(output.unlabelled)] :
    !strcontains(s, "client-secret")])
    error_message = "no deployer secret in a non-sensitive output"
  }
}

# The resolved reference must be a project URN of that entry's project id; `*`, another project's
# URN, another resource type or a suffix is refused at the input (defence in depth for G5); a `ca`
# account's URN is accepted (`manage-and-operate/iam/authenticate-api-openstack-with-service-account.mdx:101`).
run "ca_project_urn_accepted" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
      }
    }
  }

  assert {
    condition     = try(toset(output.tenants["demo"].policy.resources) == toset(["urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"]), false)
    error_message = "a ca project URN is accepted and becomes the tenant policy's only resource"
  }
}

run "other_resource_type_urn_refused" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:eu:resource:dedicatedServer:0123456789abcdef0123456789abcdef"
      }
    }
  }

  expect_failures = [var.tenants]
}

run "suffixed_project_urn_refused" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef/*"
      }
    }
  }

  expect_failures = [var.tenants]
}

# No tenant: refused (a platform policy without a resource would otherwise invite a widening fallback).
run "empty_tenants_refused" {
  command = plan

  variables {
    tenants = {}
  }

  expect_failures = [var.tenants]
}

run "wildcard_project_urn_refused" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "*"
      }
    }
  }

  expect_failures = [var.tenants]
}

run "other_project_urn_refused" {
  command = plan

  variables {
    tenants = {
      demo = {
        project_id  = "0123456789abcdef0123456789abcdef"
        project_urn = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }

  expect_failures = [var.tenants]
}
