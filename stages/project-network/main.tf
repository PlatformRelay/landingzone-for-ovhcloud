# Stub (spec 005 T029): the component call the tests pin, no behaviour. T030 calls
# components/network/island once, unkeyed, at `module.island` with the project's id, tenant,
# environment and regions from `var.project` and the instance's region and network row.
module "island" {
  source          = "../../components/network/island"
  org             = "not-implemented"
  tenant          = "not-implemented"
  environment     = "not-implemented"
  region          = "NOT-IMPLEMENTED"
  instance        = "not-implemented"
  managed_in      = "not-implemented"
  project_id      = "not-implemented"
  project_regions = []
  cidr            = "192.0.2.0/24"
}
