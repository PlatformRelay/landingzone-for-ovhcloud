# An example is a root of its own: it may configure its provider and backend.
terraform {
  backend "local" {}
}

provider "ovh" {
  endpoint = "ovh-eu"
}

module "state_backend" {
  source = "../../components/state-backend"
}
