# Project-factory component, adopt mode with both toggles on (spec 005 T027; FR-002, FR-004, FR-005;
# ADR-0004, ADR-0005, ADR-0006; research R7, R14, R24; data-model *Per-stage values* `project`,
# *Label set*, *Resolved-reference input*; KD-3). Mocked provider, no credential, no API call.
#
# One existing project through one unrepeated modules/cloud-project call at `module.project`: the
# stack imports the adopted project at module.project.ovh_cloud_project.this[0] (T025, T037), so the
# URN override below sits on exactly that address; a renamed, keyed or repeated call would leave the
# override unused and the mock default URN (`…mock-default-not-the-import-target`) would show. The
# project's retained protection (`prevent_destroy`, `deletion_protection`, one project address) is
# modules/cloud-project's, pinned by its own tests and `task test:dependencies`
# (RETAINED_UNPROTECTED). Reference mode: reference.tftest.hcl; toggles off: off.tftest.hcl.
#
# KD-3: the published project id is the bound reference given as `project_id`, the same id the
# module manages or reads; the URN is the one the provider reports for that project (mocked with a
# `ca` or another region part, so a URN built from the id fails), and a URN naming another project is
# refused at `output.project_urn`.
#
# Labels: exactly the mandatory label set of data-model *Label set* (FR-002): `lz:tenant` from the
# `tenant` input only (G4), no environment key (the set has none); `lz:run-id` is the live lane's.
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
  target = module.project.ovh_cloud_project.this[0]
  values = {
    urn        = "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    project_id = "0123456789abcdef0123456789abcdef"
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
  mode        = "adopt"
  project_id  = "0123456789abcdef0123456789abcdef"
  budget_alert = {
    enabled           = true
    monthly_threshold = 100
    email             = "finops@example.org"
  }
  quota_guard = { enabled = true }
}

run "adopted_project_bound_to_the_given_id" {
  command = plan

  assert {
    condition     = output.project_id == "0123456789abcdef0123456789abcdef"
    error_message = "project_id is the given (bound) project id"
  }

  assert {
    condition     = try(module.project.project_id, null) == "0123456789abcdef0123456789abcdef"
    error_message = "the given (bound) project id is passed to modules/cloud-project, not another"
  }

  assert {
    condition     = output.project_urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "project_urn is the URN of the project adopted at module.project.ovh_cloud_project.this[0], read from it, not built from the id"
  }

  assert {
    condition     = try(module.project.urn, null) == output.project_urn
    error_message = "project_urn is modules/cloud-project's URN"
  }
}

run "project_follows_the_bound_reference" {
  command = plan

  variables {
    project_id = "fedcba9876543210fedcba9876543210"
  }

  override_resource {
    target = module.project.ovh_cloud_project.this[0]
    values = {
      urn        = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      project_id = "fedcba9876543210fedcba9876543210"
    }
  }

  assert {
    condition     = output.project_id == "fedcba9876543210fedcba9876543210" && try(module.project.project_id, null) == "fedcba9876543210fedcba9876543210"
    error_message = "the project id follows the bound reference, not a constant"
  }

  assert {
    condition     = output.project_urn == "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
    error_message = "the URN follows the adopted project, not a constant or a fixed region part"
  }
}

run "labels_are_the_mandatory_set_of_the_scope" {
  command = plan

  assert {
    condition = try(tomap(output.labels), null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/project"
      "lz:instance"   = "demo-dev-project"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the project carries exactly the mandatory label set of its scope (FR-002)"
  }
}

run "labels_follow_the_inputs" {
  command = plan

  variables {
    tenant      = "other"
    environment = "prod"
    instance    = "other-prod-project"
    managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/other/prod/project"
  }

  assert {
    condition = try(tomap(output.labels), null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/other/prod/project"
      "lz:instance"   = "other-prod-project"
      "lz:tenant"     = "other"
      "lz:release"    = "unreleased"
    })
    error_message = "lz:tenant, lz:instance and lz:managed-in follow the inputs, not constants"
  }
}

run "budget_alert_and_quota_guard_pass_through" {
  command = plan

  assert {
    condition     = output.budget_alert_id == "mock-alert-id-0123"
    error_message = "an enabled budget alert is planned at module.project.ovh_cloud_project_alerting.this[0] and its id is published"
  }

  assert {
    condition     = output.prevent_automatic_quota_upgrade == true
    error_message = "an enabled quota guard reaches modules/cloud-quota (flag true)"
  }

  assert {
    condition = (length(output.unlabelled) == 2 && toset(output.unlabelled) == toset([
      "module.project.ovh_cloud_project_alerting.this[0]",
      "module.quota.ovh_cloud_quota.this[0]",
    ]))
    error_message = "unlabelled lists exactly the alert and the quota setting, once each (no tags in their API, research R14)"
  }
}

run "alert_on_quota_off" {
  command = plan

  variables {
    quota_guard = { enabled = false }
  }

  assert {
    condition     = output.budget_alert_id == "mock-alert-id-0123" && output.prevent_automatic_quota_upgrade == null
    error_message = "the toggles are independent: alert on, quota guard off"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == jsonencode(["module.project.ovh_cloud_project_alerting.this[0]"])
    error_message = "unlabelled lists the alert only"
  }
}

run "quota_on_alert_off" {
  command = plan

  variables {
    budget_alert = { enabled = false }
  }

  assert {
    condition     = output.budget_alert_id == null && output.prevent_automatic_quota_upgrade == true
    error_message = "the toggles are independent: alert off, quota guard on"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == jsonencode(["module.quota.ovh_cloud_quota.this[0]"])
    error_message = "unlabelled lists the quota setting only"
  }
}

run "urn_of_another_project_refused" {
  command = plan

  override_resource {
    target = module.project.ovh_cloud_project.this[0]
    values = {
      urn        = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      project_id = "fedcba9876543210fedcba9876543210"
    }
  }

  expect_failures = [output.project_urn]
}

run "blank_tenant_rejected" {
  command = plan

  variables {
    tenant = " "
  }

  expect_failures = [var.tenant]
}

run "blank_environment_rejected" {
  command = plan

  variables {
    environment = ""
  }

  expect_failures = [var.environment]
}
