# Provider-free root for the output-contract tests (005 T017, FR-005, SC-005). `tofu output -json`
# of this root, captured through capture.sh, is what the envelope builder reads: plain outputs of
# every JSON shape a stage publishes, and sensitive ones that must never reach an envelope. The
# sensitive values are synthetic seeds, not secrets; the tests look for them in everything the
# builder returns.

output "state_bucket" {
  value = "lz-bkt-state"
}

output "regions" {
  value = ["GRA11"]
}

output "scope" {
  value = {
    instance   = "demo-dev-gra11-runtime"
    project_id = "0123456789abcdef0123456789abcdef"
    region     = "GRA11"
  }
}

output "replicas" {
  value = 3
}

output "versioned" {
  value = true
}

output "unlabelled" {
  value = ["module.state_backend.ovh_cloud_project_user.s3[\"platform\"]"]
}

output "platform_s3" {
  value = {
    access_key_id     = "LZSEEDT017ACCESSKEY"
    secret_access_key = "lz-seed-t017-secret-access-key"
  }
  sensitive = true
}

# A sensitive output whose name and value match no secret pattern: only the sensitive marker
# can drop it.
output "bootstrap_handle" {
  value     = "lz-seed-t017-neutral-handle"
  sensitive = true
}

output "platform_deployer_secret" {
  value     = "lz-seed-t017-deployer-secret"
  sensitive = true
}
