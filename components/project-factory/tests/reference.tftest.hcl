# Project-factory component, reference mode (spec 005 T027; research R7 fallback, R24; KD-3). Mocked
# provider, no credential, no API call. See unit.tftest.hcl for the shared reading.
#
# Reference mode is the sandbox's path (T009 live probe, 2026-10-07: P5 refuted, the import plans a
# replacement; R7 fallback, sandbox mode set in T039): the component publishes the same outputs here
# as in adopt mode.
#
# Reference mode reads the given project through modules/cloud-project's data source and manages no
# project: a managed `ovh_cloud_project` without an import would order one (cloud_project.md). Tripwire:
# the mock default on the non-computed `deletion_protection` of `ovh_cloud_project` is inert unless a
# project is planned, and then fails the run (`Non-computed field … is not allowed to be overridden`,
# observed tofu 1.13.0, T023). The project is still labelled through its IAM tags.

mock_provider "ovh" {
  mock_resource "ovh_cloud_project" {
    defaults = {
      deletion_protection = false
    }
  }

  mock_data "ovh_cloud_project" {
    defaults = {
      iam = {
        display_name = "mock-default"
        id           = "mock-default"
        tags         = {}
        urn          = "urn:v1:eu:resource:publicCloudProject:mock-default-not-the-read-project"
      }
    }
  }

  mock_resource "ovh_cloud_project_alerting" {
    defaults = {
      id = "mock-default-alert-id"
    }
  }
}

override_data {
  target = module.project.data.ovh_cloud_project.this[0]
  values = {
    iam = {
      display_name = "demo-dev"
      id           = "mock-iam-id"
      tags         = {}
      urn          = "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    }
  }
}

override_resource {
  target = module.project.ovh_cloud_project_alerting.this[0]
  values = {
    id = "mock-alert-id-0123"
  }
}

variables {
  org         = "lz"
  tenant      = "demo"
  environment = "dev"
  instance    = "demo-dev-project"
  managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/project"
  mode        = "reference"
  project_id  = "0123456789abcdef0123456789abcdef"
  budget_alert = {
    enabled           = true
    monthly_threshold = 50
    email             = "finops@example.org"
  }
}

run "reference_reads_the_bound_project_and_orders_none" {
  command = plan

  assert {
    condition     = output.project_id == "0123456789abcdef0123456789abcdef" && try(module.project.project_id, null) == "0123456789abcdef0123456789abcdef"
    error_message = "reference mode passes the given (bound) project id to modules/cloud-project"
  }

  assert {
    condition     = output.project_urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef" && try(module.project.urn, null) == output.project_urn
    error_message = "project_urn is the read project's IAM URN, not built from the id"
  }
}

run "reference_follows_the_bound_reference" {
  command = plan

  variables {
    project_id = "fedcba9876543210fedcba9876543210"
  }

  override_data {
    target = module.project.data.ovh_cloud_project.this[0]
    values = {
      iam = {
        display_name = "other"
        id           = "mock-iam-id-other"
        tags         = {}
        urn          = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }

  assert {
    condition     = output.project_id == "fedcba9876543210fedcba9876543210" && output.project_urn == "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
    error_message = "the read project follows the bound reference, not a constant"
  }
}

run "reference_labels_and_alert" {
  command = plan

  assert {
    condition = try(tomap(output.labels), null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/project"
      "lz:instance"   = "demo-dev-project"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "a referenced project is labelled with the mandatory label set too"
  }

  assert {
    condition     = output.budget_alert_id == "mock-alert-id-0123" && jsonencode(output.unlabelled) == jsonencode(["module.project.ovh_cloud_project_alerting.this[0]"])
    error_message = "the budget alert is available in reference mode; the quota guard stays off by default"
  }
}

run "reference_quota_guard" {
  command = plan

  variables {
    quota_guard = { enabled = true }
  }

  assert {
    condition     = output.prevent_automatic_quota_upgrade == true && output.budget_alert_id == "mock-alert-id-0123"
    error_message = "the quota guard and the alert reach the modules in reference mode"
  }

  assert {
    condition = (length(output.unlabelled) == 2 && toset(output.unlabelled) == toset([
      "module.project.ovh_cloud_project_alerting.this[0]",
      "module.quota.ovh_cloud_quota.this[0]",
    ]))
    error_message = "unlabelled lists exactly the alert and the quota setting in reference mode"
  }
}

run "reference_quota_on_alert_off" {
  command = plan

  variables {
    budget_alert = { enabled = false }
    quota_guard  = { enabled = true }
  }

  assert {
    condition     = output.budget_alert_id == null && output.prevent_automatic_quota_upgrade == true
    error_message = "reference mode: the toggles are independent (alert off, quota guard on)"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == jsonencode(["module.quota.ovh_cloud_quota.this[0]"])
    error_message = "reference mode: unlabelled lists the quota setting only"
  }
}

run "reference_urn_of_another_project_refused" {
  command = plan

  override_data {
    target = module.project.data.ovh_cloud_project.this[0]
    values = {
      iam = {
        display_name = "other"
        id           = "mock-iam-id-other"
        tags         = {}
        urn          = "urn:v1:ca:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }

  expect_failures = [output.project_urn]
}
