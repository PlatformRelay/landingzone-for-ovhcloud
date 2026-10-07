# Stub (spec 005 T025): the addresses the tests pin, no behaviour. T026 implements the optional
# guard: only when enabled, `data.ovh_cloud_quota.current` reads the given project's current regions
# and profiles and `ovh_cloud_quota.this` sends them unchanged with
# `prevent_automatic_quota_upgrade = true`.
data "ovh_cloud_quota" "current" {
  count = 0

  service_name = "not-implemented"
}

resource "ovh_cloud_quota" "this" {
  count = 0

  service_name                    = "not-implemented"
  prevent_automatic_quota_upgrade = false
  regions                         = []
}
