# Hand-written generator configuration is allowed under stacks/; only *.tf must be generated.
generate_hcl "main.tf" {
  content {
    module "stage" {
      source = "../../../stages/bootstrap"
    }
  }
}
