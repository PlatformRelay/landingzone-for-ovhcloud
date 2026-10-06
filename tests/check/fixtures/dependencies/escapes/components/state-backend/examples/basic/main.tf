provider "ovh" {
  endpoint = "ovh-eu"
}

module "state" {
  source = "../.."
}
