// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

variable "state_passphrase" {
  description = "State and plan encryption passphrase (TF_VAR_state_passphrase from the bound account, research R5)."
  nullable    = false
  sensitive   = true
  type        = string
}
variable "project" {
  description = "Published values of the project stage (schemas/outputs/project.schema.json)."
  nullable    = false
  type = object({
    tenant          = string
    environment     = string
    project_id      = string
    project_urn     = string
    regions         = list(string)
    budget_alert_id = optional(string)
    unlabelled      = list(string)
  })
}
