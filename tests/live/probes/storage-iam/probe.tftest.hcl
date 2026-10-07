# Offline naming controls (T008): mocked provider, plan only, no credential, no API call.
mock_provider "ovh" {
  mock_data "ovh_cloud_projects" {
    defaults = {
      projects = [{
        service_name  = "0123456789abcdef0123456789abcdef"
        project_id    = "0123456789abcdef0123456789abcdef"
        project_name  = "sandbox"
        description   = "sandbox"
        status        = "ok"
        access        = "full"
        creation_date = "2026-09-01T00:00:00Z"
        expiration    = ""
        manual_quota  = false
        order_id      = 1
        plan_code     = "project.2018"
        unleash       = true
        iam = {
          id           = "00000000-0000-0000-0000-000000000000"
          urn          = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
          display_name = "sandbox"
          tags         = {}
        }
      }]
    }
  }
}

variables {
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/storage-iam.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
  run_id           = "20261006T120000Z-ab12"
  project_id       = "0123456789abcdef0123456789abcdef"
}

run "names_carry_prefix_and_run_id" {
  command = plan

  assert {
    condition     = ovh_me_api_oauth2_client.tenant.name == "lzprobe-storage-iam-20261006T120000Z-ab12" && ovh_iam_policy.tenant.name == "lzprobe-storage-iam-20261006T120000Z-ab12"
    error_message = "tenant identity and policy carry lzprobe-storage-iam-<run-id>"
  }

  assert {
    condition     = ovh_me_api_oauth2_client.p25.name == "lzprobe-p25-20261006T120000Z-ab12" && ovh_iam_policy.p25.name == "lzprobe-p25-20261006T120000Z-ab12"
    error_message = "P25 identity and policy carry lzprobe-p25-<run-id>"
  }

  assert {
    condition     = ovh_cloud_project_storage.p25["a"].name == "lzprobe-p25a-20261006t120000z-ab12" && ovh_cloud_project_storage.p25["b"].name == "lzprobe-p25b-20261006t120000z-ab12"
    error_message = "two P25 buckets named lzprobe-p25<a|b>-<run-id> (lowercased)"
  }

  assert {
    condition     = ovh_cloud_project_storage.p25["a"].tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12", "lz:tenant" = "lzprobe-a" }) && ovh_cloud_project_storage.p25["b"].tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12", "lz:tenant" = "lzprobe-b" })
    error_message = "both buckets carry lz:run-id; they differ only in lz:tenant"
  }
}

run "tenant_allowlist_is_p9_exactly" {
  command = plan

  assert {
    condition = ovh_iam_policy.tenant.allow == toset([
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
    error_message = "the tenant policy allows exactly the 13 actions of research R6, nothing else"
  }

  assert {
    condition     = ovh_iam_policy.tenant.resources == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"]) && ovh_iam_policy.tenant.deny == null && ovh_iam_policy.tenant.except == null
    error_message = "the tenant policy targets the sandbox project URN only"
  }

  assert {
    condition     = ovh_iam_policy.p25.allow == toset(["publicCloudProject:apiovh:region/storage/get"]) && ovh_iam_policy.p25.conditions[0].values == tomap({ "resource.Tag(lz:tenant)" = "lzprobe-a" })
    error_message = "the P25 policy allows one storage read, conditioned on bucket A's tenant tag"
  }
}

# T075: the companion's state bucket and S3 user (P1–P3), scoped to that bucket, and the variables
# this root publishes for its companion: the probe identities only.
run "companion_state_and_published_identity" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.p1.name == "lzprobe-p1-20261006t120000z-ab12" && ovh_cloud_project_storage.p1.tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12" }) && ovh_cloud_project_user.p1.description == "lzprobe-p1-20261006T120000Z-ab12"
    error_message = "the companion's state bucket lzprobe-p1-<run-id> (lowercased, lz:run-id) and its S3 user lzprobe-p1-<run-id>"
  }

  assert {
    condition     = jsondecode(ovh_cloud_project_user_s3_policy.p1.policy).Statement[0].Resource == ["arn:aws:s3:::lzprobe-p1-20261006t120000z-ab12", "arn:aws:s3:::lzprobe-p1-20261006t120000z-ab12/*"] && length(jsondecode(ovh_cloud_project_user_s3_policy.p1.policy).Statement) == 1
    error_message = "the S3 user's policy covers the companion's state bucket only"
  }

  assert {
    condition     = toset(keys(output.companion_env)) == toset(["OVH_CLIENT_ID", "OVH_CLIENT_SECRET", "TF_VAR_p25_client_id", "TF_VAR_p25_client_secret", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"])
    error_message = "companion_env publishes the two probe identities and the S3 keys, nothing else"
  }
}

# Each published variable comes from its own identity (mocked apply, offline; distinct values per
# identity, since the mock gives every computed string the same value).
run "companion_env_maps_each_identity" {
  command = apply

  override_resource {
    target = ovh_me_api_oauth2_client.tenant
    values = { client_id = "offline-tenant-id", client_secret = "offline-tenant-secret" }
  }

  override_resource {
    target = ovh_me_api_oauth2_client.p25
    values = { client_id = "offline-p25-id", client_secret = "offline-p25-secret" }
  }

  override_resource {
    target = ovh_cloud_project_user_s3_credential.p1
    values = { access_key_id = "offline-s3-access", secret_access_key = "offline-s3-secret" }
  }

  assert {
    condition     = output.companion_env.OVH_CLIENT_ID == "offline-tenant-id" && output.companion_env.OVH_CLIENT_SECRET == "offline-tenant-secret"
    error_message = "OVH_CLIENT_ID/OVH_CLIENT_SECRET are the P9 tenant identity's"
  }

  assert {
    condition     = output.companion_env.TF_VAR_p25_client_id == "offline-p25-id" && output.companion_env.TF_VAR_p25_client_secret == "offline-p25-secret"
    error_message = "TF_VAR_p25_client_id/_secret are the P25 identity's"
  }

  assert {
    condition     = output.companion_env.AWS_ACCESS_KEY_ID == "offline-s3-access" && output.companion_env.AWS_SECRET_ACCESS_KEY == "offline-s3-secret"
    error_message = "AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY are the P1 S3 user's"
  }

}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/storage-iam/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_with_run_id_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/.local/live/20261006T120000Z-ab12/storage-iam.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_of_another_root_refused" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/state-backend.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_under_checkout_state_probes_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/storage-iam.tfstate"
  }

  expect_failures = [var.state_path]
}

