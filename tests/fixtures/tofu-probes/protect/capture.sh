#!/bin/sh
# Capture the retained-resource guard's plan fixtures (005 T063/T064) through the installed
# offline entry's capture admission (T070):
#
#   tests/fixtures/tofu-probes/protect/capture.sh <absolute entry> <absolute checkout>
#
# For each case the entry verifies the pinned identities and the isolation (no network, no
# credential, no host mount) and then runs `task capture:protect-<case>` inside the sandbox, which
# runs roots.sh with the pinned OpenTofu; its stdout is the fixture <case>.json next to this
# script. A <case>.meta.json sidecar records the command, the entry's exit status, the pinned
# identities qualified by `task verify:toolchain` in the same session, the fixture's file name and
# digest, and the digest of roots.sh (the case table and roots). truncated.json is derived on the
# host from the recaptured org-change.json (a killed `show -json`), not captured. A case is written
# to a temporary file and moved over its fixture only when the entry exits 0; the first failure
# stops the run and leaves the committed fixtures as they were. The script writes only those files
# and its temporary directory (stderr goes to the caller) and deletes nothing.
set -eu
entry=$1
checkout=$2
here=$checkout/tests/fixtures/tofu-probes/protect
input=tests/fixtures/tofu-probes/protect/roots.sh
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

for case in create noop update unretained-replace removed-block org-change create-before-destroy \
  tenant-removed project-replaced forget module-removed two-replacements region-removed \
  replace-requested moved-replace moved-out moved-only state-not-plan; do
  status=0
  run "capture:protect-$case" > "$tmp/$case.json" || status=$?
  if [ "$status" -ne 0 ]; then
    echo "$case: entry exit $status; fixtures left unchanged (output in $tmp)" >&2
    exit 1
  fi
  mv "$tmp/$case.json" "$here/$case.json"
  printf '{"case":"%s","file":"%s.json","command":"lz-offline --candidate <checkout> -- task capture:protect-%s","inner_command":"sh roots.sh /tcb/tofu %s","toolchain":"%s","tool":"tofu","tool_version":"%s","entry_exit":%d,"sha256":"%s","input":"%s","input_sha256":"%s","captured_on":"%s"}\n' \
    "$case" "$case" "$case" "$case" "$toolchain" "$version" "$status" "$(digest "$here/$case.json")" \
    "$input" "$(digest "$checkout/$input")" "$(date -u +%Y-%m-%d)" > "$here/$case.meta.json"
  echo "$case: entry exit $status" >&2
done

# A killed `show -json`: org-change.json cut in the middle.
size=$(wc -c < "$here/org-change.json")
keep=$((size / 2))
head -c "$keep" "$here/org-change.json" > "$here/truncated.json"
printf '{"case":"truncated","file":"truncated.json","derived_from":"org-change.json","bytes_kept":%d,"bytes_original":%d,"sha256":"%s"}\n' \
  "$keep" "$size" "$(digest "$here/truncated.json")" > "$here/truncated.meta.json"
