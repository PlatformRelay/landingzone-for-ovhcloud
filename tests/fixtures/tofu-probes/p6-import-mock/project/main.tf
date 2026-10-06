terraform {
  required_providers {
    ovh = {
      source = "ovh/ovh"
    }
  }
}

resource "ovh_cloud_project" "this" {
  ovh_subsidiary = "FR"
  description    = "p6-fixture"
  plan {
    duration     = "P1M"
    plan_code    = "project.2018"
    pricing_mode = "default"
  }
}

output "project_id" {
  value = ovh_cloud_project.this.id
}
