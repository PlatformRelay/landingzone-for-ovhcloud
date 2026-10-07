# Refused: the stage is the last hop before the published stack outputs.
module "identity" {
  source = "../../components/identity/ovh-native"
}

output "deployer" {
  value = { secret = nonsensitive(module.identity.secret) }
}
