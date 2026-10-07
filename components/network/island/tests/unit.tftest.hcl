# Network/island component (spec 005 T029; FR-002, FR-004, FR-005; ADR-0004, ADR-0017; research R9,
# R14; data-model *Per-stage values* `project-network`; P12 UNVERIFIED until T010). Mocked
# provider, no credential, no API call.
#
# The component names the network through modules/naming (kind `private_network`, abbreviation
# `pn`, role `main`) and calls modules/private-network once, unkeyed, at `module.network`. A
# `tofu test` assertion reads only a child module's outputs, never its resources (T027 *Observed*),
# so the subnet's DHCP and no-gateway settings are pinned in modules/private-network's tests and
# here only through the tripwires below; this file pins what the component passes to the module and
# what it publishes. The overrides sit on the unkeyed `module.network` resources, and the module's
# outputs are read as `module.network.<output>`, so a renamed or keyed call fails.
#
# Tripwires: `ovh_cloud_project_gateway` (`model`) and `ovh_cloud_project_network_private_subnet_v2`
# (`name`) fail the run if planned anywhere below the component (observed tofu 1.13.0, T023).
#
# Region rule: the region must be one of the project's regions (`project_regions`, the `project`
# stage's published `regions`).

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
  target = module.network.ovh_cloud_project_network_private.this
  values = {
    id                    = "pn-mock-0042_net"
    regions_openstack_ids = { GRA11 = "mock-os-gra11-7f3a" }
  }
}

override_resource {
  target = module.network.ovh_cloud_project_network_private_subnet.this
  values = {
    id = "mock-subnet-9b1c"
  }
}

variables {
  org             = "lz"
  tenant          = "demo"
  environment     = "dev"
  region          = "GRA11"
  instance        = "demo-dev-gra11-network"
  managed_in      = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/project-network"
  project_id      = "0123456789abcdef0123456789abcdef"
  project_regions = ["GRA11"]
  cidr            = "10.20.0.0/24"
}

run "one_network_in_the_given_region_and_project" {
  command = plan

  assert {
    condition     = output.project_id == "0123456789abcdef0123456789abcdef" && try(module.network.project_id, null) == "0123456789abcdef0123456789abcdef"
    error_message = "the network belongs to the given project (read back from modules/private-network)"
  }

  assert {
    condition     = jsonencode(output.regions) == jsonencode(["GRA11"]) && jsonencode(try(module.network.regions, null)) == jsonencode(["GRA11"])
    error_message = "the network is in exactly the given region"
  }

  assert {
    condition     = output.vlan_id == 0 && try(module.network.vlan_id, null) == 0
    error_message = "vlan_id defaults to 0"
  }
}

run "one_subnet_with_the_given_cidr" {
  command = plan

  assert {
    condition     = output.cidr == "10.20.0.0/24" && try(module.network.cidr, null) == "10.20.0.0/24"
    error_message = "the subnet has the given CIDR"
  }

  assert {
    condition     = output.subnet_id == "mock-subnet-9b1c" && try(module.network.subnet_id, null) == "mock-subnet-9b1c"
    error_message = "subnet_id is the id of the subnet at module.network"
  }
}

run "outputs_read_from_the_network_module" {
  command = plan

  assert {
    condition     = output.network_id == "pn-mock-0042_net" && try(module.network.network_id, null) == "pn-mock-0042_net"
    error_message = "network_id is the id of the network at module.network"
  }

  assert {
    condition     = output.regions_openstack_ids == tomap({ GRA11 = "mock-os-gra11-7f3a" })
    error_message = "regions_openstack_ids is the network's map"
  }
}

run "network_named_by_naming" {
  command = plan

  assert {
    condition     = output.network_name == "lz-demo-dev-gra11-pn-main" && try(module.network.name, null) == "lz-demo-dev-gra11-pn-main"
    error_message = "the network's name is modules/naming's for org, tenant, environment, region, kind private_network, role main"
  }
}

run "unlabelled_lists_the_network_and_the_subnet" {
  command = plan

  assert {
    condition = (length(output.unlabelled) == 2 && toset(output.unlabelled) == toset([
      "module.network.ovh_cloud_project_network_private.this",
      "module.network.ovh_cloud_project_network_private_subnet.this",
    ]))
    error_message = "unlabelled lists exactly the network and the subnet, once each, from the component root (no tags in their schema, R14)"
  }
}

