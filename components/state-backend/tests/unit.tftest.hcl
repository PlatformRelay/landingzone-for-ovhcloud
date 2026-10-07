# State-backend component (spec 005 T015; FR-002, FR-004, FR-005, FR-008; ADR-0004, ADR-0009;
# research R1, R5 *Per-tenant buckets*, R14; contracts/checks.md G6). Mocked provider, no
# credential, no API call. One protected, versioned state bucket in the given project and the S3
# users it is given, each with one credential and a policy confined to that bucket; names and labels
# from modules/naming (data-model *Derived names*: `lz-bkt-state`, `lz-demo-bkt-state`).
#
# Plan runs only: the bucket comes from modules/object-storage-protected, whose `prevent_destroy`
# refuses the cleanup destroy of an apply run (T013 *Observed*). The mock defaults below are the
# computed values at plan time too (user id, access key id, secret; observed, tofu 1.13.0), so the
# outputs are checked against them. That the bucket is the protected, versioned one is not observable here (the bucket
# is a child module's resource); see evidence/T015.md *Harness gaps*.
#
# Policy (G6): the Resource of every Allow statement together is exactly `arn:aws:s3:::<bucket>` and
# `arn:aws:s3:::<bucket>/*` of this component's bucket (form of docs.ovhcloud.com
# storage-and-backup/object-storage/s3-identity-and-access-management.mdx:116-125), never the
# account bucket from a tenant bucket, another tenant's bucket or `*`. Coordinator decision
# (2026-10-07, T014 review): each user's policy also explicitly denies `s3:DeleteObjectVersion`,
# `s3:PutBucketVersioning`, the bucket-configuration writes (policy, lifecycle, CORS, encryption, ACL)
# and `s3:DeleteBucket`, unconditioned, on `*` or on the bucket and its objects; a state backend needs
# none of them. Action names from the same guide's *List of supported actions* (:263-317);
# `s3:DeleteObjectVersion`, `s3:PutBucketPolicy` and `s3:DeleteBucketPolicy` are not in that list,
# so whether OVHcloud enforces (or accepts) a Deny on them is UNVERIFIED (T010 probe territory).
#
# Credentials: the access key id and the secret appear only in the sensitive `s3_credentials`
# output; the mocked literals below are checked against every other output.

mock_provider "ovh" {
  mock_resource "ovh_cloud_project_user" {
    defaults = {
      id = "mock-user-id"
    }
  }

  mock_resource "ovh_cloud_project_user_s3_credential" {
    defaults = {
      access_key_id     = "mock-access-key-id"
      secret_access_key = "mock-secret-access-key"
    }
  }
}

variables {
  project_id = "0123456789abcdef0123456789abcdef"
  region     = "GRA"
  org        = "lz"
  instance   = "account-bootstrap"
  managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
  s3_users   = ["platform"]
}

run "account_bucket_named_in_project_and_region" {
  command = plan

  assert {
    condition     = output.bucket == "lz-bkt-state"
    error_message = "the account state bucket is named by modules/naming: org, kind bucket, role state"
  }

  assert {
    condition     = output.project_id == "0123456789abcdef0123456789abcdef" && output.region == "GRA"
    error_message = "the bucket is in the given project and region"
  }

  assert {
    condition     = output.endpoint == "https://s3.gra.io.cloud.ovh.net"
    error_message = "the endpoint is the S3 endpoint of the bucket's region"
  }
}

run "endpoint_follows_the_region" {
  command = plan

  variables {
    region = "SBG"
  }

  assert {
    condition     = output.region == "SBG" && output.endpoint == "https://s3.sbg.io.cloud.ovh.net"
    error_message = "the endpoint is derived from the region, not a constant"
  }
}

run "account_labels_mandatory_set" {
  command = plan

  assert {
    condition = output.labels == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
      "lz:instance"   = "account-bootstrap"
      "lz:release"    = "unreleased"
    })
    error_message = "the bucket carries exactly the mandatory labels of the account scope (no lz:tenant)"
  }
}

