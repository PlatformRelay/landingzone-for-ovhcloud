# Inputs of the naming module (FR-001, FR-002, ADR-0003, data-model.md *Naming template* and
# *Label set*). Name errors surface on `output.name`; a label-set key in `labels` fails here.

variable "org" {
  description = "Organisation discriminator, the first naming segment of the default template (`lz`, D87)."
  type        = string
  nullable    = false
}

variable "tenant" {
  description = "Tenant name; null at account scope, where the segment and the `lz:tenant` label are absent."
  type        = string
  default     = null
}

variable "environment" {
  description = "Environment name; null above environment scope."
  type        = string
  default     = null
}

variable "region" {
  description = "Region name (e.g. `GRA11`); null above region scope. The template's case rule applies."
  type        = string
  default     = null
}

variable "kind" {
  description = "Logical resource kind, a key of `template.kinds` with a row in `kinds.yaml` (e.g. `bucket`)."
  type        = string
  nullable    = false
}

variable "role" {
  description = "Human role of the resource within its scope (e.g. `state`, `runtime`)."
  type        = string
  nullable    = false
}

variable "slot" {
  description = "Runtime slot; null when the scope holds one runtime. Two slots in one scope give two names."
  type        = string
  default     = null
}

variable "template" {
  description = "Naming template as data: ordered segments, separator, case rule and kind abbreviations. Only the default template ships."
  type = object({
    version   = number
    segments  = list(string)
    separator = string
    case      = string
    kinds     = map(string)
  })
  default = {
    version   = 1
    segments  = ["org", "tenant", "environment", "region", "kind", "role", "slot"]
    separator = "-"
    case      = "lower"
    kinds = {
      bucket          = "bkt"
      private_network = "pn"
      subnet          = "sn"
      service_account = "sa"
      iam_policy      = "pol"
      identity_group  = "grp"
      s3_user         = "s3u"
      project         = "prj"
    }
  }
  nullable = false

  validation {
    condition     = var.template.version == 1
    error_message = "template.version: only name algorithm version 1 exists."
  }

  validation {
    condition     = alltrue([for s in var.template.segments : contains(["org", "tenant", "environment", "region", "kind", "role", "slot"], s)])
    error_message = "template.segments: only org, tenant, environment, region, kind, role and slot are segments."
  }

  validation {
    condition     = length(var.template.segments) == length(distinct(var.template.segments))
    error_message = "template.segments: each segment at most once."
  }

  validation {
    condition     = contains(var.template.segments, "kind") && contains(var.template.segments, "role")
    error_message = "template.segments: kind and role are required."
  }

  validation {
    condition     = alltrue([for k, v in var.template.kinds : v != null && v != ""])
    error_message = "template.kinds: every kind needs a non-empty abbreviation; a null or empty one would drop the kind segment."
  }

  validation {
    condition     = var.template.case == "lower"
    error_message = "template.case: only \"lower\" is implemented."
  }

  validation {
    condition     = var.template.separator != ""
    error_message = "template.separator must not be empty."
  }
}

variable "name_override" {
  description = "Existing name to keep (import); passed through unchanged after validation against the kind's limits."
  type        = string
  default     = null
}

variable "instance" {
  description = "Immutable deployment instance id, the `lz:instance` label."
  type        = string
  nullable    = false

  validation {
    condition     = var.instance != ""
    error_message = "instance must not be empty: it is the lz:instance label."
  }
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`, the `lz:managed-in` label."
  type        = string
  nullable    = false

  validation {
    condition     = var.managed_in != ""
    error_message = "managed_in must not be empty: it is the lz:managed-in label."
  }
}

variable "labels" {
  description = "Extra labels merged into the mandatory set; keys of the `lz:` label set are refused."
  type        = map(string)
  default     = {}
  nullable    = false

  validation {
    condition = length(setintersection(
      [for k in keys(var.labels) : lower(k)],
      ["lz:managed-by", "lz:managed-in", "lz:instance", "lz:tenant", "lz:release", "lz:run-id"],
    )) == 0
    error_message = "labels: a key of the lz: label set (lz:managed-by, lz:managed-in, lz:instance, lz:tenant, lz:release, lz:run-id; any case) may not be set as an extra label."
  }
}
