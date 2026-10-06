# P11 (T009, plan only — never applied): `ovh_cloud_quota` can set prevent_automatic_quota_upgrade
# on the sandbox project. The resource is a singleton per project with no name and no tag
# (cloud_quota.md); its regions are copied from the project's current target profiles so the plan
# changes the flag alone. Destroy semantics of the singleton are UNVERIFIED: this root is run with
# --plan-only only (run sheet).
data "ovh_cloud_quota" "current" {
  service_name = local.project.service_name
}

resource "ovh_cloud_quota" "probe" {
  service_name                    = local.project.service_name
  prevent_automatic_quota_upgrade = true
  regions = [for r in data.ovh_cloud_quota.current.regions : {
    region  = r.region
    profile = r.profile
  }]
}

output "current_prevent_automatic_quota_upgrade" {
  description = "The project's flag before the probe (P11 observation)."
  value       = data.ovh_cloud_quota.current.prevent_automatic_quota_upgrade
}

output "run_id" {
  description = "Run id this probe ran under."
  value       = local.run_id
}
