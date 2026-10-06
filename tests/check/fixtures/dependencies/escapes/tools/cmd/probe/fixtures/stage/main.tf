provider "ovh" {
  endpoint = "ovh-eu"
}

data "terraform_remote_state" "up" {
  backend = "s3"
  config  = {}
}
