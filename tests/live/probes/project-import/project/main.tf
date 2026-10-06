# The adopted project as research R7's modules/cloud-project will hold it: prevent_destroy and
# deletion_protection. The order arguments (`ovh_subsidiary`, `plan`) are required to create a
# project and are not known for an existing one: the plan values below are the documented ovh-eu
# ones (cloud_project.md: plan_code `project.2018`); duration and pricing mode are UNVERIFIED and
# are exactly what the P5 plan shows a difference for, or not.
terraform {
  required_version = ">= 1.13.0"

  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "2.21.0"
    }
  }
}

variable "ovh_subsidiary" {
  description = "OVHcloud subsidiary of the account (data source ovh_me)."
  type        = string
}

variable "description" {
  description = "The project's current description, kept so the import plans no change to it."
  type        = string
}

resource "ovh_cloud_project" "this" {
  ovh_subsidiary      = var.ovh_subsidiary
  description         = var.description
  deletion_protection = true

  plan {
    duration     = "P1M"
    plan_code    = "project.2018"
    pricing_mode = "default"
  }

  lifecycle {
    prevent_destroy = true
  }
}

output "project_id" {
  description = "Project id after import."
  value       = ovh_cloud_project.this.project_id
}
