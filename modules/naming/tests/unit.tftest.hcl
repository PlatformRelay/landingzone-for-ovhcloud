# Name algorithm (FR-001, ADR-0003, research R15, data-model *Naming template*): plan only, no
# provider. Every name error surfaces on `output.name`; each rejection run has exactly one flaw, and
# an accepted neighbour pins that the flaw, not the input shape, is refused.
# Bucket rules: 3-63 characters, lowercase alphanumerics, `.` and `-`, alphanumeric at both ends, no
# doubled punctuation ("`..` or `-.` or `.-` or `--`"), not an IP address (kb:
# storage-and-backup/object-storage/s3-limitations.mdx:49-54;
# terraform-provider-ovh docs/resources/cloud_storage_object_bucket.md:58). Other kinds' limits are
# UNVERIFIED, so their names here stay short and use only `[a-z0-9-]`.

variables {
  org         = "lz"
  tenant      = "demo"
  environment = "dev"
  region      = "GRA11"
  kind        = "bucket"
  role        = "runtime"
  instance    = "demo-dev-gra11-runtime"
  managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
}

# --- Default template: every kind, empty segments omitted, region lowered by the case rule.

run "default_bucket_regional" {
  command = plan

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "default template: bucket name must be lz-demo-dev-gra11-bkt-runtime"
  }
}

run "case_rule_every_coordinate" {
  command = plan

  variables {
    org         = "LZ"
    tenant      = "Demo"
    environment = "DEV"
    role        = "Runtime"
    slot        = "Blue"
  }

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-runtime-blue"
    error_message = "the lower-case rule applies to every coordinate, not only the region"
  }
}

run "default_bucket_account_state" {
  command = plan

  variables {
    tenant      = null
    environment = null
    region      = null
    role        = "state"
  }

  assert {
    condition     = output.name == "lz-bkt-state"
    error_message = "account scope omits tenant, environment and region: lz-bkt-state"
  }
}

run "default_bucket_tenant_state" {
  command = plan

  variables {
    environment = null
    region      = null
    role        = "state"
  }

  assert {
    condition     = output.name == "lz-demo-bkt-state"
    error_message = "tenant scope: lz-demo-bkt-state"
  }
}

run "default_private_network" {
  command = plan

  variables {
    kind = "private_network"
    role = "core"
  }

  assert {
    condition     = output.name == "lz-demo-dev-gra11-pn-core"
    error_message = "default template: private network name must be lz-demo-dev-gra11-pn-core"
  }
}

run "default_subnet" {
  command = plan

  variables {
    kind = "subnet"
    role = "core"
  }

  assert {
    condition     = output.name == "lz-demo-dev-gra11-sn-core"
    error_message = "default template: subnet name must be lz-demo-dev-gra11-sn-core"
  }
}

run "default_service_account" {
  command = plan

  variables {
    tenant      = null
    environment = null
    region      = null
    kind        = "service_account"
    role        = "platform"
  }

  assert {
    condition     = output.name == "lz-sa-platform"
    error_message = "default template: service account name must be lz-sa-platform"
  }
}

run "default_iam_policy" {
  command = plan

  variables {
    region = null
    kind   = "iam_policy"
    role   = "deployer"
  }

  assert {
    condition     = output.name == "lz-demo-dev-pol-deployer"
    error_message = "default template: IAM policy name must be lz-demo-dev-pol-deployer"
  }
}

run "default_identity_group" {
  command = plan

  variables {
    environment = null
    region      = null
    kind        = "identity_group"
    role        = "admins"
  }

  assert {
    condition     = output.name == "lz-demo-grp-admins"
    error_message = "default template: identity group name must be lz-demo-grp-admins"
  }
}

run "default_s3_user" {
  command = plan

  variables {
    environment = null
    region      = null
    kind        = "s3_user"
    role        = "state"
  }

  assert {
    condition     = output.name == "lz-demo-s3u-state"
    error_message = "default template: S3 user name must be lz-demo-s3u-state"
  }
}

