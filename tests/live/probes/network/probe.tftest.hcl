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
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/network.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
}

run "names_carry_prefix_and_run_id" {
  command = plan

  assert {
    condition     = ovh_cloud_project_network_private.probe.name == "lzprobe-network-20261006T120000Z-ab12"
    error_message = "network name must be lzprobe-network-<run-id>"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.probe.service_name == "0123456789abcdef0123456789abcdef" && ovh_cloud_project_network_private.probe.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "every resource targets the discovered sandbox project"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.probe.no_gateway && toset(ovh_cloud_project_network_private.probe.regions) == toset(["GRA11"])
    error_message = "no gateway (billed), GRA11 only"
  }
}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/network/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_of_another_root_refused" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/iam.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_with_run_id_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/.local/live/20261006T120000Z-ab12/network.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_under_checkout_state_probes_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/network.tfstate"
  }

  expect_failures = [var.state_path]
}
