# Outputs of the state-backend component (spec 005 T015 interface). The bucket's project, region
# and tags and each user's policy are read from the resources through module outputs, not
# recomputed (T015 gap 3).

output "bucket" {
  description = "State bucket name (modules/naming: org, tenant, kind bucket, role state)."
  value       = module.bucket.name
}

output "region" {
  description = "Object Storage region of the bucket."
  value       = module.bucket.region
}

output "project_id" {
  description = "Project that holds the bucket and the users."
  value       = module.bucket.project_id
}

output "endpoint" {
  description = "S3 endpoint of the bucket's region (`https://s3.<region>.io.cloud.ovh.net`)."
  value       = "https://s3.${lower(module.bucket.region)}.io.cloud.ovh.net"
}

output "labels" {
  description = "Tags the bucket carries: the modules/naming labels of this scope."
  value       = module.bucket.tags
}

output "s3_users" {
  description = "Per given S3 user key: `id`, `description` and the `policy` document its S3 policy carries. No credential."
  value = { for k, u in module.s3_user : k => {
    id          = u.user_id
    description = module.user_name[k].name
    policy      = u.policy
  } }
}

output "s3_credentials" {
  description = "Per given S3 user key: `access_key_id` and `secret_access_key`; sensitive, never published."
  value = { for k, u in module.s3_user : k => {
    access_key_id     = u.access_key_id
    secret_access_key = u.secret_access_key
  } }
  sensitive = true
}

output "unlabelled" {
  description = "Addresses of the component's resources whose API carries no tags (S3 user, credential, policy; research R14)."
  value = flatten([for k in sort(var.s3_users) : [
    "module.s3_user[\"${k}\"].ovh_cloud_project_user.this",
    "module.s3_user[\"${k}\"].ovh_cloud_project_user_s3_credential.this",
    "module.s3_user[\"${k}\"].ovh_cloud_project_user_s3_policy.this",
  ]])
}
