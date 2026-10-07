# Observations of the probe identities (T010, through lz-live's second stage):
# - P9 usage: the tenant identity (narrowed R6 allowlist) creates and destroys a private network,
#   a subnet and a bucket. A denied create fails the apply: the action it names is the finding.
# - P26 tenant half: the provider authenticates the tenant identity through GET /auth/details at
#   configuration (provider docs index.md:211), and GET /me (account:apiovh:me/get) is denied.
# - P25 evaluation: the tag-conditioned identity reads bucket A (lz:tenant = lzprobe-a) and is
#   denied bucket B (lz:tenant = lzprobe-b), both created by storage-iam.
# A denial expected by a check block is reported as a warning naming the failed read, not an
# error (check-block scoped data sources; believed, UNVERIFIED for OpenTofu 1.13); a check that
# fails with its own error_message is the refutation. Read both P25 warnings: bucket B's is the
# expected denial, but a warning on bucket A (the P25 identity denied its own tenant's bucket)
# refutes P25 as well.
locals {
  name = "lzprobe-p9-${var.run_id}"

  buckets = {
    a = "lzprobe-p25a-${lower(var.run_id)}"
    b = "lzprobe-p25b-${lower(var.run_id)}"
  }
}

resource "ovh_cloud_project_network_private" "p9" {
  service_name = var.project_id
  name         = local.name
  regions      = ["GRA11"]
}

resource "ovh_cloud_project_network_private_subnet" "p9" {
  service_name = var.project_id
  network_id   = ovh_cloud_project_network_private.p9.id
  region       = "GRA11"
  network      = "10.251.0.0/24"
  start        = "10.251.0.10"
  end          = "10.251.0.200"
  dhcp         = true
  no_gateway   = true
}

resource "ovh_cloud_project_storage" "p9" {
  service_name = var.project_id
  region_name  = "GRA"
  name         = lower(local.name)
  tags = {
    "lz:run-id" = var.run_id
  }
}

check "p26_me_denied" {
  data "ovh_me" "tenant" {}

  assert {
    condition     = data.ovh_me.tenant.nichandle == ""
    error_message = "P26 refuted for the tenant class: GET /me answered for the P9 probe identity."
  }
}

check "p25_bucket_a_readable" {
  data "ovh_cloud_project_storage" "a" {
    provider     = ovh.p25
    service_name = var.project_id
    region_name  = "GRA"
    name         = local.buckets.a
  }

  assert {
    condition     = data.ovh_cloud_project_storage.a.tags["lz:tenant"] == "lzprobe-a"
    error_message = "P25: the tag-conditioned identity read bucket A, but it does not carry lz:tenant = lzprobe-a."
  }
}

check "p25_bucket_b_denied" {
  data "ovh_cloud_project_storage" "b" {
    provider     = ovh.p25
    service_name = var.project_id
    region_name  = "GRA"
    name         = local.buckets.b
  }

  assert {
    condition     = data.ovh_cloud_project_storage.b.name != local.buckets.b
    error_message = "P25 refuted: the tag-conditioned identity read bucket B (lz:tenant = lzprobe-b)."
  }
}

output "network_name" {
  description = "P9 probe network name (leftover match: name prefix)."
  value       = ovh_cloud_project_network_private.p9.name
}

output "bucket" {
  description = "P9 probe bucket name (leftover match: name prefix)."
  value       = ovh_cloud_project_storage.p9.name
}
