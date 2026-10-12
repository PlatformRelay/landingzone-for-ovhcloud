# Offline controls of the companion root (T075): mocked providers, plan only, no credential, no
# API call, no backend.

# The P25 reads, GET /me and the IAM write are observed by lz-live's storage-iam check (T100,
# tools/internal/probes/live/storageiam), not here: the companion holds no check block and no P25
# identity.
mock_provider "ovh" {
  mock_data "ovh_cloud_project_network_privates" {
    defaults = { networks = [] }
  }
}

variables {
  state_passphrase = "offline-test-passphrase-not-a-secret"
  run_id           = "20261006T120000Z-ab12"
  project_id       = "0123456789abcdef0123456789abcdef"
}

# The tenant identity's resources (P9) carry the prefix and the run id, in the sandbox project.
run "p9_names_carry_prefix_and_run_id" {
  command = plan

  assert {
    condition     = ovh_cloud_project_network_private.p9.name == "lzprobe-p9-20261006T120000Z-ab12" && ovh_cloud_project_network_private.p9.service_name == "0123456789abcdef0123456789abcdef" && ovh_cloud_project_network_private.p9.regions == toset(["GRA11"])
    error_message = "P9 network lzprobe-p9-<run-id> in GRA11 of the sandbox project"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.p9.network == "10.251.0.0/24" && ovh_cloud_project_network_private_subnet.p9.no_gateway == true && ovh_cloud_project_network_private_subnet.p9.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "P9 subnet 10.251.0.0/24 without gateway in the sandbox project"
  }

  assert {
    condition     = ovh_cloud_project_storage.p9.name == "lzprobe-p9-20261006t120000z-ab12" && ovh_cloud_project_storage.p9.tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12" }) && ovh_cloud_project_storage.p9.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "P9 bucket lzprobe-p9-<run-id> (lowercased) tagged lz:run-id in the sandbox project"
  }
}

# T098: a project holds one private network per VLAN id (T010 run 20261010T184645Z-f7de: 400
# `A private network already exists for this vlan ID`, every probe network had VLAN 0). The probe
# network starts at 3000 + (the run id's four hex digits mod 1000), inside 3000..3999: the
# storage-iam probe's own range, disjoint from the network probe's (2000..2999) and inside the
# 0..4000 the modules admit. These runs have no other network in the project (the mocked listing
# is empty); the runs below skip ids other networks hold, so two runs never share one.

run "vlan_id_derived_from_run_id" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ab12"
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3794
    error_message = "the VLAN id is 3000 + (ab12 = 43794) mod 1000: want 3794"
  }
}

run "vlan_id_differs_for_another_run_id" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ab13"
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3795
    error_message = "another run id gives another VLAN id: want 3795"
  }
}

run "vlan_id_lowest_of_the_range" {
  command = plan

  variables {
    run_id = "20261006T120000Z-0000"
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3000
    error_message = "suffix 0000 gives the range's first id: want 3000"
  }
}

run "vlan_id_highest_of_the_range" {
  command = plan

  variables {
    run_id = "20261006T120000Z-03e7"
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3999
    error_message = "suffix 03e7 (999) gives the range's last id: want 3999"
  }
}

run "vlan_id_wraps_into_the_range" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ffff"
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3535
    error_message = "suffix ffff (65535) wraps into the range: want 3535"
  }
}

# T098 review r1: the id skips ids other networks of the project hold (a leftover, an applied
# concurrent run), keeps its own network's id on a re-plan, wraps at the range's end.
# (The precondition refusing an exhausted range is not run here: override_data values cannot
# be built with a function, and 1000 literal networks are not worth the file.)

run "vlan_id_skips_an_id_another_network_holds" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ab12"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-3794", status = "ACTIVE", name = "lzprobe-other", type = "private", vlan_id = 3794, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3795
    error_message = "an id another network holds is skipped: want 3795"
  }
}

run "vlan_id_skips_a_run_of_taken_ids" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ab12"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-3794", status = "ACTIVE", name = "lzprobe-a", type = "private", vlan_id = 3794, regions = [] },
        { id = "pn-3795", status = "ACTIVE", name = "lzprobe-b", type = "private", vlan_id = 3795, regions = [] },
        { id = "pn-3797", status = "ACTIVE", name = "lzprobe-c", type = "private", vlan_id = 3797, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3796
    error_message = "the first free id after the start is taken: want 3796"
  }
}

run "vlan_id_kept_for_its_own_network" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ab12"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-3794", status = "ACTIVE", name = "lzprobe-p9-20261006T120000Z-ab12", type = "private", vlan_id = 3794, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3794
    error_message = "the probe's own network does not move its id on a re-plan: want 3794"
  }
}

run "vlan_id_kept_when_an_earlier_id_frees" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ab12"
  }

  # Review r2: applied on 3795 while 3794 was held; the holder is gone now. The
  # probe's own network keeps 3795 (vlan_id forces a replacement).
  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-3795", status = "ACTIVE", name = "lzprobe-p9-20261006T120000Z-ab12", type = "private", vlan_id = 3795, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3795
    error_message = "the probe's own network keeps its id when an earlier one frees: want 3795"
  }
}

run "vlan_id_wraps_past_a_taken_last_id" {
  command = plan

  variables {
    run_id = "20261006T120000Z-03e7"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-3999", status = "ACTIVE", name = "lzprobe-other", type = "private", vlan_id = 3999, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3000
    error_message = "past a taken last id the search wraps to the range's first: want 3000"
  }
}

run "vlan_id_ignores_ids_outside_its_range" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ab12"
  }

  override_data {
    target = data.ovh_cloud_project_network_privates.existing
    values = {
      networks = [
        { id = "pn-0", status = "ACTIVE", name = "lz-demo-dev-gra11-pn-main", type = "private", vlan_id = 0, regions = [] },
        { id = "pn-2794", status = "ACTIVE", name = "lzprobe-other", type = "private", vlan_id = 2794, regions = [] },
      ]
    }
  }

  assert {
    condition     = ovh_cloud_project_network_private.p9.vlan_id == 3794
    error_message = "ids outside the range do not move it: want 3794"
  }
}

run "run_id_malformed_refused" {
  command = plan

  variables {
    run_id = "../20261006T120000Z-ab12"
  }

  expect_failures = [var.run_id]
}

run "project_id_malformed_refused" {
  command = plan

  variables {
    project_id = "sandbox"
  }

  expect_failures = [var.project_id]
}
