# Inputs of the cloud-project module (spec 005; research R7: adopt or reference mode).

variable "mode" {
  description = "`adopt`: the existing project is managed (imported by the stack) as `ovh_cloud_project`; `reference`: the project itself is only read (its IAM tags are still managed)."
  type        = string
  nullable    = false

  validation {
    condition     = contains(["adopt", "reference"], var.mode)
    error_message = "mode must be adopt or reference."
  }
}

variable "project_id" {
  description = "Public Cloud project id (service name) of the existing project."
  type        = string
  nullable    = false

  validation {
    condition     = trimspace(var.project_id) != ""
    error_message = "project_id must not be empty."
  }
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

  validation {
    condition     = alltrue([for k, v in var.tags : v != null])
    error_message = "tags: a null value is refused."
  }
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

  validation {
    condition     = !var.budget_alert.enabled || (try(var.budget_alert.monthly_threshold > 0, false) && try(trimspace(var.budget_alert.email) != "", false))
    error_message = "budget_alert: an enabled alert needs a monthly_threshold above 0 and an email."
  }

  validation {
    condition     = contains([3600, 10800, 21600, 43200, 86400, 172800, 259200, 604800], var.budget_alert.delay)
    error_message = "budget_alert.delay must be one of the API's cloud.AlertingDelayEnum values."
  }
}
