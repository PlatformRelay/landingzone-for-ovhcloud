# Cloud project module (spec 005 T025; FR-002, FR-004; ADR-0003, ADR-0005, ADR-0006; research R7,
# premise waiver R23: P5 is unqualified until T009, so both modes are tested offline). Mocked
# provider, no credential, no API call. Attribute names from the pinned ovh 2.21.0 schema
# (`ovh_cloud_project`, data `ovh_cloud_project`, `ovh_iam_resource_tags`,
# `ovh_cloud_project_alerting`).
#
# Plan runs only. In adopt mode the project carries a literal `prevent_destroy`: an apply run's
# cleanup destroy is refused and leaves errored_test.tfstate in the module directory (T013), and
# whether `prevent_destroy` is present is not observable from an assertion, so `lz-check deps`
# (rule RETAINED_UNPROTECTED, T026 adds this module to its list) pins it, not this file. The stack's
# `import` block is not here either: an import into a mocked `ovh` resource crashes `tofu test`
# (T007, P6); modules hold no import.
#
# The adopted project is retained infrastructure: the runs pin everything that could order a
# project (a project in reference mode, invented order arguments, a plan option),
# replace it (an address other than `ovh_cloud_project.this[0]`, changed order arguments) or let it
# be destroyed (`deletion_protection` other than a literal true). Address-sensitive assertions go
# through try() so a moved address fails the assertion instead of erroring.

