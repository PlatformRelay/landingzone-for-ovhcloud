# Protected bucket module for state buckets (spec 005 T013; FR-002, FR-004, FR-013; ADR-0003,
# ADR-0008, ADR-0009; research R5 *Protection*). Mocked provider, no credential, no API call.
# Attribute names from the pinned ovh 2.21.0 schema (`ovh_cloud_project_storage`).
# The name and the tags are modules/naming outputs for the tenant `demo` state bucket
# (data-model *Derived names*: `lz-demo-bkt-state`).
#
# Plan runs only. The module carries a literal `prevent_destroy`: `tofu test` destroys what an apply
# run created, that destroy is refused, and the run still reports pass while it leaves
# errored_test.tfstate in the module directory (observed, tofu 1.10.3). Whether `prevent_destroy` is
# present is not observable from an assertion (a refused replacement is an error, not a checkable
# failure), so the lifecycle scan of T026 (guard G7, code part) pins it, not this file.

mock_provider "ovh" {}

variables {
  project_id = "0123456789abcdef0123456789abcdef"
  region     = "GRA"
  name       = "lz-demo-bkt-state"
  # T099: the component's default (components/state-backend `state_retention_days`).
  retention_days = 7
  tags = {
    "lz:managed-by" = "opentofu"
    "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
    "lz:instance"   = "demo-tenant-state"
    "lz:tenant"     = "demo"
    "lz:release"    = "unreleased"
  }
}

run "bucket_given_name_region_project" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.this.name == "lz-demo-bkt-state"
    error_message = "the bucket carries the given name, unchanged"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.region_name == "GRA"
    error_message = "the bucket is in the given region"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "the bucket is in the given project"
  }
}

run "bucket_account_state" {
  command = plan

  variables {
    name   = "lz-bkt-state"
    region = "SBG"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.name == "lz-bkt-state" && ovh_cloud_project_storage.this.region_name == "SBG"
    error_message = "another name and region are applied as given (no constant)"
  }
}

run "tags_exactly_given" {
  command = plan

  assert {
    condition = ovh_cloud_project_storage.this.tags == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/tenant-state/demo"
      "lz:instance"   = "demo-tenant-state"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "the bucket carries exactly the given tags"
  }
}

# Account scope: no `lz:tenant` (naming omits it); the module must not add one.
run "tags_account_scope_exactly_given" {
  command = plan

  variables {
    name = "lz-bkt-state"
    tags = {
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
      "lz:instance"   = "account-bootstrap"
      "lz:release"    = "unreleased"
    }
  }

  assert {
    condition = ovh_cloud_project_storage.this.tags == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
      "lz:instance"   = "account-bootstrap"
      "lz:release"    = "unreleased"
    })
    error_message = "another tag set is applied exactly: nothing added, nothing dropped"
  }
}

run "versioning_always_enabled" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.this.versioning.status == "enabled"
    error_message = "a state bucket is always versioned"
  }
}

# `versioning` is not an input of this module; a run setting it must not switch versioning off
# (a test variable the module does not declare is ignored, so a module that adds the input fails).
run "versioning_not_switchable" {
  command = plan

  variables {
    versioning = false
  }

  assert {
    condition     = ovh_cloud_project_storage.this.versioning.status == "enabled"
    error_message = "versioning stays enabled whatever the caller passes"
  }
}

run "outputs" {
  command = plan

  assert {
    condition     = output.name == "lz-demo-bkt-state"
    error_message = "output name is the bucket name"
  }

  assert {
    condition     = output.region == "GRA"
    error_message = "output region is the bucket region"
  }
}

# Spec 005 T016 (T015 gap 3): the state-backend component publishes the bucket's tags and project
# from these outputs, so they are the bucket's own attributes.
run "outputs_tags_and_project_of_the_bucket" {
  command = plan

  assert {
    condition     = output.tags == ovh_cloud_project_storage.this.tags && output.tags == tomap(var.tags)
    error_message = "output tags are the bucket's tags"
  }

  assert {
    condition     = output.project_id == ovh_cloud_project_storage.this.service_name && output.project_id == "0123456789abcdef0123456789abcdef"
    error_message = "output project_id is the bucket's project"
  }
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

# T099 (D104): state history is protected by S3 Object Lock in governance mode. OVHcloud enables the
# lock only at bucket creation (docs.ovhcloud.com storage-and-backup/object-storage/
# s3-managing-object-lock.mdx:140, :153; provider cloud_project_storage.md:12-14), so it is part of
# the bucket from the first apply; the default rule locks every new object version for
# `retention_days` days (same guide :51, :160). Governance, not compliance: a holder of
# s3:BypassGovernanceRetention (never a state user) can still empty the bucket (same guide :57-59).
run "object_lock_governance_default" {
  command = plan

  assert {
    condition     = ovh_cloud_project_storage.this.object_lock.status == "enabled"
    error_message = "a state bucket is created with Object Lock enabled"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.object_lock.rule.mode == "governance"
    error_message = "the default retention is in governance mode"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.object_lock.rule.period == "P7D"
    error_message = "the default retention is retention_days days, as an ISO 8601 duration"
  }
}

run "object_lock_output_is_the_bucket_s" {
  command = plan

  assert {
    condition = output.object_lock == {
      status = ovh_cloud_project_storage.this.object_lock.status
      mode   = ovh_cloud_project_storage.this.object_lock.rule.mode
      period = ovh_cloud_project_storage.this.object_lock.rule.period
    } && output.object_lock.period == "P7D"
    error_message = "output object_lock is the bucket's own lock configuration"
  }
}

run "object_lock_period_follows_the_input" {
  command = plan

  variables {
    retention_days = 30
  }

  assert {
    condition     = ovh_cloud_project_storage.this.object_lock.rule.period == "P30D" && ovh_cloud_project_storage.this.object_lock.rule.mode == "governance"
    error_message = "another retention is applied as given, still in governance mode"
  }
}

# The mode is not an input: a run setting one (a test variable the module does not declare is
# ignored) must not turn the lock into compliance mode or off.
run "object_lock_mode_not_switchable" {
  command = plan

  variables {
    object_lock_mode   = "compliance"
    object_lock_status = "disabled"
  }

  assert {
    condition     = ovh_cloud_project_storage.this.object_lock.status == "enabled" && ovh_cloud_project_storage.this.object_lock.rule.mode == "governance"
    error_message = "Object Lock stays enabled in governance mode whatever the caller passes"
  }
}

# Bounds: at least one day (same guide :51, "a minimum of one day"); at most 365 (the guide sets no
# maximum; a longer retention is an owner decision, evidence/T099.md *Design* 2); whole days only.
run "retention_one_day_admitted" {
  command = plan

  variables {
    retention_days = 1
  }

  assert {
    condition     = ovh_cloud_project_storage.this.object_lock.rule.period == "P1D"
    error_message = "one day, the guide's minimum, is admitted"
  }
}

run "retention_365_days_admitted" {
  command = plan

  variables {
    retention_days = 365
  }

  assert {
    condition     = ovh_cloud_project_storage.this.object_lock.rule.period == "P365D"
    error_message = "365 days, the upper bound, is admitted"
  }
}

run "retention_zero_refused" {
  command = plan

  variables {
    retention_days = 0
  }

  expect_failures = [var.retention_days]
}

run "retention_above_365_refused" {
  command = plan

  variables {
    retention_days = 366
  }

  expect_failures = [var.retention_days]
}

run "retention_fraction_refused" {
  command = plan

  variables {
    retention_days = 1.5
  }

  expect_failures = [var.retention_days]
}
