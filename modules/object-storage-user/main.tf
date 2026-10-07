# Stub (spec 005 T013): the resource addresses the tests pin, no behaviour. T014 implements.
resource "ovh_cloud_project_user" "this" {
  service_name = "not-implemented"
}

resource "ovh_cloud_project_user_s3_credential" "this" {
  service_name = "not-implemented"
  user_id      = "not-implemented"
}

resource "ovh_cloud_project_user_s3_policy" "this" {
  service_name = "not-implemented"
  user_id      = "not-implemented"
  policy       = jsonencode({ Statement = [] })
}
