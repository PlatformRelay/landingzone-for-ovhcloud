output "name" {
  description = "Resource name: the template applied to the coordinates, or the validated override, checked against the kind's row in kinds.yaml."
  value       = local.name

  precondition {
    condition     = local.row != null
    error_message = "kind \"${var.kind}\": no abbreviation in the template or no row in kinds.yaml; names are refused for kinds without limits."
  }

  precondition {
    condition     = length(local.empty_coordinates) == 0
    error_message = "empty segment: ${join(", ", local.empty_coordinates)}; omit an absent coordinate with null."
  }

  precondition {
    condition     = local.checks == null ? true : local.checks.length
    error_message = "name \"${local.name}\" (${length(local.name)} characters) is outside the ${var.kind} length limits; names are never truncated."
  }

  precondition {
    condition     = local.checks == null ? true : local.checks.charset
    error_message = "name \"${local.name}\" contains a character the ${var.kind} kind forbids."
  }

  precondition {
    condition     = local.checks == null ? true : local.checks.ends
    error_message = "name \"${local.name}\" must begin and end with a lowercase letter or digit."
  }

  precondition {
    condition     = local.checks == null ? true : local.checks.doubled
    error_message = "name \"${local.name}\" contains doubled punctuation (two of . and - in a row)."
  }

  precondition {
    condition     = local.checks == null ? true : local.checks.not_ip
    error_message = "name \"${local.name}\" looks like an IP address."
  }
}

output "labels" {
  description = "The mandatory lz: labels (lz:tenant absent at account scope) merged with the extra labels; mandatory keys win."
  value       = merge(var.labels, local.mandatory_labels)
}

output "algorithm_version" {
  description = "Version of the name algorithm; a name-affecting change is a new version and a migration."
  value       = 1
}
