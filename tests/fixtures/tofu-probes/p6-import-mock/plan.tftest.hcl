mock_provider "ovh" {}

run "import_nested_plan" {
  command = plan
}
