output "id" {
  description = "Id of the IAM policy."
  value       = ovh_iam_policy.this.id
}

output "name" {
  description = "Name of the IAM policy."
  value       = ovh_iam_policy.this.name
}

output "identities" {
  description = "Identity URNs of the policy, as the resource carries them."
  value       = ovh_iam_policy.this.identities
}

output "resources" {
  description = "Resource URNs of the policy, as the resource carries them."
  value       = ovh_iam_policy.this.resources
}

output "allow" {
  description = "Allowed actions of the policy, as the resource carries them."
  value       = ovh_iam_policy.this.allow
}
