# Probe root boilerplate (spec 005 T008, research R12 *Probe state*). Run only through
# `task live:probe` (lz-live probe), which passes the state path and the per-run passphrase:
# state stays outside the checkout, under
# ~/.config/ovh-lz/accounts/<account>/state/probes/<run-id>/<root>.tfstate, encrypted (P4).
terraform {
  required_version = ">= 1.13.0"

  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "2.21.0"
    }
  }

  backend "local" {
    path = var.state_path
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

variable "state_path" {
  description = "Probe state file, set by lz-live probe (TF_VAR_state_path); its directory name is the run id."
  type        = string

  validation {
    condition     = can(regex("^/.+/\\.config/ovh-lz/accounts/[^/]+/state/probes/[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}/state-backend\\.tfstate$", var.state_path))
    error_message = "state_path must be ~/.config/ovh-lz/accounts/<account>/state/probes/<run-id>/state-backend.tfstate: run this root through `task live:probe`."
  }
}

variable "state_passphrase" {
  description = "Per-run state passphrase, set by lz-live probe (TF_VAR_state_passphrase)."
  type        = string
  sensitive   = true
}

variable "run_id" {
  description = "Run id, set by lz-live probe (TF_VAR_run_id): the directory of state_path."
  type        = string

  validation {
    condition     = var.run_id == basename(dirname(var.state_path))
    error_message = "run_id must be the run id of state_path's directory: run this root through `task live:probe`."
  }
}

variable "project_id" {
  description = "Sandbox project id, set by lz-live probe from LZ_PROJECT_ID_STATE of account.env (TF_VAR_project_id)."
  type        = string
}

# The sandbox project's record: the plan fails when the account has no project with that id.
data "ovh_cloud_projects" "all" {
  lifecycle {
    postcondition {
      condition     = length([for p in self.projects : p if p.service_name == var.project_id]) == 1
      error_message = "project_id is not a public cloud project of this account: check LZ_PROJECT_ID_STATE in account.env."
    }
  }
}

locals {
  run_id  = var.run_id
  project = one([for p in data.ovh_cloud_projects.all.projects : p if p.service_name == var.project_id])
}
