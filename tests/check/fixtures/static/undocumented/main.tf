terraform {
  required_version = ">= 1.13.0"
}

variable "name" {
  type = string
}

output "upper" {
  value = upper(var.name)
}
