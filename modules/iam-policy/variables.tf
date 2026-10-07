# Inputs of the IAM policy module (spec 005 T019 pins them; T020 implements). The module passes
# them to `ovh_iam_policy` unchanged; which actions and resources a policy may hold is the
# caller's decision (components/identity/ovh-native, guard G5).

variable "name" {
  description = "Policy name, from modules/naming (kind `iam_policy`)."
  type        = string
  nullable    = false
}

variable "description" {
  description = "Policy description."
  type        = string
  nullable    = false
}

variable "identities" {
  description = "Identity URNs the policy applies to (OAuth2 client `identity`, group `urn`)."
  type        = set(string)
  nullable    = false
}

variable "resources" {
  description = "Resource URNs the policy applies to."
  type        = set(string)
  nullable    = false
}

variable "allow" {
  description = "IAM actions allowed on every given resource for every given identity."
  type        = set(string)
  nullable    = false
}

variable "conditions" {
  description = "Optional condition tree (at most three levels, provider schema); null = no conditions."
  type = object({
    operator = string
    values   = optional(map(string))
    condition = optional(list(object({
      operator = string
      values   = optional(map(string))
      condition = optional(list(object({
        operator = string
        values   = optional(map(string))
      })), [])
    })), [])
  })
  default = null
}
