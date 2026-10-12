# P9, P26 (T010): a probe tenant identity — an OAuth2 client bound by an IAM policy holding exactly
# the narrowed tenant-deployer allowlist of research R6 (action names verified in
# kb/api/v1/cloud.json, 2026-10-06) on the sandbox project URN. The same identity makes the P26
# binding call (GET /auth/details) and is denied GET /me.
# P25 (T010, optional): two probe buckets that differ only in an `lz:tenant` tag, and a second
# probe client whose policy allows region/storage/get only under a resource-tag condition naming
# bucket A's tag. A kept separate from the P9 identity, whose unconditional region/storage/* would
# hide the condition.
# P1–P3 (T010): the companion's state bucket lzprobe-p1-<run-id> and a cloud project user whose one
# S3 credential and bucket-scoped S3 policy (as state-backend's) let the companion's S3 backend
# write its state and lock file there. The companion's destroy leaves its (empty) state object in
# the bucket; the provider removes a bucket's objects before deleting it
# (resources/cloud_project_storage.md:10, kb; believed for 2.21.0).
# The identities are exercised by the companion root (companion/, T075) and by lz-live's storage-iam
# check (T100): a provider configured from a resource created in the same run is not supported, so
# this root publishes the tenant identity and the S3 key as the sensitive output companion_env,
# which lz-live hands to the companion in place of the admin credential, and every identity as the
# sensitive output iam_check, which lz-live's check reads into memory.
locals {
  name = "lzprobe-storage-iam-${local.run_id}"

  tenant_allow = [
    "publicCloudProject:apiovh:network/private/create",
    "publicCloudProject:apiovh:network/private/get",
    "publicCloudProject:apiovh:network/private/edit",
    "publicCloudProject:apiovh:network/private/delete",
    "publicCloudProject:apiovh:network/private/region/create",
    "publicCloudProject:apiovh:network/private/subnet/create",
    "publicCloudProject:apiovh:network/private/subnet/get",
    "publicCloudProject:apiovh:network/private/subnet/delete",
    "publicCloudProject:apiovh:region/storage/create",
    "publicCloudProject:apiovh:region/storage/get",
    "publicCloudProject:apiovh:region/storage/edit",
    "publicCloudProject:apiovh:region/storage/delete",
    "publicCloudProject:apiovh:region/storage/bulkDeleteObjects",
    "publicCloudProject:apiovh:region/storage/object/get",
  ]

  buckets = {
    a = "lzprobe-p25a-${lower(local.run_id)}"
    b = "lzprobe-p25b-${lower(local.run_id)}"
  }

  # The companion's backend names the same bucket (companion/probe.tf).
  state_bucket = "lzprobe-p1-${lower(local.run_id)}"
}

resource "ovh_me_api_oauth2_client" "tenant" {
  name        = local.name
  description = "${local.name}: P9/P26 probe tenant identity, destroyed by the run"
  flow        = "CLIENT_CREDENTIALS"
}

resource "ovh_iam_policy" "tenant" {
  name        = local.name
  description = "${local.name}: narrowed tenant-deployer allowlist (research R6)"
  identities  = [ovh_me_api_oauth2_client.tenant.identity]
  resources   = [local.project.iam.urn]
  allow       = local.tenant_allow
}

resource "ovh_cloud_project_storage" "p25" {
  for_each = local.buckets

  service_name = local.project.service_name
  region_name  = "GRA"
  name         = each.value
  tags = {
    "lz:run-id" = local.run_id
    "lz:tenant" = "lzprobe-${each.key}"
  }
}

resource "ovh_me_api_oauth2_client" "p25" {
  name        = "lzprobe-p25-${local.run_id}"
  description = "lzprobe-p25-${local.run_id}: P25 tag-conditioned probe identity, destroyed by the run"
  flow        = "CLIENT_CREDENTIALS"
}

