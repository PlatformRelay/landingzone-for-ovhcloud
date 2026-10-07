# Inputs of the identity group module (spec 005 T019 pins them; T020 implements).

variable "name" {
  description = "Group name, from modules/naming (kind `identity_group`)."
  type        = string
  nullable    = false
}

variable "description" {
  description = "Group description."
  type        = string
  nullable    = false
}

variable "role" {
  description = "Account role of the group's members: ADMIN, REGULAR, UNPRIVILEGED or NONE (default)."
  type        = string
  default     = "NONE"
  nullable    = false
}
