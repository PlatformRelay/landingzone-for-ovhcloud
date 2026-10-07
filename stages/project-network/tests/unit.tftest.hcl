# Project-network stage (spec 005 T029; FR-002, FR-004, FR-005; ADR-0004, ADR-0017; research R9,
# R14; data-model *Stage table*, *Per-stage values* `project-network`, *Envelope-to-input adapter*;
# P12 UNVERIFIED until T010). Mocked provider, no credential, no API call.
#
# The stage composes its one components/network/island call (`module.island`, the only edge a stage
# has, ADR-0002) and consumes only the `project` stage's published values (`var.project`, the data
# edge; KD-3 is checked by the adapter before the plan). The overrides sit on the unkeyed
# `module.island.module.network` resources and the component's outputs are read as
# `module.island.<output>`, so a renamed or keyed call fails. The component's and the module's
# tests pin DHCP, no gateway and the naming in detail; this file checks what the stage passes on and
# that it publishes exactly the `project-network` values.
#
# Outputs (schemas/outputs/project-network.schema.json): `network_id`, `regions_openstack_ids`,
# `subnet_id`, `cidr`, `unlabelled`, all published, so none sensitive. That no other published
# output exists is not observable here: the envelope validator's `additionalProperties: false`
# refuses one (T018).
#
# Tripwires: `ovh_cloud_project_gateway` (`model`) and `ovh_cloud_project_network_private_subnet_v2`
# (`name`) fail the run if planned anywhere below the stage (observed tofu 1.13.0, T023).

mock_provider "ovh" {
  mock_resource "ovh_cloud_project_network_private" {
    defaults = {
      id                    = "mock-default-network-id"
      regions_openstack_ids = { MOCK = "mock-default-openstack-id" }
    }
  }

  mock_resource "ovh_cloud_project_network_private_subnet" {
    defaults = {
      id   = "mock-default-subnet-id"
      cidr = "198.51.100.0/24"
    }
  }

  mock_resource "ovh_cloud_project_gateway" {
    defaults = {
      model = "tripwire-no-gateway"
    }
  }

  mock_resource "ovh_cloud_project_network_private_subnet_v2" {
    defaults = {
      name = "tripwire-one-subnet-of-the-pinned-type"
    }
  }
}

override_resource {
  target = module.island.module.network.ovh_cloud_project_network_private.this
  values = {
    id                    = "pn-mock-0042_net"
    regions_openstack_ids = { GRA11 = "mock-os-gra11-7f3a" }
  }
}

override_resource {
  target = module.island.module.network.ovh_cloud_project_network_private_subnet.this
  values = {
    id = "mock-subnet-9b1c"
  }
}

variables {
  org        = "lz"
  region     = "GRA11"
  instance   = "demo-dev-gra11-network"
  managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/project-network"
  project = {
    tenant      = "demo"
    environment = "dev"
    project_id  = "0123456789abcdef0123456789abcdef"
    project_urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    regions     = ["GRA11"]
    unlabelled  = []
  }
  network = {
    cidr = "10.20.0.0/24"
  }
}

run "published_outputs_match_the_schema" {
  command = plan

  assert {
    condition     = output.network_id == "pn-mock-0042_net" && try(module.island.network_id, null) == output.network_id
    error_message = "network_id is the id of the network at module.island.module.network"
  }

  assert {
    condition     = output.regions_openstack_ids == tomap({ GRA11 = "mock-os-gra11-7f3a" })
    error_message = "regions_openstack_ids is the network's map"
  }

  assert {
    condition     = output.subnet_id == "mock-subnet-9b1c" && try(module.island.subnet_id, null) == output.subnet_id
    error_message = "subnet_id is the id of the subnet at module.island.module.network"
  }

  assert {
    condition     = output.cidr == "10.20.0.0/24" && try(module.island.cidr, null) == "10.20.0.0/24"
    error_message = "cidr is the region's network row's CIDR"
  }

  assert {
    condition = (length(output.unlabelled) == 2 && toset(output.unlabelled) == toset([
      "module.island.module.network.ovh_cloud_project_network_private.this",
      "module.island.module.network.ovh_cloud_project_network_private_subnet.this",
    ]))
    error_message = "unlabelled lists exactly the network and the subnet, once each, from the stage root"
  }

  assert {
    condition = !anytrue([
      issensitive(output.network_id), issensitive(output.regions_openstack_ids), issensitive(output.subnet_id),
      issensitive(output.cidr), issensitive(output.unlabelled),
    ])
    error_message = "the published outputs are not sensitive"
  }
}

run "network_in_the_project_and_region" {
  command = plan

  assert {
    condition     = try(module.island.project_id, null) == "0123456789abcdef0123456789abcdef"
    error_message = "the network belongs to the project of the `project` values"
  }

  assert {
    condition     = jsonencode(try(module.island.regions, null)) == jsonencode(["GRA11"])
    error_message = "the network is in exactly the instance's region"
  }

  assert {
    condition     = try(module.island.vlan_id, null) == 0
    error_message = "vlan_id defaults to 0 when the network row has none"
  }

  assert {
    condition     = try(module.island.network_name, null) == "lz-demo-dev-gra11-pn-main"
    error_message = "the network is named from org, the project's tenant and environment, and the region"
  }
}

