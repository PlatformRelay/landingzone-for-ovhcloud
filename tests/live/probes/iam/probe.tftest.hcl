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
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/iam.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
}

run "names_carry_prefix_and_run_id" {
  command = plan

  assert {
    condition     = ovh_me_api_oauth2_client.probe.name == "lzprobe-iam-20261006T120000Z-ab12" && ovh_iam_policy.probe.name == "lzprobe-iam-20261006T120000Z-ab12"
    error_message = "client and policy names must be lzprobe-iam-<run-id>"
  }

  assert {
    condition     = ovh_iam_resource_tags.probe.tags == tomap({ "lzprobe-p15:run-id" = "20261006T120000Z-ab12" })
    error_message = "the project URN carries exactly one probe tag: an lzprobe- key holding `:`, valued with the run id"
  }

  assert {
    condition     = ovh_iam_policy.probe.allow == toset(["publicCloudProject:apiovh:get"]) && ovh_iam_policy.probe.resources == toset(["urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"]) && ovh_iam_resource_tags.probe.urn == "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "the policy grants one read on the sandbox project only, and the tags target the project URN"
  }
}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/iam/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_with_run_id_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/.local/live/20261006T120000Z-ab12/iam.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_of_another_root_refused" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/network.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_under_checkout_state_probes_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/iam.tfstate"
  }

  expect_failures = [var.state_path]
}
