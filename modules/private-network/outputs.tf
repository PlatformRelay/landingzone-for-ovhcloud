output "project_id" {
  description = "Project (service name) the network belongs to, read from the network."
  value       = ovh_cloud_project_network_private.this.service_name
}

output "network_id" {
  description = "Id of the private network."
  value       = ovh_cloud_project_network_private.this.id
}

output "name" {
  description = "Name of the private network."
  value       = ovh_cloud_project_network_private.this.name
}

output "regions" {
  description = "Regions of the private network (one: the given region)."
  value       = sort(tolist(ovh_cloud_project_network_private.this.regions))
}

output "vlan_id" {
  description = "VLAN id of the private network."
  value       = ovh_cloud_project_network_private.this.vlan_id
}

output "regions_openstack_ids" {
  description = "OpenStack id of the network per region."
  value       = ovh_cloud_project_network_private.this.regions_openstack_ids
}

output "subnet_id" {
  description = "Id of the subnet."
  value       = ovh_cloud_project_network_private_subnet.this.id
}

output "cidr" {
  description = "CIDR of the subnet, as given."
  value       = ovh_cloud_project_network_private_subnet.this.network
}
