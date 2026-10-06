// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

terraform {
  backend "s3" {
    key = "x.tfstate"
  }
}

provider "ovh" {
  endpoint = "ovh-eu"
}

module "platform" {
  source = "../../stages/platform"
}
