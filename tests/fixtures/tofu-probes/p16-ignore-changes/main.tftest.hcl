mock_provider "ovh" {}

run "create" {
  command = apply

  variables {
    run_id = "run-1"
    tenant = "alpha"
  }

  assert {
    condition     = ovh_cloud_project_storage.kept.tags["lz:run-id"] == "run-1"
    error_message = "the first apply does not record its run id"
  }
}

run "later_apply" {
  command = apply

  variables {
    run_id = "run-2"
    tenant = "beta"
  }

  assert {
    condition     = ovh_cloud_project_storage.kept.tags["lz:run-id"] == "run-1"
    error_message = "a later apply replaced the creating run id"
  }

  assert {
    condition     = ovh_cloud_project_storage.kept.tags["lz:tenant"] == "beta"
    error_message = "ignore_changes on one key also froze another tag"
  }

  assert {
    condition     = ovh_cloud_project_storage.control.tags["lz:run-id"] == "run-2"
    error_message = "without ignore_changes the run id does not follow the configuration"
  }
}
