# A component over a library module, plus two external sources: a registry
# module and another repository's subdirectory.
module "net" {
  source = "../../../modules/net"
}

module "registry" {
  source  = "ovh/thing/ovh"
  version = "1.0.0"
}

module "foreign" {
  source = "git::https://github.com/example/other.git//modules/x?ref=v1.0.0"
}
