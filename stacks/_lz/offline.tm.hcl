// The offline test of every stack (005 T038; research R16, contracts/checks.md *task
// test:stack-plans*): one plan under the mocked provider, with a fixed, non-secret passphrase; the
// remaining inputs come as a var file of fixture values (test:stack-plans).
//
// A project stack also carries its resolved project: a fixed fixture id and the project's URN for
// that id, set on the address the project is read at (the adopted resource, or the read data
// source in reference mode). The component refuses a URN that is not the bound project's (KD-3),
// and a mock's own URN never is.
generate_hcl "tests/_lz_offline.tftest.hcl" {
  condition = global.lz.stage != "project"
  content {
    mock_provider "ovh" {
    }
    variables {
      state_passphrase = "lz-offline-test-passphrase"
    }
    run "plan" {
      command = plan
    }
  }
}

generate_hcl "tests/_lz_offline.tftest.hcl" {
  condition = global.lz.stage == "project"
  content {
    mock_provider "ovh" {
    }
    variables {
      state_passphrase = "lz-offline-test-passphrase"
      project_id       = "0123456789abcdef0123456789abcdef"
    }
    tm_dynamic "override_resource" {
      for_each = global.lz.env.project.mode == "adopt" ? ["adopt"] : []
      content {
        target = tm_hcl_expression("module.project.module.project_factory.module.project.ovh_cloud_project.this[0]")
        values = {
          urn = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
        }
      }
    }
    tm_dynamic "override_data" {
      for_each = global.lz.env.project.mode == "reference" ? ["reference"] : []
      content {
        target = tm_hcl_expression("module.project.module.project_factory.module.project.data.ovh_cloud_project.this[0]")
        values = {
          iam = {
            display_name = "offline"
            id           = "offline"
            tags         = {}
            urn          = "urn:v1:eu:resource:publicCloudProject:0123456789abcdef0123456789abcdef"
          }
        }
      }
    }
    run "plan" {
      command = plan
    }
  }
}