run "account_users_as_given" {
  command = plan

  assert {
    condition     = keys(output.s3_users) == ["platform"]
    error_message = "exactly the given S3 users"
  }

  assert {
    condition     = output.s3_users["platform"].id == "mock-user-id" && output.s3_users["platform"].description == "lz-s3u-state-platform"
    error_message = "each user's id is the user resource's, its description from modules/naming (kind s3_user, role state-<key>)"
  }
}

run "account_policy_confined_to_the_bucket" {
  command = plan

  assert {
    condition = toset(flatten([
      for s in jsondecode(output.s3_users["platform"].policy).Statement : lookup(s, "Resource", []) if lookup(s, "Effect", "") == "Allow"
      ])) == toset([
      "arn:aws:s3:::lz-bkt-state",
      "arn:aws:s3:::lz-bkt-state/*",
    ])
    error_message = "Allow statements cover exactly this bucket and its objects"
  }

  assert {
    condition = alltrue([
      for s in jsondecode(output.s3_users["platform"].policy).Statement : !contains(keys(s), "NotResource") && !contains(keys(s), "NotAction") if lookup(s, "Effect", "") == "Allow"
    ])
    error_message = "no Allow statement uses NotResource or NotAction"
  }

  # Each action the backend needs, on the resource it applies to (object actions on `<b>/*`,
  # ListBucket on `<b>`), in one unconditioned Allow statement.
  assert {
    condition = alltrue([
      for p in [["s3:GetObject", "/*"], ["s3:PutObject", "/*"], ["s3:DeleteObject", "/*"], ["s3:ListBucket", ""]] :
      anytrue([
        for s in jsondecode(output.s3_users["platform"].policy).Statement :
        lookup(s, "Effect", "") == "Allow" && !contains(keys(s), "Condition") && contains(flatten([lookup(s, "Action", [])]), p[0]) && contains(flatten([lookup(s, "Resource", [])]), "arn:aws:s3:::lz-bkt-state${p[1]}")
      ])
    ])
    error_message = "the policy allows what an S3 state backend with a lock file needs, each action on its resource"
  }

  assert {
    condition = length(setsubtract(
      flatten([for s in jsondecode(output.s3_users["platform"].policy).Statement : lookup(s, "Action", []) if lookup(s, "Effect", "") == "Allow"]),
      ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketVersions", "s3:GetObjectVersion", "s3:ListMultipartUploadParts", "s3:ListBucketMultipartUploads", "s3:AbortMultipartUpload"],
    )) == 0
    error_message = "every allowed action is an object or listing action (no wildcard, no bucket configuration)"
  }
}

run "account_policy_denies_history_and_bucket_configuration" {
  command = plan

  assert {
    condition = alltrue([
      for a in ["s3:DeleteObjectVersion", "s3:PutBucketVersioning", "s3:PutBucketPolicy", "s3:DeleteBucketPolicy", "s3:PutLifecycleConfiguration", "s3:PutBucketCORS", "s3:PutEncryptionConfiguration", "s3:PutBucketAcl", "s3:DeleteBucket"] :
      anytrue([
        for s in jsondecode(output.s3_users["platform"].policy).Statement :
        lookup(s, "Effect", "") == "Deny" && length(setintersection(keys(s), ["Condition", "NotAction", "NotResource", "Principal", "NotPrincipal"])) == 0
        && contains(flatten([lookup(s, "Action", [])]), a)
        && (contains(flatten([lookup(s, "Resource", [])]), "*") || (contains(flatten([lookup(s, "Resource", [])]), "arn:aws:s3:::lz-bkt-state") && contains(flatten([lookup(s, "Resource", [])]), "arn:aws:s3:::lz-bkt-state/*")))
      ])
    ])
    error_message = "an unconditioned Deny refuses each of s3:DeleteObjectVersion, s3:PutBucketVersioning, the bucket-configuration writes and s3:DeleteBucket on this bucket and its objects"
  }
}

