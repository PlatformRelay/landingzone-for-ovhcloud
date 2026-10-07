# Bootstrap stage (spec 005 T015; FR-002, FR-004, FR-005, FR-008; ADR-0004, ADR-0009; research R5,
# R14; data-model *Per-stage values* `bootstrap`). Mocked provider, no credential, no API call.
# The stage creates only the account state bucket (modules/naming: `org`, kind bucket, role state;
# no tenant segment) in `state_project_id`, plus the platform S3 user, through its one
# components/state-backend call (`module.state_backend`, the only edge a stage has, ADR-0002).
# Tenant state buckets come from the `tenant-state` stage (D87, D88), never from here.
# No backend or provider configuration: `task test:dependencies` refuses one in a stage
# (LIBRARY_BACKEND, LIBRARY_PROVIDER_CONFIG); this file checks behaviour only.
#
# Plan runs only (the state bucket carries `prevent_destroy`; T013 *Observed*). The mock defaults
# are the computed values at plan time (observed, tofu 1.13.0), so the outputs are checked against
# them. Outputs: `state_bucket`, `state_project_id`, `state_region`, `state_endpoint`,
# `platform_s3_user_id`, `unlabelled` (published, data-model) and the sensitive `platform_s3`
# (never published, written to `state.env` by the live lane).

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

variables {
  org              = "lz"
  state_project_id = "0123456789abcdef0123456789abcdef"
  state_region     = "GRA"
  instance         = "account-bootstrap"
  managed_in       = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
}

run "account_state_bucket_outputs" {
  command = plan

  assert {
    condition     = output.state_bucket == "lz-bkt-state"
    error_message = "the account state bucket is named by modules/naming: org, kind bucket, role state"
  }

  assert {
    condition     = output.state_project_id == "0123456789abcdef0123456789abcdef" && output.state_region == "GRA"
    error_message = "the state bucket is in state_project_id and the given region"
  }

  assert {
    condition     = output.state_endpoint == "https://s3.gra.io.cloud.ovh.net"
    error_message = "state_endpoint is the S3 endpoint of the bucket's region"
  }
}

run "project_and_region_follow_the_inputs" {
  command = plan

  variables {
    state_project_id = "fedcba9876543210fedcba9876543210"
    state_region     = "SBG"
  }

  assert {
    condition     = output.state_project_id == "fedcba9876543210fedcba9876543210" && module.state_backend.project_id == "fedcba9876543210fedcba9876543210"
    error_message = "the bucket is in state_project_id, not a constant project"
  }

  assert {
    condition     = output.state_region == "SBG" && output.state_endpoint == "https://s3.sbg.io.cloud.ovh.net"
    error_message = "the bucket's region and endpoint follow state_region, not a constant"
  }
}

run "bucket_name_follows_org" {
  command = plan

  variables {
    org = "acme"
  }

  assert {
    condition     = output.state_bucket == "acme-bkt-state"
    error_message = "the bucket name comes from naming with the given org, not a constant"
  }
}

run "account_bucket_only_mandatory_labels" {
  command = plan

  assert {
    condition     = module.state_backend.bucket == output.state_bucket && module.state_backend.project_id == output.state_project_id
    error_message = "the published bucket is the component's one bucket"
  }

  assert {
    condition = module.state_backend.labels == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
      "lz:instance"   = "account-bootstrap"
      "lz:release"    = "unreleased"
    })
    error_message = "the account bucket carries exactly the mandatory labels of the account scope: no lz:tenant, no tenant bucket"
  }
}

run "platform_s3_user_only" {
  command = plan

  assert {
    condition     = keys(module.state_backend.s3_users) == ["platform"]
    error_message = "the stage creates the platform S3 user and no other (no tenant user)"
  }

  assert {
    condition     = output.platform_s3_user_id == "mock-user-id"
    error_message = "platform_s3_user_id is the platform user's id"
  }
}

run "platform_policy_confined_to_the_account_bucket" {
  command = plan

  # try: a missing platform user or a malformed policy fails the assertion instead of erroring.
  assert {
    condition = try(toset(flatten([
      for s in jsondecode(module.state_backend.s3_users["platform"].policy).Statement : lookup(s, "Resource", []) if lookup(s, "Effect", "") == "Allow"
      ])) == toset([
      "arn:aws:s3:::lz-bkt-state",
      "arn:aws:s3:::lz-bkt-state/*",
    ]), false)
    error_message = "the platform user's Allow statements cover exactly the account bucket and its objects"
  }
}

run "unlabelled_user_credential_policy" {
  command = plan

  assert {
    condition     = length(output.unlabelled) == 3
    error_message = "three unlabelled resources: the platform S3 user, its credential and its policy"
  }

  assert {
    condition = toset([for a in output.unlabelled : regex("(ovh_[a-z0-9_]+)\\.[a-z0-9_]+(\\[\"[^\"]*\"\\])?$", a)[0]]) == toset([
      "ovh_cloud_project_user",
      "ovh_cloud_project_user_s3_credential",
      "ovh_cloud_project_user_s3_policy",
    ])
    error_message = "the unlabelled addresses are the S3 user, its credential and its policy"
  }

  assert {
    condition     = alltrue([for a in output.unlabelled : startswith(a, "module.state_backend.")])
    error_message = "unlabelled holds resource addresses from the stage root"
  }
}

run "platform_credential_only_sensitive" {
  command = plan

  assert {
    condition     = issensitive(output.platform_s3)
    error_message = "the platform credential output is sensitive"
  }

  assert {
    condition = nonsensitive(output.platform_s3) == {
      access_key_id     = "mock-access-key-id"
      secret_access_key = "mock-secret-access-key"
    }
    error_message = "platform_s3 is the platform user's S3 credential"
  }

  assert {
    condition = alltrue([
      for o in [output.state_bucket, output.state_project_id, output.state_region, output.state_endpoint, output.platform_s3_user_id, jsonencode(output.unlabelled)] :
      !strcontains(o, "mock-secret-access-key") && !strcontains(o, "mock-access-key-id")
    ])
    error_message = "neither the secret nor the access key id is in a published output"
  }
}
