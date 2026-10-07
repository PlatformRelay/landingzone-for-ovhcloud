# Stub (spec 005 T029): placeholder values. T030 reads the published values
# (schemas/outputs/project-network.schema.json) from `module.island`.

output "network_id" {
  description = "Id of the private network."
  value       = "not-implemented"
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
  description = "Addresses (from the stage root) of the resources whose API carries no tags (research R14)."
  value       = []
}
