// The one stage call, the adopt-only import and the root variables of every stack (005 T038;
// FR-002, FR-007, data-model *Resolved-reference input*, *Envelope-to-input adapter*). Manifest
// values are literals; resolved references and producer values are root variables the live lane
// fills.

generate_hcl "_lz_main.tf" {
  condition = global.lz.stage == "bootstrap"
  content {
    module "bootstrap" {
      source           = global.lz.source
      org              = global.lz.org
      state_project_id = var.state_project_id
      state_region     = tm_upper(global.lz.spec.state.region)
      instance         = terramate.stack.id
      managed_in       = global.lz.managed_in
    }
  }
}

generate_hcl "_lz_main.tf" {
  condition = global.lz.stage == "tenant-state"
  content {
    module "tenant_state" {
      source           = global.lz.source
      org              = global.lz.org
      tenant           = global.lz.tenant
      state_project_id = var.state_project_id
      state_region     = tm_upper(global.lz.spec.state.region)
      instance         = terramate.stack.id
      managed_in       = global.lz.managed_in
    }
  }
}

generate_hcl "_lz_main.tf" {
  condition = global.lz.stage == "account-governance"
  content {
    module "account_governance" {
      source     = global.lz.source
      org        = global.lz.org
      instance   = terramate.stack.id
      managed_in = global.lz.managed_in
      tenants    = { for t in var.lz_tenants : t => var.tenants[t] }
    }
  }
}

generate_hcl "_lz_main.tf" {
  condition = global.lz.stage == "project"
  content {
    module "project" {
      source       = global.lz.source
      org          = global.lz.org
      tenant       = global.lz.tenant
      environment  = global.lz.envname
      instance     = terramate.stack.id
      managed_in   = global.lz.managed_in
      project_mode = global.lz.env.project.mode
      project_id   = var.project_id
      regions      = [for r in global.lz.env.regions : r.name]
      budget_alert = {
        enabled = tm_try(global.lz.env.budget_alert.enabled, false)
      }
      quota_guard = {
        enabled = tm_try(global.lz.env.quota_guard.enabled, false)
      }
    }
  }
}

generate_hcl "_lz_main.tf" {
  condition = global.lz.stage == "project-network"
  content {
    module "project_network" {
      source     = global.lz.source
      org        = global.lz.org
      region     = global.lz.region
      instance   = terramate.stack.id
      managed_in = global.lz.managed_in
      project    = var.project
      network = {
        cidr    = global.lz.network.cidr
        vlan_id = global.lz.network.vlan_id
      }
    }
  }
}

generate_hcl "_lz_main.tf" {
  condition = global.lz.stage == "runtime"
  content {
    module "runtime" {
      source     = global.lz.source
      org        = global.lz.org
      region     = global.lz.region
      instance   = terramate.stack.id
      managed_in = global.lz.managed_in
      project    = var.project
      slot       = global.lz.slot
    }
  }
}

generate_hcl "_lz_import.tf" {
  condition = global.lz.stage == "project" && tm_try(global.lz.env.project.mode, "") == "adopt"
  content {
    import {
      to = module.project.module.project_factory.module.project.ovh_cloud_project.this[0]
      id = var.project_id
    }
  }
}

generate_file "_lz_tenants.auto.tfvars.json" {
  condition = global.lz.stage == "account-governance"
  content   = tm_jsonencode({ lz_tenants = global.lz.tenant_names })
}

generate_hcl "_lz_variables.tf" {
  content {
    variable "state_passphrase" {
      description = "State and plan encryption passphrase (TF_VAR_state_passphrase from the bound account, research R5)."
      type        = string
      sensitive   = true
      nullable    = false
    }

    tm_dynamic "variable" {
      for_each = global.lz.stage == "bootstrap" ? ["lz_account_dir"] : []
      labels   = ["lz_account_dir"]
      content {
        description = "Absolute path of the bound account directory accounts/<account> (local state, research R5)."
        type        = string
        nullable    = false
        validation {
          condition     = can(regex("^/.*/accounts/[^/]+$", var.lz_account_dir))
          error_message = "lz_account_dir must be an absolute path ending in accounts/<account>."
        }
      }
    }

    tm_dynamic "variable" {
      for_each = tm_contains(["bootstrap", "tenant-state"], global.lz.stage) ? ["state_project_id"] : []
      labels   = ["state_project_id"]
      content {
        description = "Resolved id of spec.state.project (data-model *Resolved-reference input*)."
        type        = string
        nullable    = false
      }
    }

    tm_dynamic "variable" {
      for_each = global.lz.stage == "account-governance" ? ["lz_tenants"] : []
      labels   = ["lz_tenants"]
      content {
        description = "Tenant names of the manifest (generated _lz_tenants.auto.tfvars.json)."
        type        = list(string)
        nullable    = false
      }
    }

    tm_dynamic "variable" {
      for_each = global.lz.stage == "account-governance" ? ["tenants"] : []
      labels   = ["tenants"]
      content {
        description = "Resolved project per tenant (data-model *Resolved-reference input*)."
        type = map(object({
          project_id  = string
          project_urn = string
        }))
        nullable = false
      }
    }

    tm_dynamic "variable" {
      for_each = global.lz.stage == "project" ? ["project_id"] : []
      labels   = ["project_id"]
      content {
        description = "Resolved project id of the environment (data-model *Resolved-reference input*)."
        type        = string
        nullable    = false
      }
    }

    tm_dynamic "variable" {
      for_each = tm_contains(["project-network", "runtime"], global.lz.stage) ? ["project"] : []
      labels   = ["project"]
      content {
        description = "Published values of the project stage (schemas/outputs/project.schema.json)."
        type = object({
          tenant          = string
          environment     = string
          project_id      = string
          project_urn     = string
          regions         = list(string)
          budget_alert_id = optional(string)
          unlabelled      = list(string)
        })
        nullable = false
      }
    }
  }
}
