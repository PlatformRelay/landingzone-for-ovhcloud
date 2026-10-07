# Private-network module (spec 005 T030; research R9, R14): one private network in exactly the given
# region of the given project and one subnet with the given CIDR, DHCP on, no gateway. Neither
# resource carries tags (R14): the name is the only label, and the stage lists both as `unlabelled`.
# `regions` is set to the one region because the provider default is every region of the project
# (cloud_project_network_private.md). `no_gateway = true`, otherwise the provider gives the subnet a
# default gateway address (cloud_project_network_private_subnet.md); no gateway resource is created
# (billed, R9). The DHCP pool leaves the network address, the first host address and the broadcast
# address out (believed; OVHcloud's own reservations in a subnet are UNVERIFIED until T010).
resource "ovh_cloud_project_network_private" "this" {
  service_name = var.project_id
  name         = var.name
  regions      = [var.region]
  vlan_id      = var.vlan_id
}

resource "ovh_cloud_project_network_private_subnet" "this" {
  service_name = var.project_id
  network_id   = ovh_cloud_project_network_private.this.id
  region       = var.region
  network      = var.cidr
  start        = cidrhost(var.cidr, 2)
  end          = cidrhost(var.cidr, -2)
  dhcp         = true
  no_gateway   = true
}