run "credentials_only_in_the_sensitive_output" {
  command = plan

  assert {
    condition     = issensitive(output.s3_credentials)
    error_message = "the credentials output is sensitive"
  }

  assert {
    condition = nonsensitive(output.s3_credentials) == {
      platform = {
        access_key_id     = "mock-access-key-id"
        secret_access_key = "mock-secret-access-key"
      }
    }
    error_message = "each given user's access key id and secret are its S3 credential's"
  }

  assert {
    condition = alltrue([
      for o in [jsonencode(output.s3_users), jsonencode(output.labels), jsonencode(output.unlabelled), output.bucket, output.region, output.project_id, output.endpoint] :
      !strcontains(o, "mock-secret-access-key") && !strcontains(o, "mock-access-key-id")
    ])
    error_message = "neither the secret nor the access key id is in another output"
  }
}

run "account_unlabelled_user_credential_policy" {
  command = plan

  assert {
    condition     = length(output.unlabelled) == 3
    error_message = "one user: three unlabelled resources (user, credential, policy)"
  }

  assert {
    condition = toset([for a in output.unlabelled : regex("(ovh_[a-z0-9_]+)\\.[a-z0-9_]+(\\[\"[^\"]*\"\\])?$", a)[0]]) == toset([
      "ovh_cloud_project_user",
      "ovh_cloud_project_user_s3_credential",
      "ovh_cloud_project_user_s3_policy",
    ])
    error_message = "the unlabelled addresses are the S3 user, its credential and its policy; the tagged bucket is not one"
  }
}

# Tenant scope (the `tenant-state` stage's call): the tenant's bucket, the tenant and the platform
# user, both confined to that bucket (G6).
run "tenant_bucket_and_labels" {
  command = plan

  variables {
    tenant     = "demo"
    instance   = "demo-state"
    managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
    s3_users   = ["tenant", "platform"]
  }

  assert {
    condition     = output.bucket == "lz-demo-bkt-state"
    error_message = "the tenant state bucket is named by modules/naming: org, tenant, kind bucket, role state"
  }

  assert {
    condition = output.labels == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
      "lz:instance"   = "demo-state"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the tenant bucket carries exactly the mandatory labels with lz:tenant"
  }
}

run "tenant_users_confined_to_the_tenant_bucket" {
  command = plan

  variables {
    tenant     = "demo"
    instance   = "demo-state"
    managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
    s3_users   = ["tenant", "platform"]
  }

  assert {
    condition     = toset(keys(output.s3_users)) == toset(["platform", "tenant"])
    error_message = "exactly the given S3 users"
  }

  assert {
    condition     = output.s3_users["tenant"].description == "lz-demo-s3u-state-tenant" && output.s3_users["platform"].description == "lz-demo-s3u-state-platform"
    error_message = "user descriptions from modules/naming in the tenant scope"
  }

  assert {
    condition = alltrue([
      for u in ["tenant", "platform"] : toset(flatten([
        for s in jsondecode(output.s3_users[u].policy).Statement : lookup(s, "Resource", []) if lookup(s, "Effect", "") == "Allow"
        ])) == toset([
        "arn:aws:s3:::lz-demo-bkt-state",
        "arn:aws:s3:::lz-demo-bkt-state/*",
      ])
    ])
    error_message = "each user's Allow statements cover exactly the tenant bucket and its objects: not the account bucket, another tenant's or *"
  }

  assert {
    condition = alltrue(flatten([
      for u in ["tenant", "platform"] : [
        for s in jsondecode(output.s3_users[u].policy).Statement : !contains(keys(s), "NotResource") && !contains(keys(s), "NotAction") if lookup(s, "Effect", "") == "Allow"
      ]
    ]))
    error_message = "no user's Allow statement uses NotResource or NotAction"
  }

  assert {
    condition = alltrue([
      for u in ["tenant", "platform"] : length(setsubtract(
        flatten([for s in jsondecode(output.s3_users[u].policy).Statement : lookup(s, "Action", []) if lookup(s, "Effect", "") == "Allow"]),
        ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketVersions", "s3:GetObjectVersion", "s3:ListMultipartUploadParts", "s3:ListBucketMultipartUploads", "s3:AbortMultipartUpload"],
      )) == 0
    ])
    error_message = "every user's allowed actions are object or listing actions"
  }

  assert {
    condition = alltrue(flatten([
      for u in ["tenant", "platform"] : [
        for p in [["s3:GetObject", "/*"], ["s3:PutObject", "/*"], ["s3:DeleteObject", "/*"], ["s3:ListBucket", ""]] :
        anytrue([
          for s in jsondecode(output.s3_users[u].policy).Statement :
          lookup(s, "Effect", "") == "Allow" && !contains(keys(s), "Condition") && contains(flatten([lookup(s, "Action", [])]), p[0]) && contains(flatten([lookup(s, "Resource", [])]), "arn:aws:s3:::lz-demo-bkt-state${p[1]}")
        ])
      ]
    ]))
    error_message = "every user's policy allows what an S3 state backend needs, each action on its resource"
  }
}

