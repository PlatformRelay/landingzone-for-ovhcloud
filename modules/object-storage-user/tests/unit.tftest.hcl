# Bucket-scoped S3 user module (spec 005 T013; FR-002, FR-004, FR-013; ADR-0003, ADR-0008,
# ADR-0009; research R5 *Per-tenant buckets*). Mocked provider, no credential, no API call.
# Attribute names from the pinned ovh 2.21.0 schema (`ovh_cloud_project_user`: service_name,
# description, role_name, role_names; `ovh_cloud_project_user_s3_credential`: user_id,
# access_key_id, secret_access_key (sensitive); `ovh_cloud_project_user_s3_policy`: user_id, policy).
# The description is a modules/naming output (kind `s3_user`: `lz-demo-s3u-state`).
#
# The S3 policy grants only on the given buckets: the Resource of every Allow statement together is
# exactly `arn:aws:s3:::<bucket>` and `arn:aws:s3:::<bucket>/*` per given bucket (ARN form and the
# read-write action set as in docs.ovhcloud.com storage-and-backup/object-storage/
# s3-identity-and-access-management.mdx:116-125 and cloud_project_user_s3_policy.md), no wildcard
# action, no NotResource or NotAction. Deny statements are not constrained (they only narrow).
# The actions an S3 state backend with `use_lockfile` needs: s3:GetObject, s3:PutObject,
# s3:DeleteObject, s3:ListBucket (s3-conditional-writes.mdx:287: no permission beyond PutObject and
# DeleteObject for conditional writes). Every allowed action is an object or listing action of the
# read-write example (plus the version reads a versioned state bucket needs): no bucket
# configuration action (versioning, lifecycle, policy, deletion) and no s3:DeleteObjectVersion, so
# the credential cannot undo the protected bucket's versioning or purge old state versions.
# The secret is checked against the module's other outputs; an output added later is not seen
# here (outputs cannot be enumerated in a test).

mock_provider "ovh" {}

variables {
  project_id  = "0123456789abcdef0123456789abcdef"
  description = "lz-demo-s3u-state"
  buckets     = ["lz-demo-bkt-state"]
}

run "user_objectstore_operator_only" {
  command = plan

  assert {
    condition     = ovh_cloud_project_user.this.role_names == tolist(["objectstore_operator"])
    error_message = "the S3 user has the objectstore_operator role and no other"
  }

  assert {
    condition     = ovh_cloud_project_user.this.role_name == null
    error_message = "no second role through role_name"
  }

  assert {
    condition     = ovh_cloud_project_user.this.description == "lz-demo-s3u-state" && ovh_cloud_project_user.this.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "the user carries the given description in the given project"
  }
}

run "credential_and_policy_belong_to_the_user" {
  command = apply

  assert {
    condition     = ovh_cloud_project_user_s3_credential.this.user_id == ovh_cloud_project_user.this.id && ovh_cloud_project_user_s3_credential.this.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "the S3 credential is the user's, in the given project"
  }

  assert {
    condition     = ovh_cloud_project_user_s3_policy.this.user_id == ovh_cloud_project_user.this.id && ovh_cloud_project_user_s3_policy.this.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "the S3 policy is the user's, in the given project"
  }
}

run "policy_allows_only_the_given_bucket" {
  command = plan

  assert {
    condition = toset(flatten([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : lookup(s, "Resource", []) if lookup(s, "Effect", "") == "Allow"
      ])) == toset([
      "arn:aws:s3:::lz-demo-bkt-state",
      "arn:aws:s3:::lz-demo-bkt-state/*",
    ])
    error_message = "Allow statements cover exactly the given bucket and its objects"
  }

  assert {
    condition = alltrue([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : !contains(keys(s), "NotResource") && !contains(keys(s), "NotAction") if lookup(s, "Effect", "") == "Allow"
    ])
    error_message = "no Allow statement uses NotResource or NotAction"
  }
}

