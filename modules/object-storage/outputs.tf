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
