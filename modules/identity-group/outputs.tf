output "urn" {
  description = "URN of the group, for the `identities` of an IAM policy."
  value       = ovh_me_identity_group.this.urn
}

output "name" {
  description = "Name of the group."
  value       = ovh_me_identity_group.this.name
}

output "role" {
  description = "Role of the group, as the resource carries it."
  value       = ovh_me_identity_group.this.role
}
