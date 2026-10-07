# Runtime stage (spec 005 T031; FR-004, FR-005; ADR-0004, ADR-0017; research R8, R22; data-model
# *Stage table*, *Per-stage values* `runtime`, *Envelope-to-input adapter*). Mocked provider, no
# credential, no API call.
#
# The stage composes its one components/runtime/managed-only call (`module.runtime`, the only edge a
# stage has, ADR-0002) and consumes only the `project` stage's published values (`var.project`, the
# data edge; KD-3 is checked by the adapter before the plan; whether the published
# `scope.project_id` equals the bound reference is T060's check, T018 decision 2). Every run sets
# only `org`, `region`, `instance`, `managed_in`, `slot` and `project`: a stage that required any
# other input (a `project_network` value, say) fails every run. The component's outputs are read as
# `module.runtime.<output>`, so a renamed or keyed call fails; the component's tests pin the bucket
# and the naming in detail, this file what the stage passes on and that it publishes exactly the
# `runtime` values.
#
# Outputs (schemas/outputs/runtime.schema.json): `kind`, `slot` (null, so absent from the published
# values, when unset), `scope`, `readiness`, `pending_actions`, `capabilities`, `unlabelled`, none
# sensitive. That no other published output exists is not observable here: the envelope
# validator's `additionalProperties: false` refuses one (T018), and T032's plan pin compares the
# output names with the schema's (`planOutputContract`, T086).
#
# Tripwires (non-computed mock defaults, observed tofu 1.13.0, T023): no network, subnet, gateway
# or S3 user planned anywhere below the stage.

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
  org        = "lz"
  region     = "GRA11"
  instance   = "demo-dev-gra11-runtime"
  managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
  project = {
    tenant      = "demo"
    environment = "dev"
    project_id  = "0123456789abcdef0123456789abcdef"
    project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    regions     = ["GRA11"]
    unlabelled  = []
  }
}

run "published_outputs_match_the_schema" {
  command = plan

  assert {
    condition     = output.kind == "managed-only" && try(module.runtime.kind, null) == output.kind
    error_message = "kind is managed-only, read from module.runtime"
  }

  assert {
    condition     = output.slot == null
    error_message = "no slot is published when none is set"
  }

  assert {
    condition = jsonencode(output.scope) == jsonencode({
      instance   = "demo-dev-gra11-runtime"
      project_id = "0123456789abcdef0123456789abcdef"
      region     = "GRA11"
    }) && jsonencode(try(module.runtime.scope, null)) == jsonencode(output.scope)
    error_message = "scope is the instance, the project id of the `project` values and the instance region"
  }

  assert {
    condition     = output.readiness == "ready" && try(module.runtime.readiness, null) == output.readiness
    error_message = "readiness is ready"
  }

  assert {
    condition     = jsonencode(output.pending_actions) == "[]" && jsonencode(try(module.runtime.pending_actions, null)) == jsonencode(output.pending_actions)
    error_message = "pending_actions is an empty list, read from module.runtime"
  }

  assert {
    condition = jsonencode(output.capabilities) == jsonencode({
      "object-storage" = {
        bucket   = "lz-demo-dev-gra11-bkt-runtime"
        endpoint = "https://s3.gra.io.cloud.ovh.net"
        region   = "GRA"
      }
    }) && jsonencode(try(module.runtime.capabilities, null)) == jsonencode(output.capabilities)
    error_message = "capabilities hold exactly object-storage {bucket, endpoint, region}, read from module.runtime"
  }

  assert {
    condition     = !contains(try(keys(output.capabilities), ["network"]), "network")
    error_message = "a managed-only runtime publishes no network capability, not even an empty one (ADR-0017)"
  }

  assert {
    condition     = jsonencode(output.unlabelled) == "[]" && jsonencode(try(module.runtime.unlabelled, null)) == "[]"
    error_message = "unlabelled is an empty list, and so is the component's: the bucket carries tags"
  }

  assert {
    condition = !anytrue([
      issensitive(output.kind), issensitive(output.slot), issensitive(output.scope), issensitive(output.readiness),
      issensitive(output.pending_actions), issensitive(output.capabilities), issensitive(output.unlabelled),
    ])
    error_message = "the published outputs are not sensitive"
  }
}

run "bucket_labelled_from_the_project_values" {
  command = plan

  assert {
    condition = try(module.runtime.labels, null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
      "lz:instance"   = "demo-dev-gra11-runtime"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the bucket carries the mandatory labels: the project's tenant, the instance and managed_in"
  }
}

run "slot_blue_names_the_bucket" {
  command = plan

  variables {
    instance   = "demo-dev-gra11-runtime-blue"
    managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime-blue"
    slot       = "blue"
  }

  assert {
    condition     = try(output.capabilities["object-storage"].bucket, null) == "lz-demo-dev-gra11-bkt-runtime-blue"
    error_message = "the instance's slot reaches the bucket's name"
  }

  assert {
    condition     = output.slot == "blue" && try(module.runtime.slot, null) == "blue"
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
    instance   = "demo-dev-gra11-runtime-green"
    managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime-green"
    slot       = "green"
  }

  assert {
    condition     = try(output.capabilities["object-storage"].bucket, null) == "lz-demo-dev-gra11-bkt-runtime-green"
    error_message = "another slot gives another bucket name"
  }

  assert {
    condition     = output.slot == "green"
    error_message = "the slot is published when set"
  }
}

run "every_input_reaches_the_component" {
  command = plan

  variables {
    org        = "ex"
    region     = "SBG5"
    instance   = "acme-prod-sbg5-runtime"
    managed_in = "example.org/forge//stacks/tenants/acme/prod/sbg5/runtime"
    project = {
      tenant          = "acme"
      environment     = "prod"
      project_id      = "fedcba9876543210fedcba9876543210"
      project_urn     = "urn:v1:ca:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      regions         = ["GRA11", "SBG5"]
      budget_alert_id = "mock-alert-id-0123"
      unlabelled      = ["module.project_factory.module.project.ovh_cloud_project_alerting.this[0]"]
    }
  }

  assert {
    condition = jsonencode(output.capabilities) == jsonencode({
      "object-storage" = {
        bucket   = "ex-acme-prod-sbg5-bkt-runtime"
        endpoint = "https://s3.sbg.io.cloud.ovh.net"
        region   = "SBG"
      }
    })
    error_message = "the bucket follows org, the project's tenant and environment, and the instance region (the second of the project's regions)"
  }

  assert {
    condition = jsonencode(output.scope) == jsonencode({
      instance   = "acme-prod-sbg5-runtime"
      project_id = "fedcba9876543210fedcba9876543210"
      region     = "SBG5"
    })
    error_message = "scope follows the instance, the project id of the `project` values and the region"
  }

  assert {
    condition = try(module.runtime.labels, null) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "example.org/forge//stacks/tenants/acme/prod/sbg5/runtime"
      "lz:instance"   = "acme-prod-sbg5-runtime"
      "lz:tenant"     = "acme"
      "lz:release"    = "unreleased"
    })
    error_message = "the labels follow the project's tenant, the instance and managed_in"
  }
}

# Refusals, last. Each input fails only the rule under test.

run "region_outside_the_project_regions_rejected" {
  command = plan

  variables {
    region = "SBG5"
  }

  expect_failures = [var.region]
}

run "region_differing_only_in_case_rejected" {
  command = plan

  variables {
    region = "gra11"
  }

  expect_failures = [var.region]
}

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
