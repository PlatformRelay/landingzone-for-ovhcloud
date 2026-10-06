#!/bin/sh
# Capture the premise fixtures of 005 T007 (P4, P6, P16, P17, P24) through the installed offline
# entry's capture admission:
#
#   tests/fixtures/tofu-probes/capture.sh <absolute entry> <absolute checkout>
#
# For each case the entry verifies the pinned identities and the isolation (no network, no
# credential, no host mount) and then runs `task capture:<case>` inside the sandbox; the roots are
# tests/fixtures/tofu-probes/<case>/ and tests/fixtures/terramate/p17/. Each case's stdout is the
# fixture, written to tests/fixtures/tofu-probes/captures/ (terramate: tests/fixtures/terramate/
# captures/). A <case>.meta.json sidecar records the command, the entry's exit status, the pinned
# identities qualified by `task verify:toolchain` in the same session, and digests of the fixture
# and of the input root; the sidecar's file name and its `file` field name the fixture it vouches
# for. The script writes only those files (stderr goes to the caller) and deletes nothing.
set -eu
entry=$1
checkout=$2
probes=$checkout/tests/fixtures/tofu-probes
tm=$checkout/tests/fixtures/terramate
mkdir -p "$probes/captures" "$tm/captures"
digest() { sha256sum "$1" | cut -d' ' -f1; }
tree_digest() { (cd "$1" && find . -type f | LC_ALL=C sort | xargs sha256sum | sha256sum | cut -d' ' -f1); }
run() { env -i PATH=/usr/bin:/bin "$entry" --candidate "$checkout" -- task "$1"; }

toolchain=$(run verify:toolchain | grep '^TOOLCHAIN_QUALIFIED ')
case "$toolchain" in
  *" tofu=1.13.0 terramate=0.17.3 "*) ;;
  *) echo "capture.sh: unexpected toolchain: $toolchain" >&2; exit 2 ;;
esac

# case | fixture file | input root | tool | inner command
while IFS='|' read -r case file root tool inner; do
  out=$(dirname "$checkout/$file")
  name=$(basename "$file")
  status=0
  run "capture:$case" > "$out/$name" || status=$?
  version=$(printf '%s\n' "$toolchain" | tr ' ' '\n' | sed -n "s/^$tool=//p")
  printf '{"case":"%s","file":"%s","command":"lz-offline --candidate <checkout> -- task capture:%s","inner_command":"%s","toolchain":"%s","tool":"%s","tool_version":"%s","entry_exit":%d,"sha256":"%s","input":"%s","input_sha256":"%s","captured_on":"%s"}\n' \
    "$case" "$name" "$case" "$inner" "$toolchain" "$tool" "$version" "$status" "$(digest "$out/$name")" \
    "$root" "$(tree_digest "$checkout/$root")" "$(date -u +%Y-%m-%d)" > "$out/$name.meta.json"
done <<'EOF'
p4-encryption|tests/fixtures/tofu-probes/captures/p4-encryption.txt|tests/fixtures/tofu-probes/p4-encryption|tofu|tofu init; tofu apply; tofu plan (wrong, then right passphrase)
p6-import|tests/fixtures/tofu-probes/captures/p6-import.json|tests/fixtures/tofu-probes/p6-import|tofu|tofu init; tofu plan -out; tofu show -json
p6-import-test|tests/fixtures/tofu-probes/captures/p6-import-test.txt|tests/fixtures/tofu-probes/p6-import|tofu|tofu init; tofu test
p6-import-mock|tests/fixtures/tofu-probes/captures/p6-import-mock.txt|tests/fixtures/tofu-probes/p6-import-mock|tofu|tofu init; tofu test
p16-ignore-changes|tests/fixtures/tofu-probes/captures/p16-ignore-changes.jsonl|tests/fixtures/tofu-probes/p16-ignore-changes|tofu|tofu init; tofu test -json
p24-apply-json|tests/fixtures/tofu-probes/captures/p24-apply-json.jsonl|tests/fixtures/tofu-probes/p24-apply-json|tofu|tofu init; tofu apply -json -auto-approve
p17-terramate|tests/fixtures/terramate/captures/p17-terramate.txt|tests/fixtures/terramate/p17|terramate|terramate create --id --name --tags --after; terramate generate; terramate list --run-order
EOF
