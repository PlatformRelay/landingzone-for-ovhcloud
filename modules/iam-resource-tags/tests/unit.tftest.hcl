# IAM resource tags module (spec 005 T105; FR-004; research R12, R14, P15). Mocked provider, no
# credential, no API call. Attribute names from the pinned ovh 2.21.0 schema
# (`ovh_iam_resource_tags`: `urn`, `tags`).
#
# The module sets exactly the given tags on exactly the given URN; destroying it removes only those
# keys (provider docs `iam_resource_tags`, Notes). Moved out of modules/cloud-project by T105: the
# account-governance stage tags the tenant projects under the admin (D109).

mock_provider "ovh" {}

variables {
  urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
  tags = {
    "lz:managed-by" = "opentofu"
    "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/account-governance"
    "lz:instance"   = "account-governance"
    "lz:tenant"     = "demo"
    "lz:release"    = "unreleased"
  }
}

run "tags_the_given_urn_exactly" {
  command = plan

  assert {
    condition     = ovh_iam_resource_tags.this.urn == "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    error_message = "the tags target the given URN"
  }

  assert {
    condition = ovh_iam_resource_tags.this.tags == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/account-governance"
      "lz:instance"   = "account-governance"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the URN carries exactly the given tags"
  }

  assert {
    condition = output.urn == "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef" && output.tags == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/account-governance"
      "lz:instance"   = "account-governance"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the outputs are the resource's urn and tags"
  }
}

run "another_urn_and_tag_set" {
  command = plan

  variables {
    urn = "urn:v1:ca:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
    tags = {
      "lz:managed-by" = "opentofu"
      "lz:tenant"     = "alpha"
    }
  }

  assert {
    condition     = ovh_iam_resource_tags.this.urn == "urn:v1:ca:resource:publicCloudProject:fedcba9876543210fedcba9876543210"
    error_message = "another URN is used as given (no constant)"
  }

  assert {
    condition     = ovh_iam_resource_tags.this.tags == tomap({ "lz:managed-by" = "opentofu", "lz:tenant" = "alpha" })
    error_message = "another tag set is applied exactly: nothing added, nothing dropped"
  }
}

# --- rejections (last: an expected failure ends the run) ----------------------------------------

run "blank_urn_rejected" {
  command = plan

  variables {
    urn = " "
  }

  expect_failures = [var.urn]
}

run "tags_null_value_rejected" {
  command = plan

  variables {
    tags = {
      "lz:managed-by" = "opentofu"
      "lz:tenant"     = null
    }
  }

  expect_failures = [var.tags]
}