run "every_input_reaches_the_network" {
  command = plan

  # `instance` and `managed_in` only feed modules/naming's label set, which nothing here carries
  # (no tags on either resource, R14): not observable, so not varied.
  variables {
    org             = "ex"
    tenant          = "acme"
    environment     = "prod"
    region          = "SBG5"
    project_id      = "fedcba9876543210fedcba9876543210"
    project_regions = ["GRA11", "SBG5"]
    cidr            = "192.168.10.0/29"
    vlan_id         = 42
  }

  assert {
    condition     = output.project_id == "fedcba9876543210fedcba9876543210" && try(module.network.project_id, null) == "fedcba9876543210fedcba9876543210"
    error_message = "the network follows the given project"
  }

  assert {
    condition     = jsonencode(output.regions) == jsonencode(["SBG5"]) && jsonencode(try(module.network.regions, null)) == jsonencode(["SBG5"])
    error_message = "the network follows the given region, the second of the project's regions"
  }

  assert {
    condition     = output.cidr == "192.168.10.0/29" && try(module.network.cidr, null) == "192.168.10.0/29"
    error_message = "the subnet follows the given CIDR"
  }

  assert {
    condition     = output.vlan_id == 42 && try(module.network.vlan_id, null) == 42
    error_message = "the network follows the given vlan_id"
  }

  assert {
    condition     = output.network_name == "ex-acme-prod-sbg5-pn-main" && try(module.network.name, null) == "ex-acme-prod-sbg5-pn-main"
    error_message = "the name follows org, tenant, environment and region"
  }
}

# CIDR range (T085; T030 review): the component's own rule admits only a network inside an RFC 1918
# block (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) and no larger than a /16. Each admitted run sits
# at an edge of that range.

run "cidr_private_slash24_admitted" {
  command = plan

  variables {
    cidr = "10.250.0.0/24"
  }

  assert {
    condition     = output.cidr == "10.250.0.0/24" && try(module.network.cidr, null) == "10.250.0.0/24"
    error_message = "a /24 inside 10.0.0.0/8 is admitted and reaches the network module"
  }
}

run "cidr_private_slash16_admitted" {
  command = plan

  variables {
    cidr = "10.250.0.0/16"
  }

  assert {
    condition     = output.cidr == "10.250.0.0/16" && try(module.network.cidr, null) == "10.250.0.0/16"
    error_message = "a /16 inside 10.0.0.0/8 is admitted (the largest network admitted)"
  }
}

run "cidr_first_slash16_of_172_16_admitted" {
  command = plan

  variables {
    cidr = "172.16.0.0/16"
  }

  assert {
    condition     = output.cidr == "172.16.0.0/16" && try(module.network.cidr, null) == "172.16.0.0/16"
    error_message = "172.16.0.0/16 is the first /16 of 172.16.0.0/12 and is admitted"
  }
}

run "cidr_last_slash16_of_172_16_admitted" {
  command = plan

  variables {
    cidr = "172.31.0.0/16"
  }

  assert {
    condition     = output.cidr == "172.31.0.0/16" && try(module.network.cidr, null) == "172.31.0.0/16"
    error_message = "172.31.0.0/16 lies inside 172.16.0.0/12 and is admitted"
  }
}

run "cidr_whole_192_168_admitted" {
  command = plan

  variables {
    cidr = "192.168.0.0/16"
  }

  assert {
    condition     = output.cidr == "192.168.0.0/16" && try(module.network.cidr, null) == "192.168.0.0/16"
    error_message = "192.168.0.0/16 is the whole RFC 1918 block and is admitted"
  }
}

# Refusals, last.

run "region_outside_the_project_regions_rejected" {
  command = plan

  variables {
    region          = "SBG5"
    project_regions = ["GRA11"]
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

run "cidr_without_prefix_rejected" {
  command = plan

  variables {
    cidr = "10.20.0.0"
  }

  expect_failures = [var.cidr]
}

run "cidr_with_host_bits_rejected" {
  command = plan

  variables {
    cidr = "10.20.0.1/24"
  }

  expect_failures = [var.cidr]
}

run "cidr_ipv6_rejected" {
  command = plan

  variables {
    cidr = "fd00::/24"
  }

  expect_failures = [var.cidr]
}

run "cidr_too_small_for_a_pool_rejected" {
  command = plan

  variables {
    cidr = "10.20.0.0/30"
  }

  expect_failures = [var.cidr]
}

# CIDR range refusals (T085). `0.0.0.0/0` is named by the task and fails both limits; every other
# input fails exactly one: outside RFC 1918 at /16 or /24, or inside it but larger than a /16.

run "cidr_whole_address_space_rejected" {
  command = plan

  variables {
    cidr = "0.0.0.0/0"
  }

  expect_failures = [var.cidr]
}

run "cidr_public_range_rejected" {
  command = plan

  variables {
    cidr = "8.8.8.0/24"
  }

  expect_failures = [var.cidr]
}

run "cidr_just_past_10_slash8_rejected" {
  command = plan

  variables {
    cidr = "11.0.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "cidr_just_before_172_16_slash12_rejected" {
  command = plan

  variables {
    cidr = "172.15.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "cidr_just_past_172_16_slash12_rejected" {
  command = plan

  variables {
    cidr = "172.32.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "cidr_just_past_192_168_slash16_rejected" {
  command = plan

  variables {
    cidr = "192.169.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "cidr_private_but_larger_than_slash16_rejected" {
  command = plan

  variables {
    cidr = "10.0.0.0/15"
  }

  expect_failures = [var.cidr]
}

run "cidr_whole_10_slash8_rejected" {
  command = plan

  variables {
    cidr = "10.0.0.0/8"
  }

  expect_failures = [var.cidr]
}
