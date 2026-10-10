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

# VLAN id (T098): a project holds one private network per VLAN id (T010 run
# 20261010T184645Z-f7de: 400 `A private network already exists for this vlan ID`, every probe
# network on VLAN 0). The probe takes the first id of its own range 3000..3999 (disjoint
# from the network probe's 2000..2999, inside the 0..4000 the modules admit), starting
# at 3000 + the run id's four hex digits mod 1000 and wrapping, that no other network of the
# project holds: a leftover or an applied concurrent run is skipped. Once its own network exists
# (matched by name) the probe keeps that network's id, so a re-plan never moves it (vlan_id forces
# a replacement). Two plans racing before either applies may pick one id, and a leftover of the
# same run id is taken for its own; the API then refuses the create (the 400 above), so two
# networks never share an id. Listing needs `network/private/get` (cloud.json), which the tenant
# allowlist holds.
data "ovh_cloud_project_network_privates" "existing" {
  service_name = var.project_id
}

locals {
  vlan_start = parseint(substr(var.run_id, 17, 4), 16)
  vlan_taken = toset([for n in data.ovh_cloud_project_network_privates.existing.networks : n.vlan_id])
  vlan_free  = [for k in range(1000) : 3000 + (local.vlan_start + k) % 1000 if !contains(local.vlan_taken, 3000 + (local.vlan_start + k) % 1000)]
  vlan_own   = [for n in data.ovh_cloud_project_network_privates.existing.networks : n.vlan_id if n.name == local.name]
  vlan_id    = try(local.vlan_own[0], local.vlan_free[0], null)
}

resource "ovh_cloud_project_network_private" "p9" {
  service_name = var.project_id
  name         = local.name
  regions      = ["GRA11"]
  vlan_id      = local.vlan_id

  lifecycle {
    precondition {
      condition     = local.vlan_id != null
      error_message = "every VLAN id of 3000..3999 is taken in the project: clear leftover probe networks (task live:probe -- --cleanup <run-id>)."
    }
  }
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
