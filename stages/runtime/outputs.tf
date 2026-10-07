# Stub (spec 005 T031): placeholder values. T032 reads the published values
# (schemas/outputs/runtime.schema.json, the ADR-0017 envelope) from `module.runtime`.

output "kind" {
  description = "Runtime kind."
  value       = "not-implemented"
}

output "slot" {
  description = "Runtime slot; null (absent from the published values) when none is set."
  value       = "not-implemented"
}

output "scope" {
  description = "Deployment scope: instance, project and region of the instance."
  value = {
    instance   = "not-implemented"
    project_id = "not-implemented"
    region     = "NOT-IMPLEMENTED"
  }
}

output "readiness" {
  description = "Readiness of the runtime."
  value       = "not-implemented"
}

output "pending_actions" {
  description = "Actions left to the operator before the runtime is usable."
  value       = ["not-implemented"]
}

output "capabilities" {
  description = "Capabilities the runtime publishes; an absent one is left out."
  value       = {}
}

output "unlabelled" {
  description = "Addresses (from the stage root) of the resources whose API carries no tags (research R14)."
  value       = ["not-implemented"]
}
