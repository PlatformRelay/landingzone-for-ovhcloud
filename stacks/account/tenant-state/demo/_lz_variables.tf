// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

variable "state_passphrase" {
  description = "State and plan encryption passphrase (TF_VAR_state_passphrase from the bound account, research R5)."
  nullable    = false
  sensitive   = true
  type        = string
}
variable "state_project_id" {
  description = "Resolved id of spec.state.project (data-model *Resolved-reference input*)."
  nullable    = false
  type        = string
}
