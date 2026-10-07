# Identity/ovh-native component (spec 005 T024; FR-002, FR-004, FR-010, FR-013; ADR-0006, ADR-0018;
# research R6, P9, P26): the platform deployer with `publicCloudProject:apiovh:*` on every tenant
# project URN, and per tenant (keyed by tenant name, so adding a tenant leaves the others' addresses
# unchanged) a deployer, its policy holding exactly the P9 allowlist on that tenant's own project
# URN (guard G5) and an identity group with role `NONE` and no members. No `account:apiovh:me/get`:
# the account binding reads `GET /auth/details` (P26). No group policy in this slice (coordinator
# decision 2026-10-07): the group has no members, so there is nothing to grant yet.
#
# Names come from modules/naming. None of these resources carries tags (research R14); outputs.tf
# lists them in `unlabelled`. The policy contents and the group role in the outputs are read from
# the modules' outputs, which read the resources; the stage's captured plan pins the planned
# resources themselves (tools/internal/stacks, TestOutputsAccountGovernanceStagePlan).

locals {
  # P9 allowlist (research R6, tenant allowlist; action names from kb api/v1/cloud.json): private
  # networks and subnets, and object-storage containers of the tenant's own project. No IAM action,
  # no `me` action, no `region/storage/policy/create` (R6: it would let a tenant attach storage
  # policies to S3 users) and no wildcard.
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
}

module "platform_name" {
  source     = "../../../modules/naming"
  for_each   = toset(["service_account", "iam_policy"])
  org        = var.org
  kind       = each.key
  role       = "platform-deployer"
  instance   = var.instance
  managed_in = var.managed_in
}

module "tenant_name" {
  source = "../../../modules/naming"
  for_each = { for p in setproduct(keys(var.tenants), ["service_account", "iam_policy", "identity_group"]) :
  "${p[0]}/${p[1]}" => { tenant = p[0], kind = p[1] } }
  org        = var.org
  tenant     = each.value.tenant
  kind       = each.value.kind
  role       = each.value.kind == "identity_group" ? "tenant" : "deployer"
  instance   = var.instance
  managed_in = var.managed_in
}

module "platform_deployer" {
  source      = "../../../modules/iam-service-account"
  name        = module.platform_name["service_account"].name
  description = "Platform deployer (account-governance)"
}

module "platform_policy" {
  source      = "../../../modules/iam-policy"
  name        = module.platform_name["iam_policy"].name
  description = "Platform deployer on the tenant projects"
  identities  = [module.platform_deployer.identity]
  resources   = [for t in var.tenants : t.project_urn]
  allow       = ["publicCloudProject:apiovh:*"]
}

module "tenant_deployer" {
  source      = "../../../modules/iam-service-account"
  for_each    = var.tenants
  name        = module.tenant_name["${each.key}/service_account"].name
  description = "Deployer of tenant ${each.key}"
}

module "tenant_policy" {
  source      = "../../../modules/iam-policy"
  for_each    = var.tenants
  name        = module.tenant_name["${each.key}/iam_policy"].name
  description = "Deployer of tenant ${each.key} on its project"
  identities  = [module.tenant_deployer[each.key].identity]
  resources   = [each.value.project_urn]
  allow       = local.tenant_allow
}

module "tenant_group" {
  source      = "../../../modules/identity-group"
  for_each    = var.tenants
  name        = module.tenant_name["${each.key}/identity_group"].name
  description = "Members of tenant ${each.key}"
  role        = "NONE"
}
