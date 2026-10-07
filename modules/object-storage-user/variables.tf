# Inputs of the bucket-scoped S3 user module.

variable "project_id" {
  description = "Public Cloud project id (service name) that holds the user and the buckets."
  type        = string
  nullable    = false
}

variable "description" {
  description = "User description, from modules/naming (kind `s3_user`); the user has no name or tags of its own."
  type        = string
  nullable    = false
}

variable "buckets" {
  description = "Names of the buckets the user's S3 policy allows, and nothing else; at least one, each within the bucket-name charset and length (no wildcard, no ARN, no path)."
  type        = list(string)
  nullable    = false

  validation {
    condition     = length(var.buckets) > 0
    error_message = "buckets: at least one bucket."
  }

  # The bucket name rule (modules/naming/kinds.yaml `bucket`: 3-63 of [a-z0-9.-], alphanumeric
  # ends), so no IAM wildcard (`*`, `?`), ARN or path reaches the policy's Resource.
  validation {
    condition     = alltrue([for b in var.buckets : can(regex("^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", b))])
    error_message = "buckets: each a bucket name; no wildcard, no ARN, no null."
  }
}
