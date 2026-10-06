# A stage receives providers from its stack and passes them on; it configures none.
terraform {
  required_providers {
    ovh = {
      source                = "ovh/ovh"
      configuration_aliases = [ovh.admin]
    }
  }
}

module "state_backend" {
  source = "../../components/state-backend"
  providers = {
    ovh = ovh.admin
  }
}
