# One retained Object Storage bucket for state (FR-002, FR-004; research R5 *Protection*): versioning
# and Object Lock (governance) always on, and a literal `prevent_destroy` (a lifecycle argument cannot depend on a variable).
# `task test:dependencies` refuses this module without it (rule RETAINED_UNPROTECTED, G7).
resource "ovh_cloud_project_storage" "this" {
  service_name = var.project_id
  region_name  = var.region
  name         = var.name
  tags         = var.tags

  versioning = {
    status = "enabled"
  }

  # Spec 005 T099 (D104): every new object version is locked for `retention_days` days in governance
  # mode, so a state user cannot delete state history (T010 run 20261010T201153Z-ffbf: the policy's
  # `s3:DeleteObjectVersion*` Deny is not enforced); a holder of s3:BypassGovernanceRetention, never
  # a state user, can (docs.ovhcloud.com storage-and-backup/object-storage/
  # s3-managing-object-lock.mdx:57-59, :241-255). Object Lock is set only at creation and cannot be
  # turned off (same guide :140, :153); changing `status` replaces the bucket (provider
  # cloud_project_storage.md:12-14), which `prevent_destroy` and guard G7 refuse.
  object_lock = {
    status = "enabled"
    rule = {
      mode   = "governance"
      period = "P${var.retention_days}D"
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}
