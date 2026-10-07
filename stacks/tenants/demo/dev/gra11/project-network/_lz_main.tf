// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

module "project_network" {
  instance   = "demo-dev-gra11-network"
  managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/project-network"
  network = {
    cidr    = "10.20.0.0/24"
    vlan_id = 0
  }
  org     = "lz"
  project = var.project
  region  = "GRA11"
  source  = "../../../../../../stages/project-network"
}