run "tenant_users_deny_history_and_bucket_configuration" {
  command = plan

  variables {
    tenant     = "demo"
    instance   = "demo-state"
    managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
    s3_users   = ["tenant", "platform"]
  }

  assert {
    condition = alltrue(flatten([
      for u in ["tenant", "platform"] : [
        for a in ["s3:DeleteObjectVersion", "s3:PutBucketVersioning", "s3:PutBucketPolicy", "s3:DeleteBucketPolicy", "s3:PutLifecycleConfiguration", "s3:PutBucketCORS", "s3:PutEncryptionConfiguration", "s3:PutBucketAcl", "s3:DeleteBucket"] :
        anytrue([
          for s in jsondecode(output.s3_users[u].policy).Statement :
          lookup(s, "Effect", "") == "Deny" && length(setintersection(keys(s), ["Condition", "NotAction", "NotResource", "Principal", "NotPrincipal"])) == 0
          && contains(flatten([lookup(s, "Action", [])]), a)
          && (contains(flatten([lookup(s, "Resource", [])]), "*") || (contains(flatten([lookup(s, "Resource", [])]), "arn:aws:s3:::lz-demo-bkt-state") && contains(flatten([lookup(s, "Resource", [])]), "arn:aws:s3:::lz-demo-bkt-state/*")))
        ])
      ]
    ]))
    error_message = "every user's policy denies, unconditioned, s3:DeleteObjectVersion, s3:PutBucketVersioning, the bucket-configuration writes and s3:DeleteBucket"
  }
}

run "tenant_credentials_and_unlabelled_per_user" {
  command = plan

  variables {
    tenant     = "demo"
    instance   = "demo-state"
    managed_in = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
    s3_users   = ["tenant", "platform"]
  }

  assert {
    condition = nonsensitive(output.s3_credentials) == {
      platform = { access_key_id = "mock-access-key-id", secret_access_key = "mock-secret-access-key" }
      tenant   = { access_key_id = "mock-access-key-id", secret_access_key = "mock-secret-access-key" }
    }
    error_message = "one credential per given user: each user's access key id and secret are its S3 credential's"
  }

  assert {
    condition = alltrue([
      for o in [jsonencode(output.s3_users), jsonencode(output.labels), jsonencode(output.unlabelled), output.bucket, output.region, output.project_id, output.endpoint] :
      !strcontains(o, "mock-secret-access-key") && !strcontains(o, "mock-access-key-id")
    ])
    error_message = "tenant scope: neither secret nor access key id is in another output"
  }

  assert {
    condition     = length(output.unlabelled) == 6
    error_message = "two users: six unlabelled resources"
  }

  assert {
    condition = alltrue([
      for u in ["tenant", "platform"] : length([for a in output.unlabelled : a if strcontains(a, "\"${u}\"")]) == 3
    ])
    error_message = "each user's user, credential and policy are listed under its key"
  }
}
