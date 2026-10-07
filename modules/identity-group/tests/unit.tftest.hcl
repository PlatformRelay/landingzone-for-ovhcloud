# Identity group module (spec 005 T019; FR-004, FR-010; ADR-0003, ADR-0018; research R6 *Group*).
# Mocked provider, no credential, no API call. Attribute names from the pinned ovh 2.21.0 schema
# (`ovh_me_identity_group`: name (required), description, role (optional); urn, default_group,
# creation, last_update (computed)).
#
# The group is for future human tenant members and gets its rights only from IAM policies that
# name its URN (research R6), so its account role defaults to `NONE`. Valid roles: ADMIN, REGULAR,
# UNPRIVILEGED, NONE (me_identity_group.md:23; kb api/v1/me.json enum). What each role grants an
# account member is UNVERIFIED here. `role` is optional and not computed in the schema, so a null
# role would leave the choice to the API; a null input therefore falls back to `NONE` as well.
# URN form `urn:v1:eu:identity:group:<nic>/<name>` (iam-policies-api.mdx:437).

mock_provider "ovh" {
  mock_resource "ovh_me_identity_group" {
    defaults = {
      id  = "mock-group-resource-id"
      urn = "urn:v1:eu:identity:group:xx1111-ovh/lz-demo-grp-members"
    }
  }
}

variables {
  name        = "lz-demo-grp-members"
  description = "lz-demo-grp-members: future human members of tenant demo"
}

run "role_defaults_to_none" {
  command = plan

  assert {
    condition     = ovh_me_identity_group.this.role == "NONE"
    error_message = "without a role the group's role is NONE"
  }

  assert {
    condition     = ovh_me_identity_group.this.name == "lz-demo-grp-members" && ovh_me_identity_group.this.description == "lz-demo-grp-members: future human members of tenant demo"
    error_message = "the group carries the given name and description"
  }
}

run "given_role_and_names" {
  command = plan

  variables {
    name        = "lz-acme-grp-viewers"
    description = "lz-acme-grp-viewers: read-only members of tenant acme"
    role        = "UNPRIVILEGED"
  }

  assert {
    condition     = ovh_me_identity_group.this.role == "UNPRIVILEGED"
    error_message = "a given role passes through unchanged"
  }

  assert {
    condition     = ovh_me_identity_group.this.name == "lz-acme-grp-viewers" && ovh_me_identity_group.this.description == "lz-acme-grp-viewers: read-only members of tenant acme"
    error_message = "a second name and description pass through unchanged"
  }
}

run "null_role_is_none" {
  command = plan

  variables {
    role = null
  }

  assert {
    condition     = ovh_me_identity_group.this.role == "NONE"
    error_message = "a null role falls back to NONE, never to the API's choice"
  }
}

run "outputs" {
  command = plan

  assert {
    condition     = output.urn == ovh_me_identity_group.this.urn && output.urn == "urn:v1:eu:identity:group:xx1111-ovh/lz-demo-grp-members"
    error_message = "the urn output is the group's URN"
  }

  assert {
    condition     = output.name == ovh_me_identity_group.this.name && output.name == "lz-demo-grp-members"
    error_message = "the name output is the group's name"
  }
}

run "unknown_role_rejected" {
  command = plan

  variables {
    role = "OWNER"
  }

  expect_failures = [var.role]
}

run "lowercase_role_rejected" {
  command = plan

  variables {
    role = "none"
  }

  expect_failures = [var.role]
}
