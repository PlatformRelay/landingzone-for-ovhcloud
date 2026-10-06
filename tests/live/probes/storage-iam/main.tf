# P9, P26 (T010): a probe tenant identity — an OAuth2 client bound by an IAM policy holding exactly
# the narrowed tenant-deployer allowlist of research R6 (action names verified in
# kb/api/v1/cloud.json, 2026-10-06) on the sandbox project URN. The same identity makes the P26
# binding call (GET /auth/details) and is denied GET /me.
# P25 (T010, optional): two probe buckets that differ only in an `lz:tenant` tag, and a second
# probe client whose policy allows region/storage/get only under a resource-tag condition naming
# bucket A's tag. A kept separate from the P9 identity, whose unconditional region/storage/* would
# hide the condition.
# The identities are exercised outside this root (run sheet, *Open: second stage*): a provider
# configured from a resource created in the same run is not supported.
locals {
  name = "lzprobe-storage-iam-${local.run_id}"

  tenant_allow = [
    "publicCloudProject:apiovh:network/private/create",
    "publicCloudProject:apiovh:network/private/get",
    "publicCloudProject:apiovh:network/private/edit",
    "publicCloudProject:apiovh:network/private/delete",
    "publicCloudProject:apiovh:network/private/region/create",
    "publicCloudProject:apiovh:network/private/subnet/create",
    "publicCloudProject:apiovh:network/private/subnet/get",
    "publicCloudProject:apiovh:network/private/subnet/delete",
    "publicCloudProject:apiovh:region/storage/create",
    "publicCloudProject:apiovh:region/storage/get",
    "publicCloudProject:apiovh:region/storage/edit",
    "publicCloudProject:apiovh:region/storage/delete",
    "publicCloudProject:apiovh:region/storage/bulkDeleteObjects",
  ]

  buckets = {
    a = "lzprobe-p25a-${lower(local.run_id)}"
    b = "lzprobe-p25b-${lower(local.run_id)}"
  }
}

resource "ovh_me_api_oauth2_client" "tenant" {
  name        = local.name
  description = "${local.name}: P9/P26 probe tenant identity, destroyed by the run"
  flow        = "CLIENT_CREDENTIALS"
}

resource "ovh_iam_policy" "tenant" {
  name        = local.name
  description = "${local.name}: narrowed tenant-deployer allowlist (research R6)"
  identities  = [ovh_me_api_oauth2_client.tenant.identity]
  resources   = [local.project.iam.urn]
  allow       = local.tenant_allow
}

resource "ovh_cloud_project_storage" "p25" {
  for_each = local.buckets

  service_name = local.project.service_name
  region_name  = "GRA"
  name         = each.value
  tags = {
    "lz:run-id" = local.run_id
    "lz:tenant" = "lzprobe-${each.key}"
  }
}

resource "ovh_me_api_oauth2_client" "p25" {
  name        = "lzprobe-p25-${local.run_id}"
  description = "lzprobe-p25-${local.run_id}: P25 tag-conditioned probe identity, destroyed by the run"
  flow        = "CLIENT_CREDENTIALS"
}

resource "ovh_iam_policy" "p25" {
  name        = "lzprobe-p25-${local.run_id}"
  description = "lzprobe-p25-${local.run_id}: region/storage/get only where lz:tenant = lzprobe-a"
  identities  = [ovh_me_api_oauth2_client.p25.identity]
  resources   = [local.project.iam.urn]
  allow       = ["publicCloudProject:apiovh:region/storage/get"]

  conditions {
    operator = "MATCH"
    values = {
      "resource.Tag(lz:tenant)" = "lzprobe-a"
    }
  }
}

output "tenant_client_identity" {
  description = "Identity URN of the P9 probe tenant identity."
  value       = ovh_me_api_oauth2_client.tenant.identity
}

output "p25_client_identity" {
  description = "Identity URN of the P25 probe identity."
  value       = ovh_me_api_oauth2_client.p25.identity
}

output "buckets" {
  description = "P25 probe bucket names by tenant tag suffix (leftover match: name prefix)."
  value       = { for k, b in ovh_cloud_project_storage.p25 : k => b.name }
}
