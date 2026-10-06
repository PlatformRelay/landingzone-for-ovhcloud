terraform {
  backend "s3" {
    key = "x.tfstate"
  }
}

provider "ovh" {
  endpoint = "ovh-eu"
}

module "kube" {
  source = "../../../../components/runtime/kube"
}