run "policy_allows_only_the_given_buckets" {
  command = plan

  variables {
    buckets = ["lz-demo-bkt-state", "lz-bkt-state"]
  }

  assert {
    condition = toset(flatten([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : lookup(s, "Resource", []) if lookup(s, "Effect", "") == "Allow"
      ])) == toset([
      "arn:aws:s3:::lz-demo-bkt-state",
      "arn:aws:s3:::lz-demo-bkt-state/*",
      "arn:aws:s3:::lz-bkt-state",
      "arn:aws:s3:::lz-bkt-state/*",
    ])
    error_message = "Allow statements cover exactly the two given buckets and their objects"
  }

  assert {
    condition = alltrue([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : !contains(keys(s), "NotResource") && !contains(keys(s), "NotAction") if lookup(s, "Effect", "") == "Allow"
    ])
    error_message = "two buckets: no Allow statement uses NotResource or NotAction"
  }

  assert {
    condition = length(setsubtract(
      flatten([for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : lookup(s, "Action", []) if lookup(s, "Effect", "") == "Allow"]),
      ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketVersions", "s3:GetObjectVersion", "s3:ListMultipartUploadParts", "s3:ListBucketMultipartUploads", "s3:AbortMultipartUpload"],
    )) == 0
    error_message = "two buckets: every allowed action is an object or listing action"
  }
}

run "policy_actions_state_backend_no_wildcard" {
  command = plan

  assert {
    condition = length(setsubtract(
      ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"],
      flatten([for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : lookup(s, "Action", []) if lookup(s, "Effect", "") == "Allow"]),
    )) == 0
    error_message = "the policy allows what an S3 state backend with a lock file needs"
  }

  assert {
    condition = alltrue([
      for a in flatten([for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : lookup(s, "Action", []) if lookup(s, "Effect", "") == "Allow"]) : !strcontains(a, "*")
    ])
    error_message = "no allowed action is a wildcard"
  }

  assert {
    condition = length(setsubtract(
      flatten([for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : lookup(s, "Action", []) if lookup(s, "Effect", "") == "Allow"]),
      ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketVersions", "s3:GetObjectVersion", "s3:ListMultipartUploadParts", "s3:ListBucketMultipartUploads", "s3:AbortMultipartUpload"],
    )) == 0
    error_message = "every allowed action is an object or listing action: no bucket configuration, no s3:DeleteObjectVersion"
  }
}

# Every S3 user may list all buckets of the account by default; an explicit Deny refuses it
# (s3-identity-and-access-management.mdx:145, example :150-158). Unconditioned, on every resource.
run "policy_denies_listing_all_buckets" {
  command = plan

  assert {
    condition = anytrue([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement :
      lookup(s, "Effect", "") == "Deny" && contains(flatten([lookup(s, "Action", [])]), "s3:ListAllMyBuckets") && contains(flatten([lookup(s, "Resource", [])]), "*") && !contains(keys(s), "Condition")
    ])
    error_message = "an unconditioned Deny statement refuses s3:ListAllMyBuckets on every resource"
  }

  assert {
    condition = !contains(flatten([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : flatten([lookup(s, "Action", [])]) if lookup(s, "Effect", "") == "Allow"
    ]), "s3:ListAllMyBuckets")
    error_message = "no Allow statement grants s3:ListAllMyBuckets"
  }
}

# Spec 005 T016: the caller may add one Deny statement for the actions it names (`deny_actions`,
# default none): unconditioned, on every resource, without NotAction, NotResource, Principal or
# NotPrincipal. It only narrows: the Allow statements stay as above. The state-backend component
# names its list (coordinator decision 2026-10-07); see that component's tests.
run "deny_actions_default_adds_no_statement" {
  command = plan

  assert {
    condition = alltrue([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement :
      flatten([lookup(s, "Action", [])]) == ["s3:ListAllMyBuckets"] if lookup(s, "Effect", "") == "Deny"
    ])
    error_message = "without deny_actions the only Deny statement is the listing one"
  }
}

