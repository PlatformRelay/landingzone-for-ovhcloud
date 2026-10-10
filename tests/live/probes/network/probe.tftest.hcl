# Offline naming controls (T008): mocked provider, plan only, no credential, no API call.
mock_provider "ovh" {
  mock_data "ovh_cloud_project_network_privates" {
    defaults = { networks = [] }
  }
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
  run_id           = "20261006T120000Z-ab12"
  project_id       = "0123456789abcdef0123456789abcdef"
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

# T098: a project holds one private network per VLAN id (T010 run 20261010T184645Z-f7de: 400
# `A private network already exists for this vlan ID`, every probe network had VLAN 0). The probe
# network starts at 2000 + (the run id's four hex digits mod 1000), inside 2000..2999: the
# network probe's own range, disjoint from the storage-iam probe's (3000..3999) and inside the
# 0..4000 the modules admit. These runs have no other network in the project (the mocked listing
# is empty); the runs below skip ids other networks hold, so two runs never share one.

run "vlan_id_derived_from_run_id" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/network.tfstate"
    run_id     = "20261006T120000Z-ab12"
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2794
    error_message = "the VLAN id is 2000 + (ab12 = 43794) mod 1000: want 2794"
  }
}

run "vlan_id_differs_for_another_run_id" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab13/network.tfstate"
    run_id     = "20261006T120000Z-ab13"
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2795
    error_message = "another run id gives another VLAN id: want 2795"
  }
}

run "vlan_id_lowest_of_the_range" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-0000/network.tfstate"
    run_id     = "20261006T120000Z-0000"
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2000
    error_message = "suffix 0000 gives the range's first id: want 2000"
  }
}

run "vlan_id_highest_of_the_range" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-03e7/network.tfstate"
    run_id     = "20261006T120000Z-03e7"
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2999
    error_message = "suffix 03e7 (999) gives the range's last id: want 2999"
  }
}

run "vlan_id_wraps_into_the_range" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ffff/network.tfstate"
    run_id     = "20261006T120000Z-ffff"
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2535
    error_message = "suffix ffff (65535) wraps into the range: want 2535"
  }
}

# T098 review r1: the id skips ids other networks of the project hold (a leftover, an applied
# concurrent run), keeps its own network's id on a re-plan, wraps at the range's end.
# (The precondition refusing an exhausted range is not run here: override_data values cannot
# be built with a function, and 1000 literal networks are not worth the file.)

run "vlan_id_skips_an_id_another_network_holds" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/network.tfstate"
    run_id     = "20261006T120000Z-ab12"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-2794", status = "ACTIVE", name = "lzprobe-other", type = "private", vlan_id = 2794, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2795
    error_message = "an id another network holds is skipped: want 2795"
  }
}

run "vlan_id_skips_a_run_of_taken_ids" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/network.tfstate"
    run_id     = "20261006T120000Z-ab12"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-2794", status = "ACTIVE", name = "lzprobe-a", type = "private", vlan_id = 2794, regions = [] },
        { id = "pn-2795", status = "ACTIVE", name = "lzprobe-b", type = "private", vlan_id = 2795, regions = [] },
        { id = "pn-2797", status = "ACTIVE", name = "lzprobe-c", type = "private", vlan_id = 2797, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2796
    error_message = "the first free id after the start is taken: want 2796"
  }
}

run "vlan_id_kept_for_its_own_network" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/network.tfstate"
    run_id     = "20261006T120000Z-ab12"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-2794", status = "ACTIVE", name = "lzprobe-network-20261006T120000Z-ab12", type = "private", vlan_id = 2794, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2794
    error_message = "the probe's own network does not move its id on a re-plan: want 2794"
  }
}

run "vlan_id_kept_when_an_earlier_id_frees" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/network.tfstate"
    run_id     = "20261006T120000Z-ab12"
  }

  # Review r2: applied on 2795 while 2794 was held; the holder is gone now. The
  # probe's own network keeps 2795 (vlan_id forces a replacement).
  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-2795", status = "ACTIVE", name = "lzprobe-network-20261006T120000Z-ab12", type = "private", vlan_id = 2795, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2795
    error_message = "the probe's own network keeps its id when an earlier one frees: want 2795"
  }
}

run "vlan_id_wraps_past_a_taken_last_id" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-03e7/network.tfstate"
    run_id     = "20261006T120000Z-03e7"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-2999", status = "ACTIVE", name = "lzprobe-other", type = "private", vlan_id = 2999, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2000
    error_message = "past a taken last id the search wraps to the range's first: want 2000"
  }
}

run "vlan_id_ignores_ids_outside_its_range" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/network.tfstate"
    run_id     = "20261006T120000Z-ab12"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-0", status = "ACTIVE", name = "lz-demo-dev-gra11-pn-main", type = "private", vlan_id = 0, regions = [] },
        { id = "pn-3794", status = "ACTIVE", name = "lzprobe-other", type = "private", vlan_id = 3794, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.probe.vlan_id == 2794
    error_message = "ids outside the range do not move it: want 2794"
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
