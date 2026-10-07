# Project stage (spec 005 T028; FR-002, FR-004, FR-005; research R7, R24; KD-3): one tenant
# environment's existing Public Cloud project through its one components/project-factory call, the
# only module this stage calls (`task test:dependencies`, STAGE_COMPONENT_CALLS). The stack imports
# an adopted project at module.project_factory.module.project.ovh_cloud_project.this[0] (R7, T037);
# the sandbox uses reference mode (T009 refuted P5). No backend, provider or resource here: the
# generated stack owns the first two, components own resources (ADR-0002).

module "project_factory" {
  source         = "../../components/project-factory"
  org            = var.org
  tenant         = var.tenant
  environment    = var.environment
  instance       = var.instance
  managed_in     = var.managed_in
  mode           = var.project_mode
  project_id     = var.project_id
  ovh_subsidiary = var.project_ovh_subsidiary
  description    = var.project_description
  plan           = var.project_plan
  budget_alert   = var.budget_alert
  quota_guard    = var.quota_guard
}
