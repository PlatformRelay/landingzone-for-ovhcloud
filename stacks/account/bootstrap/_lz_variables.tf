// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

variable "state_passphrase" {
  description = "State and plan encryption passphrase (TF_VAR_state_passphrase from the bound account, research R5)."
  nullable    = false
  sensitive   = true
  type        = string
}
variable "lz_account_dir" {
  description = "Absolute path of the bound account directory accounts/<account> (local state, research R5)."
  nullable    = false
  type        = string
  validation {
    condition     = can(regex("^/.*/accounts/[^/]+$", var.lz_account_dir))
    error_message = "lz_account_dir must be an absolute path ending in accounts/<account>."
  }
}
variable "state_project_id" {
  description = "Resolved id of spec.state.project (data-model *Resolved-reference input*)."
  nullable    = false
  type        = string
}
