# Spec 003 T032 (US3; FR-007 C007.5, C007.6): how long an access token already issued to an OAuth2
# client stays usable after the client is deleted (UNVERIFIED until T026). The window needs a
# delete inside the run, so lz-live's check (tools/internal/probes/live/identity/residual.go), not
# this root, creates the client and its one-read policy lzprobe-token-residual-<run id>, deletes the
# client, and sweeps both by that exact name. This root creates nothing: it only checks that the
# project is the account's and publishes the check's variables.
locals {
  # The one read the check's policy grants: GET /cloud/project/{serviceName}
  # (publicCloudProject:apiovh:get, kb api/v1/cloud.json). The check refuses any other path.
  read_path = "/cloud/project/${local.project.service_name}"
}

# The check's variables, read by lz-live from `tofu output -json` into memory only. All three are
# public (identity.TokenResidual Public): no secret passes through this root.
output "residual_check" {
  description = "Variables of lz-live's token-residual check (spec 003 T032): run id, project and the read its policy grants."
  sensitive   = true
  value = {
    run_id     = local.run_id
    project_id = local.project.service_name
    read_path  = local.read_path
  }
}
