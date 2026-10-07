// TERRAMATE: GENERATED AUTOMATICALLY DO NOT EDIT

mock_provider "ovh" {
}
variables {
  project_id       = "0123456789abcdef0123456789abcdef"
  state_passphrase = "lz-offline-test-passphrase"
}
override_data {
  target = module.project.module.project_factory.module.project.data.ovh_cloud_project.this[0]
  values = {
    iam = {
      display_name = "offline"
      id           = "offline"
      tags         = {}
      urn          = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
    }
  }
}
run "plan" {
  command = plan
}
