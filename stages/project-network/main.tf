# Project-network stage (spec 005 T030; FR-002, FR-004, FR-005; research R9): one private network
# and one subnet in the instance's region of the environment's project through its one
# components/network/island call, the only module this stage calls (`task test:dependencies`,
# STAGE_COMPONENT_CALLS). Tenant, environment, project id and regions come only from the `project`
# stage's published values (`var.project`). No backend, provider or resource here: the generated
# stack owns the first two, components own resources (ADR-0002).
module "island" {
  source          = "../../components/network/island"
  org             = var.org
  tenant          = var.project.tenant
  environment     = var.project.environment
  region          = var.region
  instance        = var.instance
  managed_in      = var.managed_in
  project_id      = var.project.project_id
  project_regions = var.project.regions
  cidr            = var.network.cidr
  vlan_id         = var.network.vlan_id
}
