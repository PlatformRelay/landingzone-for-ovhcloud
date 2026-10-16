# Spec 003 T030 (US2; FR-005 C005.1, FR-006 C006.4, C006.5): whether a tenant S3 user's cloud
# project (OpenStack) user reaches the other tenant's bucket through Keystone and Swift, bypassing
# the bucket-scoped S3 policy (spec 005 G6; UNVERIFIED, as is whether Swift serves S3-API buckets).
# Two buckets, each with a tenant user (an S3 credential and a policy on its own bucket only, the
# tenant S3 user pattern of spec 005 T049 and state-backend) and a cleanup user whose S3 policy
# lists and deletes on that bucket only and writes its seed, independent of the tenant users.
# lz-live's check (tools/internal/probes/live/storageiam/swiftreach.go) writes the seeds with the
# cleanup keys (the provider has no object resource: provider docs resources/), observes both
# directions through Swift with the tenant users' passwords, undoes its writes with the cleanup
# keys, and sweeps the buckets and users by their exact names. The destroy removes the seeds with
# the buckets (the provider empties a bucket first, provider docs
# resources/cloud_project_storage.md:10).
locals {
  # Bucket names are lowercase and global (P21): the run id, lowercased, keeps them unique; the
  # users' descriptions use the same prefix, so the check's Sweep derives both from the run id.
  prefix = "lzprobe-swift-reach-${lower(local.run_id)}"
  sides  = toset(["a", "b"])
  region = "GRA"
  # The object every read step reads, written by the check's first step (seed_key).
  seed_key = "lzprobe-swift-reach/seed"
  # OVHcloud's Keystone v3 endpoint: docs.ovhcloud.com storage-and-backup/object-storage/
  # pcs-curl-commands-memo.mdx:15 and manage-and-operate/iam/
  # authenticate-api-openstack-with-service-account.mdx:87. The check refuses any other.
  auth_url = "https://auth.cloud.ovh.net/v3"
  # tenant_id below is the Keystone project id, which is the Public Cloud project's service_name:
  # the OpenStack guides scope a token to OS_TENANT_ID and call GET /cloud/project/{serviceName}
  # with the same value (docs.ovhcloud.com storage-and-backup/object-storage/
  # pca-curl-commands-memo.mdx:20, public-cloud/containers-orchestration/managed-kubernetes/
  # vrack-custom-gateway.mdx:110-119; how-to-use-terraform.mdx:370 "service name / OS_PROJECT_ID").
  # That guide also labels a project_id variable "Project Tenant ID", but the record's project_id
  # attribute was empty on the live run 20261016T145959Z-42fd (spec 003 T039). Documentation only,
  # until T025's Keystone login: the check refuses an empty or malformed tenant_id, and a wrong one
  # fails at Keystone login.
}

resource "ovh_cloud_project_storage" "bucket" {
  for_each     = local.sides
  service_name = local.project.service_name
  region_name  = local.region
  name         = "${local.prefix}-${each.key}"
  tags = {
    "lz:run-id" = local.run_id
  }
}

# The tenant users under test: objectstore_operator, as a tenant S3 user is (state-backend), and
# an S3 policy on the own bucket only (bucket-level ARNs, cloud_project_user_s3_policy.md). Their
# S3 credentials are not published: they exist so each user is the tenant S3 user pattern whose
# Keystone reach the check observes (tasks.md T030).
resource "ovh_cloud_project_user" "tenant" {
  for_each     = local.sides
  service_name = local.project.service_name
  description  = "${local.prefix}-${each.key}"
  role_names   = ["objectstore_operator"]
}

resource "ovh_cloud_project_user_s3_credential" "tenant" {
  for_each     = local.sides
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.tenant[each.key].id
}

resource "ovh_cloud_project_user_s3_policy" "tenant" {
  for_each     = local.sides
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.tenant[each.key].id
  policy = jsonencode({
    Statement = [{
      Sid      = "TenantOwnBucket"
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation"]
      Resource = ["arn:aws:s3:::${ovh_cloud_project_storage.bucket[each.key].name}", "arn:aws:s3:::${ovh_cloud_project_storage.bucket[each.key].name}/*"]
    }]
  })
}

