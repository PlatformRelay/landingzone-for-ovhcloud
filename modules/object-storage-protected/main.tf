# Stub (spec 005 T013): the resource address the tests pin, no behaviour. T014 implements it with
# versioning always on and a literal `lifecycle { prevent_destroy = true }` (research R5
# *Protection*: a lifecycle argument cannot depend on a variable).
resource "ovh_cloud_project_storage" "this" {
  service_name = "not-implemented"
  region_name  = "not-implemented"
  name         = "not-implemented"
  tags         = {}
}
