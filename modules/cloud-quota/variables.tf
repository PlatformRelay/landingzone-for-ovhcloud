# Inputs of the cloud-quota module (spec 005, P11). The flag and the regions are not inputs.

variable "project_id" {
  description = "Public Cloud project id (service name) whose quota envelope is set."
  type        = string
  nullable    = false

  validation {
    condition     = trimspace(var.project_id) != ""
    error_message = "project_id must not be empty."
  }
}

variable "enabled" {
  description = "Quota guard (P11): when true, automatic quota upgrades are disabled; when false, the module manages nothing."
  type        = bool
  default     = false
  nullable    = false
}
