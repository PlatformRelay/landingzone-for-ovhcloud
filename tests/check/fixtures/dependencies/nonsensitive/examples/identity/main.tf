# Accepted: an example is not deployed.
module "identity" {
  source = "../../components/identity/ovh-native"
}

output "secret" {
  value = nonsensitive(module.identity.secret)
}