run "every_input_reaches_the_component" {
  command = plan

  # `instance` and `managed_in` only feed modules/naming's label set, which nothing here carries
  # (no tags on either resource, R14): not observable, so not varied.
  variables {
    org    = "ex"
    region = "SBG5"
    project = {
      tenant          = "acme"
      environment     = "prod"
      project_id      = "fedcba9876543210fedcba9876543210"
      project_urn     = "urn:v1:ca:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
      regions         = ["GRA11", "SBG5"]
      budget_alert_id = "mock-alert-id-0123"
      unlabelled      = ["module.project_factory.module.project.ovh_cloud_project_alerting.this[0]"]
    }
    network = {
      cidr    = "192.168.10.0/29"
      vlan_id = 42
    }
  }

  assert {
    condition     = try(module.island.project_id, null) == "fedcba9876543210fedcba9876543210"
    error_message = "the network follows the project id of the `project` values"
  }

  assert {
    condition     = jsonencode(try(module.island.regions, null)) == jsonencode(["SBG5"])
    error_message = "the network follows the instance's region, the second of the project's regions"
  }

  assert {
    condition     = output.cidr == "192.168.10.0/29" && try(module.island.cidr, null) == "192.168.10.0/29"
    error_message = "the subnet follows the network row's CIDR"
  }

  assert {
    condition     = try(module.island.vlan_id, null) == 42
    error_message = "the network follows the network row's vlan_id"
  }

  assert {
    condition     = try(module.island.network_name, null) == "ex-acme-prod-sbg5-pn-main"
    error_message = "the name follows org, the project's tenant and environment, and the region"
  }
}

# CIDR range (T085; T030 review): the stage's own rule on the network row admits only a network
# inside an RFC 1918 block (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) and no larger than a /16. Each
# admitted run sits at an edge of that range.

run "cidr_private_slash24_admitted" {
  command = plan

  variables {
    network = { cidr = "10.250.0.0/24" }
  }

  assert {
    condition     = output.cidr == "10.250.0.0/24" && try(module.island.cidr, null) == "10.250.0.0/24"
    error_message = "a /24 inside 10.0.0.0/8 is admitted and reaches the component"
  }
}

run "cidr_private_slash16_admitted" {
  command = plan

  variables {
    network = { cidr = "10.250.0.0/16" }
  }

  assert {
    condition     = output.cidr == "10.250.0.0/16" && try(module.island.cidr, null) == "10.250.0.0/16"
    error_message = "a /16 inside 10.0.0.0/8 is admitted (the largest network admitted)"
  }
}

run "cidr_first_slash16_of_172_16_admitted" {
  command = plan

  variables {
    network = { cidr = "172.16.0.0/16" }
  }

  assert {
    condition     = output.cidr == "172.16.0.0/16" && try(module.island.cidr, null) == "172.16.0.0/16"
    error_message = "172.16.0.0/16 is the first /16 of 172.16.0.0/12 and is admitted"
  }
}

run "cidr_last_slash16_of_172_16_admitted" {
  command = plan

  variables {
    network = { cidr = "172.31.0.0/16" }
  }

  assert {
    condition     = output.cidr == "172.31.0.0/16" && try(module.island.cidr, null) == "172.31.0.0/16"
    error_message = "172.31.0.0/16 lies inside 172.16.0.0/12 and is admitted"
  }
}

run "cidr_whole_192_168_admitted" {
  command = plan

  variables {
    network = { cidr = "192.168.0.0/16" }
  }

  assert {
    condition     = output.cidr == "192.168.0.0/16" && try(module.island.cidr, null) == "192.168.0.0/16"
    error_message = "192.168.0.0/16 is the whole RFC 1918 block and is admitted"
  }
}

# Refusals, last.

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

run "cidr_not_an_address_rejected" {
  command = plan

  variables {
    network = { cidr = "not-a-cidr/24" }
  }

  expect_failures = [var.network]
}

run "cidr_without_prefix_rejected" {
  command = plan

  variables {
    network = { cidr = "10.20.0.0" }
  }

  expect_failures = [var.network]
}

run "cidr_with_host_bits_rejected" {
  command = plan

  variables {
    network = { cidr = "10.20.0.1/24" }
  }

  expect_failures = [var.network]
}

run "cidr_ipv6_rejected" {
  command = plan

  variables {
    network = { cidr = "fd00::/24" }
  }

  expect_failures = [var.network]
}

run "cidr_too_small_for_a_pool_rejected" {
  command = plan

  variables {
    network = { cidr = "10.20.0.0/30" }
  }

  expect_failures = [var.network]
}

# CIDR range refusals (T085). `0.0.0.0/0` is named by the task and fails both limits; every other
# input fails exactly one: outside RFC 1918 at /16 or /24, or inside it but larger than a /16.

run "cidr_whole_address_space_rejected" {
  command = plan

  variables {
    network = { cidr = "0.0.0.0/0" }
  }

  expect_failures = [var.network]
}

run "cidr_public_range_rejected" {
  command = plan

  variables {
    network = { cidr = "8.8.8.0/24" }
  }

  expect_failures = [var.network]
}

run "cidr_just_past_10_slash8_rejected" {
  command = plan

  variables {
    network = { cidr = "11.0.0.0/16" }
  }

  expect_failures = [var.network]
}

run "cidr_just_before_172_16_slash12_rejected" {
  command = plan

  variables {
    network = { cidr = "172.15.0.0/16" }
  }

  expect_failures = [var.network]
}

run "cidr_just_past_172_16_slash12_rejected" {
  command = plan

  variables {
    network = { cidr = "172.32.0.0/16" }
  }

  expect_failures = [var.network]
}

run "cidr_just_past_192_168_slash16_rejected" {
  command = plan

  variables {
    network = { cidr = "192.169.0.0/16" }
  }

  expect_failures = [var.network]
}

run "cidr_private_but_larger_than_slash16_rejected" {
  command = plan

  variables {
    network = { cidr = "10.0.0.0/15" }
  }

  expect_failures = [var.network]
}

run "cidr_whole_10_slash8_rejected" {
  command = plan

  variables {
    network = { cidr = "10.0.0.0/8" }
  }

  expect_failures = [var.network]
}
