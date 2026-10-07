// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

terraform {
  backend "s3" {
    bucket = "lz-bkt-state"
    endpoints = {
      s3 = "https://s3.gra.io.cloud.ovh.net"
    }
    key                         = "account/account-governance/terraform.tfstate"
    region                      = "gra"
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
    use_lockfile                = true
  }
  encryption {
    key_provider "pbkdf2" "main" {
      passphrase = var.state_passphrase
    }
    method "aes_gcm" "main" {
      keys = key_provider.pbkdf2.main
    }
    state {
      enforced = true
      method   = method.aes_gcm.main
    }
    plan {
      enforced = true
      method   = method.aes_gcm.main
    }
  }
}
