# Inputs of the runtime/managed-only component (spec 005 T031 interface; research R8, R15, R22;
# ADR-0017): one empty, labelled Object Storage bucket in the environment's project, named through
# modules/naming with the instance's optional `slot`, and the runtime envelope. Stub (T031): the
# `slot` rule the tests pin is T032's.

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "tenant" {
  description = "Tenant of the environment; a naming segment and the `lz:tenant` label."
  type        = string
  nullable    = false
}

variable "environment" {
  description = "Environment; a naming segment of the bucket's name."
  type        = string
  nullable    = false
}

variable "region" {
  description = "Region of the instance (e.g. `GRA11`): a naming segment and `scope.region`. The bucket's Object Storage region is its leading letters, upper case (`GRA`; UNVERIFIED until T010); the derivation must not fail on any string, so a refused region fails only its own rule."
  type        = string
  nullable    = false
}

variable "instance" {
  description = "Deployment instance id (e.g. `demo-dev-gra11-runtime`): the `lz:instance` label and `scope.instance`."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`, the `lz:managed-in` label."
  type        = string
  nullable    = false
}

variable "project_id" {
  description = "Project id from the `project` stage's outputs (bound reference, KD-3)."
  type        = string
  nullable    = false
}

variable "slot" {
  description = "Runtime slot (`^[a-z][a-z0-9]{0,15}$`); null when the scope holds one runtime. A naming segment, so two slots give two bucket names."
  type        = string
  default     = null
}
