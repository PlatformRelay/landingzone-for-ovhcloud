# Tenant-state stage (spec 005 T021; FR-002, FR-004, FR-008, FR-013; ADR-0004, ADR-0009; research
# R5 *Per-tenant buckets*, R14; data-model *Stage table*, *Derived instance fields*, *Per-stage
# values* `tenant-state`, *Sensitive outputs*; contracts/checks.md G6; D87, D88). Replaces the
# withdrawn account-admin stage (D88). Mocked provider, no credential, no API call.
# One tenant's state bucket (modules/naming: `org`, tenant, kind bucket, role state) in
# `state_project_id` (`spec.state.project`; one shared state project in the sandbox, KD-1), plus a
# tenant S3 user and a platform S3 user, each confined to that bucket, through the stage's one
# components/state-backend call (`module.state_backend`, the only edge a stage has, ADR-0002). The
# bucket's protection (`prevent_destroy`, versioning) is the component's: `task test:dependencies`
# refuses a state-backend bucket other than through modules/object-storage-protected
# (STATE_BUCKET_UNPROTECTED, G7). No backend or provider configuration: `task test:dependencies`
# refuses one in a stage (LIBRARY_BACKEND, LIBRARY_PROVIDER_CONFIG); this file checks behaviour only.
#
# Plan runs only (the state bucket carries `prevent_destroy`; T013 *Observed*). Mock values are the
# computed values at plan time (observed, tofu 1.13.0). The tenant user's id and both credentials
# are overridden per user (`override_resource` on the keyed instance), so a swapped user or
# credential is visible.
#
# Policy (G6): the Resource of every statement that is not a Deny (any case of `Effect`, fail-closed)
# of each user together is exactly `arn:aws:s3:::<tenant bucket>` and `…/*` (form of docs.ovhcloud.com
# storage-and-backup/object-storage/s3-identity-and-access-management.mdx:116-125): never the account
# bucket, another tenant's bucket or `*`. The component's tests pin the Deny statements and the
# allowed actions (T015).
#
# Outputs (schemas/outputs/tenant-state.schema.json): `tenant`, `state_bucket`, `tenant_s3_user_id`,
# `platform_s3_user_id` (non-empty strings) and `unlabelled` (addresses from the stage root),
# published, so not sensitive; `tenant_s3` and `platform_s3` (`{access_key_id, secret_access_key}`)
# only sensitive, never published (written to `tenants/<t>/state.env` and `platform-state.env` by
# the live lane). That no other non-sensitive output exists is not observable here: the envelope
# validator's `additionalProperties: false` refuses one (T018).

mock_provider "ovh" {
  mock_resource "ovh_cloud_project_user" {
    defaults = {
      id = "mock-user-id"
    }
  }

  mock_resource "ovh_cloud_project_user_s3_credential" {
    defaults = {
      access_key_id     = "mock-access-key-id"
      secret_access_key = "mock-secret-access-key"
    }
  }
}

override_resource {
  target = module.state_backend.module.s3_user["tenant"].ovh_cloud_project_user.this
  values = {
    id = "mock-tenant-user-id"
  }
}

override_resource {
  target = module.state_backend.module.s3_user["tenant"].ovh_cloud_project_user_s3_credential.this
  values = {
    access_key_id     = "mock-tenant-access-key-id"
    secret_access_key = "mock-tenant-secret-access-key"
  }
}

variables {
  org              = "lz"
  tenant           = "demo"
  state_project_id = "0123456789abcdef0123456789abcdef"
  state_region     = "GRA"
  instance         = "demo-state"
  managed_in       = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
}

run "tenant_bucket_named_in_the_state_project" {
  command = plan

  assert {
    condition     = output.state_bucket == "lz-demo-bkt-state"
    error_message = "the tenant state bucket is named by modules/naming: org, tenant, kind bucket, role state"
  }

  assert {
    condition     = module.state_backend.bucket == output.state_bucket
    error_message = "the published bucket is the component's one bucket"
  }

  assert {
    condition     = module.state_backend.project_id == "0123456789abcdef0123456789abcdef" && module.state_backend.region == "GRA"
    error_message = "the bucket is in state_project_id and state_region"
  }

  assert {
    condition     = output.tenant == "demo"
    error_message = "tenant is the given tenant"
  }
}

