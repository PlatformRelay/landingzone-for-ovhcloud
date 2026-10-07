output "prevent_automatic_quota_upgrade" {
  description = "Whether the module disables automatic quota upgrades (null when the guard is off)."
  value       = one(ovh_cloud_quota.this[*].prevent_automatic_quota_upgrade)
}
