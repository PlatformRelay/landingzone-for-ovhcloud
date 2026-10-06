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
  run_id           = "20261006T120000Z-ab12"
  project_id       = "0123456789abcdef0123456789abcdef"
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
