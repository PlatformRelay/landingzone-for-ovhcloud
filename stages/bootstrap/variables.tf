# Inputs of the bootstrap stage (spec 005 T015 interface; research R5, data-model *Stage table*):
# the account state bucket and the platform S3 user, nothing tenant-scoped (tenant buckets come from
# `tenant-state`, D87/D88). No backend or provider configuration: the generated stack owns both.

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "state_project_id" {
  description = "Public Cloud project id that holds the account state bucket (`spec.state.project`)."
  type        = string
  nullable    = false
}

variable "state_region" {
  description = "Object Storage region of the account state bucket (`spec.state.region`, e.g. `GRA`)."
  type        = string
  nullable    = false
}

variable "instance" {
  description = "Deployment instance id (`account-bootstrap`), the `lz:instance` label."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`, the `lz:managed-in` label."
  type        = string
  nullable    = false
}
