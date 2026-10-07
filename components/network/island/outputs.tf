output "project_id" {
  description = "Project the network belongs to."
  value       = module.network.project_id
}

output "network_id" {
  description = "Id of the private network."
  value       = module.network.network_id
}

output "network_name" {
  description = "Name of the private network (modules/naming)."
  value       = module.network.name
}

output "regions" {
  description = "Regions of the private network."
  value       = module.network.regions
}

output "vlan_id" {
  description = "VLAN id of the private network."
  value       = module.network.vlan_id
}

output "regions_openstack_ids" {
  description = "OpenStack id of the network per region."
  value       = module.network.regions_openstack_ids
}

output "subnet_id" {
  description = "Id of the subnet."
  value       = module.network.subnet_id
}

output "cidr" {
  description = "CIDR of the subnet."
  value       = module.network.cidr
}

output "unlabelled" {
  description = "Addresses (from the component root) of the resources whose API carries no tags."
  value = [
    "module.network.ovh_cloud_project_network_private.this",
    "module.network.ovh_cloud_project_network_private_subnet.this",
  ]
}
