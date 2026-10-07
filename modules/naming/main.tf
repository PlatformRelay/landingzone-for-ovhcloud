# Name algorithm version 1 (FR-001, ADR-0003, research R15): the template's ordered segments,
# absent (null) coordinates omitted, joined by the separator, the case rule applied. An override
# (import) replaces the generated name unchanged. Either name is checked against the kind's row in
# kinds.yaml; the checks are the preconditions of `output.name`.

locals {
  kinds = yamldecode(file("${path.module}/kinds.yaml"))

  # null when the kind has no abbreviation in the template or no row in kinds.yaml.
  row = contains(keys(var.template.kinds), var.kind) ? lookup(local.kinds, var.kind, null) : null

  coordinates = {
    org         = var.org
    tenant      = var.tenant
    environment = var.environment
    region      = var.region
    kind        = lookup(var.template.kinds, var.kind, null)
    role        = var.role
    slot        = var.slot
  }

  # An empty string is a coordinate given without a value, never an absent one.
  empty_coordinates = [for k, v in local.coordinates : k if v == ""]

  generated = lower(join(var.template.separator, [
    for s in var.template.segments : local.coordinates[s] if local.coordinates[s] != null
  ]))

  name = var.name_override != null ? var.name_override : local.generated

  checks = local.row == null ? null : {
    length  = length(local.name) >= local.row.min_length && length(local.name) <= local.row.max_length
    charset = can(regex(local.row.charset, local.name))
    ends    = !local.row.alphanumeric_ends || can(regex("^[a-z0-9](.*[a-z0-9])?$", local.name))
    doubled = !local.row.no_doubled_punctuation || !can(regex("[.-]{2}", local.name))
    not_ip  = !local.row.not_ip_address || !can(regex("^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$", local.name))
  }

  # Mandatory labels (data-model *Label set*). `lz:tenant` comes only from the tenant coordinate
  # (guard G4); `lz:run-id` is the live lane's, not this module's.
  mandatory_labels = merge(
    {
      "lz:managed-by" = "opentofu"
      "lz:managed-in" = var.managed_in
      "lz:instance"   = var.instance
      "lz:release"    = "unreleased"
    },
    var.tenant == null ? {} : { "lz:tenant" = var.tenant },
  )
}
