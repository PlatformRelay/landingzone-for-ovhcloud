// State backend, client-side encryption and provider of every stack (005 T038;
// FR-008, ADR-0009, research R5, R16). S3 backend flags per the OVHcloud guide
// (use-object-storage-terraform-backend-state); their OpenTofu behaviour is premise P1-P3 (T010).
// Encryption is the P4 shape: PBKDF2 from var.state_passphrase, aes_gcm, state and plan enforced.
// The provider carries no credential: the live lane supplies it through the environment.
generate_hcl "_lz_backend.tf" {
  condition = global.lz.stage != "bootstrap"
  content {
    terraform {
      backend "s3" {
        bucket = global.lz.bucket
        key    = global.lz.key
        region = global.lz.spec.state.region
        endpoints = {
          s3 = global.lz.spec.state.endpoint
        }
        use_lockfile                = true
        skip_credentials_validation = true
        skip_region_validation      = true
        skip_requesting_account_id  = true
        skip_s3_checksum            = true
      }
      encryption {
        key_provider "pbkdf2" "main" {
          passphrase = var.state_passphrase
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
  }
}

generate_hcl "_lz_backend.tf" {
  condition = global.lz.stage == "bootstrap"
  content {
    terraform {
      backend "local" {
        path = "${var.lz_account_dir}/state/${terramate.stack.id}.tfstate"
      }
      encryption {
        key_provider "pbkdf2" "main" {
          passphrase = var.state_passphrase
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
  }
}

generate_hcl "_lz_providers.tf" {
  content {
    terraform {
      required_version = ">= 1.13.0"
      required_providers {
        ovh = {
          source  = "ovh/ovh"
          version = "~> 2.21"
        }
      }
    }
    provider "ovh" {
    }
  }
}
