# P1–P3, P13–P15 (T010): the infrastructure of an S3 state backend on OVHcloud Object Storage —
# a versioned probe bucket tagged with the run id, and a cloud project user with one S3
# credential whose S3 policy covers only that bucket (bucket-level ARNs as in
# cloud_project_user_s3_policy.md). The two state writers that observe `use_lockfile` and the
# encrypted state object (P1–P3) are not this root: they need the user's S3 credential, which this
# root keeps in its encrypted state only (see the run sheet, *Open: second stage*).
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
        Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketVersions", "s3:GetObjectVersion"]
        Resource = ["arn:aws:s3:::${local.bucket}", "arn:aws:s3:::${local.bucket}/*"]
      },
      # The state-backend users' Deny statement, verbatim (spec 005 T016: components/state-backend
      # `deny_actions`, modules/object-storage-user `DenyGivenActions`). DeleteObjectVersion,
      # PutBucketPolicy and DeleteBucketPolicy are not in OVHcloud's supported-action list
      # (s3-identity-and-access-management.mdx:263-317): whether OVHcloud accepts the policy is
      # what this run observes (UNVERIFIED until T010).
      {
        Sid    = "DenyGivenActions"
        Effect = "Deny"
        Action = [
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
        Resource = ["*"]
      },
    ]
  })
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
