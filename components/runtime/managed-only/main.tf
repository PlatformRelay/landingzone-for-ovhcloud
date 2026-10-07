# One empty, labelled Object Storage bucket for a managed-only runtime (spec 005 T032; FR-003,
# FR-004; research R5, R8, R14, R22; ADR-0017). The bucket is ephemeral, so it goes through the
# plain, replaceable modules/object-storage (never the protected state-bucket module), named and
# labelled through modules/naming (kind `bucket`, role `runtime`, the instance's `slot`).
#
# Object Storage region: the instance region's leading letters, upper case (`GRA11` -> `GRA`).
# UNVERIFIED until T010 (see README); `replace` never fails, so a refused region fails only its own
# rule at the stage.
locals {
  storage_region = upper(replace(var.region, "/[^A-Za-z].*$/", ""))
}

module "bucket_name" {
  source      = "../../../modules/naming"
  org         = var.org
  tenant      = var.tenant
  environment = var.environment
  region      = var.region
  kind        = "bucket"
  role        = "runtime"
  slot        = var.slot
  instance    = var.instance
  managed_in  = var.managed_in
}

module "bucket" {
  source     = "../../../modules/object-storage"
  project_id = var.project_id
  region     = local.storage_region
  name       = module.bucket_name.name
  tags       = module.bucket_name.labels
}
