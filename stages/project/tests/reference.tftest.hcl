# Project stage, reference mode (spec 005 T027; research R7 fallback, R24; KD-3). Mocked provider, no
# credential, no API call. See unit.tftest.hcl for the shared reading.
#
# Reference mode is the sandbox's path: T009's live probe (2026-10-07, plan-only) refuted P5 (the
# import plans a replacement), so per R7 the sandbox manifest uses `reference` (T039). This file
# therefore pins the same published values as the adopt file: `project_id`, `project_urn`,
# `regions`, `tenant`, `environment`, `budget_alert_id` and `unlabelled`, with both toggles.
#
# `project_mode = "reference"` reaches modules/cloud-project: the project is read, never managed.
# Tripwire: the mock default on the non-computed `deletion_protection` of `ovh_cloud_project` fails
# the run if a project is planned (observed tofu 1.13.0, T023); a managed project without an import
# would order one (cloud_project.md).

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
  target = module.project_factory.module.project.data.ovh_cloud_project.this[0]
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
  target = module.project_factory.module.project.ovh_cloud_project_alerting.this[0]
  values = {
    id = "mock-alert-id-ref"
  }
}

variables {
  org          = "lz"
  tenant       = "demo"
  environment  = "dev"
  instance     = "demo-dev-project"
  managed_in   = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/project"
  project_mode = "reference"
  project_id   = "0123456789abcdef0123456789abcdef"
  regions      = ["GRA11"]
}

run "reference_mode_through_the_stage" {
  command = plan

  assert {
    condition     = output.project_id == "0123456789abcdef0123456789abcdef" && try(module.project_factory.project_id, null) == output.project_id
    error_message = "the read project is the resolved reference (KD-3)"
  }

  assert {
    condition     = output.project_urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "project_urn is the read project's IAM URN: project_mode reaches the component"
  }

  assert {
    condition     = output.tenant == "demo" && output.environment == "dev" && jsonencode(output.regions) == jsonencode(["GRA11"])
    error_message = "tenant, environment and regions are published in reference mode too"
  }

  assert {
    condition     = output.budget_alert_id == null && jsonencode(output.unlabelled) == "[]"
    error_message = "no alert or quota setting by default, nothing unlabelled"
  }
}

run "reference_published_outputs_with_both_toggles" {
  command = plan

  variables {
    regions = ["SBG5", "GRA11"]
    budget_alert = {
      enabled           = true
      monthly_threshold = 50
      email             = "finops@example.org"
    }
    quota_guard = { enabled = true }
  }

  assert {
    condition     = output.project_id == "0123456789abcdef0123456789abcdef" && output.project_urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "project_id and project_urn are published from the read project"
  }

  assert {
    condition     = output.tenant == "demo" && output.environment == "dev" && jsonencode(output.regions) == jsonencode(["SBG5", "GRA11"])
    error_message = "tenant, environment and regions (as given) are published in reference mode"
  }

  assert {
    condition     = output.budget_alert_id == "mock-alert-id-ref" && try(module.project_factory.prevent_automatic_quota_upgrade, null) == true
    error_message = "both toggles reach the component in reference mode"
  }

  assert {
    condition = (length(output.unlabelled) == 2 && toset(output.unlabelled) == toset([
      "module.project_factory.module.project.ovh_cloud_project_alerting.this[0]",
      "module.project_factory.module.quota.ovh_cloud_quota.this[0]",
    ]))
    error_message = "unlabelled lists exactly the alert and the quota setting from the stage root, once each"
  }

  assert {
    condition = !anytrue([
      issensitive(output.tenant), issensitive(output.environment), issensitive(output.project_id),
      issensitive(output.project_urn), issensitive(output.regions), issensitive(output.budget_alert_id),
      issensitive(output.unlabelled),
    ])
    error_message = "the published outputs are not sensitive"
  }
}

run "reference_toggles_independent_alert" {
  command = plan

  variables {
    budget_alert = {
      enabled           = true
      monthly_threshold = 50
      email             = "finops@example.org"
    }
    quota_guard = { enabled = false }
  }

  assert {
    condition     = output.budget_alert_id == "mock-alert-id-ref" && try(module.project_factory.prevent_automatic_quota_upgrade, "missing") == null
    error_message = "reference mode: alert on, quota guard off reach the component separately"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == jsonencode(["module.project_factory.module.project.ovh_cloud_project_alerting.this[0]"])
    error_message = "reference mode: unlabelled lists the alert only"
  }
}

run "reference_toggles_independent_quota" {
  command = plan

  variables {
    budget_alert = { enabled = false }
    quota_guard  = { enabled = true }
  }

  assert {
    condition     = output.budget_alert_id == null && try(module.project_factory.prevent_automatic_quota_upgrade, null) == true
    error_message = "reference mode: alert off, quota guard on reach the component separately"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == jsonencode(["module.project_factory.module.quota.ovh_cloud_quota.this[0]"])
    error_message = "reference mode: unlabelled lists the quota setting only"
  }
}

run "reference_bound_to_the_resolved_reference" {
  command = plan

  variables {
    project_id = "fedcba9876543210fedcba9876543210"
  }

  override_data {
    target = module.project_factory.module.project.data.ovh_cloud_project.this[0]
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
    condition     = output.project_id == "fedcba9876543210fedcba9876543210" && try(module.project_factory.project_id, null) == "fedcba9876543210fedcba9876543210"
    error_message = "the published id is the resolved reference and the id the component passes on, not a constant"
  }

  assert {
    condition     = output.project_urn == "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
    error_message = "the published URN follows the read project, not a constant or a fixed region part"
  }
}

run "reference_labels_follow_the_inputs" {
  command = plan

  variables {
    tenant      = "other"
    environment = "prod"
    instance    = "other-prod-project"
    managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/other/prod/project"
  }

  assert {
    condition     = output.tenant == "other" && output.environment == "prod"
    error_message = "tenant and environment follow the inputs in reference mode"
  }

  assert {
    condition = try(tomap(module.project_factory.labels), null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/other/prod/project"
      "lz:instance"   = "other-prod-project"
      "lz:tenant"     = "other"
      "lz:release"    = "unreleased"
    })
    error_message = "a referenced project carries the mandatory label set of the given scope"
  }
}
