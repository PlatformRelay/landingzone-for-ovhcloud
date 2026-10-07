# Stub (spec 005 T031): placeholder values. T032 publishes the runtime envelope (ADR-0017,
# schemas/outputs/runtime.schema.json) read from `module.bucket`, plus `labels`.

output "kind" {
  description = "Runtime kind."
  value       = "not-implemented"
}

output "slot" {
  description = "Runtime slot; null when none is set."
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
  description = "Addresses (from the component root) of the resources whose API carries no tags."
  value       = ["not-implemented"]
}

output "labels" {
  description = "Tags the bucket carries: the modules/naming labels of this instance."
  value       = {}
}
