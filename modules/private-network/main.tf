# Stub (spec 005 T029): the addresses the tests pin, no behaviour. T030 implements one private
# network in the given region (`ovh_cloud_project_network_private.this`) and one subnet with the
# given CIDR, DHCP on and no gateway (`ovh_cloud_project_network_private_subnet.this`).
resource "ovh_cloud_project_network_private" "this" {
  service_name = "not-implemented"
  name         = "not-implemented"
  regions      = ["NOT-IMPLEMENTED"]
}

resource "ovh_cloud_project_network_private_subnet" "this" {
  service_name = "not-implemented"
  network_id   = "not-implemented"
  region       = "NOT-IMPLEMENTED"
  network      = "192.0.2.0/24"
  start        = "192.0.2.10"
  end          = "192.0.2.20"
}