# The cleanup users: Undo's credentials (list and delete on one bucket), independent of the tenant
# users, and the seed's writer (PutObject on the seed key only).
resource "ovh_cloud_project_user" "cleanup" {
  for_each     = local.sides
  service_name = local.project.service_name
  description  = "${local.prefix}-${each.key}-cleanup"
  role_names   = ["objectstore_operator"]
}

resource "ovh_cloud_project_user_s3_credential" "cleanup" {
  for_each     = local.sides
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.cleanup[each.key].id
}

resource "ovh_cloud_project_user_s3_policy" "cleanup" {
  for_each     = local.sides
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.cleanup[each.key].id
  policy = jsonencode({
    Statement = [
      # Each action valid under OVHcloud's accepted-action rule
      # (tests/fixtures/ovhcloud/s3-policy-actions.json; s3:DeleteObjectVersion is not). The
      # buckets are unversioned, so Undo deletes by key.
      {
        Sid      = "CleanupListDelete"
        Effect   = "Allow"
        Action   = ["s3:ListBucket", "s3:ListBucketVersions", "s3:DeleteObject"]
        Resource = ["arn:aws:s3:::${ovh_cloud_project_storage.bucket[each.key].name}", "arn:aws:s3:::${ovh_cloud_project_storage.bucket[each.key].name}/*"]
      },
      {
        Sid      = "CleanupSeed"
        Effect   = "Allow"
        Action   = ["s3:PutObject"]
        Resource = ["arn:aws:s3:::${ovh_cloud_project_storage.bucket[each.key].name}/${local.seed_key}"]
      },
    ]
  })
}

# The check's variables, read by lz-live from `tofu output -json` into memory only; every value
# but the public ones (storageiam.SwiftReach Public) is redacted from the run's output and record.
output "swift_check" {
  description = "Variables of lz-live's swift-reach check (spec 003 T030): Keystone URL, project, region, buckets, seed key, the tenant users' names and passwords, the cleanup users' S3 keys."
  sensitive   = true
  value = {
    run_id           = local.run_id
    auth_url         = local.auth_url
    region           = local.region
    tenant_id        = local.project.service_name
    seed_key         = local.seed_key
    bucket_a         = ovh_cloud_project_storage.bucket["a"].name
    bucket_b         = ovh_cloud_project_storage.bucket["b"].name
    user_a_name      = ovh_cloud_project_user.tenant["a"].username
    user_a_password  = ovh_cloud_project_user.tenant["a"].password
    user_b_name      = ovh_cloud_project_user.tenant["b"].username
    user_b_password  = ovh_cloud_project_user.tenant["b"].password
    cleanup_a_key_id = ovh_cloud_project_user_s3_credential.cleanup["a"].access_key_id
    cleanup_a_secret = ovh_cloud_project_user_s3_credential.cleanup["a"].secret_access_key
    cleanup_b_key_id = ovh_cloud_project_user_s3_credential.cleanup["b"].access_key_id
    cleanup_b_secret = ovh_cloud_project_user_s3_credential.cleanup["b"].secret_access_key
  }
  # Published only once every policy exists (as state-backend's deny_check): after an apply that
  # failed on a policy, the output stays absent, so lz-live neither observes with a tenant policy
  # missing nor undoes with a cleanup user that has no rights; the root is destroyed instead.
  depends_on = [ovh_cloud_project_user_s3_policy.tenant, ovh_cloud_project_user_s3_policy.cleanup]
}

output "buckets" {
  description = "Probe bucket names (leftover match: name prefix)."
  value       = sort([for b in ovh_cloud_project_storage.bucket : b.name])
}

output "user_descriptions" {
  description = "Probe user descriptions (leftover match: description prefix)."
  value       = sort(concat([for u in ovh_cloud_project_user.tenant : u.description], [for u in ovh_cloud_project_user.cleanup : u.description]))
}
