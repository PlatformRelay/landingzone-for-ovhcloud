mock_provider "ovh" {}

run "import_nested" {
  command = apply

  assert {
    condition     = module.project.project_id == "p6-fixture-project"
    error_message = "the nested resource does not carry the imported id"
  }
}
