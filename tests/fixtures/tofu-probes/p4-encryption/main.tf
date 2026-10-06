# P4 (005 T007): the local backend path and the PBKDF2 passphrase of client-side state
# encryption both come from variables, evaluated before init reads the backend. Provider-free.
variable "state_path" {
  type = string
}

variable "passphrase" {
  type      = string
  sensitive = true
}

terraform {
  backend "local" {
    path = var.state_path
  }

  encryption {
    key_provider "pbkdf2" "main" {
      passphrase = var.passphrase
    }
    method "aes_gcm" "main" {
      keys = key_provider.pbkdf2.main
    }
    state {
      method   = method.aes_gcm.main
      enforced = true
    }
    plan {
      method   = method.aes_gcm.main
      enforced = true
    }
  }
}

resource "terraform_data" "marker" {
  input = "p4"
}
