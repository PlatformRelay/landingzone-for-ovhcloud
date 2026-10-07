output "client_id" {
  description = "Client id of the OAuth2 client."
  value       = ovh_me_api_oauth2_client.this.client_id
}

output "identity" {
  description = "Identity URN of the OAuth2 client, for the `identities` of an IAM policy."
  value       = ovh_me_api_oauth2_client.this.identity
}

output "client_secret" {
  description = "Client secret of the OAuth2 client; sensitive, never published."
  value       = ovh_me_api_oauth2_client.this.client_secret
  sensitive   = true
}
