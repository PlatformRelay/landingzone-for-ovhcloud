# Tenant-state stage (spec 005 T022; FR-002, FR-003, FR-004, FR-008, FR-013; ADR-0004, ADR-0009):
# one tenant's state bucket in the state project (KD-1) and its tenant and platform S3 users, each
# confined to that bucket (G6), through the one state-backend component call (D87/D88). `lz-check
# deps` refuses a resource here and a second state-backend call. No backend or provider
# configuration; the generated stack owns both.
module "state_backend" {
  source     = "../../components/state-backend"
  project_id = var.state_project_id
  region     = var.state_region
  org        = var.org
  tenant     = var.tenant
  instance   = var.instance
  managed_in = var.managed_in
  s3_users   = ["tenant", "platform"]
}
