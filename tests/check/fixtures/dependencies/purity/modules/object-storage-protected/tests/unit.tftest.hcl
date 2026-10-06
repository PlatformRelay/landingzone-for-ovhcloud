# Test files may configure and mock providers; they are not module configuration.
mock_provider "ovh" {}

provider "ovh" {
  alias    = "real"
  endpoint = "ovh-eu"
}

run "setup" {
  module {
    source = "./tests/setup"
  }
}

run "plan" {
  command = plan
}