run "bucket_follows_the_inputs" {
  command = plan

  variables {
    org              = "acme"
    tenant           = "other"
    state_project_id = "fedcba9876543210fedcba9876543210"
    state_region     = "SBG"
    instance         = "other-state"
    managed_in       = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/other"
  }

  assert {
    condition     = output.state_bucket == "acme-other-bkt-state" && output.tenant == "other"
    error_message = "the bucket name and tenant follow org and tenant, not constants"
  }

  assert {
    condition     = module.state_backend.project_id == "fedcba9876543210fedcba9876543210" && module.state_backend.region == "SBG"
    error_message = "the bucket's project and region follow state_project_id and state_region, not constants"
  }

  assert {
    condition = (lookup(module.state_backend.labels, "lz:tenant", "") == "other" && lookup(module.state_backend.labels, "lz:instance", "") == "other-state"
    && lookup(module.state_backend.labels, "lz:managed-in", "") == "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/other")
    error_message = "lz:tenant, lz:instance and lz:managed-in follow the inputs"
  }

  # try: a missing user or a malformed policy fails the assertion instead of erroring.
  assert {
    condition = try(alltrue([
      for u in ["tenant", "platform"] : toset(flatten([
        for s in jsondecode(module.state_backend.s3_users[u].policy).Statement : lookup(s, "Resource", []) if lower(lookup(s, "Effect", "")) != "deny"
        ])) == toset([
        "arn:aws:s3:::acme-other-bkt-state",
        "arn:aws:s3:::acme-other-bkt-state/*",
      ])
    ]), false)
    error_message = "each user's Allow statements follow this tenant's bucket, not another tenant's (lz-demo-bkt-state)"
  }
}

run "tenant_bucket_mandatory_labels" {
  command = plan

  assert {
    condition = module.state_backend.labels == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
      "lz:instance"   = "demo-state"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the tenant bucket carries exactly the mandatory labels with lz:tenant"
  }
}

run "tenant_and_platform_s3_users" {
  command = plan

  assert {
    condition     = toset(keys(module.state_backend.s3_users)) == toset(["tenant", "platform"])
    error_message = "the stage creates a tenant S3 user and a platform S3 user and no other"
  }

  assert {
    condition     = output.tenant_s3_user_id == "mock-tenant-user-id"
    error_message = "tenant_s3_user_id is the tenant user's id"
  }

  assert {
    condition     = output.platform_s3_user_id == "mock-user-id"
    error_message = "platform_s3_user_id is the platform user's id, not the tenant user's"
  }
}

run "tenant_user_policy_confined_to_the_tenant_bucket" {
  command = plan

  assert {
    condition = try(toset(flatten([
      for s in jsondecode(module.state_backend.s3_users["tenant"].policy).Statement : lookup(s, "Resource", []) if lower(lookup(s, "Effect", "")) != "deny"
      ])) == toset([
      "arn:aws:s3:::lz-demo-bkt-state",
      "arn:aws:s3:::lz-demo-bkt-state/*",
    ]), false)
    error_message = "G6: the tenant user's Allow statements cover exactly the tenant bucket and its objects: not the account bucket, another tenant's or *"
  }

  assert {
    condition = try(alltrue([
      for s in jsondecode(module.state_backend.s3_users["tenant"].policy).Statement : !contains(keys(s), "NotResource") && !contains(keys(s), "NotAction") if lower(lookup(s, "Effect", "")) != "deny"
    ]), false)
    error_message = "G6: no Allow statement of the tenant user uses NotResource or NotAction"
  }
}

run "platform_user_policy_confined_to_the_tenant_bucket" {
  command = plan

  assert {
    condition = try(toset(flatten([
      for s in jsondecode(module.state_backend.s3_users["platform"].policy).Statement : lookup(s, "Resource", []) if lower(lookup(s, "Effect", "")) != "deny"
      ])) == toset([
      "arn:aws:s3:::lz-demo-bkt-state",
      "arn:aws:s3:::lz-demo-bkt-state/*",
    ]), false)
    error_message = "the platform user's Allow statements cover exactly this tenant's bucket and its objects: not the account bucket, another tenant's or *"
  }

  assert {
    condition = try(alltrue([
      for s in jsondecode(module.state_backend.s3_users["platform"].policy).Statement : !contains(keys(s), "NotResource") && !contains(keys(s), "NotAction") if lower(lookup(s, "Effect", "")) != "deny"
    ]), false)
    error_message = "no Allow statement of the platform user uses NotResource or NotAction"
  }
}

