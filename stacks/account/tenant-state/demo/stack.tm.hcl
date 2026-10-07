stack {
  name        = "demo-state"
  description = "demo-state"
  tags        = ["lz-scope-account-tenant", "lz-stage-tenant-state", "lz-tenant-demo"]
  after       = ["tag:lz-stage-bootstrap"]
  id          = "demo-state"
}
