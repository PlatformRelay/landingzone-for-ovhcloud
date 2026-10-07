output "client_id" {
  description = "Client id of the OAuth2 client."
  value       = null
}

output "identity" {
  description = "Identity URN of the OAuth2 client, for the `identities` of an IAM policy."
  value       = null
}

output "client_secret" {
  description = "Client secret of the OAuth2 client; sensitive, never published."
  value       = null
  sensitive   = true
}
