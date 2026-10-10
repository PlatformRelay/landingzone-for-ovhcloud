# Inputs of the state-backend component (spec 005 T015 interface; research R1, R5): one protected,
# versioned state bucket and the S3 users given, each confined to that bucket. Names and labels
# come from modules/naming inside the component (a stage may not call naming, ADR-0002).

variable "project_id" {
  description = "Public Cloud project id (service name) that holds the bucket and the users (`spec.state.project`)."
  type        = string
  nullable    = false
}

variable "region" {
  description = "Object Storage region of the bucket (e.g. `GRA`); the S3 endpoint is derived from it."
  type        = string
  nullable    = false
}

variable "org" {
  description = "Organisation discriminator, the first naming segment (`lz`, D87)."
  type        = string
  nullable    = false
}

variable "tenant" {
  description = "Tenant whose state bucket this is; null for the account bucket (no tenant segment, no `lz:tenant` label)."
  type        = string
  default     = null
}

variable "instance" {
  description = "Deployment instance id, the `lz:instance` label."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`, the `lz:managed-in` label."
  type        = string
  nullable    = false
}

variable "s3_users" {
  description = "Keys of the S3 users to create (e.g. `platform`, `tenant`); each gets one credential and a policy confined to this bucket."
  type        = set(string)
  nullable    = false
}

variable "state_retention_days" {
  description = "Days every state object version stays locked (S3 Object Lock, governance mode; T099): a whole number from 1 to 365, default 7."
  type        = number
  default     = 7
  nullable    = false

  # 1: docs.ovhcloud.com storage-and-backup/object-storage/s3-managing-object-lock.mdx:51 ("a
  # minimum of one day and no maximum limit"); 365: ours (spec 005 T099), a longer retention is an
  # owner decision. Default 7: a short week of history (D104 asks for a short default).
  validation {
    condition     = var.state_retention_days == floor(var.state_retention_days) && var.state_retention_days >= 1 && var.state_retention_days <= 365
    error_message = "state_retention_days: a whole number of days from 1 to 365."
  }
}
