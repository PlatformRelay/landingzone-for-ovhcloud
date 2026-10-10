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

  validation {
    condition     = contains(var.project_regions, var.region)
    error_message = "region must be one of the project's regions."
  }
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

  validation {
    condition     = can(cidrnetmask(var.cidr)) && try(cidrsubnet(var.cidr, 0, 0) == var.cidr, false)
    error_message = "cidr must be an IPv4 network in CIDR form without host bits."
  }

  validation {
    condition     = try(tonumber(split("/", var.cidr)[1]) >= 16 && tonumber(split("/", var.cidr)[1]) <= 29, false)
    error_message = "cidr must have a prefix length from /16 to /29."
  }

  validation {
    condition     = try(anytrue([for b in ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"] : cidrcontains(b, var.cidr)]), false)
    error_message = "cidr must lie inside one RFC 1918 block (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)."
  }
}

# A project holds one private network per VLAN id (T098; T010 run 20261010T184645Z-f7de: 400
# `A private network already exists for this vlan ID`), so the id is required: a default would give
# every network that omits it the same id. Bounds 0..4000: the API admits 0..4095, 0 meaning no
# VLAN (API schema cloud.ProjectNetworkPrivateCreation.vlanId), the Control Panel 0..4000 (vRack
# guide); the narrower holds. That the API refuses 4001..4095 is UNVERIFIED.
variable "vlan_id" {
  description = "VLAN id of the network (`regions[].network.vlan_id`), 0..4000, distinct per project."
  type        = number
  nullable    = false

  validation {
    condition     = var.vlan_id >= 0 && var.vlan_id <= 4000 && floor(var.vlan_id) == var.vlan_id
    error_message = "vlan_id must be a whole number from 0 to 4000."
  }
}
