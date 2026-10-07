# Project stage with the budget alert and the quota guard off (spec 005 T027; P10, P11). Mocked
# provider, no credential, no API call. See unit.tftest.hcl for the shared reading.
#
# Tripwires: mock defaults on non-computed fields of the alert, the quota resource and the quota
# data source fail the run if one of them is planned or read (observed tofu 1.13.0, T023), in adopt
# and in reference mode (the sandbox's mode since T009 refuted P5).

mock_provider "ovh" {
  mock_resource "ovh_cloud_project" {
    defaults = {
      urn        = "urn:v1:eu:resource:publicCloudProject:mock-default-not-the-import-target"
      project_id = "mock-default-not-the-import-target"
    }
  }

  mock_resource "ovh_cloud_project_alerting" {
    defaults = {
      email = "tripwire-no-alert-when-off"
    }
  }

  mock_resource "ovh_cloud_quota" {
    defaults = {
      service_name = "tripwire-no-quota-when-off"
    }
  }

  mock_data "ovh_cloud_quota" {
    defaults = {
      service_name = "tripwire-no-quota-read-when-off"
    }
  }
}

override_resource {
  target = module.project_factory.module.project.ovh_cloud_project.this[0]
  values = {
    urn        = "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    project_id = "0123456789abcdef0123456789abcdef"
  }
}

override_data {
  target = module.project_factory.module.project.data.ovh_cloud_project.this[0]
  values = {
    iam = {
      display_name = "demo-dev"
      id           = "mock-iam-id"
      tags         = {}
      urn          = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    }
  }
}

variables {
  org          = "lz"
  tenant       = "demo"
  environment  = "dev"
  instance     = "demo-dev-project"
  managed_in   = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/project"
  project_mode = "adopt"
  project_id   = "0123456789abcdef0123456789abcdef"
  regions      = ["GRA11"]
}

run "toggles_off_by_default_through_the_stage" {
  command = plan

  assert {
    condition     = output.project_urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "the adopted project is planned without the optional parts"
  }

  assert {
    condition     = output.budget_alert_id == null && jsonencode(output.unlabelled) == "[]" && try(module.project_factory.prevent_automatic_quota_upgrade, "missing") == null
    error_message = "alert and quota guard are off by default: no alert id, no quota flag, nothing unlabelled"
  }
}

run "toggles_off_explicitly_through_the_stage" {
  command = plan

  variables {
    budget_alert = { enabled = false }
    quota_guard  = { enabled = false }
  }

  assert {
    condition     = output.project_urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "the adopted project is planned without the optional parts"
  }

  assert {
    condition     = output.budget_alert_id == null && jsonencode(output.unlabelled) == "[]" && try(module.project_factory.prevent_automatic_quota_upgrade, "missing") == null
    error_message = "enabled = false reaches the component for both toggles"
  }
}

run "reference_toggles_off_explicitly_through_the_stage" {
  command = plan

  variables {
    project_mode = "reference"
    budget_alert = { enabled = false }
    quota_guard  = { enabled = false }
  }

  assert {
    condition     = output.project_urn == "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "reference mode: the read project is published without the optional parts"
  }

  assert {
    condition     = output.budget_alert_id == null && jsonencode(output.unlabelled) == "[]" && try(module.project_factory.prevent_automatic_quota_upgrade, "missing") == null
    error_message = "reference mode: alert and quota guard off, nothing managed or read for them"
  }
}
