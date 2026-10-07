# Project stage, adopt mode with both toggles on (spec 005 T027; FR-002, FR-004, FR-005; ADR-0004,
# ADR-0005, ADR-0006; research R7, R14, R24; data-model *Stage table*, *Per-stage values* `project`,
# *Resolved-reference input*; KD-3). Mocked provider, no credential, no API call.
#
# The stage composes its one components/project-factory call (`module.project_factory`, the only
# edge a stage has, ADR-0002). The stack imports the adopted project at
# module.project_factory.module.project.ovh_cloud_project.this[0] (R7, T037): the URN override sits
# on exactly that address, so a renamed, keyed or repeated call on the way leaves it unused and the
# mock default URN shows. The component's tests pin labels, toggles and the URN rule in detail; this
# file checks that the stage passes its inputs through and publishes exactly the `project` values.
# Reference mode: reference.tftest.hcl; toggles off: off.tftest.hcl. No backend or provider
# configuration and no resource in a stage: `task test:dependencies` (LIBRARY_BACKEND,
# LIBRARY_PROVIDER_CONFIG, STAGE_RESOURCE).
#
# Outputs (schemas/outputs/project.schema.json): `tenant`, `environment`, `project_id`,
# `project_urn`, `regions`, `budget_alert_id` (only when the alert is on; a null output is absent
# from `tofu output -json`) and `unlabelled`, all published, so none sensitive. KD-3: `project_id` is
# the resolved reference given to the stage and the id the component passes on; `project_urn` is the
# adopted project's own URN (mocked with a region part a URN built from the id would not have). That
# no other published output exists is not observable here: the envelope validator's
# `additionalProperties: false` refuses one (T018).
#
# Plan runs only (the adopted project carries `prevent_destroy`; T013 *Observed*).

