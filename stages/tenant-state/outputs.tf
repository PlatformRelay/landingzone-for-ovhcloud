# Outputs of the tenant-state stage (data-model *Per-stage values*, `tenant-state`; *Sensitive
# outputs*). Values are read from the component, not recomputed.

output "tenant" {
  description = "Tenant whose state bucket this is: the bucket's `lz:tenant` label."
  value       = module.state_backend.labels["lz:tenant"]
}

output "state_bucket" {
  description = "Tenant state bucket name."
  value       = module.state_backend.bucket
}

output "tenant_s3_user_id" {
  description = "Id of the tenant S3 user."
  value       = module.state_backend.s3_users["tenant"].id
}

output "platform_s3_user_id" {
  description = "Id of the platform S3 user of this tenant's bucket."
  value       = module.state_backend.s3_users["platform"].id
}

output "unlabelled" {
  description = "Addresses of the resources whose API carries no tags (research R14), from the stage root."
  value       = [for a in module.state_backend.unlabelled : "module.state_backend.${a}"]
}

output "tenant_s3" {
  description = "Tenant S3 credential `{access_key_id, secret_access_key}`; sensitive, never published."
  value       = module.state_backend.s3_credentials["tenant"]
  sensitive   = true
}

output "platform_s3" {
  description = "Platform S3 credential for this tenant's bucket `{access_key_id, secret_access_key}`; sensitive, never published."
  value       = module.state_backend.s3_credentials["platform"]
  sensitive   = true
}
