# Stub (spec 005 T015): the outputs T016 implements (data-model *Per-stage values*, `bootstrap`);
# placeholder values.

output "state_bucket" {
  description = "Account state bucket name."
  value       = ""
}

output "state_project_id" {
  description = "Project that holds the account state bucket."
  value       = ""
}

output "state_region" {
  description = "Object Storage region of the account state bucket."
  value       = ""
}

output "state_endpoint" {
  description = "S3 endpoint of the account state bucket."
  value       = ""
}

output "platform_s3_user_id" {
  description = "Id of the platform S3 user."
  value       = ""
}

output "unlabelled" {
  description = "Addresses of the resources whose API carries no tags (research R14)."
  value       = []
}

output "platform_s3" {
  description = "Platform S3 credential `{access_key_id, secret_access_key}`; sensitive, never published."
  value       = {}
  sensitive   = true
}
