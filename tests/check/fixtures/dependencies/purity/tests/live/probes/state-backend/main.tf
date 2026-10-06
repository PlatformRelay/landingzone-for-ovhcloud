# A live probe root (test layer) configures its own backend and provider.
terraform {
  backend "s3" {
    key          = "probes/state-backend.tfstate"
    use_lockfile = true
  }
}

provider "ovh" {
  endpoint = "ovh-eu"
}
