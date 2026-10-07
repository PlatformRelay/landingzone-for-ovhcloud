# Stub (spec 005 T025): the resource addresses the tests pin, no behaviour. T026 implements adopt
# mode (`ovh_cloud_project.this[0]` with `deletion_protection = true` and a literal
# `lifecycle { prevent_destroy = true }`, research R7), reference mode (`data.ovh_cloud_project`),
# the tags on the project URN and the optional budget alert.
resource "ovh_cloud_project" "this" {
  count = 0
}

data "ovh_cloud_project" "this" {
  count = 0

  service_name = "not-implemented"
}

resource "ovh_iam_resource_tags" "this" {
  urn  = "not-implemented"
  tags = {}
}

resource "ovh_cloud_project_alerting" "this" {
  count = 0

  delay             = 3600
  email             = "not-implemented"
  monthly_threshold = 0
}
