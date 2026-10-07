# Stub (spec 005 T029): placeholder values. T030 reads each output from `module.network`.

output "project_id" {
  description = "Project the network belongs to."
  value       = "not-implemented"
}

output "network_id" {
  description = "Id of the private network."
  value       = "not-implemented"
}

output "network_name" {
  description = "Name of the private network (modules/naming)."
  value       = "not-implemented"
}

output "regions" {
  description = "Regions of the private network."
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
  description = "CIDR of the subnet."
  value       = "not-implemented"
}

output "unlabelled" {
  description = "Addresses (from the component root) of the resources whose API carries no tags."
  value       = []
}
