# Inputs of the account-governance stage (spec 005 T023 interface; research R6, data-model *Stage
# table* and *Resolved-reference input*): the platform deployer and, per tenant, a deployer, its
# policy and an identity group. `tenants` is the resolved-reference input the live lane writes per
# run (`tenants = {<t>: {project_id, project_urn}}`, ids from `LZ_PROJECT_ID_<REF>`). No backend or
# provider configuration: the generated stack owns both.

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "instance" {
  description = "Deployment instance id (`account-governance`)."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>` (`…//stacks/account/account-governance`)."
  type        = string
  nullable    = false
}

variable "tenants" {
  description = "Per tenant name: its project id and project URN (data-model *Resolved-reference input*)."
  type = map(object({
    project_id  = string
    project_urn = string
  }))
  nullable = false
}
