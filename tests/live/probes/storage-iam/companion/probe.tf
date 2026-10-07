# Companion root of storage-iam (spec 005 T075; T008 decision request 1, option A). lz-live runs
# it after storage-iam's apply, under the probe identity that root publishes (output
# companion_env: OVH_CLIENT_ID/OVH_CLIENT_SECRET of the P9 tenant identity, the P25 identity as
# TF_VAR_p25_client_id/_secret, the P1 S3 user's keys as AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY),
# never under the admin credential, and destroys it before storage-iam's destroy removes the
# identities. Run only through `task live:probe`.
#
# Its state is the P1–P3 object: an S3 backend on storage-iam's bucket lzprobe-p1-<run-id> with
# the lock file (use_lockfile, P1), the OVHcloud backend options (P2; values from
# use-object-storage-terraform-backend-state.mdx:109-125, endpoint host as in
# export-billing-data-to-bucket.mdx:25) and client-side encryption (P3). While this root's apply
# holds the lock, lz-live starts a second writer of the same state, which the lock must refuse.
terraform {
  required_version = ">= 1.13.0"

  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "2.21.0"
    }
  }

  # Variables in a backend block: OpenTofu early evaluation (lower() on a variable observed with
  # host tofu 1.10.3 on a local backend, t075-tofu-probe.sh, 2026-10-07).
  backend "s3" {
    bucket = "lzprobe-p1-${lower(var.run_id)}"
    key    = "storage-iam-companion.tfstate"
    region = "gra"
    endpoints = {
      s3 = "https://s3.gra.io.cloud.ovh.net/"
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

variable "state_passphrase" {
  description = "Per-run state passphrase, set by lz-live probe (TF_VAR_state_passphrase)."
  type        = string
  sensitive   = true
}

variable "run_id" {
  description = "Run id, set by lz-live probe (TF_VAR_run_id): names the state bucket and every resource."
  type        = string

  validation {
    condition     = can(regex("^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$", var.run_id))
    error_message = "run_id must be YYYYMMDDThhmmssZ-<4 hex>: run this root through `task live:probe`."
  }
}

# The tenant identity holds no project read (P9 allowlist), so the project comes from lz-live only.
variable "project_id" {
  description = "Sandbox project id, set by lz-live probe (TF_VAR_project_id)."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{32}$", var.project_id))
    error_message = "project_id must be a public cloud project id (32 hex characters)."
  }
}

variable "p25_client_id" {
  description = "Client id of storage-iam's P25 identity (companion_env TF_VAR_p25_client_id)."
  type        = string
  sensitive   = true
}

variable "p25_client_secret" {
  description = "Client secret of storage-iam's P25 identity (companion_env TF_VAR_p25_client_secret)."
  type        = string
  sensitive   = true
}

# The default configuration reads OVH_ENDPOINT, OVH_CLIENT_ID and OVH_CLIENT_SECRET (the P9 tenant
# identity) from the environment (provider docs index.md:51). The P25 identity takes the endpoint
# from OVH_ENDPOINT too (per-argument fallback: believed, UNVERIFIED).
provider "ovh" {
  alias         = "p25"
  client_id     = var.p25_client_id
  client_secret = var.p25_client_secret
}
