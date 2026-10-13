# Inputs of the iam-resource-tags module (spec 005 T105).

variable "urn" {
  description = "IAM URN of the resource to tag, exactly as given (e.g. a project's `urn:v1:eu:resource:publicCloudProject:<id>`)."
  type        = string
  nullable    = false

  validation {
    condition     = trimspace(var.urn) != ""
    error_message = "urn must not be empty."
  }
}

variable "tags" {
  description = "IAM resource tags for the URN, exactly as given: the labels output of modules/naming. A null value is refused."
  type        = map(string)
  nullable    = false

  validation {
    condition     = alltrue([for k, v in var.tags : v != null])
    error_message = "tags: a null value is refused."
  }
}
