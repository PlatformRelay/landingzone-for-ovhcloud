// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

terraform {
  backend "s3" {
    bucket       = "lz-state"
    key          = "account/bootstrap.tfstate"
    use_lockfile = true
  }
  encryption {}
}
