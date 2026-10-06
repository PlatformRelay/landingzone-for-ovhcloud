terraform {
  backend "s3" {
    key = "x.tfstate"
  }
}

data "terraform_remote_state" "up" {
  backend = "s3"
  config  = {}
}

module "m" {
  source = "../missing"
}
