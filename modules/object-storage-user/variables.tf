# Inputs of the bucket-scoped S3 user module (spec 005 T013 stub; T014 implements).

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
  description = "Names of the buckets the user's S3 policy allows, and nothing else; at least one, each a valid bucket name (no wildcard, no ARN)."
  type        = list(string)
  nullable    = false
}
