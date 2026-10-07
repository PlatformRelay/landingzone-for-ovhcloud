# STUB (spec 005 T027): the stage's one components/project-factory call, which the tests' overrides
# name; the component is itself a stub, so every run is behaviourally red. T028 implements the
# stage. The stack imports the adopted project at
# module.project_factory.module.project.ovh_cloud_project.this[0] (R7, T037).

module "project_factory" {
  source      = "../../components/project-factory"
  org         = var.org
  tenant      = var.tenant
  environment = var.environment
  instance    = var.instance
  managed_in  = var.managed_in
  mode        = var.project_mode
  project_id  = var.project_id
}
