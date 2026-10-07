# Inputs of the identity/ovh-native component (spec 005 T023 interface; research R6, data-model
# *Resolved-reference input*): the platform deployer, and per tenant a deployer, its policy and an
# identity group. Names come from modules/naming inside the component (a stage may not call naming,
# ADR-0002); none of these resources carries tags (research R14), so `instance` and `managed_in`
# only feed the naming call.

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
  description = "Owning code location `<forge>//<stack path>`."
  type        = string
  nullable    = false
}

variable "tenants" {
  description = "Per tenant name: its project id and project URN (data-model *Resolved-reference input*); the URN must be `urn:v1:<eu|ca>:resource:publicCloudProject:<project_id>`."
  type = map(object({
    project_id  = string
    project_urn = string
  }))
  nullable = false

  validation {
    condition     = length(var.tenants) > 0
    error_message = "tenants: at least one tenant."
  }

  # Defence in depth for G5 at the resolved-reference input (coordinator decision 2026-10-07): the
  # URN a tenant policy is scoped to is exactly the project of the same entry (form: research R6;
  # `ca` region part, manage-and-operate/iam/authenticate-api-openstack-with-service-account.mdx).
  validation {
    condition     = alltrue([for t in var.tenants : can(regex("^urn:v1:(eu|ca):resource:publicCloudProject:[0-9a-f]+$", t.project_urn)) && endswith(t.project_urn, ":${t.project_id}")])
    error_message = "tenants: project_urn must be urn:v1:<eu|ca>:resource:publicCloudProject:<project_id> of the same entry."
  }
}
