// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

terraform {
  required_providers {
    ovh = {
      source = "ovh/ovh"
    }
  }
}

provider "ovh" {
  endpoint = "ovh-eu"
}

provider "ovh" {
  alias    = "admin"
  endpoint = "ovh-eu"
}
