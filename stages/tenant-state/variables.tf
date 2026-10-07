# Inputs of the tenant-state stage (spec 005 T021 interface; research R5 *Per-tenant buckets*,
# data-model *Stage table* and *Derived instance fields*): one tenant's state bucket in the state
# project (`spec.state.project`, KD-1) and its tenant and platform S3 users. No backend or provider
# configuration: the generated stack owns both.

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "tenant" {
  description = "Tenant whose state bucket this is (`spec.tenants[].name`); the `lz:tenant` label."
  type        = string
  nullable    = false
}

variable "state_project_id" {
  description = "Public Cloud project id that holds the state buckets (`spec.state.project`, KD-1)."
  type        = string
  nullable    = false
}

variable "state_region" {
  description = "Object Storage region of the state buckets (`spec.state.region`, e.g. `GRA`)."
  type        = string
  nullable    = false
}

variable "instance" {
  description = "Deployment instance id (e.g. `demo-state`), the `lz:instance` label."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`, the `lz:managed-in` label."
  type        = string
  nullable    = false
}
