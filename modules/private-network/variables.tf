# Inputs of the private-network module (spec 005 T029 interface; research R9): one private network
# in one region of a Public Cloud project and one subnet with the given CIDR.

variable "project_id" {
  description = "Public Cloud project id (service name) the network belongs to."
  type        = string
  nullable    = false
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
}

variable "cidr" {
  description = "IPv4 CIDR of the subnet (`regions[].network.cidr`, e.g. `10.20.0.0/24`)."
  type        = string
  nullable    = false
}

variable "vlan_id" {
  description = "VLAN id of the network (`regions[].network.vlan_id`, default 0)."
  type        = number
  default     = 0
  nullable    = false
}
