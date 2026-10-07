# Bootstrap stage (spec 005 T016; FR-002, FR-003, FR-004, FR-005, FR-008; ADR-0004, ADR-0009): the
# account state bucket and the platform S3 user, through the one state-backend component call.
# Account scope (no tenant): tenant buckets come from `tenant-state` (D87/D88). No backend or
# provider configuration; the generated stack owns both.
module "state_backend" {
  source     = "../../components/state-backend"
  project_id = var.state_project_id
  region     = var.state_region
  org        = var.org
  instance   = var.instance
  managed_in = var.managed_in
  s3_users   = ["platform"]
}
