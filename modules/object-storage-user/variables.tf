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

variable "deny_actions" {
  description = "S3 actions the policy denies on every resource, in one unconditioned Deny statement; empty (default) adds none. Each an `s3:<Action>` name, or an `s3:<Prefix>*` pattern of at most 256 characters for an action OVHcloud accepts only as a pattern."
  type        = list(string)
  default     = []
  nullable    = false

  # OVHcloud accepts a named action or an `s3:` pattern ending in `*` of at most 256 characters
  # (tests/fixtures/ovhcloud/s3-policy-actions.json, T094). A Deny only narrows, so a trailing `*`
  # is admitted; a bare `s3:*` (it would deny everything) and a `*` or `?` inside the name are not.
  # This checks the shape only: whether a name is one OVHcloud accepts, and whether a pattern spares
  # the actions the caller still needs, is the caller's to test (components/state-backend tests).
  validation {
    condition     = alltrue([for a in var.deny_actions : a != null && can(regex("^s3:[A-Za-z]+\\*?$", a)) && length(a) <= 256])
    error_message = "deny_actions: each an s3:<Action> name or an s3:<Prefix>* pattern of at most 256 characters; no bare s3:*, no inner wildcard, no other service, no null."
  }
}