resource "ovh_iam_policy" "p25" {
  name        = "lzprobe-p25-${local.run_id}"
  description = "lzprobe-p25-${local.run_id}: region/storage/get only where lz:tenant = lzprobe-a"
  identities  = [ovh_me_api_oauth2_client.p25.identity]
  resources   = [local.project.iam.urn]
  allow       = ["publicCloudProject:apiovh:region/storage/get"]

  conditions {
    operator = "MATCH"
    values = {
      "resource.Tag(lz:tenant)" = "lzprobe-a"
    }
  }
}

resource "ovh_cloud_project_storage" "p1" {
  service_name = local.project.service_name
  region_name  = "GRA"
  name         = local.state_bucket
  tags = {
    "lz:run-id" = local.run_id
  }
}

resource "ovh_cloud_project_user" "p1" {
  service_name = local.project.service_name
  description  = "lzprobe-p1-${local.run_id}"
  role_names   = ["objectstore_operator"]
}

resource "ovh_cloud_project_user_s3_credential" "p1" {
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.p1.id
}

resource "ovh_cloud_project_user_s3_policy" "p1" {
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.p1.id
  policy = jsonencode({
    Statement = [{
      Sid      = "CompanionStateBucket"
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation"]
      Resource = ["arn:aws:s3:::${local.state_bucket}", "arn:aws:s3:::${local.state_bucket}/*"]
    }]
  })
}

# The companion's variables (lz-live: companion_env, written 0600 by files.go, never in the
# checkout, removed on exit). Keys are environment variables of the companion child.
output "companion_env" {
  description = "Credentials of the probe tenant identity and the P1 S3 user for the companion root, in place of the admin credential."
  sensitive   = true
  value = {
    OVH_CLIENT_ID         = ovh_me_api_oauth2_client.tenant.client_id
    OVH_CLIENT_SECRET     = ovh_me_api_oauth2_client.tenant.client_secret
    AWS_ACCESS_KEY_ID     = ovh_cloud_project_user_s3_credential.p1.access_key_id
    AWS_SECRET_ACCESS_KEY = ovh_cloud_project_user_s3_credential.p1.secret_access_key
  }
}

# The variables of lz-live's storage-iam check (T100, tools/internal/probes/live/storageiam): read by
# lz-live from `tofu output -json` into memory only, every value but the public ones (bucket_a,
# bucket_b, project_id, project_urn, region, run_id, state_bucket, state_key, tenant_identity)
# redacted. After the companion's apply the check reads bucket A and B with the P25 identity, calls
# GET /me and attempts POST /v2/iam/policy with the tenant identity, and reads the companion's state
# object back with the P1 S3 key; an answer other than the expected one fails the run.
output "iam_check" {
  description = "Variables of lz-live's storage-iam check: the probe identities, the P1 S3 key and what they act on."
  sensitive   = true
  value = {
    bucket_a             = ovh_cloud_project_storage.p25["a"].name
    bucket_b             = ovh_cloud_project_storage.p25["b"].name
    project_id           = local.project.service_name
    project_urn          = local.project.iam.urn
    region               = "GRA"
    run_id               = local.run_id
    state_bucket         = ovh_cloud_project_storage.p1.name
    state_key            = "storage-iam-companion.tfstate"
    tenant_identity      = ovh_me_api_oauth2_client.tenant.identity
    tenant_client_id     = ovh_me_api_oauth2_client.tenant.client_id
    tenant_client_secret = ovh_me_api_oauth2_client.tenant.client_secret
    p25_client_id        = ovh_me_api_oauth2_client.p25.client_id
    p25_client_secret    = ovh_me_api_oauth2_client.p25.client_secret
    s3_key_id            = ovh_cloud_project_user_s3_credential.p1.access_key_id
    s3_secret            = ovh_cloud_project_user_s3_credential.p1.secret_access_key
  }
}

output "tenant_client_identity" {
  description = "Identity URN of the P9 probe tenant identity."
  value       = ovh_me_api_oauth2_client.tenant.identity
}

output "p25_client_identity" {
  description = "Identity URN of the P25 probe identity."
  value       = ovh_me_api_oauth2_client.p25.identity
}

output "buckets" {
  description = "P25 probe bucket names by tenant tag suffix (leftover match: name prefix)."
  value       = { for k, b in ovh_cloud_project_storage.p25 : k => b.name }
}