run "deny_actions_one_unconditioned_deny" {
  command = plan

  variables {
    deny_actions = ["s3:DeleteObjectVersion", "s3:PutBucketVersioning"]
  }

  assert {
    condition = anytrue([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement :
      lookup(s, "Effect", "") == "Deny" && toset(flatten([lookup(s, "Action", [])])) == toset(["s3:DeleteObjectVersion", "s3:PutBucketVersioning"]) && flatten([lookup(s, "Resource", [])]) == ["*"] && length(setsubtract(keys(s), ["Sid", "Effect", "Action", "Resource"])) == 0
    ])
    error_message = "one Deny statement refuses exactly the given actions on every resource, with no Condition, NotAction, NotResource, Principal or NotPrincipal"
  }

  assert {
    condition = toset(flatten([
      for s in jsondecode(ovh_cloud_project_user_s3_policy.this.policy).Statement : lookup(s, "Resource", []) if lookup(s, "Effect", "") == "Allow"
      ])) == toset([
      "arn:aws:s3:::lz-demo-bkt-state",
      "arn:aws:s3:::lz-demo-bkt-state/*",
    ])
    error_message = "with deny_actions the Allow statements still cover exactly the given bucket and its objects"
  }
}

run "deny_actions_wildcard_rejected" {
  command = plan

  variables {
    deny_actions = ["s3:Delete*"]
  }

  expect_failures = [var.deny_actions]
}

run "deny_actions_other_service_rejected" {
  command = plan

  variables {
    deny_actions = ["iam:DeletePolicy"]
  }

  expect_failures = [var.deny_actions]
}

run "deny_actions_null_rejected" {
  command = plan

  variables {
    deny_actions = ["s3:DeleteBucket", null]
  }

  expect_failures = [var.deny_actions]
}

# Spec 005 T016 (T015 gap 3): the state-backend component publishes the policy from this output.
run "policy_output_is_the_policy_document" {
  command = plan

  variables {
    deny_actions = ["s3:DeleteBucket"]
  }

  assert {
    condition     = output.policy == ovh_cloud_project_user_s3_policy.this.policy
    error_message = "output policy is the S3 policy resource's document"
  }
}

run "secret_only_a_sensitive_output" {
  command = apply

  assert {
    condition     = issensitive(output.secret_access_key)
    error_message = "the secret access key output is sensitive"
  }

  assert {
    condition     = nonsensitive(output.secret_access_key) == nonsensitive(ovh_cloud_project_user_s3_credential.this.secret_access_key)
    error_message = "the secret access key output is the credential's secret"
  }

  assert {
    condition     = output.access_key_id == ovh_cloud_project_user_s3_credential.this.access_key_id && output.user_id == ovh_cloud_project_user.this.id
    error_message = "access_key_id and user_id outputs are the credential's key id and the user's id"
  }

  assert {
    condition     = output.access_key_id != nonsensitive(ovh_cloud_project_user_s3_credential.this.secret_access_key) && output.user_id != nonsensitive(ovh_cloud_project_user_s3_credential.this.secret_access_key)
    error_message = "the secret is in no other output"
  }
}

run "buckets_empty_rejected" {
  command = plan

  variables {
    buckets = []
  }

  expect_failures = [var.buckets]
}

run "bucket_wildcard_rejected" {
  command = plan

  variables {
    buckets = ["*"]
  }

  expect_failures = [var.buckets]
}

run "bucket_prefix_wildcard_rejected" {
  command = plan

  variables {
    buckets = ["lz-demo-bkt-state", "lz-demo-bkt-*"]
  }

  expect_failures = [var.buckets]
}

run "bucket_arn_rejected" {
  command = plan

  variables {
    buckets = ["arn:aws:s3:::lz-demo-bkt-state"]
  }

  expect_failures = [var.buckets]
}

run "bucket_null_rejected" {
  command = plan

  variables {
    buckets = ["lz-demo-bkt-state", null]
  }

  expect_failures = [var.buckets]
}

# `?` is an IAM resource wildcard too: the ARN would match other buckets.
run "bucket_question_mark_rejected" {
  command = plan

  variables {
    buckets = ["lz-demo-bkt-stat?"]
  }

  expect_failures = [var.buckets]
}

# Not bucket names: the check is the bucket rule, not a list of bad characters.
run "bucket_uppercase_rejected" {
  command = plan

  variables {
    buckets = ["LZ-demo-bkt-state"]
  }

  expect_failures = [var.buckets]
}

run "bucket_slash_rejected" {
  command = plan

  variables {
    buckets = ["lz-demo-bkt-state/x"]
  }

  expect_failures = [var.buckets]
}
