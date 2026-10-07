# Offline controls of the companion root (T075): mocked providers, plan only, no credential, no
# API call, no backend.

# The tenant identity (default configuration) answers GET /me with no account and sees no tenant
# tag; the P25 identity (alias p25) sees bucket A's. A P25 read through the default configuration
# therefore fails check p25_bucket_a_readable.
mock_provider "ovh" {
  mock_data "ovh_me" {
    defaults = { nichandle = "" }
  }
  mock_data "ovh_cloud_project_storage" {
    defaults = { tags = { "lz:tenant" = "read-by-the-tenant-identity" } }
  }
}

mock_provider "ovh" {
  alias = "p25"

  mock_data "ovh_cloud_project_storage" {
    defaults = { tags = { "lz:tenant" = "lzprobe-a" } }
  }
}

variables {
  state_passphrase  = "offline-test-passphrase-not-a-secret"
  run_id            = "20261006T120000Z-ab12"
  project_id        = "0123456789abcdef0123456789abcdef"
  p25_client_id     = "offline-p25-client-id"
  p25_client_secret = "offline-p25-client-secret-not-a-secret"
}

# The tenant identity's resources (P9) carry the prefix and the run id, in the sandbox project.
# GET /me denied and bucket A read through the P25 identity hold; bucket B is always readable under
# the mock (its denial cannot be modelled offline).
run "p9_names_carry_prefix_and_run_id" {
  command = plan

  expect_failures = [check.p25_bucket_b_denied]

  assert {
    condition     = ovh_cloud_project_network_private.p9.name == "lzprobe-p9-20261006T120000Z-ab12" && ovh_cloud_project_network_private.p9.service_name == "0123456789abcdef0123456789abcdef" && ovh_cloud_project_network_private.p9.regions == toset(["GRA11"])
    error_message = "P9 network lzprobe-p9-<run-id> in GRA11 of the sandbox project"
  }

  assert {
    condition     = ovh_cloud_project_network_private_subnet.p9.network == "10.251.0.0/24" && ovh_cloud_project_network_private_subnet.p9.no_gateway == true && ovh_cloud_project_network_private_subnet.p9.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "P9 subnet 10.251.0.0/24 without gateway in the sandbox project"
  }

  assert {
    condition     = ovh_cloud_project_storage.p9.name == "lzprobe-p9-20261006t120000z-ab12" && ovh_cloud_project_storage.p9.tags == tomap({ "lz:run-id" = "20261006T120000Z-ab12" }) && ovh_cloud_project_storage.p9.service_name == "0123456789abcdef0123456789abcdef"
    error_message = "P9 bucket lzprobe-p9-<run-id> (lowercased) tagged lz:run-id in the sandbox project"
  }
}

# A refuted premise fails its own check: GET /me answered (P26), bucket B readable (P25), bucket A
# without its tenant tag.
run "refutations_fail_their_checks" {
  command = plan

  override_data {
    target = data.ovh_me.tenant
    values = { nichandle = "zz00000-ovh" }
  }

  override_data {
    target = data.ovh_cloud_project_storage.a
    values = { tags = { "lz:tenant" = "lzprobe-b" } }
  }

  expect_failures = [check.p26_me_denied, check.p25_bucket_a_readable, check.p25_bucket_b_denied]
}

run "run_id_malformed_refused" {
  command = plan

  variables {
    run_id = "../20261006T120000Z-ab12"
  }

  expect_failures = [var.run_id]
}

run "project_id_malformed_refused" {
  command = plan

  variables {
    project_id = "sandbox"
  }

  expect_failures = [var.project_id]
}
