# Stub (spec 005 T015): the stage's one component call, not yet configured; T016 implements it.
module "state_backend" {
  source     = "../../components/state-backend"
  project_id = var.state_project_id
  region     = ""
  org        = ""
  instance   = ""
  managed_in = ""
  s3_users   = []
}
