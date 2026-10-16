# Offline controls (spec 003 T030): mocked provider, plan only, no credential, no API call.
mock_provider "ovh" {
  mock_data "ovh_cloud_projects" {
    defaults = {
      projects = [{
        service_name = "0123456789abcdef0123456789abcdef"
        # Empty, as the real API answers it (spec 003 T039: live run 20261016T145959Z-42fd).
        project_id    = ""
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
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/swift-reach.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
  run_id           = "20261006T120000Z-ab12"
  project_id       = "0123456789abcdef0123456789abcdef"
}

# Spec 003 T030: two buckets and four users, each named for the run (lzprobe-swift-reach-<lower(run
# id)>-a|b, the users' descriptions with the same prefix), so the check's Sweep finds them by name.
run "names_carry_prefix_and_run_id" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.bucket["a"].name == "lzprobe-swift-reach-20261006t120000z-ab12-a" && ovh_cloud_project_storage.bucket["b"].name == "lzprobe-swift-reach-20261006t120000z-ab12-b"
    error_message = "the buckets are lzprobe-swift-reach-<lower(run id)>-a and -b"
  }

  assert {
    condition     = alltrue([for b in ovh_cloud_project_storage.bucket : b.region_name == "GRA" && b.tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12" })])
    error_message = "both buckets are in GRA and tagged lz:run-id = <run-id>"
  }

  assert {
    condition = (ovh_cloud_project_user.tenant["a"].description == "lzprobe-swift-reach-20261006t120000z-ab12-a" && ovh_cloud_project_user.tenant["b"].description == "lzprobe-swift-reach-20261006t120000z-ab12-b"
    && ovh_cloud_project_user.cleanup["a"].description == "lzprobe-swift-reach-20261006t120000z-ab12-a-cleanup" && ovh_cloud_project_user.cleanup["b"].description == "lzprobe-swift-reach-20261006t120000z-ab12-b-cleanup")
    error_message = "the tenant and cleanup users carry lzprobe-swift-reach-<lower(run id)>-a|b[-cleanup]"
  }

  assert {
    condition     = alltrue([for u in concat(values(ovh_cloud_project_user.tenant), values(ovh_cloud_project_user.cleanup)) : toset(u.role_names) == toset(["objectstore_operator"])])
    error_message = "every user holds objectstore_operator only, as a tenant S3 user does"
  }
}

# The tenant users' S3 policies cover their own bucket only; each credential and policy belongs to
# its own user.
run "tenant_policy_own_bucket_only" {
  command = plan

  assert {
    condition = alltrue([for k in ["a", "b"] : jsondecode(ovh_cloud_project_user_s3_policy.tenant[k].policy) == {
      Statement = [{
        Sid      = "TenantOwnBucket"
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation"]
        Resource = ["arn:aws:s3:::lzprobe-swift-reach-20261006t120000z-ab12-${k}", "arn:aws:s3:::lzprobe-swift-reach-20261006t120000z-ab12-${k}/*"]
      }]
    }])
    error_message = "each tenant policy allows the S3 object actions on its own bucket only"
  }

  assert {
    condition = alltrue([for k in ["a", "b"] : ovh_cloud_project_user_s3_policy.tenant[k].user_id == ovh_cloud_project_user.tenant[k].id && ovh_cloud_project_user_s3_credential.tenant[k].user_id == ovh_cloud_project_user.tenant[k].id
    && ovh_cloud_project_user_s3_policy.cleanup[k].user_id == ovh_cloud_project_user.cleanup[k].id && ovh_cloud_project_user_s3_credential.cleanup[k].user_id == ovh_cloud_project_user.cleanup[k].id])
    error_message = "each credential and policy belongs to its own user"
  }
}

# The cleanup users list and delete on their own bucket and write only its seed: Undo's
# credentials, independent of the tenant users; every action valid under OVHcloud's
# accepted-action rule (tests/fixtures/ovhcloud/s3-policy-actions.json).
run "cleanup_policy_scoped" {
  command = plan

  assert {
    condition = alltrue([for k in ["a", "b"] : jsondecode(ovh_cloud_project_user_s3_policy.cleanup[k].policy) == {
      Statement = [
        {
          Sid      = "CleanupListDelete"
          Effect   = "Allow"
          Action   = ["s3:ListBucket", "s3:ListBucketVersions", "s3:DeleteObject"]
          Resource = ["arn:aws:s3:::lzprobe-swift-reach-20261006t120000z-ab12-${k}", "arn:aws:s3:::lzprobe-swift-reach-20261006t120000z-ab12-${k}/*"]
        },
        {
          Sid      = "CleanupSeed"
          Effect   = "Allow"
          Action   = ["s3:PutObject"]
          Resource = ["arn:aws:s3:::lzprobe-swift-reach-20261006t120000z-ab12-${k}/lzprobe-swift-reach/seed"]
        },
      ]
    }])
    error_message = "each cleanup policy lists and deletes on its own bucket and writes its seed only"
  }
}

# lz-live's check reads output swift_check (data-model.md *Probe checks*): exactly its fifteen
# keys, the public ones with the run's values, published only once every policy exists.
run "swift_check_published" {
  command = plan

  assert {
    condition = nonsensitive(sort(keys(output.swift_check))) == tolist(["auth_url", "bucket_a", "bucket_b", "cleanup_a_key_id", "cleanup_a_secret", "cleanup_b_key_id", "cleanup_b_secret",
    "region", "run_id", "seed_key", "tenant_id", "user_a_name", "user_a_password", "user_b_name", "user_b_password"])
    error_message = "swift_check publishes exactly the fifteen keys the check reads"
  }

  assert {
    condition = (nonsensitive(output.swift_check.run_id) == "20261006T120000Z-ab12" && nonsensitive(output.swift_check.auth_url) == "https://auth.cloud.ovh.net/v3"
      && nonsensitive(output.swift_check.region) == "GRA" && nonsensitive(output.swift_check.tenant_id) == "0123456789abcdef0123456789abcdef"
      && nonsensitive(output.swift_check.seed_key) == "lzprobe-swift-reach/seed"
    && nonsensitive(output.swift_check.bucket_a) == "lzprobe-swift-reach-20261006t120000z-ab12-a" && nonsensitive(output.swift_check.bucket_b) == "lzprobe-swift-reach-20261006t120000z-ab12-b")
    error_message = "swift_check names the run, OVHcloud's Keystone, GRA, the project, the seed key and both buckets"
  }

  # A plan cannot show depends_on, so the file is read (as state-backend's deny_check run does).
  assert {
    condition     = length(output.buckets) == 2 && length(regexall("depends_on = \\[ovh_cloud_project_user_s3_policy.tenant, ovh_cloud_project_user_s3_policy.cleanup\\]\n}", file("main.tf"))) == 1
    error_message = "output swift_check depends on every S3 policy"
  }
}

# Spec 003 T039: the project record's project_id was empty on the live run 20261016T145959Z-42fd,
# while every mock supplied one. The tenant (Keystone project) id is the project's service_name,
# whatever project_id holds: empty (live) or another value.
run "tenant_id_is_the_service_name" {
  command = plan

  override_data {
    target = data.ovh_cloud_projects.all
    values = {
      projects = [{
        service_name  = "0123456789abcdef0123456789abcdef"
        project_id    = "fedcba9876543210fedcba9876543210"
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

  assert {
    condition     = nonsensitive(output.swift_check.tenant_id) == "0123456789abcdef0123456789abcdef"
    error_message = "swift_check's tenant_id is the project's service_name, not its project_id attribute"
  }
}

run "tenant_id_set_when_project_id_empty" {
  command = plan

  assert {
    condition     = nonsensitive(output.swift_check.tenant_id) != "" && nonsensitive(output.swift_check.tenant_id) == var.project_id
    error_message = "swift_check's tenant_id is non-empty and the project id when the record's project_id is empty"
  }
}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/swift-reach/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_with_run_id_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/.local/live/20261006T120000Z-ab12/swift-reach.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_of_another_root_refused" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/storage-iam.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_under_checkout_state_probes_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/swift-reach.tfstate"
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
