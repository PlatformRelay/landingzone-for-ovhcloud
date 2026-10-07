# Stub (spec 005 T021): the stage's one component call with placeholder arguments (the component's
# naming refuses empty labels, so the placeholders are non-empty); T022 implements it.
module "state_backend" {
  source     = "../../components/state-backend"
  project_id = "stub"
  region     = "STUB"
  org        = "stub"
  instance   = "stub"
  managed_in = "stub"
  s3_users   = []
}
