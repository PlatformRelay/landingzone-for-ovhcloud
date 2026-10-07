# Identity/ovh-native component (spec 005; FR-002, FR-004, FR-010, FR-013; research R6): STUB for
# T023, behaviourally red. T024 implements it: the platform deployer (`module.platform_deployer`,
# modules/iam-service-account) with `publicCloudProject:apiovh:*` on every tenant project URN
# (`module.platform_policy`, modules/iam-policy), and per tenant (`for_each` over `var.tenants`) a
# deployer (`module.tenant_deployer[<t>]`), its policy holding exactly the P9 allowlist on that
# tenant's project URN (`module.tenant_policy[<t>]`, guard G5) and an identity group with role `NONE`
# and no members (`module.tenant_group[<t>]`, modules/identity-group).
#
# The stub declares the module calls the tests' `override_resource` targets name (a target in an
# undeclared module call is an error, observed tofu 1.13.0) without any instance.

module "platform_deployer" {
  source      = "../../../modules/iam-service-account"
  count       = 0
  name        = "not-implemented"
  description = "not-implemented"
}

module "tenant_deployer" {
  source      = "../../../modules/iam-service-account"
  for_each    = toset([])
  name        = "not-implemented"
  description = "not-implemented"
}

module "tenant_group" {
  source      = "../../../modules/identity-group"
  for_each    = toset([])
  name        = "not-implemented"
  description = "not-implemented"
}
