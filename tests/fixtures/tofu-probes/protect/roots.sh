#!/bin/sh
# Inner capture of one retained-resource guard plan (005 T063/T064, T070), run by
# `task capture:protect-<case>` inside the offline entry's capture admission:
#
#   sh roots.sh <pinned tofu> <case>
#
# Provider-free roots: terraform_data stands in for the state buckets, the platform S3 user, the
# tenant buckets, the adopted project, a governance module and a keyed (for_each) region module.
# Each case applies the base root to a local state in /tmp/lz-protect/<case> (except `create`),
# changes the root or a variable, runs `tofu plan -out=plan.bin` and prints `tofu show -json
# plan.bin` on stdout, which is the fixture; state-not-plan prints `tofu show -json` without a
# plan file (the state). Tool output goes to log files in the scratch directory and is printed to
# stderr only when a step fails. The case table below is the capture's input: its digest is in
# every sidecar (capture.sh).
set -eu
tofu=$1
case=$2
scratch=/tmp/lz-protect/$case
# The scratch directory must not exist yet: the script never deletes a path it did not create.
if [ -e "$scratch" ]; then
  echo "roots.sh: $scratch exists" >&2
  exit 2
fi
mkdir -p "$scratch"
export TF_IN_AUTOMATION=1 CHECKPOINT_DISABLE=1

# root <dir> <variant>: variant drops or replaces one block of the base root.
root() {
  mkdir -p "$1/governance"
  cat > "$1/governance/main.tf" <<'EOF'
resource "terraform_data" "policy" {
  input = "platform-deployer"
}
EOF
  cat > "$1/main.tf" <<'EOF'
variable "org" {
  type    = string
  default = "acme"
}
variable "project_id" {
  type    = string
  default = "p-1111"
}
variable "tenants" {
  type    = list(string)
  default = ["alpha", "beta"]
}
variable "user_label" {
  type    = string
  default = "platform"
}
variable "user_rotation" {
  type    = string
  default = "r1"
}
variable "regions" {
  type    = list(string)
  default = ["eu", "ca"]
}
variable "scratch" {
  type    = string
  default = "s1"
}
resource "terraform_data" "platform_user" {
  input            = var.user_label
  triggers_replace = [var.user_rotation]
  lifecycle {
    create_before_destroy = true
  }
}
resource "terraform_data" "tenant_bucket" {
  for_each = toset(var.tenants)
  input    = "${each.key}-state"
}
resource "terraform_data" "project" {
  input            = var.project_id
  triggers_replace = [var.project_id]
}
resource "terraform_data" "project_scratch" {
  input            = "lzseed-7f3a9c"
  triggers_replace = [var.scratch]
}
EOF
  case $2 in
  removed-block) ;;
  forget)
    cat >> "$1/main.tf" <<'EOF'
removed {
  from = terraform_data.state_bucket
  lifecycle {
    destroy = false
  }
}
EOF
    ;;
  moved-replace | moved-only)
    # T064: the retained address is moved (moved-only) or moved and replaced (with an org change)
    # in the same plan.
    cat >> "$1/main.tf" <<'EOF'
moved {
  from = terraform_data.state_bucket
  to   = terraform_data.bucket
}
resource "terraform_data" "bucket" {
  input            = "${var.org}-lz-state"
  triggers_replace = [var.org]
}
EOF
    ;;
  moved-out)
    # T064: the retained address is moved to an address with no block, so it is deleted.
    cat >> "$1/main.tf" <<'EOF'
moved {
  from = terraform_data.state_bucket
  to   = terraform_data.other
}
EOF
    ;;
  *)
    cat >> "$1/main.tf" <<'EOF'
resource "terraform_data" "state_bucket" {
  input            = "${var.org}-lz-state"
  triggers_replace = [var.org]
}
EOF
    ;;
  esac
  case $2 in
  module-removed) ;;
  *)
    cat >> "$1/main.tf" <<'EOF'
module "governance" {
  source = "./governance"
}
EOF
    ;;
  esac
  cat >> "$1/main.tf" <<'EOF'
module "region" {
  for_each = toset(var.regions)
  source   = "./governance"
}
EOF
}

# step <log> <tofu args...>: run tofu in the scratch root; on failure print its log to stderr.
step() {
  log=$scratch/$1
  shift
  "$tofu" -chdir="$scratch" "$@" > "$log" 2>&1 || {
    status=$?
    cat "$log" >&2
    exit "$status"
  }
}

# capture <variant> <apply base first: yes|no> [plan args...]
capture() {
  variant=$1
  base=$2
  shift 2
  root "$scratch" base
  step init.log init -input=false -no-color
  if [ "$base" = yes ]; then
    step apply.log apply -input=false -no-color -auto-approve
    root "$scratch" "$variant"
  fi
  step plan.log plan -input=false -no-color -out=plan.bin "$@"
  "$tofu" -chdir="$scratch" show -json plan.bin
}

# case | variant | apply base first | plan args (T063, T064)
case $case in
create) capture base no ;;
noop) capture base yes ;;
update) capture base yes -var=user_label=platform-renamed ;;
unretained-replace) capture base yes -var=scratch=s2 ;;
removed-block) capture removed-block yes ;;
org-change) capture base yes -var=org=acme2 ;;
create-before-destroy) capture base yes -var=user_rotation=r2 ;;
tenant-removed) capture base yes '-var=tenants=["alpha"]' ;;
project-replaced) capture base yes -var=project_id=p-2222 ;;
forget) capture forget yes ;;
module-removed) capture module-removed yes ;;
two-replacements) capture base yes -var=org=acme2 -var=project_id=p-2222 ;;
region-removed) capture base yes '-var=regions=["eu"]' ;;
replace-requested) capture base yes -replace=terraform_data.state_bucket ;;
moved-replace) capture moved-replace yes -var=org=acme2 ;;
moved-out) capture moved-out yes ;;
moved-only) capture moved-only yes ;;
state-not-plan)
  # T064: `tofu show -json` without a plan file prints the state, which is not a plan.
  root "$scratch" base
  step init.log init -input=false -no-color
  step apply.log apply -input=false -no-color -auto-approve
  "$tofu" -chdir="$scratch" show -json
  ;;
*)
  echo "roots.sh: unknown case $case" >&2
  exit 2
  ;;
esac
