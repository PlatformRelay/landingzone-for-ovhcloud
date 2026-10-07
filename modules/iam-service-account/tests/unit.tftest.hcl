# OAuth2 service-account module (spec 005 T019; FR-004, FR-010; ADR-0003, ADR-0018; research R6).
# Mocked provider, no credential, no API call. Attribute names from the pinned ovh 2.21.0 schema
# (`ovh_me_api_oauth2_client`: name, description, flow (required), callback_urls (optional),
# client_id, identity (computed), client_secret (computed, sensitive); no `discard_client_secret`,
# which is 2.22.0 only, spec P20).
#
# The client is a machine identity: flow `CLIENT_CREDENTIALS`, which needs no callback URL
# (docs.ovhcloud.com manage-and-operate/api/manage-service-account.mdx:130-135; provider docs
# me_api_oauth2_client.md:22-29,47-48). The flow is fixed, not an input. Its `identity` URN
# (`urn:v1:eu:identity:credential:<nic>/oauth2-<clientId>`, manage-service-account.mdx:154) is
# what an IAM policy names in `identities` (me_api_oauth2_client.md:60; spec P8, UNVERIFIED live).
# The client secret leaves the module only as a sensitive output; it is checked against the
# module's other outputs by its literal mock value (an output added later is not seen here:
# outputs cannot be enumerated in a test). The name is a modules/naming output (kind
# `service_account`); the module does not validate it.
#
# Mock values are fixed so that they are known at plan time (OpenTofu 1.13.0 mock_resource defaults).

mock_provider "ovh" {
  mock_resource "ovh_me_api_oauth2_client" {
    defaults = {
      id            = "mock-oauth2-resource-id"
      client_id     = "0f0f0f0f0f0f0f0f"
      client_secret = "mock-oauth2-client-secret-not-a-secret"
      identity      = "urn:v1:eu:identity:credential:xx1111-ovh/oauth2-0f0f0f0f0f0f0f0f"
    }
  }
}

variables {
  name        = "lz-demo-sa-deployer"
  description = "lz-demo-sa-deployer: tenant deployer of demo"
}

run "client_credentials_flow" {
  command = plan

  assert {
    condition     = ovh_me_api_oauth2_client.this.flow == "CLIENT_CREDENTIALS"
    error_message = "the OAuth2 client uses the CLIENT_CREDENTIALS flow"
  }

  assert {
    condition     = ovh_me_api_oauth2_client.this.callback_urls == null ? true : length(ovh_me_api_oauth2_client.this.callback_urls) == 0
    error_message = "a CLIENT_CREDENTIALS client has no callback URL"
  }
}

run "flow_not_switchable" {
  command = plan

  # Not module inputs: a caller cannot switch the flow or add a callback URL. A variable the module
  # does not declare is ignored by `tofu test` (observed in T013), so this run checks the outcome.
  variables {
    flow          = "AUTHORIZATION_CODE"
    callback_urls = ["https://example.invalid/callback"]
  }

  assert {
    condition     = ovh_me_api_oauth2_client.this.flow == "CLIENT_CREDENTIALS"
    error_message = "the flow stays CLIENT_CREDENTIALS whatever the caller passes"
  }

  assert {
    condition     = ovh_me_api_oauth2_client.this.callback_urls == null ? true : length(ovh_me_api_oauth2_client.this.callback_urls) == 0
    error_message = "no callback URL whatever the caller passes"
  }
}

run "given_name_and_description" {
  command = plan

  assert {
    condition     = ovh_me_api_oauth2_client.this.name == "lz-demo-sa-deployer"
    error_message = "the client carries the given name"
  }

  assert {
    condition     = ovh_me_api_oauth2_client.this.description == "lz-demo-sa-deployer: tenant deployer of demo"
    error_message = "the client carries the given description"
  }
}

run "other_name_and_description" {
  command = plan

  variables {
    name        = "lz-acme-sa-platform"
    description = "lz-acme-sa-platform: platform deployer of acme"
  }

  assert {
    condition     = ovh_me_api_oauth2_client.this.name == "lz-acme-sa-platform" && ovh_me_api_oauth2_client.this.description == "lz-acme-sa-platform: platform deployer of acme"
    error_message = "a second name and description pass through unchanged"
  }
}

run "identity_urn_exported" {
  command = plan

  assert {
    condition     = output.identity == ovh_me_api_oauth2_client.this.identity && output.identity == "urn:v1:eu:identity:credential:xx1111-ovh/oauth2-0f0f0f0f0f0f0f0f"
    error_message = "the identity output is the client's identity URN"
  }

  assert {
    condition     = !issensitive(output.identity)
    error_message = "the identity URN is not sensitive (it is published for policies)"
  }

  assert {
    condition     = output.client_id == ovh_me_api_oauth2_client.this.client_id && output.client_id == "0f0f0f0f0f0f0f0f" && !issensitive(output.client_id)
    error_message = "the client_id output is the client's id, not sensitive"
  }
}

run "secret_only_a_sensitive_output" {
  command = plan

  assert {
    condition     = issensitive(output.client_secret)
    error_message = "the client secret output is sensitive"
  }

  assert {
    condition     = nonsensitive(output.client_secret) == "mock-oauth2-client-secret-not-a-secret"
    error_message = "the client secret output is the client's secret"
  }

  assert {
    condition     = !strcontains(jsonencode([output.client_id, output.identity]), "mock-oauth2-client-secret-not-a-secret")
    error_message = "the secret is in no other output"
  }
}
