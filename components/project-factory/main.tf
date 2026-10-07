# STUB (spec 005 T027): declares the module calls the tests' overrides name, with no instance, so
# every run is behaviourally red. T028 implements the component: modules/cloud-project at
# `module.project` (unrepeated: the stack imports the adopted project at
# module.project.ovh_cloud_project.this[0]), modules/cloud-quota at `module.quota`, labels from
# modules/naming.

module "project" {
  source     = "../../modules/cloud-project"
  count      = 0
  mode       = var.mode
  project_id = var.project_id
  tags       = {}
}

module "quota" {
  source     = "../../modules/cloud-quota"
  count      = 0
  project_id = var.project_id
}