# T074: run id and project come from lz-live (TF_VAR_run_id, TF_VAR_project_id), not derived.
run "run_id_other_than_state_path_refused" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ffff"
  }

  expect_failures = [var.run_id]
}

run "project_not_on_account_refused" {
  command = plan

  variables {
    project_id = "ffffffffffffffffffffffffffffffff"
  }

  expect_failures = [data.ovh_cloud_projects.all]
}

# Three projects: the sandbox sorts between the others (set order), and another has its name.
run "project_selected_by_id_among_several" {
  command = plan

  override_data {
    target = data.ovh_cloud_projects.all
    values = {
      projects = [
        {
          service_name  = "fedcba9876543210fedcba9876543210"
          project_id    = "fedcba9876543210fedcba9876543210"
          project_name  = "other"
          description   = "other"
          status        = "ok"
          access        = "full"
          creation_date = "2026-09-01T00:00:00Z"
          expiration    = ""
          manual_quota  = false
          order_id      = 1
          plan_code     = "project.2018"
          unleash       = true
          iam = {
            id           = "00000000-0000-0000-0000-000000000000"
            urn          = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
            display_name = "other"
            tags         = {}
          }
        },
        {
          service_name  = "0123456789abcdef0123456789abcdef"
          project_id    = "0123456789abcdef0123456789abcdef"
          project_name  = "sandbox"
          description   = "sandbox"
          status        = "ok"
          access        = "full"
          creation_date = "2026-09-01T00:00:00Z"
          expiration    = ""
          manual_quota  = false
          order_id      = 1
          plan_code     = "project.2018"
          unleash       = true
          iam = {
            id           = "00000000-0000-0000-0000-000000000000"
            urn          = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
            display_name = "sandbox"
            tags         = {}
          }
        },
        {
          service_name  = "11111111111111111111111111111111"
          project_id    = "11111111111111111111111111111111"
          project_name  = "sandbox"
          description   = "sandbox"
          status        = "ok"
          access        = "full"
          creation_date = "2026-09-01T00:00:00Z"
          expiration    = ""
          manual_quota  = false
          order_id      = 1
          plan_code     = "project.2018"
          unleash       = true
          iam = {
            id           = "00000000-0000-0000-0000-000000000000"
            urn          = "urn:v1:eu:resource:publicCloudProject:11111111111111111111111111111111"
            display_name = "sandbox"
            tags         = {}
          }
        },
      ]
    }
  }

  assert {
    condition     = local.project.service_name == "0123456789abcdef0123456789abcdef" && local.project.iam.urn == "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "the probe targets the project of project_id, not the account's first, last or only one, nor another of the same name"
  }
}
