# Inputs of the cloud-project module (spec 005 T025 stub; T026 implements; research R7: adopt or
# reference mode). The validations the tests expect are T026's.

variable "mode" {
  description = "`adopt`: the existing project is managed (imported by the stack) as `ovh_cloud_project`; `reference`: the project itself is only read (its IAM tags are still managed)."
  type        = string
  nullable    = false
}

variable "project_id" {
  description = "Public Cloud project id (service name) of the existing project."
  type        = string
  nullable    = false
}

variable "ovh_subsidiary" {
  description = "Adopt mode: the project's OVHcloud subsidiary, as the import plan shows it. Passed unchanged; never invented."
  type        = string
  default     = null
}

variable "description" {
  description = "Adopt mode: the project's current description, passed unchanged so the import plans no change to it."
  type        = string
  default     = null
}

variable "plan" {
  description = "Adopt mode: the project's order plan as the import plan shows it (duration, plan_code, pricing_mode). Passed unchanged; null sends no plan."
  type = object({
    duration     = string
    plan_code    = string
    pricing_mode = string
  })
  default = null
}

variable "tags" {
  description = "IAM resource tags for the project URN, exactly as given: the labels output of modules/naming. A null value is refused."
  type        = map(string)
  nullable    = false
}

variable "budget_alert" {
  description = "Optional budget alert on the project (P10): monthly threshold in the account currency, contact email, delay between alerts in seconds."
  type = object({
    enabled           = bool
    monthly_threshold = optional(number)
    email             = optional(string)
    delay             = optional(number, 3600)
  })
  default  = { enabled = false }
  nullable = false
}
