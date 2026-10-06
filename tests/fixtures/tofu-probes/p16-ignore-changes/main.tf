# P16 (005 T007): `ignore_changes = [tags["lz:run-id"]]` keeps the creating run id while another
# tag still follows the configuration. `control` has no ignore_changes: it shows the run id does
# change without the rule. Applied twice against a mock provider in main.tftest.hcl.
terraform {
  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "2.21.0"
    }
  }
}

variable "run_id" {
  type = string
}

variable "tenant" {
  type = string
}

resource "ovh_cloud_project_storage" "kept" {
  service_name = "p16-fixture-project"
  region_name  = "GRA"
  name         = "p16-kept"
  tags = {
    "lz:run-id" = var.run_id
    "lz:tenant" = var.tenant
  }

  lifecycle {
    ignore_changes = [tags["lz:run-id"]]
  }
}

resource "ovh_cloud_project_storage" "control" {
  service_name = "p16-fixture-project"
  region_name  = "GRA"
  name         = "p16-control"
  tags = {
    "lz:run-id" = var.run_id
    "lz:tenant" = var.tenant
  }
}
