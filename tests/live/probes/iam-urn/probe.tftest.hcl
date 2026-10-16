# Offline controls (spec 003 T034): mocked provider, plan only, no credential, no API call.
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

  mock_data "ovh_me" {
    defaults = {
      urn       = "urn:v1:eu:resource:account:xx1111-ovh"
      nichandle = "xx1111-ovh"
    }
  }
}

variables {
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/iam-urn.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
  run_id           = "20261006T120000Z-ab12"
  project_id       = "0123456789abcdef0123456789abcdef"
}

# Spec 003 T034: the root owns one resource, the probe bucket lzprobe-iam-urn-<lower(run id)> in
# GRA (where the check's Sweep looks), and never tags the retained project: no IAM tag resource,
# no other resource. The check owns the clients, policies and the tag key.
run "creates_only_the_bucket" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.bucket.name == "lzprobe-iam-urn-20261006t120000z-ab12" && ovh_cloud_project_storage.bucket.region_name == "GRA" && ovh_cloud_project_storage.bucket.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "the bucket is lzprobe-iam-urn-<lower(run id)> in GRA on the sandbox project"
  }

  assert {
    condition     = ovh_cloud_project_storage.bucket.tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12" })
    error_message = "the bucket carries lz:run-id = <run-id> only (the leftover check's tag)"
  }

  assert {
    condition = (ovh_cloud_project_storage.bucket.name != "" && length(regexall("(?m)^\\s*resource\\s", file("main.tf"))) == 1 && length(regexall("(?m)^\\s*resource\\s+\"ovh_cloud_project_storage\"\\s+\"bucket\"", file("main.tf"))) == 1
    && length(regexall("(?m)^\\s*resource\\s", file("probe.tf"))) == 0 && length(regexall("ovh_iam_resource_tags|ovh_iam_policy|ovh_me_api_oauth2_client", file("main.tf"))) == 0)
    error_message = "the iam-urn root declares the bucket and nothing else: no IAM tag, policy or client (the check owns them; the retained project is never tagged)"
  }
}

# lz-live's check reads output urn_check (data-model.md *Probe checks*): exactly its five keys,
# with the run's values; the write's target is the bucket's URN, never the project's or the
# account's.
run "urn_check_published" {
  command = plan

  assert {
    condition     = nonsensitive(sort(keys(output.urn_check))) == tolist(["account_urn", "bucket", "bucket_urn", "project_urn", "run_id"])
    error_message = "urn_check publishes exactly the five keys the check reads"
  }

  assert {
    condition = (nonsensitive(output.urn_check.run_id) == "20261006T120000Z-ab12"
      && nonsensitive(output.urn_check.project_urn) == "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
      && nonsensitive(output.urn_check.account_urn) == "urn:v1:eu:resource:account:xx1111-ovh"
    && nonsensitive(output.urn_check.bucket) == "lzprobe-iam-urn-20261006t120000z-ab12")
    error_message = "urn_check names the run, the sandbox project's URN, the account's URN and the probe bucket"
  }

  assert {
    condition = (nonsensitive(output.urn_check.bucket_urn) == "urn:v1:eu:resource:storageS3:0123456789abcdef0123456789abcdef/lzprobe-iam-urn-20261006t120000z-ab12"
      && nonsensitive(output.urn_check.bucket_urn) != nonsensitive(output.urn_check.project_urn)
    && nonsensitive(output.urn_check.bucket_urn) != nonsensitive(output.urn_check.account_urn))
    error_message = "bucket_urn names the probe bucket (form UNVERIFIED) and is neither the project's nor the account's URN"
  }
}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/iam-urn/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_with_run_id_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/.local/live/20261006T120000Z-ab12/iam-urn.tfstate"
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
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/iam-urn.tfstate"
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
