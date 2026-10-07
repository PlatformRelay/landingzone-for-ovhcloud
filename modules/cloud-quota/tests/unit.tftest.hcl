# Cloud quota module (spec 005 T025; FR-002, FR-004; ADR-0003, ADR-0005, ADR-0006; research R7
# applicability table, P11 optional and off by default). Mocked provider, no credential, no API
# call. Attribute names from the pinned ovh 2.21.0 schema (`ovh_cloud_quota`, data `ovh_cloud_quota`).
#
# `ovh_cloud_quota` is a singleton per project whose `regions` argument sets the quota profile per
# region (cloud_quota.md, kb mirror, read 2026-10-07): the guard must change the
# `prevent_automatic_quota_upgrade` flag only, so the regions it sends are the project's current
# ones, read through the data source (as the T009 quota probe does), never a profile of its own.
# Plan runs only: destroy semantics of the singleton are UNVERIFIED (the quota probe is plan-only).

mock_provider "ovh" {
  mock_data "ovh_cloud_quota" {
    defaults = {
      regions = [
        { region = "GRA11", profile = "default" },
        { region = "SBG5", profile = "50vms" },
      ]
    }
  }
}

variables {
  project_id = "0123456789abcdef0123456789abcdef"
}

run "quota_off_by_default" {
  command = plan

  assert {
    condition     = length(ovh_cloud_quota.this) == 0
    error_message = "no quota resource unless enabled (P11 is optional, default off)"
  }

  assert {
    condition     = output.prevent_automatic_quota_upgrade == null
    error_message = "the flag output is null when the guard is off"
  }

  assert {
    condition     = length(data.ovh_cloud_quota.current) == 0
    error_message = "the guard off reads no quota (no API call, no quota read permission needed)"
  }
}

run "quota_when_enabled" {
  command = plan

  variables {
    enabled = true
  }

  assert {
    condition     = length(ovh_cloud_quota.this) == 1
    error_message = "exactly one quota resource when enabled"
  }

  assert {
    condition     = try(ovh_cloud_quota.this[0].prevent_automatic_quota_upgrade, null) == true
    error_message = "the quota resource sets prevent_automatic_quota_upgrade = true"
  }

  assert {
    condition     = try(ovh_cloud_quota.this[0].service_name, null) == "0123456789abcdef0123456789abcdef"
    error_message = "the quota resource is on the given project"
  }

  assert {
    condition     = length(data.ovh_cloud_quota.current) == 1 && try(data.ovh_cloud_quota.current[0].service_name, null) == "0123456789abcdef0123456789abcdef"
    error_message = "the current regions are read from the given project (data.ovh_cloud_quota.current)"
  }

  assert {
    condition     = output.prevent_automatic_quota_upgrade == true
    error_message = "the flag output is true when the guard is on"
  }
}

# The guard sends the project's current regions and profiles unchanged: no profile of its own
# (which could raise or lower a quota), no region dropped or added.
run "quota_regions_unchanged" {
  command = plan

  variables {
    enabled = true
  }

  assert {
    condition = try(ovh_cloud_quota.this[0].regions, null) == tolist([
      { region = "GRA11", profile = "default" },
      { region = "SBG5", profile = "50vms" },
    ])
    error_message = "regions are exactly the project's current regions and profiles, in order"
  }
}

# A second project with other current regions: the guard reads that project and sends its regions
# (review r1: a data source bound to a fixed project would copy another project's profiles).
run "quota_other_project" {
  command = plan

  variables {
    enabled    = true
    project_id = "fedcba9876543210fedcba9876543210"
  }

  override_data {
    target = data.ovh_cloud_quota.current
    values = {
      regions = [
        { region = "DE1", profile = "default" },
      ]
    }
  }

  assert {
    condition     = try(ovh_cloud_quota.this[0].service_name, null) == "fedcba9876543210fedcba9876543210"
    error_message = "a second project id is applied as given (no constant)"
  }

  assert {
    condition     = try(data.ovh_cloud_quota.current[0].service_name, null) == "fedcba9876543210fedcba9876543210"
    error_message = "the current regions are read from the second project"
  }

  assert {
    condition     = try(ovh_cloud_quota.this[0].regions, null) == tolist([{ region = "DE1", profile = "default" }])
    error_message = "the second project's own current regions are sent"
  }
}

# The flag is not an input: a caller cannot turn the guard into its opposite (a test variable the
# module does not declare is ignored, so a module that adds the input fails here).
run "flag_not_switchable" {
  command = plan

  variables {
    enabled                         = true
    prevent_automatic_quota_upgrade = false
  }

  assert {
    condition     = try(ovh_cloud_quota.this[0].prevent_automatic_quota_upgrade, null) == true
    error_message = "prevent_automatic_quota_upgrade stays true whatever the caller passes"
  }
}

# Regions are not an input either: a caller cannot send a profile through the guard.
run "regions_not_switchable" {
  command = plan

  variables {
    enabled = true
    regions = [{ region = "GRA11", profile = "200vms" }]
  }

  assert {
    condition = try(ovh_cloud_quota.this[0].regions, null) == tolist([
      { region = "GRA11", profile = "default" },
      { region = "SBG5", profile = "50vms" },
    ])
    error_message = "regions stay the project's current ones whatever the caller passes"
  }
}

run "empty_project_id_rejected" {
  command = plan

  variables {
    enabled    = true
    project_id = " "
  }

  expect_failures = [var.project_id]
}
