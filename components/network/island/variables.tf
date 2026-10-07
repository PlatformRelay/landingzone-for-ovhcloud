# Inputs of the network/island component (spec 005 T029 interface; research R9, R14): one private
# network and one subnet in one region of the environment's project, no gateway.

variable "org" {
  description = "Organisation discriminator, the first naming segment (`spec.org`, `lz`, D87)."
  type        = string
  nullable    = false
}

variable "tenant" {
  description = "Tenant of the environment; a naming segment of the network's name."
  type        = string
  nullable    = false
}

variable "environment" {
  description = "Environment; a naming segment of the network's name."
  type        = string
  nullable    = false
}

variable "region" {
  description = "Region of the instance (e.g. `GRA11`); must be one of `project_regions`."
  type        = string
  nullable    = false
}

variable "instance" {
  description = "Deployment instance id (e.g. `demo-dev-gra11-network`), for modules/naming."
  type        = string
  nullable    = false
}

variable "managed_in" {
  description = "Owning code location `<forge>//<stack path>`, for modules/naming."
  type        = string
  nullable    = false
}

variable "project_id" {
  description = "Project id from the `project` stage's outputs (bound reference, KD-3)."
  type        = string
  nullable    = false
}

variable "project_regions" {
  description = "Region names of the project from the `project` stage's outputs (`regions`)."
  type        = list(string)
  nullable    = false
}

variable "cidr" {
  description = "IPv4 CIDR of the subnet (`regions[].network.cidr`)."
  type        = string
  nullable    = false
}

variable "vlan_id" {
  description = "VLAN id of the network (`regions[].network.vlan_id`, default 0)."
  type        = number
  default     = 0
  nullable    = false
}
