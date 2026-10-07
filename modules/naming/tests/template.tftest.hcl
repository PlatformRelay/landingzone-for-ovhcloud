# Template and coordinate refusals added by T012 (data-model *Naming template*, research R15):
# a template is refused on `var.template` when a name built from it could drop a required segment
# or use an unimplemented rule; `""` in an optional coordinate is an empty segment on `output.name`.
# Non-bucket kinds are used where the bucket punctuation rules would refuse the input on their own.

variables {
  org         = "lz"
  tenant      = "demo"
  environment = "dev"
  region      = "GRA11"
  kind        = "private_network"
  role        = "core"
  instance    = "demo-dev-gra11-core"
  managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/core"
}

# --- Accepted neighbour: another separator is data, not a rule.

run "separator_dot_bucket_accepted" {
  command = plan

  variables {
    kind = "bucket"
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
      separator = "."
      case      = "lower"
      kinds     = { bucket = "bkt" }
    }
  }

  assert {
    condition     = output.name == "lz.demo.dev.gra11.bkt.core"
    error_message = "a dot separator is legal for a bucket: lz.demo.dev.gra11.bkt.core"
  }
}

# --- Kind abbreviation missing: the kind segment would be dropped silently.

run "kind_abbreviation_null_rejected" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
      separator = "-"
      case      = "lower"
      kinds     = { private_network = null }
    }
  }

  expect_failures = [var.template]
}

run "kind_abbreviation_empty_rejected" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
      separator = "-"
      case      = "lower"
      kinds     = { private_network = "" }
    }
  }

  expect_failures = [var.template]
}

# --- Segments: known, distinct, kind and role present.

run "segment_unknown_rejected" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["org", "domain", "tenant", "environment", "region", "kind", "role", "slot"]
      separator = "-"
      case      = "lower"
      kinds     = { private_network = "pn" }
    }
  }

  expect_failures = [var.template]
}

run "segment_duplicate_rejected" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "kind", "role", "role"]
      separator = "-"
      case      = "lower"
      kinds     = { private_network = "pn" }
    }
  }

  expect_failures = [var.template]
}

run "segment_without_kind_rejected" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "role", "slot"]
      separator = "-"
      case      = "lower"
      kinds     = { private_network = "pn" }
    }
  }

  expect_failures = [var.template]
}

run "segment_without_role_rejected" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "kind", "slot"]
      separator = "-"
      case      = "lower"
      kinds     = { private_network = "pn" }
    }
  }

  expect_failures = [var.template]
}

# --- Case rule and separator.

run "case_upper_rejected" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
      separator = "-"
      case      = "upper"
      kinds     = { private_network = "pn" }
    }
  }

  expect_failures = [var.template]
}

run "separator_empty_rejected" {
  command = plan

  variables {
    template = {
      version   = 1
      segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
      separator = ""
      case      = "lower"
      kinds     = { private_network = "pn" }
    }
  }

  expect_failures = [var.template]
}

# --- `""` in an optional coordinate is an empty segment, not an absent one (null).

run "empty_tenant_rejected" {
  command = plan

  variables {
    tenant = ""
  }

  expect_failures = [output.name]
}

run "empty_environment_rejected" {
  command = plan

  variables {
    environment = ""
  }

  expect_failures = [output.name]
}

run "empty_region_rejected" {
  command = plan

  variables {
    region = ""
  }

  expect_failures = [output.name]
}

run "empty_slot_rejected" {
  command = plan

  variables {
    slot = ""
  }

  expect_failures = [output.name]
}

# --- Template version: only algorithm 1 exists, so another version is refused, not ignored.

run "template_version_2_rejected" {
  command = plan

  variables {
    template = {
      version   = 2
      segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
      separator = "-"
      case      = "lower"
      kinds     = { private_network = "pn" }
    }
  }

  expect_failures = [var.template]
}

# --- Empty values for the generated mandatory labels.

run "empty_instance_rejected" {
  command = plan

  variables {
    instance = ""
  }

  expect_failures = [var.instance]
}

run "empty_managed_in_rejected" {
  command = plan

  variables {
    managed_in = ""
  }

  expect_failures = [var.managed_in]
}
