# Offline controls (spec 003 T036): mocked provider, plan only, no credential, no API call. The
# root is a copy of state-backend's bucket, state user and janitor; these runs pin the names lz-live's
# check and Sweep derive from the run id, the lock, the policies and the published variables.
mock_provider "ovh" {
  mock_data "ovh_cloud_projects" {
    defaults = {
      projects = [{
        service_name  = "0123456789abcdef0123456789abcdef"
        project_id    = "0123456789abcdef0123456789abcdef"
        project_name  = "sandbox"
        description   = "sandbox"
        status        = "ok"
        access        = "full"
        creation_date = "2026-09-01T00:00:00Z"
        expiration    = ""
        manual_quota  = false
        order_id      = 1
        plan_code     = "project.2018"
        unleash       = true
        iam = {
          id           = "00000000-0000-0000-0000-000000000000"
          urn          = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
          display_name = "sandbox"
          tags         = {}
        }
      }]
    }
  }
}

variables {
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261016T111500Z-7c3d/locked-recovery.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
  run_id           = "20261016T111500Z-7c3d"
  project_id       = "0123456789abcdef0123456789abcdef"
}

# The names Sweep lists by (recovery.go RecoveryBucket, RecoveryUsers).
run "names_carry_prefix_and_run_id" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.probe.name == "lzprobe-locked-recovery-20261016t111500z-7c3d" && ovh_cloud_project_storage.probe.region_name == "GRA"
    error_message = "the bucket is lzprobe-locked-recovery-<lower(run-id)> in GRA"
  }

  assert {
    condition     = ovh_cloud_project_user.probe.description == "lzprobe-locked-recovery-20261016T111500Z-7c3d" && ovh_cloud_project_user.janitor.description == "lzprobe-locked-recovery-janitor-20261016T111500Z-7c3d"
    error_message = "the users are lzprobe-locked-recovery-<run-id> and lzprobe-locked-recovery-janitor-<run-id>"
  }

  assert {
    condition     = toset(ovh_cloud_project_user.probe.role_names) == toset(["objectstore_operator"]) && toset(ovh_cloud_project_user.janitor.role_names) == toset(["objectstore_operator"])
    error_message = "both users are object storage operators only"
  }

  assert {
    condition     = ovh_cloud_project_storage.probe.tags == tomap({ "lz:run-id" = "20261016T111500Z-7c3d" })
    error_message = "the bucket is tagged lz:run-id = <run-id>"
  }
}

# D104: locked as a state bucket is, for one day (s3-managing-object-lock.mdx:51), versioned.
run "bucket_locked_in_governance_mode_for_a_day" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.probe.versioning.status == "enabled" && ovh_cloud_project_storage.probe.object_lock.status == "enabled"
    error_message = "the bucket is versioned with Object Lock"
  }

  assert {
    condition     = ovh_cloud_project_storage.probe.object_lock.rule.mode == "governance" && ovh_cloud_project_storage.probe.object_lock.rule.period == "P1D"
    error_message = "the default retention is governance, one day"
  }
}

# The state credential is state-backend's probe state user: the bucket only, and the state-backend
# users' Deny statement verbatim (spec 005 T016, T094), so the unlock and the restore run with no
# more than a state user may do.
run "state_user_is_the_state_backend_one" {
  command = plan

  assert {
    condition = jsondecode(ovh_cloud_project_user_s3_policy.probe.policy) == {
      Statement = [
        {
          Sid      = "ProbeStateBucket"
          Effect   = "Allow"
          Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketVersions"]
          Resource = ["arn:aws:s3:::lzprobe-locked-recovery-20261016t111500z-7c3d", "arn:aws:s3:::lzprobe-locked-recovery-20261016t111500z-7c3d/*"]
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
    }
    error_message = "the state user's policy is state-backend's: the probe bucket only, with the Deny statement"
  }

  assert {
    condition     = ovh_cloud_project_user_s3_policy.probe.user_id == ovh_cloud_project_user.probe.id && ovh_cloud_project_user_s3_credential.probe.user_id == ovh_cloud_project_user.probe.id
    error_message = "the state credential and policy belong to the state user"
  }
}

run "janitor_empties_the_bucket_only" {
  command = plan

  assert {
    condition = jsondecode(ovh_cloud_project_user_s3_policy.janitor.policy) == {
      Statement = [{
        Sid      = "ProbeLockedRecoveryJanitor"
        Effect   = "Allow"
        Action   = ["s3:*", "s3:BypassGovernanceRetention"]
        Resource = ["arn:aws:s3:::lzprobe-locked-recovery-20261016t111500z-7c3d", "arn:aws:s3:::lzprobe-locked-recovery-20261016t111500z-7c3d/*"]
      }]
    }
    error_message = "the janitor's policy allows S3, the governance bypass named, on the probe bucket only"
  }

  assert {
    condition     = ovh_cloud_project_user_s3_policy.janitor.user_id == ovh_cloud_project_user.janitor.id && ovh_cloud_project_user_s3_credential.janitor.user_id == ovh_cloud_project_user.janitor.id
    error_message = "the janitor's credential and policy belong to the janitor"
  }
}

# lz-live's check reads output recovery_check (recovery.go recEnvKeys); it waits for both policies
# (a plan cannot show depends_on, so the file is read).
run "recovery_check_published" {
  command = plan

  assert {
    condition     = nonsensitive(sort(keys(output.recovery_check))) == tolist(["bucket", "janitor_key_id", "janitor_secret", "region", "run_id", "state_key_id", "state_secret"])
    error_message = "recovery_check publishes exactly the seven keys lz-live's check reads"
  }

  assert {
    condition     = nonsensitive(output.recovery_check.run_id) == "20261016T111500Z-7c3d" && nonsensitive(output.recovery_check.bucket) == "lzprobe-locked-recovery-20261016t111500z-7c3d" && nonsensitive(output.recovery_check.region) == "GRA"
    error_message = "recovery_check names the run, the probe bucket and its region"
  }

  assert {
    condition     = output.bucket != "" && length(regexall("depends_on = \\[ovh_cloud_project_user_s3_policy.probe, ovh_cloud_project_user_s3_policy.janitor\\]\n}", file("main.tf"))) == 1
    error_message = "output recovery_check depends on both S3 policies"
  }
}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/locked-recovery/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_of_another_root_refused" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261016T111500Z-7c3d/state-backend.tfstate"
  }

  expect_failures = [var.state_path]
}

run "run_id_other_than_state_path_refused" {
  command = plan

  variables {
    run_id = "20261016T111500Z-ffff"
  }

  expect_failures = [var.run_id]
}

run "project_not_on_account_refused" {
  command = plan

  variables {
    project_id = "ffffffffffffffffffffffffffffffff"
  }

  expect_failures = [data.ovh_cloud_projects.all]
}
