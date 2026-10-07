# Stub (spec 005 T029): placeholder values. T030 reads each output from the network or the subnet.

output "project_id" {
  description = "Project (service name) the network belongs to, read from the network."
  value       = "not-implemented"
}

output "network_id" {
  description = "Id of the private network."
  value       = "not-implemented"
}

output "name" {
  description = "Name of the private network."
  value       = "not-implemented"
}

output "regions" {
  description = "Regions of the private network (one: the given region)."
  value       = ["NOT-IMPLEMENTED"]
}

output "vlan_id" {
  description = "VLAN id of the private network."
  value       = -1
}

output "regions_openstack_ids" {
  description = "OpenStack id of the network per region."
  value       = {}
}

output "subnet_id" {
  description = "Id of the subnet."
  value       = "not-implemented"
}

output "cidr" {
  description = "CIDR of the subnet, as given."
  value       = "not-implemented"
}
