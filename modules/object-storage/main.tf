# One Object Storage bucket with the name and tags it is given (FR-002, FR-004; research R5).
# Replaceable: runtime buckets are ephemeral; state buckets use modules/object-storage-protected.
# `versioning` is sent only when requested: false leaves the attribute to the provider (the default
# of a new bucket without it is UNVERIFIED, T010).
resource "ovh_cloud_project_storage" "this" {
  service_name = var.project_id
  region_name  = var.region
  name         = var.name
  tags         = var.tags
  versioning   = var.versioning ? { status = "enabled" } : null
}
