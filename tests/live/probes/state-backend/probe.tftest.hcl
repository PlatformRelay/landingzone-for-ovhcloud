# Offline naming controls (T008): mocked provider, plan only, no credential, no API call.
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
  state_path       = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/state-backend.tfstate"
  state_passphrase = "offline-test-passphrase-not-a-secret"
  run_id           = "20261006T120000Z-ab12"
  project_id       = "0123456789abcdef0123456789abcdef"
}

run "names_carry_prefix_and_run_id" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.probe.name == "lzprobe-state-20261006t120000z-ab12" && ovh_cloud_project_user.probe.description == "lzprobe-state-20261006T120000Z-ab12"
    error_message = "bucket and user carry lzprobe-state-<run-id> (bucket lowercased)"
  }

  assert {
    condition     = ovh_cloud_project_storage.probe.tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12" }) && ovh_cloud_project_storage.probe.versioning.status == "enabled"
    error_message = "the bucket is versioned and tagged lz:run-id = <run-id>"
  }

  assert {
    condition     = jsondecode(ovh_cloud_project_user_s3_policy.probe.policy).Statement[0].Resource == ["arn:aws:s3:::lzprobe-state-20261006t120000z-ab12", "arn:aws:s3:::lzprobe-state-20261006t120000z-ab12/*"]
    error_message = "the S3 policy covers the probe bucket only"
  }
}

# Spec 005 T016: the probe user's policy carries the state-backend users' Deny statement verbatim
# (components/state-backend `deny_actions`, modules/object-storage-user `DenyGivenActions`), so T010
# observes whether OVHcloud accepts it and keeps every action, the three `s3:...*` patterns included
# (T094: the earlier list was refused by run 20261010T172836Z-5bcc; what the patterns match when a
# request is evaluated stays UNVERIFIED after that run).
run "policy_carries_the_state_backend_deny" {
  command = plan

  assert {
    condition = anytrue([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.probe.policy).Statement :
      s == {
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
      }
    ])
    error_message = "the probe policy carries the state-backend Deny statement exactly"
  }
}

# Spec 005 T096: lz-live's Deny check reads output deny_check; its janitor may do anything, but on
# the probe bucket only, and is named like every probe user (the leftover check matches lzprobe-).
run "deny_check_published" {
  command = plan

  assert {
    condition     = nonsensitive(sort(keys(output.deny_check))) == tolist(["bucket", "janitor_key_id", "janitor_secret", "region", "state_key_id", "state_secret"])
    error_message = "deny_check publishes exactly the six keys lz-live's check reads"
  }

  assert {
    condition     = nonsensitive(output.deny_check.bucket) == "lzprobe-state-20261006t120000z-ab12" && nonsensitive(output.deny_check.region) == "GRA"
    error_message = "deny_check names the probe bucket and its region"
  }

  # Review r2: the output waits for both policies (a plan cannot show depends_on, so the file is read).
  assert {
    condition     = output.bucket != "" && length(regexall("depends_on = \\[ovh_cloud_project_user_s3_policy.probe, ovh_cloud_project_user_s3_policy.janitor\\]\n}", file("main.tf"))) == 1
    error_message = "output deny_check depends on both S3 policies"
  }

  assert {
    condition     = ovh_cloud_project_user.janitor.description == "lzprobe-state-janitor-20261006T120000Z-ab12" && toset(ovh_cloud_project_user.janitor.role_names) == toset(["objectstore_operator"])
    error_message = "the janitor is a probe user (lzprobe-state-janitor-<run-id>)"
  }

  assert {
    condition = jsondecode(ovh_cloud_project_user_s3_policy.janitor.policy) == {
      Statement = [{
        Sid      = "ProbeStateBucketJanitor"
        Effect   = "Allow"
        Action   = ["s3:*"]
        Resource = ["arn:aws:s3:::lzprobe-state-20261006t120000z-ab12", "arn:aws:s3:::lzprobe-state-20261006t120000z-ab12/*"]
      }]
    }
    error_message = "the janitor's policy allows S3 on the probe bucket only"
  }

  assert {
    condition     = ovh_cloud_project_user_s3_policy.janitor.user_id == ovh_cloud_project_user.janitor.id && ovh_cloud_project_user_s3_credential.janitor.user_id == ovh_cloud_project_user.janitor.id
    error_message = "the janitor's credential and policy belong to the janitor"
  }
}

run "state_path_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/tests/live/probes/state-backend/terraform.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_with_run_id_inside_checkout_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/.local/live/20261006T120000Z-ab12/state-backend.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_of_another_root_refused" {
  command = plan

  variables {
    state_path = "/home/owner/.config/ovh-lz/accounts/xx1111-ovh/state/probes/20261006T120000Z-ab12/storage-iam.tfstate"
  }

  expect_failures = [var.state_path]
}

run "state_path_under_checkout_state_probes_refused" {
  command = plan

  variables {
    state_path = "/home/owner/lz-live/state/probes/20261006T120000Z-ab12/state-backend.tfstate"
  }

  expect_failures = [var.state_path]
}

# T074: run id and project come from lz-live (TF_VAR_run_id, TF_VAR_project_id), not derived.
run "run_id_other_than_state_path_refused" {
  command = plan

  variables {
    run_id = "20261006T120000Z-ffff"
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

# Three projects: the sandbox sorts between the others (set order), and another has its name.
run "project_selected_by_id_among_several" {
  command = plan

  override_data {
    target = data.ovh_cloud_projects.all
    values = {
      projects = [
        {
          service_name  = "fedcba9876543210fedcba9876543210"
          project_id    = "fedcba9876543210fedcba9876543210"
          project_name  = "other"
          description   = "other"
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
            urn          = "urn:v1:eu:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
            display_name = "other"
            tags         = {}
          }
        },
        {
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
        },
        {
          service_name  = "11111111111111111111111111111111"
          project_id    = "11111111111111111111111111111111"
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
            urn          = "urn:v1:eu:resource:publicCloudProject:11111111111111111111111111111111"
            display_name = "sandbox"
            tags         = {}
          }
        },
      ]
    }
  }

  assert {
    condition     = local.project.service_name == "0123456789abcdef0123456789abcdef" && local.project.iam.urn == "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "the probe targets the project of project_id, not the account's first, last or only one, nor another of the same name"
  }
}
