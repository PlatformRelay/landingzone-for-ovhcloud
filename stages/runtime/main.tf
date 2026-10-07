# One managed-only runtime (spec 005 T032; FR-003, FR-005; ADR-0002, ADR-0017): the stage's one
# components/runtime/managed-only call, unkeyed (lz-check deps, `singleComponentStages`), with the
# project's id, tenant and environment from the `project` stage's published values only.
module "runtime" {
  source      = "../../components/runtime/managed-only"
  org         = var.org
  tenant      = var.project.tenant
  environment = var.project.environment
  region      = var.region
  instance    = var.instance
  managed_in  = var.managed_in
  project_id  = var.project.project_id
  slot        = var.slot
}
