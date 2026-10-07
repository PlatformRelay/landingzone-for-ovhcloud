# Inputs of the project stage (spec 005 T027 interface; research R7, R24; data-model *Stage table*,
# *Per-stage values* `project`, *Resolved-reference input*): one tenant environment's existing
# Public Cloud project, adopted or referenced, with its labels, regions and optional budget alert and
# quota guard. `project_id` is the resolved reference of the environment (`LZ_PROJECT_ID_<REF>`,
# written by the live lane, KD-3), never a manifest value. No backend or provider configuration:
# the generated stack owns both.

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "tenant" {
  description = "Tenant of the environment (`spec.tenants[].name`); the `lz:tenant` label and the published `tenant`."
  type        = string
  nullable    = false
}

variable "environment" {
  description = "Environment (`spec.tenants[].environments[].name`); the published `environment`."
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

variable "project_mode" {
  description = "`adopt` or `reference` (`spec.tenants[].environments[].project.mode`, research R7)."
  type        = string
  nullable    = false
}

variable "project_id" {
  description = "Project id resolved from the environment's reference (`LZ_PROJECT_ID_<REF>`, data-model *Resolved-reference input*)."
  type        = string
  nullable    = false
}

variable "project_ovh_subsidiary" {
  description = "Adopt mode: the project's subsidiary as the import plan shows it (R7, P5; filled in T039 from T009)."
  type        = string
  default     = null
}

variable "project_description" {
  description = "Adopt mode: the project's current description, passed unchanged."
  type        = string
  default     = null
}

variable "project_plan" {
  description = "Adopt mode: the project's order plan as the import plan shows it; null sends none."
  type = object({
    duration     = string
    plan_code    = string
    pricing_mode = string
  })
  default = null
}

variable "regions" {
  description = "Region names of the environment (`spec.tenants[].environments[].regions[].name`, e.g. `GRA11`), published as given."
  type        = list(string)
  nullable    = false
}

variable "budget_alert" {
  description = "Optional budget alert (`budget_alert`, P10)."
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
  description = "Optional quota guard (`quota_guard`, P11)."
  type = object({
    enabled = bool
  })
  default  = { enabled = false }
  nullable = false
}
