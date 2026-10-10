# State-backend component (spec 005 T016; FR-002, FR-004, FR-005, FR-008; research R1, R5
# *Per-tenant buckets* and *Protection*, R14): one protected, versioned state bucket and the S3
# users given, each with one credential and a policy confined to that bucket. Names and labels come
# from modules/naming (data-model *Derived names*). The bucket comes only from
# modules/object-storage-protected (literal `prevent_destroy`, versioning always on); `task
# test:dependencies` refuses any other bucket here (rule STATE_BUCKET_UNPROTECTED, G7).

locals {
  # Coordinator decision 2026-10-07 (T014 review): a state backend needs none of these, so each
  # user's policy denies them: purging old state versions, suspending versioning, and changing or
  # deleting the bucket. T094: only actions OVHcloud accepts, i.e. one of its 49 named actions or an
  # `s3:` pattern ending in `*` (400 answer of T010 run 20261010T172836Z-5bcc, recorded in
  # tests/fixtures/ovhcloud/s3-policy-actions.json; the named ones are also docs.ovhcloud.com
  # storage-and-backup/object-storage/s3-identity-and-access-management.mdx *List of supported
  # actions*). Named where a name exists. Object versions and bucket policy have no named action, so
  # they are denied by prefix patterns; that OVHcloud accepts these follows from its rule, what they
  # match when a request is evaluated is UNVERIFIED (re-run of T010 row 7, evidence/T094.md).
  # Bucket policies are not implemented on OVHcloud yet (same guide, *user policy evaluation*); the
  # two policy patterns keep the intent for when they are. Object lock: its configuration and the
  # governance-retention bypass, which would let a locked version be deleted.
  deny_actions = [
    "s3:DeleteObjectVersion*",
    "s3:BypassGovernanceRetention",
    "s3:PutBucketVersioning",
    "s3:PutBucketObjectLockConfiguration",
    "s3:PutLifecycleConfiguration",
    "s3:PutBucketPolicy*",
    "s3:DeleteBucketPolicy*",
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
  # T099: each state version locked (governance) for this many days.
  retention_days = var.state_retention_days
}

module "s3_user" {
  source       = "../../modules/object-storage-user"
  for_each     = var.s3_users
  project_id   = var.project_id
  description  = module.user_name[each.key].name
  buckets      = [module.bucket.name]
  deny_actions = local.deny_actions
}
