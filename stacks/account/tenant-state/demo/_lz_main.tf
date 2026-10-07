// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

module "tenant_state" {
  instance         = "demo-state"
  managed_in       = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
  org              = "lz"
  source           = "../../../../stages/tenant-state"
  state_project_id = var.state_project_id
  state_region     = "GRA"
  tenant           = "demo"
}
