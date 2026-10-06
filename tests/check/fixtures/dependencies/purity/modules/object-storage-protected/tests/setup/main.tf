# Test helper configuration (test layer) may configure a provider and a backend.
terraform {
  backend "local" {}
}

provider "ovh" {
  endpoint = "ovh-eu"
}
