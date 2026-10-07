output "name" {
  description = "Bucket name."
  value       = ovh_cloud_project_storage.this.name
}

output "region" {
  description = "Object Storage region name of the bucket."
  value       = ovh_cloud_project_storage.this.region_name
}
