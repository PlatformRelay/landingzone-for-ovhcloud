# Accepted: a test file asserts on the secret it unmarks.
mock_provider "ovh" {}

run "secret" {
  command = plan

  variables {
    secret = "x"
  }

  assert {
    condition     = nonsensitive(output.client_secret_plain) == "x"
    error_message = "secret"
  }
}
