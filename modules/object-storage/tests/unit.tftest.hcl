# Plain bucket module (spec 005 T013; FR-002, FR-004, FR-013; ADR-0003, ADR-0008, ADR-0009).
# Mocked provider, no credential, no API call. Attribute names from the pinned ovh 2.21.0 schema
# (`ovh_cloud_project_storage`: service_name, region_name, name, tags, versioning.status).
# The name and the tags are modules/naming outputs for a runtime bucket (tenant `demo`, `dev`, `GRA11`),
# copied from its contract tests; this module applies them unchanged.
# This module is the runtime's (ephemeral, data-model *Stack set*): it carries no prevent_destroy,
# so a replacement plans; state buckets use modules/object-storage-protected.

mock_provider "ovh" {}

variables {
  project_id = "0123456789abcdef0123456789abcdef"
  region     = "GRA"
  name       = "lz-demo-dev-gra11-bkt-runtime"
  tags = {
    "lz:managed-by" = "opentofu"
    "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
    "lz:instance"   = "demo-dev-gra11-runtime"
    "lz:tenant"     = "demo"
    "lz:release"    = "unreleased"
  }
}

run "bucket_given_name_region_project" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.this.name == "lz-demo-dev-gra11-bkt-runtime"
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

run "bucket_other_name_slot" {
  command = plan

  variables {
    name   = "lz-demo-dev-gra11-bkt-runtime-blue"
    region = "SBG"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.name == "lz-demo-dev-gra11-bkt-runtime-blue" && ovh_cloud_project_storage.this.region_name == "SBG"
    error_message = "another name and region are applied as given (no constant)"
  }
}

run "tags_exactly_given" {
  command = plan

  assert {
    condition = ovh_cloud_project_storage.this.tags == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
      "lz:instance"   = "demo-dev-gra11-runtime"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the bucket carries exactly the given tags"
  }
}

run "tags_other_set_exactly_given" {
  command = plan

  variables {
    tags = {
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
      "lz:instance"   = "demo-dev-gra11-runtime-blue"
      "lz:release"    = "unreleased"
      "cost-centre"   = "4711"
    }
  }

  assert {
    condition = ovh_cloud_project_storage.this.tags == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
      "lz:instance"   = "demo-dev-gra11-runtime-blue"
      "lz:release"    = "unreleased"
      "cost-centre"   = "4711"
    })
    error_message = "another tag set is applied exactly: nothing added, nothing dropped"
  }
}

run "versioning_requested" {
  command = plan

  variables {
    versioning = true
  }

  assert {
    condition     = ovh_cloud_project_storage.this.versioning.status == "enabled"
    error_message = "versioning = true enables versioning"
  }
}

# The provider computes `versioning` when it is not configured, so this run applies against the
# mock: not requested must not end up "enabled". The name assertion makes the run fail on a stub.
run "versioning_not_requested" {
  command = apply

  assert {
    condition     = ovh_cloud_project_storage.this.versioning.status != "enabled"
    error_message = "versioning is not enabled when not requested"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.name == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "the bucket carries the given name"
  }
}

# Follows an apply run, so the bucket is in state: a plain bucket can be replaced (no prevent_destroy).
run "replacement_plans" {
  command = plan

  plan_options {
    replace = [ovh_cloud_project_storage.this]
  }

  assert {
    condition     = ovh_cloud_project_storage.this.name == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "the replacement plans with the given name"
  }
}

run "outputs" {
  command = plan

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "output name is the bucket name"
  }

  assert {
    condition     = output.region == "GRA"
    error_message = "output region is the bucket region"
  }
}

# Spec 005 T032 (T031 interface): the runtime component publishes the bucket's tags as its labels
# and its project as `scope.project_id` from these outputs, so they are the bucket's own attributes.
run "outputs_tags_and_project_of_the_bucket" {
  command = plan

  assert {
    condition     = try(output.tags, null) == ovh_cloud_project_storage.this.tags && try(output.tags, null) == tomap(var.tags)
    error_message = "output tags are the bucket's tags"
  }

  assert {
    condition     = try(output.project_id, null) == ovh_cloud_project_storage.this.service_name && try(output.project_id, null) == "0123456789abcdef0123456789abcdef"
    error_message = "output project_id is the bucket's project"
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