mock_provider "ovh" {
  mock_resource "ovh_cloud_project" {
    defaults = {
      urn        = "urn:v1:eu:resource:publicCloudProject:mock-default-not-the-import-target"
      project_id = "mock-default-not-the-import-target"
    }
  }

  mock_resource "ovh_cloud_project_alerting" {
    defaults = {
      id = "mock-default-alert-id"
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

override_resource {
  target = module.project_factory.module.project.ovh_cloud_project_alerting.this[0]
  values = {
    id = "mock-alert-id-0123"
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
  budget_alert = {
    enabled           = true
    monthly_threshold = 100
    email             = "finops@example.org"
  }
  quota_guard = { enabled = true }
}

run "published_outputs_match_the_schema" {
  command = plan

  # Adopt-mode order arguments as the import plan would show them (R7). This run's plan is captured
  # (`task capture:project-plan`) and pinned in tools/internal/stacks (TestOutputsProjectStagePlan):
  # the order arguments reach the project unchanged (spec 005 T028, T027 gap 1).
  variables {
    project_ovh_subsidiary = "FR"
    project_description    = "demo-dev"
    project_plan = {
      duration     = "P1M"
      plan_code    = "project.2018"
      pricing_mode = "default"
    }
  }

  assert {
    condition     = output.tenant == "demo" && output.environment == "dev"
    error_message = "tenant and environment are the given ones"
  }

  assert {
    condition     = output.project_id == "0123456789abcdef0123456789abcdef"
    error_message = "project_id is the resolved reference given to the stage (KD-3)"
  }

  assert {
    condition     = output.project_urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef" && try(module.project_factory.project_urn, null) == output.project_urn
    error_message = "project_urn is the URN of the project adopted at module.project_factory.module.project.ovh_cloud_project.this[0] (one unkeyed call), not built from the id"
  }

  assert {
    condition     = jsonencode(output.regions) == jsonencode(["GRA11"])
    error_message = "regions are the given region names"
  }

  assert {
    condition     = output.budget_alert_id == "mock-alert-id-0123"
    error_message = "the enabled alert's id is published"
  }

  assert {
    condition = (length(output.unlabelled) == 2 && toset(output.unlabelled) == toset([
      "module.project_factory.module.project.ovh_cloud_project_alerting.this[0]",
      "module.project_factory.module.quota.ovh_cloud_quota.this[0]",
    ]))
    error_message = "unlabelled lists exactly the alert and the quota setting, once each, from the stage root"
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

run "project_bound_to_the_resolved_reference" {
  command = plan

  variables {
    project_id = "fedcba9876543210fedcba9876543210"
  }

  override_resource {
    target = module.project_factory.module.project.ovh_cloud_project.this[0]
    values = {
      urn        = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      project_id = "fedcba9876543210fedcba9876543210"
    }
  }

  assert {
    condition     = output.project_id == "fedcba9876543210fedcba9876543210" && try(module.project_factory.project_id, null) == "fedcba9876543210fedcba9876543210"
    error_message = "the published project id is the resolved reference and the id the component passes on, not a constant"
  }

  assert {
    condition     = output.project_urn == "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210" && try(module.project_factory.project_urn, null) == output.project_urn
    error_message = "the published URN is the component's, following the adopted project"
  }
}

run "labels_and_scope_follow_the_inputs" {
  command = plan

  variables {
    org         = "acme"
    tenant      = "other"
    environment = "prod"
    instance    = "other-prod-project"
    managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/other/prod/project"
  }

  assert {
    condition     = output.tenant == "other" && output.environment == "prod"
    error_message = "tenant and environment follow the inputs, not constants"
  }

  assert {
    condition = try(tomap(module.project_factory.labels), null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/other/prod/project"
      "lz:instance"   = "other-prod-project"
      "lz:tenant"     = "other"
      "lz:release"    = "unreleased"
    })
    error_message = "the project's labels are the mandatory set of the given tenant, instance and stack path"
  }
}

run "toggles_pass_through_independently_alert" {
  command = plan

  variables {
    quota_guard = { enabled = false }
  }

  assert {
    condition     = output.budget_alert_id == "mock-alert-id-0123" && try(module.project_factory.prevent_automatic_quota_upgrade, "missing") == null
    error_message = "budget_alert and quota_guard reach the component separately: alert on, guard off"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == jsonencode(["module.project_factory.module.project.ovh_cloud_project_alerting.this[0]"])
    error_message = "unlabelled lists the alert only"
  }
}

run "toggles_pass_through_independently_quota" {
  command = plan

  variables {
    budget_alert = { enabled = false }
  }

  assert {
    condition     = output.budget_alert_id == null && try(module.project_factory.prevent_automatic_quota_upgrade, null) == true
    error_message = "budget_alert and quota_guard reach the component separately: alert off, guard on"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == jsonencode(["module.project_factory.module.quota.ovh_cloud_quota.this[0]"])
    error_message = "unlabelled lists the quota setting only"
  }
}

run "regions_published_as_given" {
  command = plan

  variables {
    regions = ["SBG5", "GRA11"]
  }

  assert {
    condition     = jsonencode(output.regions) == jsonencode(["SBG5", "GRA11"])
    error_message = "regions are published as given (names and order), not a constant"
  }
}

run "blank_tenant_rejected" {
  command = plan

  variables {
    tenant = ""
  }

  expect_failures = [var.tenant]
}

run "blank_environment_rejected" {
  command = plan

  variables {
    environment = " "
  }

  expect_failures = [var.environment]
}

run "unknown_project_mode_rejected" {
  command = plan

  variables {
    project_mode = "order"
  }

  expect_failures = [var.project_mode]
}

run "blank_project_id_rejected" {
  command = plan

  variables {
    project_id = " "
  }

  expect_failures = [var.project_id]
}

run "blank_region_rejected" {
  command = plan

  variables {
    regions = ["GRA11", ""]
  }

  expect_failures = [var.regions]
}

run "duplicate_region_rejected" {
  command = plan

  variables {
    regions = ["GRA11", "GRA11"]
  }

  expect_failures = [var.regions]
}
