stack {
  name        = "account-governance"
  description = "account-governance"
  tags        = ["lz-scope-account", "lz-stage-account-governance"]
  after       = ["tag:lz-stage-bootstrap"]
  id          = "account-governance"
}
