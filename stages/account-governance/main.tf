# Account-governance stage (spec 005; FR-002, FR-004, FR-010, FR-013; ADR-0006, ADR-0009,
# ADR-0018; research R6; D88): the platform deployer and, per tenant, a deployer, its policy and an
# identity group, through the one components/identity/ovh-native call (`lz-check deps`
# STAGE_COMPONENT_CALLS refuses a second, repeated or other module call here). No backend or
# provider configuration; the generated stack owns both.
module "identity" {
  source     = "../../components/identity/ovh-native"
  org        = var.org
  instance   = var.instance
  managed_in = var.managed_in
  tenants    = var.tenants
}
