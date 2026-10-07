# Runtime/managed-only component (spec 005 T031; FR-004, FR-005; ADR-0004, ADR-0017; research R8,
# R14, R15, R22; data-model *Derived instance fields* `bucket name (runtime)`, *Per-stage values*
# `runtime`, *Label set*). Mocked provider, no credential, no API call.
#
# The component names one bucket through modules/naming (kind `bucket`, role `runtime`, the
# instance's `slot`) and creates it through the unprotected modules/object-storage, called once,
# unkeyed, at `module.bucket`; it publishes the ADR-0017 envelope (`kind = managed-only`, `slot`,
# `scope`, `readiness`, `pending_actions`, `capabilities`, `unlabelled`) plus `labels`. A
# `tofu test` assertion reads only a child module's outputs, never its resources (T027 *Observed*):
# the bucket's name, region, project and tags are read as `module.bucket.<output>` (T032 adds the
# `project_id` and `tags` outputs to modules/object-storage), so a renamed or keyed call fails. That
# the bucket is the unprotected, replaceable one is pinned in replaceable.tftest.hcl.
#
# Region: the instance region (`GRA11`) is the naming segment and `scope.region`; the bucket's
# Object Storage region is its leading letters (`GRA`, the provider's storage examples,
# cloud_project_storage.md:23; the mapping is UNVERIFIED until T010), the endpoint
# `https://s3.<lower storage region>.io.cloud.ovh.net` (as components/state-backend).
#
# Tripwires (a mock default for a non-computed field fails the run if that type is planned anywhere
# below the component; observed tofu 1.13.0, T023): no network (`ovh_cloud_project_network_private`
# `name`, `_subnet` `network`, `_subnet_v2` `name`), no gateway (`model`), no S3 user
# (`ovh_cloud_project_user` `description`).

mock_provider "ovh" {
  mock_resource "ovh_cloud_project_network_private" {
    defaults = {
      name = "tripwire-no-network"
    }
  }

  mock_resource "ovh_cloud_project_network_private_subnet" {
    defaults = {
      network = "tripwire-no-subnet"
    }
  }

  mock_resource "ovh_cloud_project_network_private_subnet_v2" {
    defaults = {
      name = "tripwire-no-subnet"
    }
  }

  mock_resource "ovh_cloud_project_gateway" {
    defaults = {
      model = "tripwire-no-gateway"
    }
  }

  mock_resource "ovh_cloud_project_user" {
    defaults = {
      description = "tripwire-no-s3-user"
    }
  }
}

variables {
  org         = "lz"
  tenant      = "demo"
  environment = "dev"
  region      = "GRA11"
  instance    = "demo-dev-gra11-runtime"
  managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
  project_id  = "0123456789abcdef0123456789abcdef"
}

run "one_bucket_named_by_naming_without_slot" {
  command = plan

  assert {
    condition     = try(module.bucket.name, null) == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "the bucket at module.bucket is named by modules/naming: org, tenant, environment, region, kind bucket, role runtime, no slot"
  }

  assert {
    condition     = try(output.capabilities["object-storage"].bucket, null) == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "the object-storage capability names the bucket"
  }

  assert {
    condition     = output.slot == null
    error_message = "no slot is published when none is set (absent, never a placeholder)"
  }
}

run "bucket_in_the_project_and_storage_region" {
  command = plan

  assert {
    condition     = try(module.bucket.project_id, null) == "0123456789abcdef0123456789abcdef"
    error_message = "the bucket belongs to the given project (read back from modules/object-storage)"
  }

  assert {
    condition     = try(module.bucket.region, null) == "GRA"
    error_message = "the bucket's Object Storage region is the instance region's leading letters"
  }
}

