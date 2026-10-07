output "network_id" {
  description = "Id of the private network."
  value       = module.island.network_id
}

output "regions_openstack_ids" {
  description = "OpenStack id of the network per region."
  value       = module.island.regions_openstack_ids
}

output "subnet_id" {
  description = "Id of the subnet."
  value       = module.island.subnet_id
}

output "cidr" {
  description = "CIDR of the subnet."
  value       = module.island.cidr
}

output "unlabelled" {
  description = "Addresses (from the stage root) of the resources whose API carries no tags (research R14)."
  value       = [for a in module.island.unlabelled : "module.island.${a}"]
}
