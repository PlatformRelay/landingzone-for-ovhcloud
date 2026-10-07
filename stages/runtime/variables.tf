# Inputs of the runtime stage (spec 005 T031 interface; research R8, R22; data-model *Stage table*,
# *Per-stage values* `runtime`, *Envelope-to-input adapter*): one managed-only runtime in the
# instance's region of the environment's project. The stage consumes only the `project` stage's
# published values (`project`, the data edge; the generated stack declares the same variable from
# schemas/outputs/project.schema.json, and the adapter has checked it against the bound reference,
# KD-3); `runtime` does not depend on `project-network`. No backend or provider configuration: the
# generated stack owns both. Stub (T031): the `region` and `slot` rules the tests pin are T032's.

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "region" {
  description = "Region of the instance (`instances[].region`, e.g. `GRA11`); must be one of `project.regions`."
  type        = string
  nullable    = false
}

variable "instance" {
  description = "Deployment instance id (e.g. `demo-dev-gra11-runtime`, `demo-dev-gra11-runtime-blue`)."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`."
  type        = string
  nullable    = false
}

variable "slot" {
  description = "Runtime slot of the instance (`instances[].slot`, `^[a-z][a-z0-9]{0,15}$`); null when the scope holds one runtime."
  type        = string
  default     = null
}

variable "project" {
  description = "Published values of the environment's `project` stage (schemas/outputs/project.schema.json)."
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
