# A nested subdirectory that reaches the naming module through a different
# spelling of the same path (an alias).
module "name" {
  source = "./../../naming/"
  name   = "sub"
}
