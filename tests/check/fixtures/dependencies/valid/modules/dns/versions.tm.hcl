generate_hcl "versions.tf" {
  content {
    terraform {
      required_version = ">= 1.13.0"
    }
  }
}
