# P12 (T010): a private network and a subnet in GRA11 without gateway or instances create and
# destroy without explicit vRack management. Neither resource carries tags (research R9): the
# name carries the prefix and the run id; the subnet is matched through its parent network.
# VLAN id (T098): a project holds one private network per VLAN id (T010 run
# 20261010T184645Z-f7de: 400 `A private network already exists for this vlan ID`, every probe
# network on VLAN 0). The probe takes the first id of its own range 2000..2999 (disjoint
# from the storage-iam probe's 3000..3999, inside the 0..4000 the modules admit), starting
# at 2000 + the run id's four hex digits mod 1000 and wrapping, that no other network of the
# project holds: a leftover or an applied concurrent run is skipped. Once its own network exists
# (matched by name) the probe keeps that network's id, so a re-plan never moves it (vlan_id forces
# a replacement). Two plans racing before either applies may pick one id, and a leftover of the
# same run id is taken for its own; the API then refuses the create (the 400 above), so two
# networks never share an id. Listing needs `network/private/get` (cloud.json), which the tenant
# allowlist holds.
data "ovh_cloud_project_network_privates" "existing" {
  service_name = local.project.service_name
}

locals {
  vlan_start = parseint(substr(local.run_id, 17, 4), 16)
  vlan_taken = toset([for n in data.ovh_cloud_project_network_privates.existing.networks : n.vlan_id])
  vlan_free  = [for k in range(1000) : 2000 + (local.vlan_start + k) % 1000 if !contains(local.vlan_taken, 2000 + (local.vlan_start + k) % 1000)]
  vlan_own   = [for n in data.ovh_cloud_project_network_privates.existing.networks : n.vlan_id if n.name == "lzprobe-network-${local.run_id}"]
  vlan_id    = try(local.vlan_own[0], local.vlan_free[0], null)
}

resource "ovh_cloud_project_network_private" "probe" {
  service_name = local.project.service_name
  name         = "lzprobe-network-${local.run_id}"
  regions      = ["GRA11"]
  vlan_id      = local.vlan_id

  lifecycle {
    precondition {
      condition     = local.vlan_id != null
      error_message = "every VLAN id of 2000..2999 is taken in the project: clear leftover probe networks (task live:probe -- --cleanup <run-id>)."
    }
  }
}

resource "ovh_cloud_project_network_private_subnet" "probe" {
  service_name = local.project.service_name
  network_id   = ovh_cloud_project_network_private.probe.id
  region       = "GRA11"
  network      = "10.250.0.0/24"
  start        = "10.250.0.10"
  end          = "10.250.0.200"
  dhcp         = true
  no_gateway   = true
}

output "network_name" {
  description = "Probe network name (leftover match: name prefix)."
  value       = ovh_cloud_project_network_private.probe.name
}

output "network_id" {
  description = "Probe network id (subnets are listed per network)."
  value       = ovh_cloud_project_network_private.probe.id
}
