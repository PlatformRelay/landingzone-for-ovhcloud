// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

module "stage" {
  source = "../../../stages/bootstrap"
  providers = {
    ovh       = ovh
    ovh.admin = ovh.admin
  }
}
