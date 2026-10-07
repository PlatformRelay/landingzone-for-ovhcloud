stack {
  name        = "demo-dev-project"
  description = "demo-dev-project"
  tags        = ["lz-env-dev", "lz-scope-environment", "lz-stage-project", "lz-tenant-demo"]
  after       = ["tag:lz-stage-account-governance", "tag:lz-stage-tenant-state:lz-tenant-demo"]
  id          = "demo-dev-project"
}
