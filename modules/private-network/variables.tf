# Inputs of the private-network module (spec 005 T029 interface; research R9): one private network
# in one region of a Public Cloud project and one subnet with the given CIDR.

variable "project_id" {
  description = "Public Cloud project id (service name) the network belongs to."
  type        = string
  nullable    = false

  validation {
    condition     = trimspace(var.project_id) != ""
    error_message = "project_id must not be blank."
  }
}

variable "name" {
  description = "Network name (modules/naming, kind `private_network`); the only label the network carries (R14)."
  type        = string
  nullable    = false
}

variable "region" {
  description = "Region the network and the subnet are created in (e.g. `GRA11`)."
  type        = string
  nullable    = false

  validation {
    condition     = trimspace(var.region) != ""
    error_message = "region must not be blank."
  }
}

# /29 is the smallest network that leaves a DHCP pool beside the network, first host and broadcast
# addresses (believed; OVHcloud's subnet limits are UNVERIFIED). cidrnetmask refuses IPv6, and
# cidrsubnet(c, 0, 0) == c refuses host bits (tofu 1.13.0, T029 probe).
variable "cidr" {
  description = "IPv4 CIDR of the subnet (`regions[].network.cidr`, e.g. `10.20.0.0/24`)."
  type        = string
  nullable    = false

  validation {
    condition     = can(cidrnetmask(var.cidr)) && try(cidrsubnet(var.cidr, 0, 0) == var.cidr, false) && try(tonumber(split("/", var.cidr)[1]) <= 29, false)
    error_message = "cidr must be an IPv4 network in CIDR form without host bits, /29 or larger."
  }
}

variable "vlan_id" {
  description = "VLAN id of the network (`regions[].network.vlan_id`, default 0)."
  type        = number
  default     = 0
  nullable    = false
}
