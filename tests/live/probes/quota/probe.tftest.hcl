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
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/quota.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
}

override_data {
  target = data.ovh_cloud_quota.current
  values = {
    prevent_automatic_quota_upgrade = false
    regions                         = [{ region = "GRA11", profile = "default" }]
  }
}

run "flag_only_regions_unchanged" {
  command = plan

  assert {
    condition     = ovh_cloud_quota.probe.prevent_automatic_quota_upgrade && ovh_cloud_quota.probe.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "the probe sets prevent_automatic_quota_upgrade on the sandbox project"
  }

  assert {
    condition     = length(ovh_cloud_quota.probe.regions) == 1 && ovh_cloud_quota.probe.regions[0].region == "GRA11" && ovh_cloud_quota.probe.regions[0].profile == "default"
    error_message = "regions are the project's current target profiles, unchanged"
  }
}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/quota/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_with_run_id_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/.local/live/20261006T120000Z-ab12/quota.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_of_another_root_refused" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/alerting.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_under_checkout_state_probes_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/quota.tfstate"
  }

  expect_failures = [var.state_path]
}
