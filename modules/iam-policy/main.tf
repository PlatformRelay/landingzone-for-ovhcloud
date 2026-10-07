# One IAM policy (FR-004, FR-010; research R6). Provider resource `ovh_iam_policy` (ovh 2.21.0):
# every `allow` action applies to every `resources` URN for every `identities` URN. The module
# adds nothing of its own — no `permissions_groups`, `except`, `deny` or `expired_at` (provider
# docs resources/iam_policy) — so the caller's action list is the whole grant (guard G5).
# Conditions are passed as given, at most three levels; `MATCH` is terminal (same docs,
# *Conditions*).
resource "ovh_iam_policy" "this" {
  name        = var.name
  description = var.description
  identities  = var.identities
  resources   = var.resources
  allow       = var.allow

  dynamic "conditions" {
    for_each = var.conditions == null ? [] : [var.conditions]
    content {
      operator = conditions.value.operator
      values   = conditions.value.values

      dynamic "condition" {
        for_each = conditions.value.condition
        iterator = second
        content {
          operator = second.value.operator
          values   = second.value.values

          dynamic "condition" {
            for_each = second.value.condition
            iterator = third
            content {
              operator = third.value.operator
              values   = third.value.values
            }
          }
        }
      }
    }
  }
}
