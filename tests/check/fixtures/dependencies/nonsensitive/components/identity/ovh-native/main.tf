# Refused: a component re-exports a module secret without its mark.
module "credential" {
  source = "../../../modules/credential"
  secret = "x"
}

output "secret" {
  value = core::nonsensitive(module.credential.client_secret_plain)
}
