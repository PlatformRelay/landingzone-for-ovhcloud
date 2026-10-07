# Stub for the T011 tests: the interface only, no validation and no logic.
# T012 implements the module (FR-001, FR-002, ADR-0003, data-model.md).

variable "org" {
  description = "Organisation discriminator, the first naming segment of the default template (`lz`, D87)."
  type        = string
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
}

variable "role" {
  description = "Human role of the resource within its scope (e.g. `state`, `runtime`)."
  type        = string
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
    }
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
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`, the `lz:managed-in` label."
  type        = string
}

variable "labels" {
  description = "Extra labels merged into the mandatory set; keys of the `lz:` label set are refused."
  type        = map(string)
  default     = {}
}
