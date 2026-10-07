# IAM policy module (spec 005 T019; FR-004, FR-010; ADR-0003, ADR-0018; research R6). Mocked
# provider, no credential, no API call. Attribute names from the pinned ovh 2.21.0 schema
# (`ovh_iam_policy`: name, identities, resources (required); description, allow, deny, except,
# permissions_groups, expired_at (optional); `conditions` block, at most one, nested `condition`
# blocks to three levels, each with operator and an optional values map).
#
# The module passes identities, resources, allow and the optional conditions to the policy
# unchanged, and adds nothing: no action, resource or identity of its own, no wildcard, no
# permissions group (a permissions group grants its own actions, iam_policy.md:93), and no deny or
# except (iam_policy.md:91-92), nor an expiry (`expired_at`, iam_policy.md:94). Dropping a condition would widen the policy, so the condition tree
# must arrive whole, at every level. Which actions a policy may hold (the tenant allowlist, no IAM
# action) is the caller's guard (components/identity/ovh-native, G5), not this module's.
#
# Fixtures: the tenant deployer policy of research R6 (the thirteen P9 actions, verified in
# kb api/v1/cloud.json 2026-10-07, on one project URN) and a platform policy
# (`publicCloudProject:apiovh:*` on two project URNs, two identities). Identity URN forms:
# OAuth2 client `urn:v1:eu:identity:credential:<nic>/oauth2-<clientId>`
# (manage-service-account.mdx:154), group `urn:v1:eu:identity:group:<nic>/<name>`
# (iam-policies-api.mdx:437). Condition keys as in iam_policy.md:45-78 (`resource.Tag(...)`,
# `request.IP`, `date(...).WeekDay.In`); whether OVHcloud evaluates them as intended is UNVERIFIED
# (spec P25).

mock_provider "ovh" {
  mock_resource "ovh_iam_policy" {
    defaults = {
      id = "00000000-0000-4000-8000-000000000001"
    }
  }
}

variables {
  name        = "lz-demo-dev-pol-deployer"
  description = "lz-demo-dev-pol-deployer: tenant deployer of demo on its project"
  identities  = ["urn:v1:eu:identity:credential:xx1111-ovh/oauth2-0f0f0f0f0f0f0f0f"]
  resources   = ["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"]
  allow = [
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
  ]
}

run "tenant_policy_unchanged" {
  command = plan

  assert {
    condition = ovh_iam_policy.this.allow == toset([
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
    ])
    error_message = "allow is exactly the given actions: none added (no wildcard, no IAM action), none dropped"
  }

  assert {
    condition     = ovh_iam_policy.this.resources == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"])
    error_message = "resources are exactly the given project URN: none added (no wildcard), none dropped"
  }

  assert {
    condition     = ovh_iam_policy.this.identities == toset(["urn:v1:eu:identity:credential:xx1111-ovh/oauth2-0f0f0f0f0f0f0f0f"])
    error_message = "identities are exactly the given OAuth2 client URN"
  }

  assert {
    condition     = ovh_iam_policy.this.name == "lz-demo-dev-pol-deployer" && ovh_iam_policy.this.description == "lz-demo-dev-pol-deployer: tenant deployer of demo on its project"
    error_message = "the policy carries the given name and description"
  }

  assert {
    condition     = (ovh_iam_policy.this.permissions_groups == null ? 0 : length(ovh_iam_policy.this.permissions_groups)) == 0
    error_message = "no permissions group (it would grant actions beyond allow)"
  }

  assert {
    condition     = (ovh_iam_policy.this.except == null ? 0 : length(ovh_iam_policy.this.except)) == 0 && (ovh_iam_policy.this.deny == null ? 0 : length(ovh_iam_policy.this.deny)) == 0 && ovh_iam_policy.this.expired_at == null
    error_message = "no except, deny or expiry of the module's own"
  }

  assert {
    condition     = length(ovh_iam_policy.this.conditions) == 0
    error_message = "no conditions when none are given"
  }
}

