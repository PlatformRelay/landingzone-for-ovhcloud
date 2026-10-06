provider "ovh" {
  endpoint = "ovh-eu"
}

module "store" {
  source = "../.."
}
