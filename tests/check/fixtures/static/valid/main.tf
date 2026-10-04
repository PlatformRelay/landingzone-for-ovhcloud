terraform {
  required_version = ">= 1.13.0"
}

variable "name" {
  type        = string
  description = "Name to upper-case."
}

output "upper" {
  value = upper(var.name)
}
