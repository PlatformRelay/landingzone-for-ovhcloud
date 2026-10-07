# The runtime envelope's values (ADR-0017, schemas/outputs/runtime.schema.json), read from
# `module.bucket`, plus `labels` (the bucket's tags; the stage does not publish them).

output "kind" {
  description = "Runtime kind."
  value       = "managed-only"
}

output "slot" {
  description = "Runtime slot; null when none is set."
  value       = var.slot
}

output "scope" {
  description = "Deployment scope: instance, project and region of the instance."
  value = {
    instance   = var.instance
    project_id = module.bucket.project_id
    region     = var.region
  }
}

output "readiness" {
  description = "Readiness of the runtime: a managed-only runtime is ready once its bucket exists."
  value       = "ready"
}

output "pending_actions" {
  description = "Actions left to the operator before the runtime is usable (none for managed-only)."
  value       = tolist([])
}

output "capabilities" {
  description = "Capabilities the runtime publishes: object-storage only; an absent one (network) is left out, never empty."
  value = {
    "object-storage" = {
      bucket   = module.bucket.name
      endpoint = "https://s3.${lower(module.bucket.region)}.io.cloud.ovh.net"
      region   = module.bucket.region
    }
  }
}

output "unlabelled" {
  description = "Addresses (from the component root) of the resources whose API carries no tags: none, the bucket carries tags (research R14)."
  value       = tolist([])
}

output "labels" {
  description = "Tags the bucket carries: the modules/naming labels of this instance."
  value       = module.bucket.tags
}
