# Stub (spec 005 T031): the component call the tests pin, no behaviour. T032 calls
# components/runtime/managed-only once, unkeyed, at `module.runtime` with the project's id, tenant
# and environment from `var.project` and the instance's region, slot and labels inputs.
module "runtime" {
  source      = "../../components/runtime/managed-only"
  org         = "not-implemented"
  tenant      = "not-implemented"
  environment = "not-implemented"
  region      = "NOT-IMPLEMENTED"
  instance    = "not-implemented"
  managed_in  = "not-implemented"
  project_id  = "not-implemented"
}
