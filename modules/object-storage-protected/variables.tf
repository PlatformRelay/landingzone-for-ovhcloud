# Inputs of the protected bucket module for state buckets.
# Versioning and Object Lock in governance mode are always on and are not inputs (only the
# retention's length is); the name and the tags come from modules/naming.

variable "project_id" {
  description = "Public Cloud project id (service name) that holds the bucket."
  type        = string
  nullable    = false
}

variable "region" {
  description = "Object Storage region name of the bucket (e.g. `GRA`)."
  type        = string
  nullable    = false
}

variable "name" {
  description = "Bucket name, from modules/naming (kind `bucket`); applied unchanged."
  type        = string
  nullable    = false
}

variable "tags" {
  description = "Bucket tags, exactly as given: the labels output of modules/naming. A null value is refused."
  type        = map(string)
  nullable    = false

  validation {
    condition     = alltrue([for k, v in var.tags : v != null])
    error_message = "tags: a null value is refused."
  }
}

variable "retention_days" {
  description = "Days every new object version stays locked in governance mode (Object Lock default rule): a whole number from 1 to 365."
  type        = number
  nullable    = false

  # 1: the guide's minimum (docs.ovhcloud.com storage-and-backup/object-storage/
  # s3-managing-object-lock.mdx:51, "a minimum of one day and no maximum limit"); 365: ours
  # (spec 005 T099), a longer retention is an owner decision.
  validation {
    condition     = var.retention_days == floor(var.retention_days) && var.retention_days >= 1 && var.retention_days <= 365
    error_message = "retention_days: a whole number of days from 1 to 365."
  }
}
