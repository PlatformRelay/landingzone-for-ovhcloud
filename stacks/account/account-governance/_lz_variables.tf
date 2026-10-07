// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

variable "state_passphrase" {
  description = "State and plan encryption passphrase (TF_VAR_state_passphrase from the bound account, research R5)."
  nullable    = false
  sensitive   = true
  type        = string
}
variable "lz_tenants" {
  description = "Tenant names of the manifest (generated _lz_tenants.auto.tfvars.json)."
  nullable    = false
  type        = list(string)
}
variable "tenants" {
  description = "Resolved project per tenant (data-model *Resolved-reference input*)."
  nullable    = false
  type = map(object({
    project_id  = string
    project_urn = string
  }))
}
