# Stub (spec 005 T015): the outputs T016 implements; placeholder values, no resource yet.

output "bucket" {
  description = "State bucket name (modules/naming: org, tenant, kind bucket, role state)."
  value       = ""
}

output "region" {
  description = "Object Storage region of the bucket."
  value       = ""
}

output "project_id" {
  description = "Project that holds the bucket and the users."
  value       = ""
}

output "endpoint" {
  description = "S3 endpoint of the bucket's region (`https://s3.<region>.io.cloud.ovh.net`)."
  value       = ""
}

output "labels" {
  description = "Tags the bucket carries: the modules/naming labels of this scope."
  value       = {}
}

output "s3_users" {
  description = "Per given S3 user key: `id`, `description` and the `policy` document its S3 policy carries. No credential."
  value       = { for k in var.s3_users : k => { id = "", description = "", policy = jsonencode({ Statement = [] }) } }
}

output "s3_credentials" {
  description = "Per given S3 user key: `access_key_id` and `secret_access_key`; sensitive, never published."
  value       = { for k in var.s3_users : k => { access_key_id = "", secret_access_key = "" } }
  sensitive   = true
}

output "unlabelled" {
  description = "Addresses of the component's resources whose API carries no tags (S3 user, credential, policy; research R14)."
  value       = []
}
