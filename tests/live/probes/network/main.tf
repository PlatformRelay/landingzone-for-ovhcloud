# P12 (T010): a private network and a subnet in GRA11 without gateway or instances create and
# destroy without explicit vRack management. Neither resource carries tags (research R9): the
# name carries the prefix and the run id; the subnet is matched through its parent network.
resource "ovh_cloud_project_network_private" "probe" {
  service_name = local.project.service_name
  name         = "lzprobe-network-${local.run_id}"
  regions      = ["GRA11"]
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
