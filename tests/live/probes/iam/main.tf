# P7, P8 (T010): the sandbox admin may create and delete an OAuth2 client and an IAM policy, and the
# client's identity URN is usable as a policy identity. The policy grants exactly one read
# (publicCloudProject:apiovh:get, GET /cloud/project/{serviceName}; kb/api/v1/cloud.json); a call
# outside it (GET /cloud/project/{serviceName}/region, region/get) must be denied.
# P15 (T010): an IAM resource tag whose key holds `:` on the project URN. The key starts with
# `lzprobe-` so it never touches the retained project's `lz:` tags, and the leftover check matches
# it by that prefix and by its value (the run id). Only the keys in `tags` are managed: destroy
# removes them and leaves other tags alone (iam_resource_tags.md).
resource "ovh_me_api_oauth2_client" "probe" {
  name        = "lzprobe-iam-${local.run_id}"
  description = "lzprobe-iam-${local.run_id}: P7/P8 probe identity, destroyed by the run"
  flow        = "CLIENT_CREDENTIALS"
}

resource "ovh_iam_policy" "probe" {
  name        = "lzprobe-iam-${local.run_id}"
  description = "lzprobe-iam-${local.run_id}: one read for the P8 probe identity"
  identities  = [ovh_me_api_oauth2_client.probe.identity]
  resources   = [local.project.iam.urn]
  allow       = ["publicCloudProject:apiovh:get"]
}

resource "ovh_iam_resource_tags" "probe" {
  urn = local.project.iam.urn
  tags = {
    "lzprobe-p15:run-id" = local.run_id
  }
}

output "client_name" {
  description = "Probe OAuth2 client name (leftover match: name prefix)."
  value       = ovh_me_api_oauth2_client.probe.name
}

output "client_identity" {
  description = "Identity URN of the probe client, as bound in the probe policy (P8)."
  value       = ovh_me_api_oauth2_client.probe.identity
}

output "policy_name" {
  description = "Probe IAM policy name (leftover match: name prefix)."
  value       = ovh_iam_policy.probe.name
}
