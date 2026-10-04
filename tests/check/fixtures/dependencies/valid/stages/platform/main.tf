# A stage composes components only; only generated instances call it.
module "kube" {
  source = "../../components/runtime/kube"
}

module "baseline" {
  source = "../../components/account-baseline"
}
