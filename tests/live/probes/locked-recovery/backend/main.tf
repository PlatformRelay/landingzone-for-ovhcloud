# Spec 003 T036: the locked-recovery probe's disposable backend root. lz-live's tofu seam runs the
# pinned OpenTofu here (never `task live:probe` on this directory): `init` with
# -backend-config=bucket=<the run's probe bucket>, its region and endpoint, then writer A's
# `apply` (killed at its approval prompt, holding the lock), writer B's `plan -lock-timeout=0s`,
# `force-unlock -force <lock id>` and B's plan again, all with the state user's key pair as the only
# credential (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY). The S3 backend is a state backend's
# (stacks/_lz/backend.tm.hcl) with the lockfile on (use_lockfile:
# https://opentofu.org/docs/language/settings/backends/s3/); the bucket, region and endpoint come
# from the command line, the key is fixed (the check's lock object is <key>.tflock). No provider:
# one terraform_data (built in), so a plan always has a change and A always reaches its prompt.
# The state is not encrypted: this probe claims nothing about C004.1, and no apply ever completes,
# so no state is written.
terraform {
  required_version = ">= 1.13.0"

  backend "s3" {
    key                         = "lzprobe-locked-recovery/terraform.tfstate"
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
    use_lockfile                = true
  }
}

resource "terraform_data" "writer" {
  input = "lzprobe-locked-recovery"
}
