# One retained Object Storage bucket for state (FR-002, FR-004; research R5 *Protection*): versioning
# always on, and a literal `prevent_destroy` (a lifecycle argument cannot depend on a variable).
# `task test:dependencies` refuses this module without it (rule RETAINED_UNPROTECTED, G7).
resource "ovh_cloud_project_storage" "this" {
  service_name = var.project_id
  region_name  = var.region
  name         = var.name
  tags         = var.tags

  versioning = {
    status = "enabled"
  }

  lifecycle {
    prevent_destroy = true
  }
}
