# Outputs of the bootstrap stage (data-model *Per-stage values*, `bootstrap`; *Sensitive outputs*).

output "state_bucket" {
  description = "Account state bucket name."
  value       = module.state_backend.bucket
}

output "state_project_id" {
  description = "Project that holds the account state bucket."
  value       = module.state_backend.project_id
}

output "state_region" {
  description = "Object Storage region of the account state bucket."
  value       = module.state_backend.region
}

output "state_endpoint" {
  description = "S3 endpoint of the account state bucket."
  value       = module.state_backend.endpoint
}

output "platform_s3_user_id" {
  description = "Id of the platform S3 user."
  value       = module.state_backend.s3_users["platform"].id
}

output "unlabelled" {
  description = "Addresses of the resources whose API carries no tags (research R14), from the stage root."
  value       = [for a in module.state_backend.unlabelled : "module.state_backend.${a}"]
}

output "platform_s3" {
  description = "Platform S3 credential `{access_key_id, secret_access_key}`; sensitive, never published."
  value       = module.state_backend.s3_credentials["platform"]
  sensitive   = true
}
