# Inputs of the OAuth2 service-account module (spec 005 T019 pins them; T020 implements).

variable "name" {
  description = "OAuth2 client name, from modules/naming (kind `service_account`)."
  type        = string
  nullable    = false
}

variable "description" {
  description = "OAuth2 client description."
  type        = string
  nullable    = false
}
