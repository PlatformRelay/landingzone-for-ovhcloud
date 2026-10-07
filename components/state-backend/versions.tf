terraform {
  required_version = ">= 1.13.0"

  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "~> 2.21"
    }
  }
}
