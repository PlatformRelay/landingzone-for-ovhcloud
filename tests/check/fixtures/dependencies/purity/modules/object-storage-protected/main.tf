terraform {
  required_providers {
    ovh = {
      source = "ovh/ovh"
    }
  }
}

module "name" {
  source = "../naming"
}