mock_provider "ovh" {
  # The adopted project's computed URN and id are known on plan here, so the tags' target can be
  # compared; the referenced project's URN differs, so a cross-wired URN fails. Neither URN is
  # `urn:v1:eu:resource:publicCloudProject:<project_id>` (other region segments), so a module that
  # builds the URN from the id instead of reading it fails too (review r1).
  override_resource {
    target = ovh_cloud_project.this
    values = {
      project_id = "0123456789abcdef0123456789abcdef"
      urn        = "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    }
  }

  mock_data "ovh_cloud_project" {
    defaults = {
      project_id = "fedcba9876543210fedcba9876543210"
      iam = {
        display_name = "referenced"
        id           = "fedcba98-7654-3210-fedc-ba9876543210"
        tags         = {}
        urn          = "urn:v1:us:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      }
    }
  }
}

variables {
  mode           = "adopt"
  project_id     = "0123456789abcdef0123456789abcdef"
  ovh_subsidiary = "FR"
  description    = "sandbox"
  plan = {
    duration     = "P1M"
    plan_code    = "project.2018"
    pricing_mode = "default"
  }
  tags = {
    "lz:managed-by"  = "opentofu"
    "lz:managed-in"  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/demo/dev/project"
    "lz:instance"    = "demo-dev-project"
    "lz:tenant"      = "demo"
    "lz:environment" = "dev"
    "lz:release"     = "unreleased"
  }
}

# --- adopt mode -------------------------------------------------------------------------------

run "adopt_manages_the_project" {
  command = plan

  assert {
    condition     = length(ovh_cloud_project.this) == 1 && try(ovh_cloud_project.this[0].urn, null) != null
    error_message = "adopt mode manages exactly one ovh_cloud_project at ovh_cloud_project.this[0] (the stack's import target)"
  }

  assert {
    condition     = try(ovh_cloud_project.this[0].deletion_protection, null) == true
    error_message = "the adopted project has deletion_protection = true"
  }

  assert {
    condition     = length(data.ovh_cloud_project.this) == 0
    error_message = "adopt mode does not also read the project through the data source (one source of the URN)"
  }
}

# A caller cannot switch deletion protection off (a test variable the module does not declare is
# ignored, so a module that adds the input fails here).
run "deletion_protection_not_switchable" {
  command = plan

  variables {
    deletion_protection = false
  }

  assert {
    condition     = try(ovh_cloud_project.this[0].deletion_protection, null) == true
    error_message = "deletion_protection stays true whatever the caller passes"
  }
}

# The order arguments arrive exactly as given (the import plan's values, research R7): a changed
# subsidiary or plan would show the imported project as changed or replaced, and a plan option would order
# something.
run "adopt_order_arguments_as_given" {
  command = plan

  assert {
    condition     = try(ovh_cloud_project.this[0].ovh_subsidiary, null) == "FR"
    error_message = "ovh_subsidiary is the given one"
  }

  assert {
    condition     = try(ovh_cloud_project.this[0].description, null) == "sandbox"
    error_message = "description is the given one (the import plans no change to it)"
  }

  assert {
    condition = (
      try(length(ovh_cloud_project.this[0].plan), -1) == 1 &&
      try(ovh_cloud_project.this[0].plan[0].duration, null) == "P1M" &&
      try(ovh_cloud_project.this[0].plan[0].plan_code, null) == "project.2018" &&
      try(ovh_cloud_project.this[0].plan[0].pricing_mode, null) == "default" &&
      try(ovh_cloud_project.this[0].plan[0].catalog_name, "unset") == null &&
      try(length(ovh_cloud_project.this[0].plan[0].configuration), -1) == 0
    )
    error_message = "the plan block is exactly the given duration, plan_code and pricing_mode, with no catalog or configuration"
  }

  assert {
    condition     = try(length(ovh_cloud_project.this[0].plan_option), -1) == 0
    error_message = "no plan option (nothing further is ordered)"
  }
}

run "adopt_other_order_arguments" {
  command = plan

  variables {
    ovh_subsidiary = "DE"
    description    = "other"
    plan = {
      duration     = "P1Y"
      plan_code    = "project.2026"
      pricing_mode = "upfront12"
    }
  }

  assert {
    condition = (
      try(ovh_cloud_project.this[0].ovh_subsidiary, null) == "DE" &&
      try(ovh_cloud_project.this[0].description, null) == "other" &&
      try(ovh_cloud_project.this[0].plan[0].duration, null) == "P1Y" &&
      try(ovh_cloud_project.this[0].plan[0].plan_code, null) == "project.2026" &&
      try(ovh_cloud_project.this[0].plan[0].pricing_mode, null) == "upfront12"
    )
    error_message = "a second set of order arguments is applied as given (no constant)"
  }

  assert {
    condition     = try(ovh_cloud_project.this[0].deletion_protection, null) == true
    error_message = "deletion_protection stays true in this run too"
  }
}

# Without a plan the module sends none: it never invents order arguments.
run "adopt_no_invented_plan" {
  command = plan

  variables {
    plan = null
  }

  assert {
    condition     = length(ovh_cloud_project.this) == 1 && try(length(ovh_cloud_project.this[0].plan), -1) == 0
    error_message = "with plan = null the adopted project carries no plan block"
  }

  assert {
    condition     = try(length(ovh_cloud_project.this[0].plan_option), -1) == 0
    error_message = "with plan = null the adopted project carries no plan option"
  }

  assert {
    condition     = try(ovh_cloud_project.this[0].deletion_protection, null) == true
    error_message = "deletion_protection stays true in this run too"
  }
}

run "adopt_tags_the_project_urn" {
  command = plan

  assert {
    condition     = ovh_iam_resource_tags.this.urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "the tags target the adopted project's URN"
  }

  assert {
    condition = ovh_iam_resource_tags.this.tags == tomap({
      "lz:managed-by"  = "opentofu"
      "lz:managed-in"  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/demo/dev/project"
      "lz:instance"    = "demo-dev-project"
      "lz:tenant"      = "demo"
      "lz:environment" = "dev"
      "lz:release"     = "unreleased"
    })
    error_message = "the project URN carries exactly the given tags"
  }
}

run "adopt_outputs" {
  command = plan

  assert {
    condition     = output.project_id == "0123456789abcdef0123456789abcdef"
    error_message = "project_id output is the given project id"
  }

  assert {
    condition     = output.urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "urn output is the adopted project's URN"
  }
}

# --- reference mode ---------------------------------------------------------------------------

run "reference_manages_no_project" {
  command = plan

  variables {
    mode       = "reference"
    project_id = "fedcba9876543210fedcba9876543210"
  }

  assert {
    condition     = length(ovh_cloud_project.this) == 0
    error_message = "reference mode manages no ovh_cloud_project (a managed one without an import would order a project)"
  }

  assert {
    condition     = length(data.ovh_cloud_project.this) == 1 && try(data.ovh_cloud_project.this[0].service_name, null) == "fedcba9876543210fedcba9876543210"
    error_message = "reference mode reads the given project through data.ovh_cloud_project"
  }
}

run "reference_tags_the_project_urn" {
  command = plan

  variables {
    mode       = "reference"
    project_id = "fedcba9876543210fedcba9876543210"
    tags = {
      "lz:managed-by"  = "opentofu"
      "lz:managed-in"  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/demo/prod/project"
      "lz:instance"    = "demo-prod-project"
      "lz:tenant"      = "demo"
      "lz:environment" = "prod"
      "lz:release"     = "v0.1.0"
    }
  }

  assert {
    condition     = ovh_iam_resource_tags.this.urn == "urn:v1:us:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
    error_message = "the tags target the read project's URN (data source iam.urn)"
  }

  assert {
    condition = ovh_iam_resource_tags.this.tags == tomap({
      "lz:managed-by"  = "opentofu"
      "lz:managed-in"  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/demo/prod/project"
      "lz:instance"    = "demo-prod-project"
      "lz:tenant"      = "demo"
      "lz:environment" = "prod"
      "lz:release"     = "v0.1.0"
    })
    error_message = "another tag set is applied exactly: nothing added, nothing dropped"
  }
}

run "reference_outputs" {
  command = plan

  variables {
    mode       = "reference"
    project_id = "fedcba9876543210fedcba9876543210"
  }

  assert {
    condition     = output.project_id == "fedcba9876543210fedcba9876543210"
    error_message = "project_id output is the given project id"
  }

  assert {
    condition     = output.urn == "urn:v1:us:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
    error_message = "urn output is the read project's URN"
  }
}

# --- budget alert (optional, P10) --------------------------------------------------------------

run "alert_off_by_default" {
  command = plan

  assert {
    condition     = length(ovh_cloud_project_alerting.this) == 0
    error_message = "no budget alert unless enabled"
  }

  assert {
    condition     = ovh_iam_resource_tags.this.urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "the project is still tagged without an alert"
  }
}

run "alert_when_enabled" {
  command = plan

  variables {
    budget_alert = {
      enabled           = true
      monthly_threshold = 20
      email             = "platform@example.invalid"
      delay             = 86400
    }
  }

  assert {
    condition     = length(ovh_cloud_project_alerting.this) == 1
    error_message = "exactly one budget alert when enabled"
  }

  assert {
    condition = (
      try(ovh_cloud_project_alerting.this[0].service_name, null) == "0123456789abcdef0123456789abcdef" &&
      try(ovh_cloud_project_alerting.this[0].monthly_threshold, null) == 20 &&
      try(ovh_cloud_project_alerting.this[0].email, null) == "platform@example.invalid" &&
      try(ovh_cloud_project_alerting.this[0].delay, null) == 86400
    )
    error_message = "the alert is on the given project with the given threshold, email and delay"
  }

  assert {
    condition     = length(ovh_cloud_project.this) == 1 && try(ovh_cloud_project.this[0].deletion_protection, null) == true
    error_message = "with the alert on, the adopted project is still managed with deletion_protection = true"
  }
}

run "alert_default_delay_reference_mode" {
  command = plan

  variables {
    mode       = "reference"
    project_id = "fedcba9876543210fedcba9876543210"
    budget_alert = {
      enabled           = true
      monthly_threshold = 5
      email             = "ops@example.invalid"
    }
  }

  assert {
    condition = (
      length(ovh_cloud_project_alerting.this) == 1 &&
      try(ovh_cloud_project_alerting.this[0].service_name, null) == "fedcba9876543210fedcba9876543210" &&
      try(ovh_cloud_project_alerting.this[0].monthly_threshold, null) == 5 &&
      try(ovh_cloud_project_alerting.this[0].email, null) == "ops@example.invalid" &&
      try(ovh_cloud_project_alerting.this[0].delay, null) == 3600
    )
    error_message = "in reference mode the alert is on the given project; delay defaults to 3600 s"
  }
}

run "alert_disabled_explicitly" {
  command = plan

  variables {
    budget_alert = {
      enabled           = false
      monthly_threshold = 20
      email             = "platform@example.invalid"
    }
  }

  assert {
    condition     = length(ovh_cloud_project_alerting.this) == 0 && ovh_iam_resource_tags.this.urn == "urn:v1:ca:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "a disabled alert with values set creates no alert"
  }
}

# --- rejections (last: an expected failure ends the run) ----------------------------------------

run "unknown_mode_rejected" {
  command = plan

  variables {
    mode = "create"
  }

  expect_failures = [var.mode]
}

run "empty_project_id_rejected" {
  command = plan

  variables {
    project_id = " "
  }

  expect_failures = [var.project_id]
}

run "tags_null_value_rejected" {
  command = plan

  variables {
    tags = {
      "lz:managed-by" = "opentofu"
      "lz:tenant"     = null
    }
  }

  expect_failures = [var.tags]
}

# A blank email passes the provider's schema; the module refuses it (a missing one too).
run "alert_blank_email_rejected" {
  command = plan

  variables {
    budget_alert = {
      enabled           = true
      monthly_threshold = 20
      email             = " "
    }
  }

  expect_failures = [var.budget_alert]
}

run "alert_without_threshold_rejected" {
  command = plan

  variables {
    budget_alert = {
      enabled = true
      email   = "platform@example.invalid"
    }
  }

  expect_failures = [var.budget_alert]
}

run "alert_zero_threshold_rejected" {
  command = plan

  variables {
    budget_alert = {
      enabled           = true
      monthly_threshold = 0
      email             = "platform@example.invalid"
    }
  }

  expect_failures = [var.budget_alert]
}

run "alert_delay_outside_enum_rejected" {
  command = plan

  variables {
    budget_alert = {
      enabled           = true
      monthly_threshold = 20
      email             = "platform@example.invalid"
      delay             = 60
    }
  }

  expect_failures = [var.budget_alert]
}
