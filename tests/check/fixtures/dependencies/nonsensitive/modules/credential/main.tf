# Refused: a library output that strips the sensitive mark of the secret it wraps.
variable "secret" {
  type      = string
  sensitive = true
}

output "client_secret_plain" {
  value = nonsensitive(var.secret)
}
