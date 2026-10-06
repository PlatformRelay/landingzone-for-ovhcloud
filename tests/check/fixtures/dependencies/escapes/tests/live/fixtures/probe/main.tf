data "terraform_remote_state" "up" {
  backend = "s3"
  config  = {}
}

module "m" {
  source = "../missing"
}
