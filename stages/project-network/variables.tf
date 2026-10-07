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
}
