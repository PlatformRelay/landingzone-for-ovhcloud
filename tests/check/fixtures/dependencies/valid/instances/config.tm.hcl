generate_hcl "main.tf" {
  content {
    module "platform" {
      source = "../../stages/platform"
    }
  }
}
