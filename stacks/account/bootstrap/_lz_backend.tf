// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

terraform {
  backend "local" {
    path = "${var.lz_account_dir}/state/account-bootstrap.tfstate"
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