run "platform_policy_unchanged" {
  command = plan

  variables {
    name        = "lz-acme-pol-platform"
    description = "lz-acme-pol-platform: platform deployer on the tenant projects"
    identities = [
      "urn:v1:eu:identity:credential:xx1111-ovh/oauth2-1a1a1a1a1a1a1a1a",
      "urn:v1:eu:identity:group:xx1111-ovh/lz-acme-grp-platform",
    ]
    resources = [
      "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef",
      "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210",
    ]
    allow = ["publicCloudProject:apiovh:*"]
  }

  assert {
    condition     = ovh_iam_policy.this.allow == toset(["publicCloudProject:apiovh:*"])
    error_message = "allow is exactly the given action set"
  }

  assert {
    condition = ovh_iam_policy.this.resources == toset([
      "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef",
      "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210",
    ])
    error_message = "resources are exactly the two given project URNs"
  }

  assert {
    condition = ovh_iam_policy.this.identities == toset([
      "urn:v1:eu:identity:credential:xx1111-ovh/oauth2-1a1a1a1a1a1a1a1a",
      "urn:v1:eu:identity:group:xx1111-ovh/lz-acme-grp-platform",
    ])
    error_message = "identities are exactly the two given URNs"
  }

  assert {
    condition     = ovh_iam_policy.this.name == "lz-acme-pol-platform" && ovh_iam_policy.this.description == "lz-acme-pol-platform: platform deployer on the tenant projects"
    error_message = "a second name and description pass through unchanged"
  }

  assert {
    condition     = (ovh_iam_policy.this.permissions_groups == null ? 0 : length(ovh_iam_policy.this.permissions_groups)) == 0 && (ovh_iam_policy.this.except == null ? 0 : length(ovh_iam_policy.this.except)) == 0 && (ovh_iam_policy.this.deny == null ? 0 : length(ovh_iam_policy.this.deny)) == 0 && ovh_iam_policy.this.expired_at == null
    error_message = "no permissions group, except, deny or expiry of the module's own"
  }

  assert {
    condition     = length(ovh_iam_policy.this.conditions) == 0
    error_message = "no conditions when none are given (second fixture)"
  }
}

run "conditions_one_level_unchanged" {
  command = plan

  variables {
    conditions = {
      operator = "MATCH"
      values = {
        "resource.Tag(lz:tenant)" = "demo"
        "request.IP"              = "192.0.2.10"
      }
    }
  }

  assert {
    condition = jsonencode(ovh_iam_policy.this.conditions) == jsonencode([{
      operator = "MATCH"
      values = {
        "resource.Tag(lz:tenant)" = "demo"
        "request.IP"              = "192.0.2.10"
      }
      condition = []
    }])
    error_message = "a one-level condition arrives unchanged (operator and every value)"
  }

  assert {
    condition     = ovh_iam_policy.this.allow == toset(var.allow) && ovh_iam_policy.this.resources == toset(var.resources) && ovh_iam_policy.this.identities == toset(var.identities)
    error_message = "conditions leave allow, resources and identities exactly as given"
  }

  assert {
    condition     = (ovh_iam_policy.this.permissions_groups == null ? 0 : length(ovh_iam_policy.this.permissions_groups)) == 0 && (ovh_iam_policy.this.except == null ? 0 : length(ovh_iam_policy.this.except)) == 0 && (ovh_iam_policy.this.deny == null ? 0 : length(ovh_iam_policy.this.deny)) == 0 && ovh_iam_policy.this.expired_at == null
    error_message = "conditions add no permissions group, except, deny or expiry of the module's own"
  }
}

run "conditions_three_levels_unchanged" {
  command = plan

  variables {
    conditions = {
      operator = "OR"
      condition = [
        {
          operator = "AND"
          condition = [
            { operator = "MATCH", values = { "resource.Tag(lz:tenant)" = "demo" } },
            { operator = "MATCH", values = { "date(Europe/Paris).WeekDay.In" = "monday,tuesday,wednesday,thursday,friday" } },
          ]
        },
        { operator = "MATCH", values = { "request.IP" = "192.0.2.10" } },
      ]
    }
  }

  assert {
    condition = jsonencode(ovh_iam_policy.this.conditions) == jsonencode([{
      operator = "OR"
      values   = null
      condition = [
        {
          operator = "AND"
          values   = null
          condition = [
            { operator = "MATCH", values = { "resource.Tag(lz:tenant)" = "demo" } },
            { operator = "MATCH", values = { "date(Europe/Paris).WeekDay.In" = "monday,tuesday,wednesday,thursday,friday" } },
          ]
        },
        { operator = "MATCH", values = { "request.IP" = "192.0.2.10" }, condition = [] },
      ]
    }])
    error_message = "a three-level condition tree arrives unchanged at every level"
  }

  assert {
    condition     = ovh_iam_policy.this.allow == toset(var.allow) && ovh_iam_policy.this.resources == toset(var.resources) && ovh_iam_policy.this.identities == toset(var.identities)
    error_message = "conditions leave allow, resources and identities exactly as given"
  }

  assert {
    condition     = (ovh_iam_policy.this.permissions_groups == null ? 0 : length(ovh_iam_policy.this.permissions_groups)) == 0 && (ovh_iam_policy.this.except == null ? 0 : length(ovh_iam_policy.this.except)) == 0 && (ovh_iam_policy.this.deny == null ? 0 : length(ovh_iam_policy.this.deny)) == 0 && ovh_iam_policy.this.expired_at == null
    error_message = "conditions add no permissions group, except, deny or expiry of the module's own"
  }
}

run "outputs" {
  command = plan

  assert {
    condition     = output.id == ovh_iam_policy.this.id && output.id == "00000000-0000-4000-8000-000000000001"
    error_message = "the id output is the policy's id"
  }

  assert {
    condition     = output.name == ovh_iam_policy.this.name && output.name == "lz-demo-dev-pol-deployer"
    error_message = "the name output is the policy's name"
  }
}
