# Spec 003 T036 (FR-005 C005.4, C005.8): the locked-recovery probe. A copy of state-backend's
# bucket, state user and janitor (spec 005 T010 row 7b, T096, T099): a versioned bucket locked in
# governance mode for one day, as the state buckets are (D104), a state user whose S3 policy covers
# only that bucket and carries the state-backend users' Deny statement verbatim, and a janitor
# that may empty it with the governance bypass. The root creates nothing else: lz-live's check
# (tools/internal/probes/live/state/recovery.go) runs the pinned tofu on the disposable backend
# root backend/ (an S3 backend with use_lockfile on this bucket, the state user's key pair the only
# credential its child holds) to kill a writer holding the lock, break the lock with
# `tofu force-unlock` and acquire it again, and with the state user's key it observes a conditional
# PUT and a version restore. The janitor's undo empties the bucket before the destroy.
locals {
  # Bucket names are lowercase and global (P21): the run id, lowercased, keeps them unique.
  bucket = "lzprobe-locked-recovery-${lower(local.run_id)}"
}

resource "ovh_cloud_project_storage" "probe" {
  service_name = local.project.service_name
  region_name  = "GRA"
  name         = local.bucket
  versioning = {
    status = "enabled"
  }
  # As a state bucket is (modules/object-storage-protected, D104), for one day, the minimum
  # (docs.ovhcloud.com storage-and-backup/object-storage/s3-managing-object-lock.mdx:51): every
  # version the check or tofu writes (the lock object included) is retained a day unless the
  # janitor deletes it with the bypass.
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
  description  = "lzprobe-locked-recovery-${local.run_id}"
  role_names   = ["objectstore_operator"]
}

resource "ovh_cloud_project_user_s3_credential" "probe" {
  service_name = local.project.service_name
  user_id      = ovh_cloud_project_user.probe.id
}

# The state credential: state-backend's probe policy (tests/live/probes/state-backend/main.tf), the
# bucket renamed. The S3 backend's lockfile needs GetObject, PutObject and DeleteObject on the lock
# object (OpenTofu 1.13.0 internal/backend/remote-state/s3/client.go:313-345, :524-557); the restore
# needs GetObject on the source version and PutObject (whether OVHcloud authorises a copy by version
# id without s3:GetObjectVersion, a name it refuses, is UNVERIFIED: T028 observes it).
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

# The janitor exists only for the check's undo: the state user may not delete versions, and the
# admin has no S3 key. As state-backend's janitor (T096, T099): every S3 action and the governance
# bypass, on the probe bucket only.
resource "ovh_cloud_project_user" "janitor" {
  service_name = local.project.service_name
  description  = "lzprobe-locked-recovery-janitor-${local.run_id}"
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
      Sid      = "ProbeLockedRecoveryJanitor"
      Effect   = "Allow"
      Action   = ["s3:*", "s3:BypassGovernanceRetention"]
      Resource = ["arn:aws:s3:::${local.bucket}", "arn:aws:s3:::${local.bucket}/*"]
    }]
  })
}

# The check's variables, read by lz-live from `tofu output -json` into memory only; every value
# but run_id, bucket and region is redacted from the run's output and record. The state key pair
# is the one published value lz-live hands a child: the tofu seam on backend/ (ProbeCheck).
output "recovery_check" {
  description = "Variables of lz-live's locked-recovery check (spec 003 T036): run id, bucket, region and the state user's and the janitor's S3 keys."
  sensitive   = true
  value = {
    run_id         = local.run_id
    bucket         = ovh_cloud_project_storage.probe.name
    region         = ovh_cloud_project_storage.probe.region_name
    state_key_id   = ovh_cloud_project_user_s3_credential.probe.access_key_id
    state_secret   = ovh_cloud_project_user_s3_credential.probe.secret_access_key
    janitor_key_id = ovh_cloud_project_user_s3_credential.janitor.access_key_id
    janitor_secret = ovh_cloud_project_user_s3_credential.janitor.secret_access_key
  }
  # Published only once both policies exist (as state-backend's, review r2 of T096): without them
  # the check would observe with a state user and a janitor that have no rights.
  depends_on = [ovh_cloud_project_user_s3_policy.probe, ovh_cloud_project_user_s3_policy.janitor]
}

output "bucket" {
  description = "Probe bucket name (leftover match: name prefix)."
  value       = ovh_cloud_project_storage.probe.name
}

output "user_description" {
  description = "Probe state user description (leftover match: description prefix)."
  value       = ovh_cloud_project_user.probe.description
}
