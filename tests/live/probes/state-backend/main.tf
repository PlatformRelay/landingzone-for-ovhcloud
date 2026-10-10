# P1–P3, P13–P15 (T010): the infrastructure of an S3 state backend on OVHcloud Object Storage —
# a versioned probe bucket tagged with the run id, and a cloud project user with one S3
# credential whose S3 policy covers only that bucket (bucket-level ARNs as in
# cloud_project_user_s3_policy.md). The two state writers that observe `use_lockfile` and the
# encrypted state object (P1–P3) are not this root (storage-iam's companion runs them). The user's
# S3 credential serves lz-live's Deny check (T096, output deny_check, below).
locals {
  # Bucket names are lowercase and global (P21): the run id, lowercased, keeps them unique.
  bucket = "lzprobe-state-${lower(local.run_id)}"
}

resource "ovh_cloud_project_storage" "probe" {
  service_name = local.project.service_name
  region_name  = "GRA"
  name         = local.bucket
  versioning = {
    status = "enabled"
  }
  # T099 (D104): locked as a state bucket is (modules/object-storage-protected), for one day, the
  # minimum (docs.ovhcloud.com storage-and-backup/object-storage/s3-managing-object-lock.mdx:51):
  # the check observes the retention, and the janitor's bypass empties the bucket before the destroy.
  object_lock = {
    status = "enabled"
    rule = {
      mode   = "governance"
      period = "P1D"
    }
  }
  tags = {
    "lz:run-id" = local.run_id
  }
}

resource "ovh_cloud_project_user" "probe" {
  service_name = local.project.service_name
  description  = "lzprobe-state-${local.run_id}"
  role_names   = ["objectstore_operator"]
}

resource "ovh_cloud_project_user_s3_credential" "probe" {
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.probe.id
}

resource "ovh_cloud_project_user_s3_policy" "probe" {
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.probe.id
  policy = jsonencode({
    Statement = [
      {
        Sid      = "ProbeStateBucket"
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketVersions"]
        Resource = ["arn:aws:s3:::${local.bucket}", "arn:aws:s3:::${local.bucket}/*"]
      },
      # The state-backend users' Deny statement, verbatim (spec 005 T016, T094: components/state-backend
      # `deny_actions`, modules/object-storage-user `DenyGivenActions`). Run 20261010T172836Z-5bcc
      # refused the earlier list (400: an action outside OVHcloud's accepted-action rule,
      # tests/fixtures/ovhcloud/s3-policy-actions.json). Every action now matches that rule; the
      # three `*` patterns are accepted by the rule, what they match is UNVERIFIED (row 7a).
      {
        Sid    = "DenyGivenActions"
        Effect = "Deny"
        Action = [
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
        Resource = ["*"]
      },
    ]
  })
}

# T096: lz-live's check after the apply (tools/internal/probes/live/state) observes the Deny with
# the state user's credential, then undoes its writes with the janitor's before the destroy. The
# janitor exists only for that undo: the state user may not delete versions (if the Deny holds),
# and the admin has no S3 key. Its policy allows every S3 action on the probe bucket only (`s3:*`
# is an accepted pattern, tests/fixtures/ovhcloud/s3-policy-actions.json rule 2). T099: it also
# names s3:BypassGovernanceRetention, the permission the guide names for deleting a version locked
# in governance mode (s3-managing-object-lock.mdx:57-59, :241-255), since what `s3:*` matches is
# UNVERIFIED (T094).
resource "ovh_cloud_project_user" "janitor" {
  service_name = local.project.service_name
  description  = "lzprobe-state-janitor-${local.run_id}"
  role_names   = ["objectstore_operator"]
}

resource "ovh_cloud_project_user_s3_credential" "janitor" {
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.janitor.id
}

resource "ovh_cloud_project_user_s3_policy" "janitor" {
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.janitor.id
  policy = jsonencode({
    Statement = [{
      Sid      = "ProbeStateBucketJanitor"
      Effect   = "Allow"
      Action   = ["s3:*", "s3:BypassGovernanceRetention"]
      Resource = ["arn:aws:s3:::${local.bucket}", "arn:aws:s3:::${local.bucket}/*"]
    }]
  })
}

# The check's variables, read by lz-live from `tofu output -json` into memory only; every value
# but bucket and region is redacted from the run's output and record.
output "deny_check" {
  description = "Variables of lz-live's Deny check (T096): bucket, region and the state user's and the janitor's S3 keys."
  sensitive   = true
  value = {
    bucket         = ovh_cloud_project_storage.probe.name
    region         = ovh_cloud_project_storage.probe.region_name
    state_key_id   = ovh_cloud_project_user_s3_credential.probe.access_key_id
    state_secret   = ovh_cloud_project_user_s3_credential.probe.secret_access_key
    janitor_key_id = ovh_cloud_project_user_s3_credential.janitor.access_key_id
    janitor_secret = ovh_cloud_project_user_s3_credential.janitor.secret_access_key
  }
  # Published only once both policies exist (review r2): after an apply that failed on a policy,
  # the output stays absent, so lz-live neither observes nor undoes with a janitor that has no
  # rights, and the root is destroyed instead of kept for a cleanup that would meet the same 403.
  depends_on = [ovh_cloud_project_user_s3_policy.probe, ovh_cloud_project_user_s3_policy.janitor]
}

output "bucket" {
  description = "Probe state bucket name (leftover match: name prefix)."
  value       = ovh_cloud_project_storage.probe.name
}

output "bucket_region" {
  description = "Region of the probe state bucket."
  value       = ovh_cloud_project_storage.probe.region
}

output "user_description" {
  description = "Probe S3 user description (leftover match: description prefix)."
  value       = ovh_cloud_project_user.probe.description
}
