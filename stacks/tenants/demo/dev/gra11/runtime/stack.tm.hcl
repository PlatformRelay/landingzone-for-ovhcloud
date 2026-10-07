stack {
  name        = "demo-dev-gra11-runtime"
  description = "demo-dev-gra11-runtime"
  tags        = ["lz-env-dev", "lz-region-gra11", "lz-scope-region", "lz-stage-runtime", "lz-tenant-demo"]
  after       = ["tag:lz-stage-account-governance", "tag:lz-stage-project:lz-tenant-demo:lz-env-dev", "tag:lz-stage-tenant-state:lz-tenant-demo"]
  id          = "demo-dev-gra11-runtime"
}
