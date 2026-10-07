# Optional quota guard (spec 005, P11). Only when enabled: the given project's current regions and
# profiles are read and sent back unchanged with `prevent_automatic_quota_upgrade = true`, so the
# module changes the flag only. `regions` is required by `ovh_cloud_quota`; regions omitted from it
# are left unchanged upstream (cloud_quota.md). When off, nothing is read or managed.
data "ovh_cloud_quota" "current" {
  count = var.enabled ? 1 : 0

  service_name = var.project_id
}

resource "ovh_cloud_quota" "this" {
  count = var.enabled ? 1 : 0

  service_name                    = var.project_id
  prevent_automatic_quota_upgrade = true
  regions = [for r in data.ovh_cloud_quota.current[0].regions : {
    region  = r.region
    profile = r.profile
  }]
}
