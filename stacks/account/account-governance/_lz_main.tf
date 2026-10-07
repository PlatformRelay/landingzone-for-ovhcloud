// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

module "account_governance" {
  instance   = "account-governance"
  managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/account-governance"
  org        = "lz"
  source     = "../../../stages/account-governance"
  tenants    = { for t in var.lz_tenants : t => var.tenants[t] }
}
