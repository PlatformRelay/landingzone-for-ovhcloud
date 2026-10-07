#!/bin/sh
# Capture the output-contract fixture of 005 T017 through the installed offline entry's capture
# admission:
#
#   tests/fixtures/outputs/capture.sh <absolute entry> <absolute checkout>
#
# The entry verifies the pinned identities and the isolation (no network, no credential, no host
# mount) and then runs `task capture:outputs-root` inside the sandbox: the pinned OpenTofu applies
# the provider-free root tests/fixtures/outputs/root/ in a scratch directory and prints
# `tofu output -json`. That stdout is the fixture captures/tofu-output.json. A
# tofu-output.json.meta.json sidecar records the command, the entry's exit status, the pinned
# identities qualified by `task verify:toolchain` in the same session, and the digests of the
# fixture and of the input root. The fixture is written to a temporary file and moved over the
# committed one only when the entry exits 0. The script writes only those files and its temporary
# directory (stderr goes to the caller) and deletes nothing.
set -eu
entry=$1
checkout=$2
here=$checkout/tests/fixtures/outputs
input=tests/fixtures/outputs/root/main.tf
digest() { sha256sum "$1" | cut -d' ' -f1; }
run() { env -i PATH=/usr/bin:/bin "$entry" --candidate "$checkout" -- task "$1"; }

tmp=$(mktemp -d)
status=0
run verify:toolchain > "$tmp/verify-toolchain.out" || status=$?
if [ "$status" -ne 0 ]; then
  echo "capture.sh: verify:toolchain exited $status (output in $tmp)" >&2
  exit 2
fi
toolchain=$(grep '^TOOLCHAIN_QUALIFIED ' "$tmp/verify-toolchain.out" || true)
case "$toolchain" in
  *" tofu=1.13.0 "*" network=none") ;;
  *) echo "capture.sh: unexpected toolchain: $toolchain" >&2; exit 2 ;;
esac
version=$(printf '%s\n' "$toolchain" | tr ' ' '\n' | sed -n 's/^tofu=//p')

status=0
run capture:outputs-root > "$tmp/tofu-output.json" || status=$?
if [ "$status" -ne 0 ]; then
  echo "outputs-root: entry exit $status; fixture left unchanged (output in $tmp)" >&2
  exit 1
fi
mkdir -p "$here/captures"
mv "$tmp/tofu-output.json" "$here/captures/tofu-output.json"
printf '{"case":"outputs-root","file":"tofu-output.json","command":"lz-offline --candidate <checkout> -- task capture:outputs-root","inner_command":"tofu init; tofu apply -auto-approve; tofu output -json","toolchain":"%s","tool":"tofu","tool_version":"%s","entry_exit":%d,"sha256":"%s","input":"%s","input_sha256":"%s","captured_on":"%s"}\n' \
  "$toolchain" "$version" "$status" "$(digest "$here/captures/tofu-output.json")" \
  "$input" "$(digest "$checkout/$input")" "$(date -u +%Y-%m-%d)" > "$here/captures/tofu-output.json.meta.json"
echo "outputs-root: entry exit $status" >&2
