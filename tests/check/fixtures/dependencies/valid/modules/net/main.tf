# Uses the naming module and a nested submodule of its own package.
module "name" {
  source = "../naming"
  name   = "net"
}

module "sub" {
  source = "./sub"
}
