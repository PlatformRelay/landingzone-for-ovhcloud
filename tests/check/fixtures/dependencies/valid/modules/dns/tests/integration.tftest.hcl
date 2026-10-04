# The tests run this module next to the net module, so a change to net must
# select dns as well. Sources in run blocks are relative to the module root.
run "with_network" {
  module {
    source = "../net"
  }
}
