# Private-network module (spec 005 T029; FR-002, FR-004; ADR-0004, ADR-0017; research R9, R14;
# P12 UNVERIFIED until T010). Mocked provider, no credential, no API call.
#
# One private network in the given region of the given project and one subnet with the given CIDR,
# DHCP on, no default gateway and no gateway resource (a gateway is billed, R9). The subnet is the
# provider's `ovh_cloud_project_network_private_subnet` (arguments as in the T008 live probe
# tests/live/probes/network/probe.tf and the pinned 2.21.0 schema: `network` is the CIDR, `start`
# and `end` bound the DHCP pool, `cidr` is computed). Neither resource carries tags (R14): the
# name is the network's only label.
#
# Tripwires: mock defaults on required, non-computed fields of `ovh_cloud_project_gateway` (`model`)
# and `ovh_cloud_project_network_private_subnet_v2` (`name`) are inert unless such a resource is
# planned, and then fail the run (observed tofu 1.13.0, T023): no gateway and no second kind of
# subnet, at any address. A second network or subnet of the pinned types at another address is
# not observable here (resources cannot be enumerated in `tofu test`).
#
# Mocked ids are not values the module could build from its inputs (`pn-mock-…`, `mock-os-…`), and
# the computed subnet `cidr` defaults to another network (198.51.100.0/24), so an id or the CIDR
# read from the wrong attribute fails. `project_id`, `name`, `regions`, `vlan_id` and `cidr` equal
# their inputs at plan time: reading them from the input instead of the resource is not observable.

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
  target = ovh_cloud_project_network_private.this
  values = {
    id                    = "pn-mock-0042_net"
    regions_openstack_ids = { GRA11 = "mock-os-gra11-7f3a" }
  }
}

override_resource {
  target = ovh_cloud_project_network_private_subnet.this
  values = {
    id = "mock-subnet-9b1c"
  }
}

variables {
  project_id = "0123456789abcdef0123456789abcdef"
  name       = "lz-demo-dev-gra11-pn-main"
  region     = "GRA11"
  cidr       = "10.20.0.0/24"
}

run "one_network_in_the_given_region" {
  command = plan

  assert {
    condition     = ovh_cloud_project_network_private.this.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "the network belongs to the given project"
  }

  assert {
    condition     = jsonencode(sort(tolist(ovh_cloud_project_network_private.this.regions))) == jsonencode(["GRA11"])
    error_message = "the network is in exactly the given region (the provider default is every region)"
  }

  assert {
    condition     = ovh_cloud_project_network_private.this.name == "lz-demo-dev-gra11-pn-main"
    error_message = "the network carries the given name"
  }

  assert {
    condition     = ovh_cloud_project_network_private.this.vlan_id == 0
    error_message = "vlan_id defaults to 0"
  }
}

run "one_subnet_with_the_given_cidr" {
  command = plan

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.network == "10.20.0.0/24"
    error_message = "the subnet's network is the given CIDR"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.service_name == "0123456789abcdef0123456789abcdef" && ovh_cloud_project_network_private_subnet.this.region == "GRA11"
    error_message = "the subnet is in the given project and region"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.network_id == "pn-mock-0042_net"
    error_message = "the subnet belongs to the module's network (its id)"
  }

  assert {
    condition = (
      cidrcontains("10.20.0.0/24", ovh_cloud_project_network_private_subnet.this.start)
      && cidrcontains("10.20.0.0/24", ovh_cloud_project_network_private_subnet.this.end)
      && parseint(join("", [for o in split(".", ovh_cloud_project_network_private_subnet.this.start) : format("%02x", tonumber(o))]), 16) > parseint(join("", [for o in split(".", "10.20.0.0") : format("%02x", tonumber(o))]), 16)
      && parseint(join("", [for o in split(".", ovh_cloud_project_network_private_subnet.this.end) : format("%02x", tonumber(o))]), 16) < parseint(join("", [for o in split(".", "10.20.0.255") : format("%02x", tonumber(o))]), 16)
      && parseint(join("", [for o in split(".", ovh_cloud_project_network_private_subnet.this.start) : format("%02x", tonumber(o))]), 16) <= parseint(join("", [for o in split(".", ovh_cloud_project_network_private_subnet.this.end) : format("%02x", tonumber(o))]), 16)
    )
    error_message = "the DHCP pool lies inside the given CIDR, after its network address, before its broadcast address, start not after end"
  }
}

run "dhcp_on_and_no_gateway" {
  command = plan

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.dhcp == true
    error_message = "DHCP is on"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.no_gateway == true
    error_message = "the subnet sets no default gateway (no_gateway = true); a gateway resource trips the mock"
  }
}

run "outputs_read_from_the_resources" {
  command = plan

  assert {
    condition     = output.network_id == "pn-mock-0042_net" && output.subnet_id == "mock-subnet-9b1c"
    error_message = "network_id and subnet_id are the ids of the network and the subnet"
  }

  assert {
    condition     = output.regions_openstack_ids == tomap({ GRA11 = "mock-os-gra11-7f3a" })
    error_message = "regions_openstack_ids is the network's map"
  }

  assert {
    condition     = output.cidr == "10.20.0.0/24"
    error_message = "cidr is the subnet's given CIDR, not the computed attribute"
  }

  assert {
    condition = (
      output.project_id == "0123456789abcdef0123456789abcdef" && output.name == "lz-demo-dev-gra11-pn-main"
      && jsonencode(output.regions) == jsonencode(["GRA11"]) && output.vlan_id == 0
    )
    error_message = "project_id, name, regions and vlan_id are the network's"
  }
}

