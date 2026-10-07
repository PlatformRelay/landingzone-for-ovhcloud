// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

module "bootstrap" {
  instance         = "account-bootstrap"
  managed_in       = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
  org              = "lz"
  source           = "../../../stages/bootstrap"
  state_project_id = var.state_project_id
  state_region     = "GRA"
}
