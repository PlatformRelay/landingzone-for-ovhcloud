#!/bin/sh
# Capture the pinned provider's schema snapshot (006 FR-015, T035) through the installed offline
# entry:
#
#   tests/fixtures/provider-schema/capture.sh <absolute entry> <absolute checkout>
#
# The entry runs `task capture:provider-schema` in its sandbox (no network, no credential): the
# pinned OpenTofu installs ovh/ovh at the version stages/bootstrap's lock pins from the runtime's
# mirror and prints `tofu providers schema -json`. That stdout is ovh-<version>.json; beside it,
# ovh-<version>.command.txt records the command, the tofu version qualified by `task
# verify:toolchain` in the same session, the provider version, the snapshot's digest and the
# date. The snapshot is moved over the committed one only when the entry exits 0; snapshots and
# records of other versions are then removed, so one snapshot sits beside the pin. The script
# writes only those files and its temporary directory, which it removes on every exit.
set -eu
umask 077
entry=$1
checkout=$2
here=$checkout/tests/fixtures/provider-schema
run() { env -i PATH=/usr/bin:/bin "$entry" --candidate "$checkout" -- task "$1"; }

version=$(sed -n '/^provider "registry.opentofu.org\/ovh\/ovh" {$/,/^}$/s/^  version *= "\([0-9][0-9.]*\)"$/\1/p' "$checkout/stages/bootstrap/.terraform.lock.hcl")
if [ -z "$version" ]; then
  echo "capture.sh: no ovh/ovh version in stages/bootstrap/.terraform.lock.hcl" >&2
  exit 2
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
status=0
run verify:toolchain > "$tmp/verify-toolchain.out" 2>&1 || status=$?
if [ "$status" -ne 0 ]; then
  echo "capture.sh: verify:toolchain exited $status:" >&2
  tail -n 20 "$tmp/verify-toolchain.out" >&2
  exit 2
fi
toolchain=$(grep '^TOOLCHAIN_QUALIFIED ' "$tmp/verify-toolchain.out" || true)
tofu=$(printf '%s\n' "$toolchain" | tr ' ' '\n' | sed -n 's/^tofu=//p')
if [ -z "$tofu" ]; then
  echo "capture.sh: unexpected toolchain: $toolchain" >&2
  exit 2
fi

status=0
run capture:provider-schema > "$tmp/schema.json" 2> "$tmp/schema.err" || status=$?
if [ "$status" -ne 0 ] || [ ! -s "$tmp/schema.json" ]; then
  echo "capture:provider-schema: entry exit $status; snapshot left unchanged:" >&2
  tail -n 20 "$tmp/schema.err" >&2
  exit 1
fi
mkdir -p "$here"
mv "$tmp/schema.json" "$here/ovh-$version.json"
{
  echo "command: lz-offline --candidate <checkout> -- task capture:provider-schema"
  echo "inner_command: tofu init -backend=false; tofu providers schema -json (root requiring only ovh/ovh = \"$version\", stages/bootstrap's lock)"
  echo "tofu: $tofu"
  echo "provider: registry.opentofu.org/ovh/ovh $version"
  echo "sha256: $(sha256sum "$here/ovh-$version.json" | cut -d' ' -f1)"
  echo "captured_on: $(date -u +%Y-%m-%d)"
} > "$tmp/record"
mv "$tmp/record" "$here/ovh-$version.command.txt"
find "$here" -maxdepth 1 -type f \( -name 'ovh-*.json' -o -name 'ovh-*.command.txt' \) \
  ! -name "ovh-$version.json" ! -name "ovh-$version.command.txt" -exec rm -f {} +
echo "capture:provider-schema: entry exit $status, ovh/ovh $version" >&2
