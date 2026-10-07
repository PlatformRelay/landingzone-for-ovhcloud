// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

module "runtime" {
  instance   = "demo-dev-gra11-runtime"
  managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
  org        = "lz"
  project    = var.project
  region     = "GRA11"
  slot       = null
  source     = "../../../../../../stages/runtime"
}
