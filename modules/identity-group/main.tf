# One identity group (FR-004, FR-010; research R6). Provider resource `ovh_me_identity_group`
# (ovh 2.21.0); `role` is one of ADMIN, REGULAR, UNPRIVILEGED, NONE (provider docs
# resources/me_identity_group, API schema `me.json`). The role always comes from the module
# (default NONE): left unset, the API would pick it (UNVERIFIED).
resource "ovh_me_identity_group" "this" {
  name        = var.name
  description = var.description
  role        = var.role
}
