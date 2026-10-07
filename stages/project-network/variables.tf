# Inputs of the project-network stage (spec 005 T029 interface; research R9; data-model *Stage
# table*, *Per-stage values* `project-network`, *Envelope-to-input adapter*): one private network
# and one subnet in the instance's region of the environment's project. The project comes only from
# the `project` stage's published values (`project`, the data edge; the generated stack declares
# the same variable from schemas/outputs/project.schema.json, and the adapter has checked it
# against the bound reference, KD-3). No backend or provider configuration: the generated stack
# owns both.

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "region" {
  description = "Region of the instance (`instances[].region`, e.g. `GRA11`); must be one of `project.regions`."
  type        = string
  nullable    = false

  validation {
    condition     = contains(var.project.regions, var.region)
    error_message = "region must be one of the project's regions."
  }
}

variable "instance" {
  description = "Deployment instance id (e.g. `demo-dev-gra11-network`)."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`."
  type        = string
  nullable    = false
}

variable "project" {
  description = "Published values of the environment's `project` stage (schemas/outputs/project.schema.json)."
  type = object({
    tenant          = string
    environment     = string
    project_id      = string
    project_urn     = string
    regions         = list(string)
    budget_alert_id = optional(string)
    unlabelled      = list(string)
  })
  nullable = false
}

variable "network" {
  description = "The region's network row (`spec.tenants[].environments[].regions[].network`): IPv4 `cidr` of the subnet and `vlan_id` (default 0)."
  type = object({
    cidr    = string
    vlan_id = optional(number, 0)
  })
  nullable = false

  validation {
    condition     = can(cidrnetmask(var.network.cidr)) && try(cidrsubnet(var.network.cidr, 0, 0) == var.network.cidr, false)
    error_message = "network.cidr must be an IPv4 network in CIDR form without host bits."
  }

  validation {
    condition     = try(tonumber(split("/", var.network.cidr)[1]) >= 16 && tonumber(split("/", var.network.cidr)[1]) <= 29, false)
    error_message = "network.cidr must have a prefix length from /16 to /29."
  }

  validation {
    condition     = try(anytrue([for b in ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"] : cidrcontains(b, var.network.cidr)]), false)
    error_message = "network.cidr must lie inside one RFC 1918 block (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)."
  }
}
