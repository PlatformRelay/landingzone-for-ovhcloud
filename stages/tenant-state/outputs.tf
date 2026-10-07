# Stub (spec 005 T021): the outputs T022 implements (data-model *Per-stage values*, `tenant-state`;
# *Sensitive outputs*); placeholder values.

output "tenant" {
  description = "Tenant whose state bucket this is."
  value       = ""
}

output "state_bucket" {
  description = "Tenant state bucket name."
  value       = ""
}

output "tenant_s3_user_id" {
  description = "Id of the tenant S3 user."
  value       = ""
}

output "platform_s3_user_id" {
  description = "Id of the platform S3 user of this tenant's bucket."
  value       = ""
}

output "unlabelled" {
  description = "Addresses of the resources whose API carries no tags (research R14)."
  value       = []
}

output "tenant_s3" {
  description = "Tenant S3 credential `{access_key_id, secret_access_key}`; sensitive, never published."
  value       = {}
  sensitive   = true
}

output "platform_s3" {
  description = "Platform S3 credential for this tenant's bucket `{access_key_id, secret_access_key}`; sensitive, never published."
  value       = {}
  sensitive   = true
}
