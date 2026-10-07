output "id" {
  description = "Id of the IAM policy."
  value       = ovh_iam_policy.this.id
}

output "name" {
  description = "Name of the IAM policy."
  value       = ovh_iam_policy.this.name
}
