# IAM tags on one existing resource URN (spec 005 T105; FR-004; research R12, R14, P15). Provider
# resource `ovh_iam_resource_tags` (ovh 2.21.0; `PUT /v2/iam/resource/{urn}`). It manages only the
# keys in `tags`: destroying it removes those keys and leaves the resource and its other tags
# untouched (provider docs `iam_resource_tags`, Notes), so it needs no `prevent_destroy`.
resource "ovh_iam_resource_tags" "this" {
  urn  = var.urn
  tags = var.tags
}
