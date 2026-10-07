# Inputs of the protected bucket module for state buckets.
# Versioning is always on and is not an input; the name and the tags come from modules/naming.

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
