// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

module "project" {
  budget_alert = {
    enabled = false
  }
  environment  = "dev"
  instance     = "demo-dev-project"
  managed_in   = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/project"
  org          = "lz"
  project_id   = var.project_id
  project_mode = "reference"
  quota_guard = {
    enabled = false
  }
  regions = [
    "GRA11",
  ]
  source = "../../../../../stages/project"
  tenant = "demo"
}