run "unlabelled_users_credentials_policies" {
  command = plan

  assert {
    condition     = try(startswith(jsonencode(output.unlabelled), "[") && length(distinct(output.unlabelled)) == 6, false)
    error_message = "unlabelled is a list (schema: array) of six distinct addresses: each of the two S3 users, its credential and its policy"
  }

  assert {
    condition = toset([for a in output.unlabelled : try(regex("(ovh_[a-z0-9_]+)\\.[a-z0-9_]+(\\[\"[^\"]*\"\\])?$", a)[0], a)]) == toset([
      "ovh_cloud_project_user",
      "ovh_cloud_project_user_s3_credential",
      "ovh_cloud_project_user_s3_policy",
    ])
    error_message = "the unlabelled addresses are the S3 users, their credentials and their policies"
  }

  assert {
    condition = alltrue([
      for u in ["tenant", "platform"] : length([for a in output.unlabelled : a if strcontains(a, "[\"${u}\"]")]) == 3 && toset([
        for a in output.unlabelled : try(regex("(ovh_[a-z0-9_]+)\\.[a-z0-9_]+(\\[\"[^\"]*\"\\])?$", a)[0], a) if strcontains(a, "[\"${u}\"]")
      ]) == toset(["ovh_cloud_project_user", "ovh_cloud_project_user_s3_credential", "ovh_cloud_project_user_s3_policy"])
    ])
    error_message = "each user's user, credential and policy (one of each) are listed under its key"
  }

  assert {
    condition     = length(output.unlabelled) > 0 && alltrue([for a in output.unlabelled : startswith(a, "module.state_backend.")])
    error_message = "unlabelled holds resource addresses from the stage root"
  }
}

run "published_outputs_match_the_schema" {
  command = plan

  assert {
    condition = alltrue([
      for o in [output.tenant, output.state_bucket, output.tenant_s3_user_id, output.platform_s3_user_id] : o != ""
    ])
    error_message = "tenant, state_bucket, tenant_s3_user_id and platform_s3_user_id are non-empty strings (schema minLength 1)"
  }

  assert {
    condition     = startswith(jsonencode(output.unlabelled), "[") && length(output.unlabelled) > 0 && alltrue([for a in output.unlabelled : a != ""])
    error_message = "unlabelled is a list (not a map or object) of non-empty strings"
  }

  assert {
    condition = !anytrue([
      issensitive(output.tenant), issensitive(output.state_bucket), issensitive(output.tenant_s3_user_id),
      issensitive(output.platform_s3_user_id), issensitive(output.unlabelled),
    ])
    error_message = "the published outputs are not sensitive (a sensitive one is left out of the envelope and fails the schema's required list)"
  }
}

run "credentials_only_sensitive" {
  command = plan

  assert {
    condition     = issensitive(output.tenant_s3) && issensitive(output.platform_s3)
    error_message = "the tenant and platform credential outputs are sensitive"
  }

  assert {
    condition = nonsensitive(output.tenant_s3) == {
      access_key_id     = "mock-tenant-access-key-id"
      secret_access_key = "mock-tenant-secret-access-key"
    }
    error_message = "tenant_s3 is the tenant user's S3 credential"
  }

  assert {
    condition = nonsensitive(output.platform_s3) == {
      access_key_id     = "mock-access-key-id"
      secret_access_key = "mock-secret-access-key"
    }
    error_message = "platform_s3 is the platform user's S3 credential, not the tenant user's"
  }

  assert {
    condition = alltrue([
      for o in [output.tenant, output.state_bucket, output.tenant_s3_user_id, output.platform_s3_user_id, jsonencode(output.unlabelled)] :
      !strcontains(o, "secret-access-key") && !strcontains(o, "access-key-id")
    ])
    error_message = "no secret and no access key id of either user is in a published output"
  }
}
