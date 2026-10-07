# One OAuth2 service account (FR-004, FR-010; research R6): a client of the `CLIENT_CREDENTIALS`
# flow, which needs no callback URL (docs.ovhcloud.com manage-and-operate/api/
# manage-service-account, *Create a service account*). Provider resource
# `ovh_me_api_oauth2_client` (ovh 2.21.0). The client secret stays in the encrypted state, from
# which the credential writer takes it once (R6); `discard_client_secret` exists from provider
# 2.22.0 only (research R6, P20).
resource "ovh_me_api_oauth2_client" "this" {
  name        = var.name
  description = var.description
  flow        = "CLIENT_CREDENTIALS"
}
