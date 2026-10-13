output "urn" {
  description = "IAM URN the tags are set on, read from the tags resource."
  value       = ovh_iam_resource_tags.this.urn
}

output "tags" {
  description = "Tags the URN carries, read from the tags resource."
  value       = ovh_iam_resource_tags.this.tags
}
