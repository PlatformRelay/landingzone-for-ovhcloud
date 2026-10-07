# Output contract (FR-001, FR-002, ADR-0003, data-model *Label set*): plan only, no provider.
# Mandatory labels under `lz:`: managed-by, managed-in, instance, tenant (absent at account scope),
# release (`unreleased` until ADR-0010's release train, D87). Extra labels may not set any key of the
# label set, `lz:run-id` included (data-model.md *Label set*), in any case and even with the value it
# would have.
# Guard G4 (contracts/checks.md): `lz:tenant` comes only from the `tenant` coordinate.

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

run "mandatory_labels_tenant_scope" {
  command = plan

  assert {
    condition = tomap(output.labels) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
      "lz:instance"   = "demo-dev-gra11-runtime"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
    })
    error_message = "tenant scope: exactly the five mandatory labels"
  }
}

run "mandatory_labels_account_scope" {
  command = plan

  variables {
    tenant      = null
    environment = null
    region      = null
    role        = "state"
    instance    = "account-bootstrap"
    managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
  }

  assert {
    condition = tomap(output.labels) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/account/bootstrap"
      "lz:instance"   = "account-bootstrap"
      "lz:release"    = "unreleased"
    })
    error_message = "account scope: the mandatory labels without lz:tenant"
  }
}

run "extra_labels_merged" {
  command = plan

  variables {
    labels = {
      team        = "platform"
      "lz:custom" = "kept"
    }
  }

  assert {
    condition = tomap(output.labels) == tomap({
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
      "lz:instance"   = "demo-dev-gra11-runtime"
      "lz:tenant"     = "demo"
      "lz:release"    = "unreleased"
      team            = "platform"
      "lz:custom"     = "kept"
    })
    error_message = "extra labels outside the label set are merged into the mandatory set"
  }
}

run "algorithm_version" {
  command = plan

  assert {
    condition     = output.algorithm_version == 1
    error_message = "the first name algorithm is version 1"
  }
}

# --- A label-only change keeps the name: labels, instance and managed-in never rename.

run "name_before_label_change" {
  command = plan

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "baseline name: lz-demo-dev-gra11-bkt-runtime"
  }
}

run "name_after_label_change" {
  command = plan

  variables {
    instance   = "demo-dev-gra11-runtime-renamed"
    managed_in = "github.com/other/fork//stacks/tenants/demo/dev/gra11/runtime"
    labels = {
      team = "platform"
    }
  }

  assert {
    condition     = output.name == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "a label-only change must keep the name (the baseline run's name)"
  }

  assert {
    condition     = lookup(output.labels, "lz:instance", null) == "demo-dev-gra11-runtime-renamed" && lookup(output.labels, "team", null) == "platform"
    error_message = "the label change itself must show in the labels"
  }
}

# --- Mandatory-key override: one forbidden key per run.

run "extra_label_managed_by_rejected" {
  command = plan

  variables {
    labels = { "lz:managed-by" = "console" }
  }

  expect_failures = [var.labels]
}

run "extra_label_managed_in_rejected" {
  command = plan

  variables {
    labels = { "lz:managed-in" = "github.com/other/fork//x" }
  }

  expect_failures = [var.labels]
}

run "extra_label_instance_rejected" {
  command = plan

  variables {
    labels = { "lz:instance" = "other" }
  }

  expect_failures = [var.labels]
}

run "extra_label_tenant_rejected" {
  command = plan

  variables {
    labels = { "lz:tenant" = "other" }
  }

  expect_failures = [var.labels]
}

run "extra_label_tenant_case_variant_rejected" {
  command = plan

  variables {
    labels = { "LZ:Tenant" = "other" }
  }

  expect_failures = [var.labels]
}

run "extra_label_tenant_same_value_rejected" {
  command = plan

  variables {
    labels = { "lz:tenant" = "demo" }
  }

  expect_failures = [var.labels]
}

run "extra_label_tenant_at_account_scope_rejected" {
  command = plan

  variables {
    tenant      = null
    environment = null
    region      = null
    role        = "state"
    labels      = { "lz:tenant" = "demo" }
  }

  expect_failures = [var.labels]
}

run "extra_label_release_rejected" {
  command = plan

  variables {
    labels = { "lz:release" = "v1.0.0" }
  }

  expect_failures = [var.labels]
}

run "extra_label_run_id_rejected" {
  command = plan

  variables {
    labels = { "lz:run-id" = "20261007T120000Z-ab12" }
  }

  expect_failures = [var.labels]
}
