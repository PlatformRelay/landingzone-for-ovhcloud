# Runtime/managed-only component (spec 005 T031; research R5 *Protection*, R8; FR-004): the runtime
# bucket is ephemeral, so it comes from the unprotected modules/object-storage, never from
# modules/object-storage-protected and never as a state bucket. Its own file, so the apply runs
# below keep their own state.
#
# Sensor: modules/object-storage-protected carries a literal `prevent_destroy`, which refuses both
# the replacement planned below and this file's cleanup destroy (components/state-backend tests,
# T013 *Observed*): a component that took the bucket from it fails here. The name pins role
# `runtime`, not `state`.

mock_provider "ovh" {}

variables {
  org         = "lz"
  tenant      = "demo"
  environment = "dev"
  region      = "GRA11"
  instance    = "demo-dev-gra11-runtime"
  managed_in  = "github.com/platformrelay/landingzone-for-ovhcloud//stacks/tenants/demo/dev/gra11/runtime"
  project_id  = "0123456789abcdef0123456789abcdef"
}

run "runtime_bucket_applies" {
  command = apply

  assert {
    condition     = try(module.bucket.name, null) == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "the runtime bucket (role runtime, not state) is created at module.bucket"
  }
}

# Follows an apply run, so the bucket is in state: the runtime bucket can be replaced and, at the
# end of the file, destroyed.
run "runtime_bucket_replacement_plans" {
  command = plan

  plan_options {
    replace = [module.bucket.ovh_cloud_project_storage.this]
  }

  assert {
    condition     = try(module.bucket.name, null) == "lz-demo-dev-gra11-bkt-runtime"
    error_message = "the replacement of the runtime bucket plans"
  }
}
