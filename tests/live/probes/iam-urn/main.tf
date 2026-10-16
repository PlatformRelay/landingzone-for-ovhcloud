# Spec 003 T034 (US3; FR-006 C006.2, C006.5; FR-008 C008.1): which URN OVHcloud checks an
# `account:apiovh:iam/*` action against, the project's or the account's (D109; UNVERIFIED until
# T027). lz-live's check (tools/internal/probes/live/identity/urn.go), not this root, creates the
# two OAuth2 clients and policies lzprobe-iam-urn-<run id>-project|account, writes the tag key
# lzprobe-iam-urn:run-id on this root's bucket and removes it, and sweeps all of them by name.
# This root creates only the probe bucket, the write's one target: the retained project is never
# tagged.
locals {
  # Bucket names are lowercase and global (P21); the check's Sweep derives the name from the run
  # id and looks for it in this region (identity.URNBucket, identity.URNRegion).
  bucket = "lzprobe-iam-urn-${lower(local.run_id)}"
  region = "GRA"
  # The bucket's IAM resource URN. Its form is UNVERIFIED (the provider exports no URN for a
  # bucket, provider docs resources/cloud_project_storage.md; whether OVHcloud lists an S3 bucket
  # as an IAM resource at all is UNVERIFIED too). The check writes only when the admin's listing
  # shows a resource named the bucket with exactly this URN, and records the URNs it showed, so
  # T027 learns the form either way.
  bucket_urn = "urn:v1:eu:resource:storageS3:${local.project.service_name}/${local.bucket}"
}

# The account's URN for the policies (provider docs data-sources/me.md `urn`; the form
# urn:v1:eu:resource:account:<account> is the one the sandbox admin's policy names, spec 005
# evidence/T042.md).
data "ovh_me" "account" {}

resource "ovh_cloud_project_storage" "bucket" {
  service_name = local.project.service_name
  region_name  = local.region
  name         = local.bucket
  tags = {
    "lz:run-id" = local.run_id
  }
}

# The check's variables, read by lz-live from `tofu output -json` into memory only. All five are
# public (identity.IamURN Public): no secret passes through this root.
output "urn_check" {
  description = "Variables of lz-live's iam-urn check (spec 003 T034): run id, the project's and the account's URNs, the probe bucket and its URN."
  sensitive   = true
  value = {
    run_id      = local.run_id
    project_urn = local.project.iam.urn
    account_urn = data.ovh_me.account.urn
    bucket      = ovh_cloud_project_storage.bucket.name
    bucket_urn  = local.bucket_urn
  }
}
