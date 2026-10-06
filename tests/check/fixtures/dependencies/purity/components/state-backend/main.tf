# A component composes modules and naming.
module "name" {
  source = "../../modules/naming"
}

module "bucket" {
  source = "../../modules/object-storage-protected"
}
