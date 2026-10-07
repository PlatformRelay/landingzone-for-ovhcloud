# State-backend component (spec 005 T016; FR-002, FR-004, FR-005, FR-008; research R1, R5
# *Per-tenant buckets* and *Protection*, R14): one protected, versioned state bucket and the S3
# users given, each with one credential and a policy confined to that bucket. Names and labels come
# from modules/naming (data-model *Derived names*). The bucket comes only from
# modules/object-storage-protected (literal `prevent_destroy`, versioning always on); `task
# test:dependencies` refuses any other bucket here (rule STATE_BUCKET_UNPROTECTED, G7).

locals {
  # Coordinator decision 2026-10-07 (T014 review): a state backend needs none of these, so each
  # user's policy denies them: purging old state versions, suspending versioning, and changing or
  # deleting the bucket. Names from docs.ovhcloud.com storage-and-backup/object-storage/
  # s3-identity-and-access-management.mdx *List of supported actions* (:263-317); DeleteObjectVersion,
  # PutBucketPolicy and DeleteBucketPolicy are not in that list, so whether OVHcloud accepts and
  # enforces a Deny on them is UNVERIFIED (T010, probe root tests/live/probes/state-backend).
  deny_actions = [
    "s3:DeleteObjectVersion",
    "s3:PutBucketVersioning",
    "s3:PutBucketPolicy",
    "s3:DeleteBucketPolicy",
    "s3:PutLifecycleConfiguration",
    "s3:PutBucketCORS",
    "s3:PutEncryptionConfiguration",
    "s3:PutBucketAcl",
    "s3:DeleteBucket",
  ]
}

module "bucket_name" {
  source     = "../../modules/naming"
  org        = var.org
  tenant     = var.tenant
  kind       = "bucket"
  role       = "state"
  instance   = var.instance
  managed_in = var.managed_in
}

module "user_name" {
  source     = "../../modules/naming"
  for_each   = var.s3_users
  org        = var.org
  tenant     = var.tenant
  kind       = "s3_user"
  role       = "state-${each.key}"
  instance   = var.instance
  managed_in = var.managed_in
}

module "bucket" {
  source     = "../../modules/object-storage-protected"
  project_id = var.project_id
  region     = var.region
  name       = module.bucket_name.name
  tags       = module.bucket_name.labels
}

module "s3_user" {
  source       = "../../modules/object-storage-user"
  for_each     = var.s3_users
  project_id   = var.project_id
  description  = module.user_name[each.key].name
  buckets      = [module.bucket.name]
  deny_actions = local.deny_actions
}
