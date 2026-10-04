# A stage over a component, and over this repository's own naming module
# addressed as a released package (a package boundary that maps back to the
# local directory).
module "kube" {
  source = "../../components/runtime/kube"
}

module "naming" {
  source = "git::https://github.com/PlatformRelay/ovh-landing-zone-accelerator.git//modules/naming?ref=v1.0.0"
  name   = "platform"
}