# The project's name is not set by this slice (the project exists; components/project-factory reads
# only `labels`), but naming refuses a kind without a row even for labels (spec 005 T027).
run "default_project" {
  command = plan

  variables {
    region = null
    kind   = "project"
    role   = "main"
  }

  assert {
    condition     = output.name == "lz-demo-dev-prj-main"
    error_message = "default template: project name must be lz-demo-dev-prj-main"
  }

  assert {
    condition     = output.labels["lz:tenant"] == "demo" && output.labels["lz:instance"] == "demo-dev-gra11-runtime"
    error_message = "the project kind gets the mandatory labels of its scope"
  }
}

# --- Optional slot: absent, and two slots in one scope.

run "slot_blue" {
  command = plan

  variables {
    slot = "blue"
  }

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-runtime-blue"
    error_message = "slot blue: lz-demo-dev-gra11-bkt-runtime-blue"
  }
}

run "slot_green" {
  command = plan

  variables {
    slot = "green"
  }

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-runtime-green"
    error_message = "slot green: lz-demo-dev-gra11-bkt-runtime-green (distinct from slot blue and from no slot)"
  }
}

# --- A reordered test template proves the template is data: kind and role first, the slot
# before the coordinates, other abbreviations. Only the default template ships.

run "reordered_bucket" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "b-runtime-lz-demo-dev-gra11"
    error_message = "reordered template: expected b-runtime-lz-demo-dev-gra11"
  }
}

run "reordered_bucket_slot" {
  command = plan

  variables {
    slot = "blue"
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "b-runtime-blue-lz-demo-dev-gra11"
    error_message = "reordered template: expected b-runtime-blue-lz-demo-dev-gra11"
  }
}

run "reordered_bucket_account_state" {
  command = plan

  variables {
    tenant      = null
    environment = null
    region      = null
    role        = "state"
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "b-state-lz"
    error_message = "reordered template: expected b-state-lz"
  }
}

run "reordered_private_network" {
  command = plan

  variables {
    kind = "private_network"
    role = "core"
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "net-core-lz-demo-dev-gra11"
    error_message = "reordered template: expected net-core-lz-demo-dev-gra11"
  }
}

run "reordered_subnet" {
  command = plan

  variables {
    kind = "subnet"
    role = "core"
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "sub-core-lz-demo-dev-gra11"
    error_message = "reordered template: expected sub-core-lz-demo-dev-gra11"
  }
}

run "reordered_service_account" {
  command = plan

  variables {
    tenant      = null
    environment = null
    region      = null
    kind        = "service_account"
    role        = "platform"
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "svc-platform-lz"
    error_message = "reordered template: expected svc-platform-lz"
  }
}

run "reordered_iam_policy" {
  command = plan

  variables {
    region = null
    kind   = "iam_policy"
    role   = "deployer"
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "policy-deployer-lz-demo-dev"
    error_message = "reordered template: expected policy-deployer-lz-demo-dev"
  }
}

run "reordered_identity_group" {
  command = plan

  variables {
    environment = null
    region      = null
    kind        = "identity_group"
    role        = "admins"
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "group-admins-lz-demo"
    error_message = "reordered template: expected group-admins-lz-demo"
  }
}

run "reordered_s3_user" {
  command = plan

  variables {
    environment = null
    region      = null
    kind        = "s3_user"
    role        = "state"
    template = {
      version   = 1
      segments  = ["kind", "role", "slot", "org", "tenant", "environment", "region"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "b"
        private_network = "net"
        subnet          = "sub"
        service_account = "svc"
        iam_policy      = "policy"
        identity_group  = "group"
        s3_user         = "s3"
      }
    }
  }

  assert {
    condition     = output.name == "s3-state-lz-demo"
    error_message = "reordered template: expected s3-state-lz-demo"
  }
}

# --- Explicit name override (import): passed through unchanged after validation, never cleaned,
# lowered or abbreviated.

