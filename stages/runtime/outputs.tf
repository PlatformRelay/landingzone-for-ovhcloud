# The published values (schemas/outputs/runtime.schema.json, the ADR-0017 envelope), read from
# `module.runtime`. A null `slot` is not persisted, so it is absent from the published values.

output "kind" {
  description = "Runtime kind."
  value       = module.runtime.kind
}

output "slot" {
  description = "Runtime slot; null (absent from the published values) when none is set."
  value       = module.runtime.slot
}

output "scope" {
  description = "Deployment scope: instance, project and region of the instance."
  value       = module.runtime.scope
}

output "readiness" {
  description = "Readiness of the runtime."
  value       = module.runtime.readiness
}

output "pending_actions" {
  description = "Actions left to the operator before the runtime is usable."
  value       = module.runtime.pending_actions
}

output "capabilities" {
  description = "Capabilities the runtime publishes; an absent one is left out."
  value       = module.runtime.capabilities
}

output "unlabelled" {
  description = "Addresses (from the stage root) of the resources whose API carries no tags (research R14)."
  value       = module.runtime.unlabelled
}
