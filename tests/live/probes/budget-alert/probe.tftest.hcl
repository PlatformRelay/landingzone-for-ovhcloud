# Offline controls of the budget-alert root (spec 007 T010; FR-005, FR-008): mocked provider, plan
# only, no credential, no API call. The first two runs are red on the T010 stub (the alert is off,
# the outputs are placeholders) until T016; the others pin the reference mode and the copied
# boilerplate. Address-sensitive assertions go through try() so a moved address fails the
# assertion instead of erroring.
mock_provider "ovh" {
  mock_data "ovh_me" {
    defaults = {
      email = "owner@example.invalid"
    }
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

  # Its URN's region (`us`) differs on purpose from the account listing's (`eu`): run
  # `module_in_reference_mode` uses it as the marker that the module read this data source.
  mock_data "ovh_cloud_project" {
    defaults = {
      iam = {
        id           = "00000000-0000-0000-0000-000000000000"
        urn          = "urn:v1:us:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
        display_name = "sandbox"
        tags         = {}
      }
    }
  }
}

variables {
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/budget-alert.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
  run_id           = "20261006T120000Z-ab12"
  project_id       = "0123456789abcdef0123456789abcdef"
}

# Red on the stub: a root that does not reach the module's alert path. A test reaches a child
# module only through its outputs, so the alert's presence is read from `budget_alert_id` (the
# module's `one()` over a count of 0 or 1), its project from `project_id` (the alert's
# service_name is the module's project_id, modules/cloud-project/main.tf:43-50), and its values
# from `local.budget_alert`, the object main.tf passes as the module's `budget_alert`; the mapping
# of those values onto the resource is pinned by modules/cloud-project's own tests.
run "module_alert_on_run_project" {
  command = plan

  override_resource {
    target = module.project.ovh_cloud_project_alerting.this
    values = {
      id = "1a2b3c4d-0000-4000-8000-000000000001"
    }
  }

  assert {
    condition     = module.project.budget_alert_id == "1a2b3c4d-0000-4000-8000-000000000001"
    error_message = "exactly one module.project.ovh_cloud_project_alerting.this[0]: the root reaches the module's alert path"
  }

  assert {
    condition = (
      try(local.budget_alert.enabled, false) == true &&
      try(local.budget_alert.monthly_threshold, null) == 1 &&
      try(local.budget_alert.delay, null) == 3600 &&
      try(local.budget_alert.email, null) == "owner@example.invalid"
    )
    error_message = "the alert is enabled with threshold 1, delay 3600 and the account's own address (ovh_me)"
  }

  assert {
    condition     = module.project.project_id == "0123456789abcdef0123456789abcdef"
    error_message = "the root passes the run's project_id to the module (the alert's service_name)"
  }

  assert {
    condition     = output.run_id == "20261006T120000Z-ab12"
    error_message = "the run id is read from run_id (TF_VAR_run_id)"
  }
}

# Red on the stub: a check output the runner cannot decode (runner.go refuses a scalar) or that
# names another id. The alert's id is fixed here so the outputs are known on plan.
run "outputs_name_the_module_alert" {
  command = plan

  override_resource {
    target = module.project.ovh_cloud_project_alerting.this
    values = {
      id = "1a2b3c4d-0000-4000-8000-000000000001"
    }
  }

  assert {
    condition     = output.alert_id == "1a2b3c4d-0000-4000-8000-000000000001"
    error_message = "alert_id is the module's budget_alert_id, a scalar"
  }

  assert {
    condition = (
      try(sort(keys(output.budget_alert_check)), []) == tolist(["alert_id"]) &&
      try(alltrue([for v in values(output.budget_alert_check) : try(tostring(v) == v, false)]), false) &&
      try(output.budget_alert_check.alert_id, null) == output.alert_id
    )
    error_message = "budget_alert_check is a map of strings whose alert_id is alert_id's value"
  }
}

# Pin: module.project is in reference mode, so it only reads the project and plans none. In adopt
# mode the URN would come from a planned `ovh_cloud_project` (a generated mock value), not from
# the data source's fixed one. This pins the one module call's mode, not a project declared
# elsewhere in the root.
run "module_in_reference_mode" {
  command = plan

  assert {
    condition     = module.project.urn == "urn:v1:us:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "module.project is in reference mode: the project is only read (the data source's URN), no ovh_cloud_project is planned by it"
  }
}

# Pins: copied boilerplate; the state path ends in /<run-id>/budget-alert.tfstate under the account.
run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/budget-alert/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_under_checkout_state_probes_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/budget-alert.tfstate"
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

run "run_id_other_than_state_path_refused" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ffff"
  }

  expect_failures = [var.run_id]
}

# Pin: two projects on the account; the module reads project_id's, not the other.
run "module_reads_project_id_among_two" {
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
      ]
    }
  }

  assert {
    condition     = module.project.project_id == "0123456789abcdef0123456789abcdef"
    error_message = "the root passes project_id's project to the module, not the account's other one"
  }
}
