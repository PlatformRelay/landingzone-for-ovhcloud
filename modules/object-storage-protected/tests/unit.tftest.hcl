# Protected bucket module for state buckets (spec 005 T013; FR-002, FR-004, FR-013; ADR-0003,
# ADR-0008, ADR-0009; research R5 *Protection*). Mocked provider, no credential, no API call.
# Attribute names from the pinned ovh 2.21.0 schema (`ovh_cloud_project_storage`).
# The name and the tags are modules/naming outputs for the tenant `demo` state bucket
# (data-model *Derived names*: `lz-demo-bkt-state`).
#
# Plan runs only. The module carries a literal `prevent_destroy`: `tofu test` destroys what an apply
# run created, that destroy is refused, and the run still reports pass while it leaves
# errored_test.tfstate in the module directory (observed, tofu 1.10.3). Whether `prevent_destroy` is
# present is not observable from an assertion (a refused replacement is an error, not a checkable
# failure), so the lifecycle scan of T026 (guard G7, code part) pins it, not this file.

mock_provider "ovh" {}

variables {
  project_id = "0123456789abcdef0123456789abcdef"
  region     = "GRA"
  name       = "lz-demo-bkt-state"
  tags = {
    "lz:managed-by" = "opentofu"
    "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
    "lz:instance"   = "demo-tenant-state"
    "lz:tenant"     = "demo"
    "lz:release"    = "unreleased"
  }
}

run "bucket_given_name_region_project" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.this.name == "lz-demo-bkt-state"
    error_message = "the bucket carries the given name, unchanged"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.region_name == "GRA"
    error_message = "the bucket is in the given region"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "the bucket is in the given project"
  }
}

run "bucket_account_state" {
  command = plan

  variables {
    name   = "lz-bkt-state"
    region = "SBG"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.name == "lz-bkt-state" && ovh_cloud_project_storage.this.region_name == "SBG"
    error_message = "another name and region are applied as given (no constant)"
  }
}

run "tags_exactly_given" {
  command = plan

  assert {
    condition = ovh_cloud_project_storage.this.tags == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
      "lz:instance"   = "demo-tenant-state"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the bucket carries exactly the given tags"
  }
}

# Account scope: no `lz:tenant` (naming omits it); the module must not add one.
run "tags_account_scope_exactly_given" {
  command = plan

  variables {
    name = "lz-bkt-state"
    tags = {
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
      "lz:instance"   = "account-bootstrap"
      "lz:release"    = "unreleased"
    }
  }

  assert {
    condition = ovh_cloud_project_storage.this.tags == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
      "lz:instance"   = "account-bootstrap"
      "lz:release"    = "unreleased"
    })
    error_message = "another tag set is applied exactly: nothing added, nothing dropped"
  }
}

run "versioning_always_enabled" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.this.versioning.status == "enabled"
    error_message = "a state bucket is always versioned"
  }
}

# `versioning` is not an input of this module; a run setting it must not switch versioning off
# (a test variable the module does not declare is ignored, so a module that adds the input fails).
run "versioning_not_switchable" {
  command = plan

  variables {
    versioning = false
  }

  assert {
    condition     = ovh_cloud_project_storage.this.versioning.status == "enabled"
    error_message = "versioning stays enabled whatever the caller passes"
  }
}

run "outputs" {
  command = plan

  assert {
    condition     = output.name == "lz-demo-bkt-state"
    error_message = "output name is the bucket name"
  }

  assert {
    condition     = output.region == "GRA"
    error_message = "output region is the bucket region"
  }
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
