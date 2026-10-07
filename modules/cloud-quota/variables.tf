# Inputs of the cloud-quota module (spec 005 T025 stub; T026 implements). The validation the tests
# expect is T026's; the flag and the regions are not inputs.

variable "project_id" {
  description = "Public Cloud project id (service name) whose quota envelope is set."
  type        = string
  nullable    = false
}

variable "enabled" {
  description = "Quota guard (P11): when true, automatic quota upgrades are disabled; when false, the module manages nothing."
  type        = bool
  default     = false
  nullable    = false
}