run "override_passed_through" {
  command = plan

  variables {
    name_override = "acme.legacy-state01"
  }

  assert {
    condition     = output.name == "acme.legacy-state01"
    error_message = "a valid override must be the name, unchanged, instead of the template's"
  }
}

run "override_at_limit_passed_through" {
  command = plan

  variables {
    name_override = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }

  assert {
    condition     = output.name == "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    error_message = "a 63-character bucket override is at the limit and passes unchanged"
  }
}

run "override_uppercase_rejected" {
  command = plan

  variables {
    name_override = "Acme-Legacy"
  }

  expect_failures = [output.name]
}

run "override_at_minimum_passed_through" {
  command = plan

  variables {
    name_override = "abc"
  }

  assert {
    condition     = output.name == "abc"
    error_message = "a 3-character bucket override is at the minimum and passes unchanged"
  }
}

run "override_too_short_rejected" {
  command = plan

  variables {
    name_override = "ab"
  }

  expect_failures = [output.name]
}

run "override_over_limit_rejected" {
  command = plan

  variables {
    name_override = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }

  expect_failures = [output.name]
}

run "override_doubled_punctuation_rejected" {
  command = plan

  variables {
    name_override = "acme.-legacy"
  }

  expect_failures = [output.name]
}

run "override_ip_address_rejected" {
  command = plan

  variables {
    name_override = "192.168.1.1"
  }

  expect_failures = [output.name]
}

# --- Per-kind limits on generated names: exactly at the bucket limit passes, one over is refused.

run "bucket_at_limit" {
  command = plan

  variables {
    role = "raaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-raaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" && length(output.name) == 63
    error_message = "a 63-character bucket name is at the limit and must be produced, not truncated"
  }
}

run "bucket_over_limit_rejected" {
  command = plan

  variables {
    role = "raaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }

  expect_failures = [output.name]
}

# --- Bucket charset and punctuation; an inner hyphen in a segment is legal.

run "bucket_inner_hyphen_accepted" {
  command = plan

  variables {
    role = "run-time"
  }

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-run-time"
    error_message = "a single inner hyphen in a segment is legal in a bucket name"
  }
}

run "bucket_forbidden_character_rejected" {
  command = plan

  variables {
    role = "run_time"
  }

  expect_failures = [output.name]
}

run "bucket_doubled_punctuation_rejected" {
  command = plan

  variables {
    tenant = "demo-"
  }

  expect_failures = [output.name]
}

run "bucket_trailing_punctuation_rejected" {
  command = plan

  variables {
    role = "runtime-"
  }

  expect_failures = [output.name]
}

# --- Empty required segment.

run "empty_org_rejected" {
  command = plan

  variables {
    org = ""
  }

  expect_failures = [output.name]
}

run "empty_role_rejected" {
  command = plan

  variables {
    role = ""
  }

  expect_failures = [output.name]
}

# A non-bucket kind has no verified edge or punctuation rule, so only the empty-segment rule
# refuses these.

run "empty_org_private_network_rejected" {
  command = plan

  variables {
    kind = "private_network"
    org  = ""
  }

  expect_failures = [output.name]
}

run "empty_role_private_network_rejected" {
  command = plan

  variables {
    kind = "private_network"
    role = ""
  }

  expect_failures = [output.name]
}

# --- Unknown kind: not in the template, or in the template without a limits row.

run "unknown_kind_rejected" {
  command = plan

  variables {
    kind = "database"
  }

  expect_failures = [output.name]
}

run "kind_without_limits_row_rejected" {
  command = plan

  variables {
    kind = "widget"
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
      separator = "-"
      case      = "lower"
      kinds = {
        bucket          = "bkt"
        private_network = "pn"
        subnet          = "sn"
        service_account = "sa"
        iam_policy      = "pol"
        identity_group  = "grp"
        s3_user         = "s3u"
        widget          = "wdg"
      }
    }
  }

  expect_failures = [output.name]
}
