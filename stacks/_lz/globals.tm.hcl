// Per-stack values derived from stacks/deployments.yaml and the stack's id (005 T038; data-model
// *Derived instance fields*, research R5, R15, R16). The manifest is decoded strictly by
// lz-stacks before generation (Generate, reconcile); here it is only read.
globals "lz" {
  manifest = tm_yamldecode(tm_file("${terramate.root.path.fs.absolute}/stacks/deployments.yaml"))
  spec     = global.lz.manifest.spec
  row      = [for r in global.lz.spec.instances : r if r.id == terramate.stack.id][0]
  stage    = global.lz.row.stage
  module   = tm_replace(global.lz.stage, "-", "_")
  org      = global.lz.spec.org
  tenant   = tm_try(global.lz.row.tenant, null)
  envname  = tm_try(global.lz.row.environment, null)
  region   = tm_try(global.lz.row.region, null)
  slot     = tm_try(global.lz.row.slot, null)
  path     = tm_trimprefix(terramate.stack.path.relative, "/")
  key      = "${tm_trimprefix(global.lz.path, "stacks/")}/terraform.tfstate"

  // Local stage source (research R22): the relative path from the stack to stages/<stage>. The
  // `git` seam is refused below and by Generate.
  stage_source = global.lz.spec.stage_source.kind
  source       = "${terramate.stack.path.to_root}/stages/${global.lz.stage}"

  managed_in = "${global.lz.spec.forge}//${global.lz.path}"

  // Account and account-tenant stacks keep their state in the account bucket, tenant stacks in
  // their tenant's bucket (FR-008, ADR-0009). bootstrap's local backend reads neither.
  account_bucket = global.lz.tenant == null || tm_contains(["tenant-state", "account-governance"], global.lz.stage)
  bucket         = global.lz.account_bucket ? "${global.lz.org}-bkt-state" : "${global.lz.org}-${global.lz.tenant}-bkt-state"

  env     = tm_try([for e in [for t in global.lz.spec.tenants : t if t.name == global.lz.tenant][0].environments : e if e.name == global.lz.envname][0], null)
  network = tm_try([for r in global.lz.env.regions : r.network if r.name == global.lz.region][0], null)

  tenant_names = [for t in global.lz.spec.tenants : t.name]

  // Every output each stage declares (stages/<stage>/outputs.tf), true where it is sensitive: the
  // generated root re-exports them unchanged, so `tofu output -json` of the root is the stage's
  // envelope input (research R4, coordinator decision 3 of T037). TestGenerateStackInvariants
  // holds this table to the stages' outputs.tf.
  stage_outputs = {
    "bootstrap" = {
      state_bucket        = false
      state_project_id    = false
      state_region        = false
      state_endpoint      = false
      platform_s3_user_id = false
      unlabelled          = false
      platform_s3         = true
    }
    "tenant-state" = {
      tenant              = false
      state_bucket        = false
      tenant_s3_user_id   = false
      platform_s3_user_id = false
      unlabelled          = false
      tenant_s3           = true
      platform_s3         = true
    }
    "account-governance" = {
      platform_deployer        = false
      tenants                  = false
      unlabelled               = false
      platform_deployer_secret = true
      tenant_deployer_secrets  = true
    }
    "project" = {
      tenant          = false
      environment     = false
      project_id      = false
      project_urn     = false
      regions         = false
      budget_alert_id = false
      unlabelled      = false
    }
    "project-network" = {
      network_id            = false
      regions_openstack_ids = false
      subnet_id             = false
      cidr                  = false
      unlabelled            = false
    }
    "runtime" = {
      kind            = false
      slot            = false
      scope           = false
      readiness       = false
      pending_actions = false
      capabilities    = false
      unlabelled      = false
    }
  }
}

assert {
  assertion = global.lz.stage_source == "local"
  message   = "STAGE_SOURCE_NOT_IMPLEMENTED: spec.stage_source.kind ${global.lz.stage_source}: only local stage sources generate"
}
