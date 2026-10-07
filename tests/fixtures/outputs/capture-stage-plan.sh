#!/bin/sh
# Capture a stage's real plan for the output-contract pins of 005 T022 (tenant-state) and T024
# (account-governance) through the installed offline entry:
#
#   tests/fixtures/outputs/capture-stage-plan.sh <absolute entry> <absolute checkout> <stage>
#
# The entry runs `task capture:<stage>-plan` in its sandbox (no network, no credential): the
# pinned OpenTofu runs the stage's own unit tests (mocked provider) on a scratch copy and prints
# the verbose JSON plan of the run `published_outputs_match_the_schema`. That stdout is the fixture
# captures/<stage>-plan.json. A .meta.json sidecar records the command, the entry's exit status,
# the pinned identities qualified by `task verify:toolchain` in the same session, the fixture's
# digest and one digest over the configuration the plan was made from (the stage's inputs below,
# each `<sha256>  <path>` line in byte order). The fixture is moved over the committed one only
# when the entry exits 0. The script writes only those files and its temporary directory.
set -eu
entry=$1
checkout=$2
stage=$3
here=$checkout/tests/fixtures/outputs
digest() { sha256sum "$1" | cut -d' ' -f1; }
run() { env -i PATH=/usr/bin:/bin "$entry" --candidate "$checkout" -- task "$1"; }
# Keep in step with stagePlans in tools/internal/stacks/outputs_stage_test.go: the directories
# the stage's plan is made from.
case "$stage" in
  tenant-state) dirs="stages/tenant-state components/state-backend modules/naming modules/object-storage-protected modules/object-storage-user" ;;
  account-governance) dirs="stages/account-governance components/identity/ovh-native modules/naming modules/iam-service-account modules/iam-policy modules/identity-group" ;;
  *) echo "capture-stage-plan.sh: unknown stage: $stage" >&2; exit 2 ;;
esac
# Every file a plan can read (configuration, data files, lock files), not Markdown, state files or
# hidden directories; test files only for the stage.
inputs() {
  # shellcheck disable=SC2086 # dirs is a word list
  cd "$checkout" && find $dirs -type d -name '.*' -prune -o \
    -type f ! -name '*.md' ! -name '*.tfstate*' \
    \( -path "stages/$stage/*" -o ! -path '*/tests/*' \) -print | LC_ALL=C sort | xargs sha256sum | sha256sum | cut -d' ' -f1
}

tmp=$(mktemp -d)
status=0
run verify:toolchain > "$tmp/verify-toolchain.out" || status=$?
if [ "$status" -ne 0 ]; then
  echo "capture-stage-plan.sh: verify:toolchain exited $status (output in $tmp)" >&2
  exit 2
fi
toolchain=$(grep '^TOOLCHAIN_QUALIFIED ' "$tmp/verify-toolchain.out" || true)
case "$toolchain" in
  *" tofu=1.13.0 "*" network=none") ;;
  *) echo "capture-stage-plan.sh: unexpected toolchain: $toolchain" >&2; exit 2 ;;
esac
version=$(printf '%s\n' "$toolchain" | tr ' ' '\n' | sed -n 's/^tofu=//p')

status=0
run "capture:$stage-plan" > "$tmp/$stage-plan.json" || status=$?
if [ "$status" -ne 0 ] || [ ! -s "$tmp/$stage-plan.json" ]; then
  echo "$stage-plan: entry exit $status; fixture left unchanged (output in $tmp)" >&2
  exit 1
fi
mkdir -p "$here/captures"
mv "$tmp/$stage-plan.json" "$here/captures/$stage-plan.json"
printf '{"case":"%s-plan","file":"%s-plan.json","command":"lz-offline --candidate <checkout> -- task capture:%s-plan","inner_command":"tofu init; tofu test -json -verbose (stages/%s); run published_outputs_match_the_schema","toolchain":"%s","tool":"tofu","tool_version":"%s","entry_exit":%d,"sha256":"%s","inputs_sha256":"%s","captured_on":"%s"}\n' \
  "$stage" "$stage" "$stage" "$stage" "$toolchain" "$version" "$status" "$(digest "$here/captures/$stage-plan.json")" \
  "$(inputs)" "$(date -u +%Y-%m-%d)" > "$here/captures/$stage-plan.json.meta.json"
echo "$stage-plan: entry exit $status" >&2
