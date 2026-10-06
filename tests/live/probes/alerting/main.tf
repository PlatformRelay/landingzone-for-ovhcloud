# P10 (T009 plan, T010 apply and destroy): `ovh_cloud_project_alerting` works on the trial project.
# The resource takes no name, so the run id goes into nothing the API lists by prefix: the leftover
# check matches the alert by the id the inventory recorded (research R12). The contact address is
# the account's own (data source ovh_me), so no address is committed. Delay: one of the API's
# cloud.AlertingDelayEnum values (kb/api/v1/cloud.json); threshold in the account currency.
data "ovh_me" "account" {}

resource "ovh_cloud_project_alerting" "probe" {
  service_name      = local.project.service_name
  delay             = 3600
  email             = data.ovh_me.account.email
  monthly_threshold = 1
}

output "alert_id" {
  description = "Probe alert id (leftover match: inventory id)."
  value       = ovh_cloud_project_alerting.probe.id
}

output "run_id" {
  description = "Run id this probe ran under."
  value       = local.run_id
}
