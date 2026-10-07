# Stub (spec 005 T029): the module call the tests pin, no behaviour. T030 names the network through
# modules/naming (kind `private_network`) and calls modules/private-network once, unkeyed, at
# `module.network` with the given project, region, CIDR and VLAN id.
module "network" {
  source     = "../../../modules/private-network"
  project_id = "not-implemented"
  name       = "not-implemented"
  region     = "NOT-IMPLEMENTED"
  cidr       = "192.0.2.0/24"
}
