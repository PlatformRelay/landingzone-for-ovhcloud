# Network/island component (spec 005 T030; research R9, R14): one private network and one subnet in
# one region of the environment's project, named through modules/naming (kind `private_network`,
# role `main`), through exactly one unkeyed modules/private-network call at `module.network`. The
# region must be one of the project's regions; DHCP on and no gateway are the module's.
module "network_name" {
  source      = "../../../modules/naming"
  org         = var.org
  tenant      = var.tenant
  environment = var.environment
  region      = var.region
  kind        = "private_network"
  role        = "main"
  instance    = var.instance
  managed_in  = var.managed_in
}

module "network" {
  source     = "../../../modules/private-network"
  project_id = var.project_id
  name       = module.network_name.name
  region     = var.region
  cidr       = var.cidr
  vlan_id    = var.vlan_id
}
