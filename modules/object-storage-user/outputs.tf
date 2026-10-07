output "user_id" {
  description = "Id of the S3 user."
  value       = ovh_cloud_project_user.this.id
}

output "access_key_id" {
  description = "Access key id of the user's S3 credential."
  value       = ovh_cloud_project_user_s3_credential.this.access_key_id
}

output "secret_access_key" {
  description = "Secret access key of the user's S3 credential; sensitive, never published."
  value       = ovh_cloud_project_user_s3_credential.this.secret_access_key
  sensitive   = true
}
