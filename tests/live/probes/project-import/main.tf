# P5, P6 (T009, plan only — never applied): importing the existing sandbox project into
# `ovh_cloud_project` at a nested module address, as the generated adopt-mode `project` root of
# research R7 does, plans no replacement and no order. The module keeps `prevent_destroy` and
# `deletion_protection`, so a planned replacement fails the plan instead of reaching an apply
# (the probe has no retained set for live.Protect to judge).
# The import cannot be exercised offline: `tofu test` with mock_provider "ovh" crashes on an import
# into an `ovh` resource (T007, P6; research R23). This root has lint only; its plan is T009's.
# `lz-live probe` runs `tofu plan -out`, not `-generate-config-out`: the configuration is written
# here, and the plan shows how the imported object differs from it (run sheet).
data "ovh_me" "account" {}

import {
  to = module.project.ovh_cloud_project.this
  id = local.project.service_name
}

module "project" {
  source = "./project"

  ovh_subsidiary = data.ovh_me.account.ovh_subsidiary
  description    = local.project.description
}

output "project_id" {
  description = "Id of the project the import targets: project_id (LZ_PROJECT_ID_STATE in account.env)."
  value       = local.project.service_name
}

output "run_id" {
  description = "Run id this probe ran under."
  value       = local.run_id
}
