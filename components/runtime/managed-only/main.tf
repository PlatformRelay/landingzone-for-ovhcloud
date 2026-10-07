# Stub (spec 005 T031): the module call the tests pin, no behaviour. T032 names the bucket through
# modules/naming (kind `bucket`, role `runtime`, the instance's `slot`) and calls the unprotected
# modules/object-storage once, unkeyed, at `module.bucket` with the project, the Object Storage
# region and the naming labels as tags.
module "bucket" {
  source     = "../../../modules/object-storage"
  project_id = "not-implemented"
  region     = "NOT-IMPLEMENTED"
  name       = "not-implemented"
  tags       = {}
}
