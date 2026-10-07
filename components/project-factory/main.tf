# Project-factory component (spec 005 T028; FR-002, FR-004; research R7, R14; KD-3): one existing
# Public Cloud project, adopted (managed at module.project.ovh_cloud_project.this[0], where the stack
# imports it) or referenced (only read), its labels on the project URN, the optional budget alert
# and the optional quota guard.
#
# modules/cloud-project is called exactly once, unkeyed and by relative source: a second call would
# manage or read a second project, and count or for_each would key the import target
# (`task test:dependencies`, RETAINED_UNPROTECTED). The adopt-mode order arguments are passed
# unchanged, never invented: T009 showed that the API returns neither `ovh_subsidiary` nor `plan`
# for an existing project, so an import plans a replacement (prevent_destroy refuses it) and the
# sandbox uses reference mode.

# Labels only: the project's name is not set by this slice (kind `project`, modules/naming).
module "labels" {
  source      = "../../modules/naming"
  org         = var.org
  tenant      = var.tenant
  environment = var.environment
  kind        = "project"
  role        = "main"
  instance    = var.instance
  managed_in  = var.managed_in
}

module "project" {
  source         = "../../modules/cloud-project"
  mode           = var.mode
  project_id     = var.project_id
  ovh_subsidiary = var.ovh_subsidiary
  description    = var.description
  plan           = var.plan
  tags           = module.labels.labels
  budget_alert   = var.budget_alert
}

module "quota" {
  source     = "../../modules/cloud-quota"
  project_id = module.project.project_id
  enabled    = var.quota_guard.enabled
}
