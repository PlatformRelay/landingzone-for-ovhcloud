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
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/state-backend.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
}

run "names_carry_prefix_and_run_id" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.probe.name == "lzprobe-state-20261006t120000z-ab12" && ovh_cloud_project_user.probe.description == "lzprobe-state-20261006T120000Z-ab12"
    error_message = "bucket and user carry lzprobe-state-<run-id> (bucket lowercased)"
  }

  assert {
    condition     = ovh_cloud_project_storage.probe.tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12" }) && ovh_cloud_project_storage.probe.versioning.status == "enabled"
    error_message = "the bucket is versioned and tagged lz:run-id = <run-id>"
  }

  assert {
    condition     = jsondecode(ovh_cloud_project_user_s3_policy.probe.policy).Statement[0].Resource == ["arn:aws:s3:::lzprobe-state-20261006t120000z-ab12", "arn:aws:s3:::lzprobe-state-20261006t120000z-ab12/*"]
    error_message = "the S3 policy covers the probe bucket only"
  }
}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/state-backend/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_with_run_id_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/.local/live/20261006T120000Z-ab12/state-backend.tfstate"
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
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/state-backend.tfstate"
  }

  expect_failures = [var.state_path]
}
