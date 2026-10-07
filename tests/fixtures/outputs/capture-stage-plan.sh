#!/bin/sh
# Capture a stage's real plan for the output-contract pins of 005 T022 (tenant-state), T024
# (account-governance), T028 (project, adopt and reference mode), T030 (project-network) and T032
# (runtime, without and with a slot) through the installed offline entry:
#
#   tests/fixtures/outputs/capture-stage-plan.sh <absolute entry> <absolute checkout> <case>
#
# A case is a stage, `project-reference` (the project stage's reference-mode run) or `runtime-slot`
# (the runtime stage's run with slot `blue`). The entry runs
# `task capture:<case>-plan` in its sandbox (no network, no credential): the pinned OpenTofu runs the
# stage's own unit tests (mocked provider) on a scratch copy and prints the verbose JSON plan of the
# case's run. That stdout is the fixture captures/<case>-plan.json. A .meta.json sidecar records the
# command, the entry's exit status, the pinned identities qualified by `task verify:toolchain` in
# the same session, the fixture's digest and one digest over the configuration the plan was made
# from (the stage's inputs below, each `<sha256>  <path>` line in byte order). The fixture is moved
# over the committed one only when the entry exits 0. The script writes only those files and its
# temporary directory.
set -eu
entry=$1
checkout=$2
capture_case=$3
here=$checkout/tests/fixtures/outputs
digest() { sha256sum "$1" | cut -d' ' -f1; }
run() { env -i PATH=/usr/bin:/bin "$entry" --candidate "$checkout" -- task "$1"; }
# Keep in step with stagePlans and planCases in tools/internal/stacks/outputs_stage_test.go: the
# stage, its run and the directories the stage's plan is made from.
testrun=published_outputs_match_the_schema
case "$capture_case" in
  tenant-state) stage=tenant-state; dirs="stages/tenant-state components/state-backend modules/naming modules/object-storage-protected modules/object-storage-user" ;;
  account-governance) stage=account-governance; dirs="stages/account-governance components/identity/ovh-native modules/naming modules/iam-service-account modules/iam-policy modules/identity-group" ;;
  project | project-reference) stage=project; dirs="stages/project components/project-factory modules/naming modules/cloud-project modules/cloud-quota" ;;
  project-network) stage=project-network; dirs="stages/project-network components/network/island modules/naming modules/private-network" ;;
  runtime | runtime-slot) stage=runtime; dirs="stages/runtime components/runtime/managed-only modules/naming modules/object-storage" ;;
  *) echo "capture-stage-plan.sh: unknown case: $capture_case" >&2; exit 2 ;;
esac
if [ "$capture_case" = project-reference ]; then testrun=reference_published_outputs_with_both_toggles; fi
if [ "$capture_case" = runtime-slot ]; then testrun=slot_blue_names_the_bucket; fi
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
run "capture:$capture_case-plan" > "$tmp/$capture_case-plan.json" || status=$?
if [ "$status" -ne 0 ] || [ ! -s "$tmp/$capture_case-plan.json" ]; then
  echo "$capture_case-plan: entry exit $status; fixture left unchanged (output in $tmp)" >&2
  exit 1
fi
mkdir -p "$here/captures"
mv "$tmp/$capture_case-plan.json" "$here/captures/$capture_case-plan.json"
printf '{"case":"%s-plan","file":"%s-plan.json","command":"lz-offline --candidate <checkout> -- task capture:%s-plan","inner_command":"tofu init; tofu test -json -verbose (stages/%s); run %s","toolchain":"%s","tool":"tofu","tool_version":"%s","entry_exit":%d,"sha256":"%s","inputs_sha256":"%s","captured_on":"%s"}\n' \
  "$capture_case" "$capture_case" "$capture_case" "$stage" "$testrun" "$toolchain" "$version" "$status" "$(digest "$here/captures/$capture_case-plan.json")" \
  "$(inputs)" "$(date -u +%Y-%m-%d)" > "$here/captures/$capture_case-plan.json.meta.json"
echo "$capture_case-plan: entry exit $status" >&2
