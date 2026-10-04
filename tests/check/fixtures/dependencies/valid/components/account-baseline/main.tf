# A singleton component with a nested subdirectory of its own package, using
# the naming module through another spelling of this repository's address.
module "policy" {
  source = "./policy"
}

module "naming" {
  source = "git::ssh://git@github.com/PlatformRelay/ovh-landing-zone-accelerator.git//modules/naming?ref=v1.0.0"
  name   = "baseline"
}
