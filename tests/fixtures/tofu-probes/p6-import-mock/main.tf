# P6 (005 T007), the task's probe method: `tofu test` with `mock_provider "ovh"` on a root whose
# import block targets the real resource type in a nested module. No credentials, no API.
terraform {
  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "2.21.0"
    }
  }
}

import {
  to = module.project.ovh_cloud_project.this
  id = "p6-fixture-project"
}

module "project" {
  source = "./project"
}
