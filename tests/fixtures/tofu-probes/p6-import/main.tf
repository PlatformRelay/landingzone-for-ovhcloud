# P6 (005 T007): import blocks in the root target resources inside nested modules, a single
# module and a keyed (for_each) one, as a generated stack root adopting the sandbox project
# would. terraform_data stands in for ovh_cloud_project: provider-free, plan only.
import {
  to = module.project.terraform_data.this
  id = "p6-fixture-project"
}

import {
  to = module.region["gra"].terraform_data.this
  id = "p6-fixture-region-gra"
}

module "project" {
  source = "./project"
}

module "region" {
  source   = "./project"
  for_each = toset(["gra"])
}