run "bucket_labelled_with_the_mandatory_set" {
  command = plan

  assert {
    condition = try(module.bucket.tags, null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
      "lz:instance"   = "demo-dev-gra11-runtime"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the bucket carries exactly the mandatory labels of the instance (modules/naming), lz:tenant included"
  }

  assert {
    condition     = output.labels == try(module.bucket.tags, null)
    error_message = "labels are the tags the bucket carries"
  }
}

run "envelope_is_managed_only" {
  command = plan

  assert {
    condition     = output.kind == "managed-only"
    error_message = "kind is managed-only"
  }

  assert {
    condition = jsonencode(output.scope) == jsonencode({
      instance   = "demo-dev-gra11-runtime"
      project_id = "0123456789abcdef0123456789abcdef"
      region     = "GRA11"
    })
    error_message = "scope is exactly the instance, the project and the instance region"
  }

  assert {
    condition     = output.readiness == "ready"
    error_message = "readiness is ready: a managed-only bucket needs no further step"
  }

  assert {
    condition     = jsonencode(output.pending_actions) == "[]"
    error_message = "pending_actions is an empty list"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == "[]"
    error_message = "unlabelled is an empty list: the bucket carries tags (R14)"
  }
}

run "capabilities_object_storage_and_no_network" {
  command = plan

  assert {
    condition = jsonencode(output.capabilities) == jsonencode({
      "object-storage" = {
        bucket   = "lz-demo-dev-gra11-bkt-runtime"
        endpoint = "https://s3.gra.io.cloud.ovh.net"
        region   = "GRA"
      }
    })
    error_message = "capabilities hold exactly object-storage {bucket, endpoint, region}"
  }

  assert {
    condition     = !contains(try(keys(output.capabilities), ["network"]), "network")
    error_message = "a managed-only runtime publishes no network capability, not even an empty one (ADR-0017)"
  }

  assert {
    condition     = try(output.capabilities["object-storage"].region, null) == try(module.bucket.region, "") && try(output.capabilities["object-storage"].bucket, null) == try(module.bucket.name, "")
    error_message = "the capability is read from the bucket at module.bucket"
  }
}

run "slot_blue_names_the_bucket" {
  command = plan

  variables {
    instance = "demo-dev-gra11-runtime-blue"
    slot     = "blue"
  }

  assert {
    condition     = try(module.bucket.name, null) == "lz-demo-dev-gra11-bkt-runtime-blue"
    error_message = "the slot is the last naming segment of the bucket's name"
  }

  assert {
    condition     = try(output.capabilities["object-storage"].bucket, null) == "lz-demo-dev-gra11-bkt-runtime-blue"
    error_message = "the capability names the slotted bucket"
  }

  assert {
    condition     = output.slot == "blue"
    error_message = "the slot is published when set"
  }

  assert {
    condition     = try(output.scope.instance, null) == "demo-dev-gra11-runtime-blue"
    error_message = "scope.instance is the slotted instance"
  }
}

run "slot_green_names_another_bucket" {
  command = plan

  variables {
    instance = "demo-dev-gra11-runtime-green"
    slot     = "green"
  }

  assert {
    condition     = try(module.bucket.name, null) == "lz-demo-dev-gra11-bkt-runtime-green"
    error_message = "another slot gives another bucket name"
  }

  assert {
    condition     = output.slot == "green"
    error_message = "the slot is published when set"
  }
}

run "every_input_reaches_the_bucket_and_the_envelope" {
  command = plan

  variables {
    org         = "ex"
    tenant      = "acme"
    environment = "prod"
    region      = "SBG5"
    instance    = "acme-prod-sbg5-runtime"
    managed_in  = "example.org/forge//stacks/tenants/acme/prod/sbg5/runtime"
    project_id  = "fedcba9876543210fedcba9876543210"
  }

  assert {
    condition     = try(module.bucket.name, null) == "ex-acme-prod-sbg5-bkt-runtime"
    error_message = "the name follows org, tenant, environment and region"
  }

  assert {
    condition     = try(module.bucket.project_id, null) == "fedcba9876543210fedcba9876543210" && try(output.scope.project_id, null) == "fedcba9876543210fedcba9876543210"
    error_message = "the bucket and scope follow the given project"
  }

  assert {
    condition     = try(module.bucket.region, null) == "SBG" && try(output.scope.region, null) == "SBG5"
    error_message = "the storage region and scope.region follow the instance region"
  }

  assert {
    condition = jsonencode(output.capabilities) == jsonencode({
      "object-storage" = {
        bucket   = "ex-acme-prod-sbg5-bkt-runtime"
        endpoint = "https://s3.sbg.io.cloud.ovh.net"
        region   = "SBG"
      }
    })
    error_message = "the capability follows the bucket, its region and its endpoint"
  }

  assert {
    condition = try(module.bucket.tags, null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "example.org/forge//stacks/tenants/acme/prod/sbg5/runtime"
      "lz:instance"   = "acme-prod-sbg5-runtime"
      "lz:tenant"     = "acme"
      "lz:release"    = "unreleased"
    })
    error_message = "the labels follow tenant, instance and managed_in"
  }

  assert {
    condition     = try(output.scope.instance, null) == "acme-prod-sbg5-runtime"
    error_message = "scope.instance follows the instance"
  }
}

# Refusals, last: the slot rule (data-model *Deployment manifest* `slot`, runtime.schema.json). Each
# value fails only that rule: modules/naming lower-cases, admits 17 characters within the bucket
# limit, a leading digit and a single hyphen.

run "slot_with_upper_case_rejected" {
  command = plan

  variables {
    slot = "Blue"
  }

  expect_failures = [var.slot]
}

run "slot_longer_than_16_rejected" {
  command = plan

  variables {
    slot = "abcdefghijklmnopq"
  }

  expect_failures = [var.slot]
}

run "slot_with_leading_digit_rejected" {
  command = plan

  variables {
    slot = "1blue"
  }

  expect_failures = [var.slot]
}

run "slot_with_hyphen_rejected" {
  command = plan

  variables {
    slot = "a-b"
  }

  expect_failures = [var.slot]
}