run "every_input_reaches_the_resources" {
  command = plan

  variables {
    project_id = "fedcba9876543210fedcba9876543210"
    name       = "lz-acme-prod-sbg5-pn-main"
    region     = "SBG5"
    cidr       = "192.168.10.0/29"
    vlan_id    = 42
  }

  assert {
    condition = (
      ovh_cloud_project_network_private.this.service_name == "fedcba9876543210fedcba9876543210"
      && ovh_cloud_project_network_private_subnet.this.service_name == "fedcba9876543210fedcba9876543210"
      && output.project_id == "fedcba9876543210fedcba9876543210"
    )
    error_message = "network and subnet follow the given project"
  }

  assert {
    condition = (
      jsonencode(sort(tolist(ovh_cloud_project_network_private.this.regions))) == jsonencode(["SBG5"])
      && ovh_cloud_project_network_private_subnet.this.region == "SBG5"
      && jsonencode(output.regions) == jsonencode(["SBG5"])
    )
    error_message = "network and subnet follow the given region"
  }

  assert {
    condition     = ovh_cloud_project_network_private.this.name == "lz-acme-prod-sbg5-pn-main" && output.name == "lz-acme-prod-sbg5-pn-main"
    error_message = "the network follows the given name"
  }

  assert {
    condition     = ovh_cloud_project_network_private.this.vlan_id == 42 && output.vlan_id == 42
    error_message = "the network follows the given vlan_id"
  }

  assert {
    condition = (
      ovh_cloud_project_network_private_subnet.this.network == "192.168.10.0/29" && output.cidr == "192.168.10.0/29"
      && cidrcontains("192.168.10.0/29", ovh_cloud_project_network_private_subnet.this.start)
      && cidrcontains("192.168.10.0/29", ovh_cloud_project_network_private_subnet.this.end)
      && parseint(join("", [for o in split(".", ovh_cloud_project_network_private_subnet.this.start) : format("%02x", tonumber(o))]), 16) > parseint(join("", [for o in split(".", "192.168.10.0") : format("%02x", tonumber(o))]), 16)
      && parseint(join("", [for o in split(".", ovh_cloud_project_network_private_subnet.this.end) : format("%02x", tonumber(o))]), 16) < parseint(join("", [for o in split(".", "192.168.10.7") : format("%02x", tonumber(o))]), 16)
      && parseint(join("", [for o in split(".", ovh_cloud_project_network_private_subnet.this.start) : format("%02x", tonumber(o))]), 16) <= parseint(join("", [for o in split(".", ovh_cloud_project_network_private_subnet.this.end) : format("%02x", tonumber(o))]), 16)
    )
    error_message = "the subnet and its DHCP pool follow the given CIDR (a /29 is the smallest admitted)"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.dhcp == true && ovh_cloud_project_network_private_subnet.this.no_gateway == true
    error_message = "DHCP on and no gateway are not input-dependent"
  }
}

# CIDR range (T085; T030 review): only a network inside an RFC 1918 block (10.0.0.0/8, 172.16.0.0/12,
# 192.168.0.0/16) and no larger than a /16 is admitted. Each admitted run below sits at an edge of
# that range: a /24 and a /16 of 10/8, the first and last /16 of 172.16/12 and the whole of 192.168/16.

run "cidr_private_slash24_admitted" {
  command = plan

  variables {
    cidr = "10.250.0.0/24"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.network == "10.250.0.0/24" && output.cidr == "10.250.0.0/24"
    error_message = "a /24 inside 10.0.0.0/8 is admitted and reaches the subnet"
  }
}

run "cidr_private_slash16_admitted" {
  command = plan

  variables {
    cidr = "10.250.0.0/16"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.network == "10.250.0.0/16" && output.cidr == "10.250.0.0/16"
    error_message = "a /16 inside 10.0.0.0/8 is admitted (the largest network admitted)"
  }
}

run "cidr_first_slash16_of_172_16_admitted" {
  command = plan

  variables {
    cidr = "172.16.0.0/16"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.network == "172.16.0.0/16" && output.cidr == "172.16.0.0/16"
    error_message = "172.16.0.0/16 is the first /16 of 172.16.0.0/12 and is admitted"
  }
}

run "cidr_last_slash16_of_172_16_admitted" {
  command = plan

  variables {
    cidr = "172.31.0.0/16"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.network == "172.31.0.0/16" && output.cidr == "172.31.0.0/16"
    error_message = "172.31.0.0/16 lies inside 172.16.0.0/12 and is admitted"
  }
}

run "cidr_whole_192_168_admitted" {
  command = plan

  variables {
    cidr = "192.168.0.0/16"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.this.network == "192.168.0.0/16" && output.cidr == "192.168.0.0/16"
    error_message = "192.168.0.0/16 is the whole RFC 1918 block and is admitted"
  }
}

# Refusals, last: an input refusal stops the file's later runs.

run "cidr_without_prefix_rejected" {
  command = plan

  variables {
    cidr = "10.20.0.0"
  }

  expect_failures = [var.cidr]
}

run "cidr_not_an_address_rejected" {
  command = plan

  variables {
    cidr = "not-a-cidr/24"
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

run "blank_region_rejected" {
  command = plan

  variables {
    region = " "
  }

  expect_failures = [var.region]
}

run "blank_project_id_rejected" {
  command = plan

  variables {
    project_id = " "
  }

  expect_failures = [var.project_id]
}
