#!/bin/sh
# Capture the retained-resource guard's plan fixtures (005 T063) with the pinned OpenTofu.
#
#   tests/fixtures/tofu-probes/protect/capture.sh <pinned tofu> <scratch dir>
#
# Provider-free roots: terraform_data stands in for the state buckets, the platform S3 user, the
# tenant buckets, the adopted project, a governance module and a keyed (for_each) region module;
# nothing leaves the host. Each case
# applies the base root to a local state in its own scratch directory (except `create`), changes
# the root or a variable, runs `tofu plan -out=plan.bin` and `tofu show -json plan.bin`. The JSON
# lands next to this script as <case>.json with a <case>.meta.json sidecar (command, tool version,
# digest). truncated.json is derived from org-change.json, not captured.
#
# Not run through lz-offline capture admission: T007 still owes the formal capture (T063 evidence).
set -eu
tofu=$1
scratch=$2
here=$(cd "$(dirname "$0")" && pwd)
# The scratch directory must not exist yet: the script never deletes a path it did not create.
if [ -e "$scratch" ]; then
  echo "capture.sh: $scratch exists; pass a new path" >&2
  exit 2
fi
mkdir -p "$scratch"
: > "$scratch/empty.tfrc"
export TF_CLI_CONFIG_FILE="$scratch/empty.tfrc" TF_IN_AUTOMATION=1 CHECKPOINT_DISABLE=1
version=$("$tofu" version | sed -n '1s/^OpenTofu v//p')

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

# capture <case> <variant> <apply base first: yes|no> [plan args...]
capture() {
  name=$1
  variant=$2
  base=$3
  shift 3
  d=$scratch/$name
  root "$d" base
  "$tofu" -chdir="$d" init -input=false -no-color > "$d/init.log" 2>&1
  if [ "$base" = yes ]; then
    "$tofu" -chdir="$d" apply -input=false -no-color -auto-approve > "$d/apply.log" 2>&1
    root "$d" "$variant"
  fi
  "$tofu" -chdir="$d" plan -input=false -no-color -out=plan.bin "$@" > "$d/plan.log" 2>&1
  "$tofu" -chdir="$d" show -json plan.bin > "$here/$name.json"
  digest=$(sha256sum "$here/$name.json" | cut -d' ' -f1)
  args=$(printf '%s' "$*" | sed 's/["\\]/\\&/g')
  printf '{"case":"%s","variant":"%s","applied_base_first":"%s","plan_args":"%s","command":"tofu plan -out=plan.bin; tofu show -json plan.bin","tool":"tofu","tool_version":"%s","json_sha256":"%s","captured_on":"%s"}\n' \
    "$name" "$variant" "$base" "$args" "$version" "$digest" "$(date -u +%Y-%m-%d)" > "$here/$name.meta.json"
  echo "$name: ok"
}

capture create base no
capture noop base yes
capture update base yes -var=user_label=platform-renamed
capture unretained-replace base yes -var=scratch=s2
capture removed-block removed-block yes
capture org-change base yes -var=org=acme2
capture create-before-destroy base yes -var=user_rotation=r2
capture tenant-removed base yes '-var=tenants=["alpha"]'
capture project-replaced base yes -var=project_id=p-2222
capture forget forget yes
capture module-removed module-removed yes
capture two-replacements base yes -var=org=acme2 -var=project_id=p-2222
capture region-removed base yes '-var=regions=["eu"]'
capture replace-requested base yes -replace=terraform_data.state_bucket
capture moved-replace moved-replace yes -var=org=acme2
capture moved-out moved-out yes
capture moved-only moved-only yes

# T064: `tofu show -json` without a plan file prints the state, which is not a plan.
d=$scratch/state-not-plan
root "$d" base
"$tofu" -chdir="$d" init -input=false -no-color > "$d/init.log" 2>&1
"$tofu" -chdir="$d" apply -input=false -no-color -auto-approve > "$d/apply.log" 2>&1
"$tofu" -chdir="$d" show -json > "$here/state-not-plan.json"
printf '{"case":"state-not-plan","variant":"base","applied_base_first":"yes","plan_args":"","command":"tofu show -json (no plan file)","tool":"tofu","tool_version":"%s","json_sha256":"%s","captured_on":"%s"}\n' \
  "$version" "$(sha256sum "$here/state-not-plan.json" | cut -d' ' -f1)" "$(date -u +%Y-%m-%d)" > "$here/state-not-plan.meta.json"
echo "state-not-plan: ok"

# A killed `show -json`: org-change.json cut in the middle.
size=$(wc -c < "$here/org-change.json")
keep=$((size / 2))
head -c "$keep" "$here/org-change.json" > "$here/truncated.json"
printf '{"case":"truncated","derived_from":"org-change.json","bytes_kept":%d,"bytes_original":%d,"json_sha256":"%s"}\n' \
  "$keep" "$size" "$(sha256sum "$here/truncated.json" | cut -d' ' -f1)" > "$here/truncated.meta.json"
