output "name" {
  description = "Bucket name."
  value       = ovh_cloud_project_storage.this.name
}

output "region" {
  description = "Object Storage region name of the bucket."
  value       = ovh_cloud_project_storage.this.region_name
}

output "project_id" {
  description = "Public Cloud project id (service name) that holds the bucket."
  value       = ovh_cloud_project_storage.this.service_name
}

output "tags" {
  description = "Tags the bucket carries."
  value       = ovh_cloud_project_storage.this.tags
}

output "object_lock" {
  description = "The bucket's Object Lock: `status`, default retention `mode` and `period` (ISO 8601 duration)."
  value = {
    status = ovh_cloud_project_storage.this.object_lock.status
    mode   = ovh_cloud_project_storage.this.object_lock.rule.mode
    period = ovh_cloud_project_storage.this.object_lock.rule.period
  }
}
