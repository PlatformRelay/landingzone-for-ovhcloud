output "user_id" {
  description = "Id of the S3 user."
  value       = null
}

output "access_key_id" {
  description = "Access key id of the user's S3 credential."
  value       = null
}

output "secret_access_key" {
  description = "Secret access key of the user's S3 credential; sensitive, never published."
  value       = null
  sensitive   = true
}
