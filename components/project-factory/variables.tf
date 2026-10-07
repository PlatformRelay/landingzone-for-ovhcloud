# Inputs of the project-factory component (spec 005 T027 interface; research R7, R14; data-model
# *Per-stage values* `project`, *Resolved-reference input*): one existing Public Cloud project,
# adopted or referenced through modules/cloud-project, its labels, the optional budget alert and the
# optional quota guard (modules/cloud-quota). Labels come from modules/naming inside the component
# (a stage may not call naming, ADR-0002).

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "tenant" {
  description = "Tenant that owns the project (`spec.tenants[].name`); the `lz:tenant` label."
  type        = string
  nullable    = false
}

variable "environment" {
  description = "Environment of the project (`spec.tenants[].environments[].name`)."
  type        = string
  nullable    = false
}

variable "instance" {
  description = "Deployment instance id (e.g. `demo-dev-project`), the `lz:instance` label."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`, the `lz:managed-in` label."
  type        = string
  nullable    = false
}

variable "mode" {
  description = "`adopt` (the project is managed, imported by the stack at module.project.ovh_cloud_project.this[0]) or `reference` (only read); research R7."
  type        = string
  nullable    = false
}

variable "project_id" {
  description = "Public Cloud project id resolved from the bound account's reference (`LZ_PROJECT_ID_<REF>`, KD-3)."
  type        = string
  nullable    = false
}

variable "ovh_subsidiary" {
  description = "Adopt mode: the project's subsidiary as the import plan shows it (R7, P5); passed unchanged."
  type        = string
  default     = null
}

variable "description" {
  description = "Adopt mode: the project's current description; passed unchanged."
  type        = string
  default     = null
}

variable "plan" {
  description = "Adopt mode: the project's order plan as the import plan shows it; passed unchanged, null sends none."
  type = object({
    duration     = string
    plan_code    = string
    pricing_mode = string
  })
  default = null
}

variable "budget_alert" {
  description = "Optional budget alert (P10), passed to modules/cloud-project unchanged."
  type = object({
    enabled           = bool
    monthly_threshold = optional(number)
    email             = optional(string)
    delay             = optional(number, 3600)
  })
  default  = { enabled = false }
  nullable = false
}

variable "quota_guard" {
  description = "Optional quota guard (P11): when enabled, modules/cloud-quota disables automatic quota upgrades."
  type = object({
    enabled = bool
  })
  default  = { enabled = false }
  nullable = false
}
