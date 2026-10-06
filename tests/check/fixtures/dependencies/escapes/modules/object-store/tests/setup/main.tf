terraform {
  backend "s3" {
    key = "x.tfstate"
  }
}

provider "ovh" {
  endpoint = "ovh-eu"
}
