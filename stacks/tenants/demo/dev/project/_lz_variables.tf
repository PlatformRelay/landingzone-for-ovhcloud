// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

variable "state_passphrase" {
  description = "State and plan encryption passphrase (TF_VAR_state_passphrase from the bound account, research R5)."
  nullable    = false
  sensitive   = true
  type        = string
}
variable "project_id" {
  description = "Resolved project id of the environment (data-model *Resolved-reference input*)."
  nullable    = false
  type        = string
}
