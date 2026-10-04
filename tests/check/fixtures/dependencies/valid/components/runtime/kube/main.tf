# A component over a library module, this repository's naming module
# addressed as a released package (a package boundary that maps back to the
# local directory), and two external sources: a registry module and another
# repository's subdirectory.
module "net" {
  source = "../../../modules/net"
}

module "naming" {
  source = "git::https://github.com/PlatformRelay/ovh-landing-zone-accelerator.git//modules/naming?ref=v1.0.0"
  name   = "kube"
}

module "registry" {
  source  = "ovh/thing/ovh"
  version = "1.0.0"
}

module "foreign" {
  source = "git::https://github.com/example/other.git//modules/x?ref=v1.0.0"
}
