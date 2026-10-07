#!/bin/sh
# Capture the tenant-state stage's real plan for the output-contract pin of 005 T022 through the
# installed offline entry:
#
#   tests/fixtures/outputs/capture-tenant-state.sh <absolute entry> <absolute checkout>
#
# The entry runs `task capture:tenant-state-plan` in its sandbox (no network, no credential): the
# pinned OpenTofu runs the stage's own unit tests (mocked provider) on a scratch copy and prints
# the verbose JSON plan of the run `published_outputs_match_the_schema`. That stdout is the fixture
# captures/tenant-state-plan.json. A .meta.json sidecar records the command, the entry's exit
# status, the pinned identities qualified by `task verify:toolchain` in the same session, the
# fixture's digest and one digest over the configuration the plan was made from (the inputs list
# below, each `<sha256>  <path>` line in byte order). The fixture is moved over the committed one
# only when the entry exits 0. The script writes only those files and its temporary directory.
set -eu
entry=$1
checkout=$2
here=$checkout/tests/fixtures/outputs
digest() { sha256sum "$1" | cut -d' ' -f1; }
run() { env -i PATH=/usr/bin:/bin "$entry" --candidate "$checkout" -- task "$1"; }
# Keep in step with stagePlanInputs in tools/internal/stacks/outputs_stage_test.go: every file a
# plan can read (configuration, data files, lock files), not Markdown, state files or hidden
# directories; test files only for the stage.
inputs() {
  cd "$checkout" && find stages/tenant-state components/state-backend modules/naming \
    modules/object-storage-protected modules/object-storage-user -type d -name '.*' -prune -o \
    -type f ! -name '*.md' ! -name '*.tfstate*' \
    \( -path 'stages/tenant-state/*' -o ! -path '*/tests/*' \) -print | LC_ALL=C sort | xargs sha256sum | sha256sum | cut -d' ' -f1
}

tmp=$(mktemp -d)
status=0
run verify:toolchain > "$tmp/verify-toolchain.out" || status=$?
if [ "$status" -ne 0 ]; then
  echo "capture-tenant-state.sh: verify:toolchain exited $status (output in $tmp)" >&2
  exit 2
fi
toolchain=$(grep '^TOOLCHAIN_QUALIFIED ' "$tmp/verify-toolchain.out" || true)
case "$toolchain" in
  *" tofu=1.13.0 "*" network=none") ;;
  *) echo "capture-tenant-state.sh: unexpected toolchain: $toolchain" >&2; exit 2 ;;
esac
version=$(printf '%s\n' "$toolchain" | tr ' ' '\n' | sed -n 's/^tofu=//p')

status=0
run capture:tenant-state-plan > "$tmp/tenant-state-plan.json" || status=$?
if [ "$status" -ne 0 ] || [ ! -s "$tmp/tenant-state-plan.json" ]; then
  echo "tenant-state-plan: entry exit $status; fixture left unchanged (output in $tmp)" >&2
  exit 1
fi
mkdir -p "$here/captures"
mv "$tmp/tenant-state-plan.json" "$here/captures/tenant-state-plan.json"
printf '{"case":"tenant-state-plan","file":"tenant-state-plan.json","command":"lz-offline --candidate <checkout> -- task capture:tenant-state-plan","inner_command":"tofu init; tofu test -json -verbose (stages/tenant-state); run published_outputs_match_the_schema","toolchain":"%s","tool":"tofu","tool_version":"%s","entry_exit":%d,"sha256":"%s","inputs_sha256":"%s","captured_on":"%s"}\n' \
  "$toolchain" "$version" "$status" "$(digest "$here/captures/tenant-state-plan.json")" \
  "$(inputs)" "$(date -u +%Y-%m-%d)" > "$here/captures/tenant-state-plan.json.meta.json"
echo "tenant-state-plan: entry exit $status" >&2
