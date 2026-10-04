#!/bin/sh
# Capture the report fixture streams through the installed offline entry.
#
#   tests/fixtures/tofu/capture.sh <absolute-entry> <absolute-checkout>
#
# For each case the entry verifies pins and isolation, then runs the pinned
# `tofu test -json` on tests/fixtures/tofu/modules/<case> inside the sandbox.
# Its stdout is the stream; a sidecar records the command, exit status and
# digests. truncated.jsonl is derived from pass.jsonl, not captured.
set -eu
entry=$1
checkout=$2
here=$checkout/tests/fixtures/tofu
streams=$here/streams
mkdir -p "$streams"
digest() { sha256sum "$1" | cut -d' ' -f1; }
for case in pass fail zero skip cleanup; do
  stream=$streams/$case.jsonl
  status=0
  env -i PATH=/usr/bin:/bin "$entry" --candidate "$checkout" -- task "capture:tofu-$case" \
    > "$stream" 2> "$streams/.stderr" || status=$?
  modules=$(cd "$here/modules/$case" && find . -type f -name '*.tf*' | sort | xargs cat | sha256sum | cut -d' ' -f1)
  version=$(head -n 1 "$stream" | sed -n 's/.*"tofu":"\([^"]*\)".*/\1/p')
  printf '{"case":"%s","command":"lz-offline --candidate <checkout> -- task capture:tofu-%s","inner_command":"tofu test -json","tool":"tofu","tool_version":"%s","entry_exit":%d,"stream_sha256":"%s","module_sha256":"%s","captured_on":"%s"}\n' \
    "$case" "$case" "$version" "$status" "$(digest "$stream")" "$modules" "$(date -u +%Y-%m-%d)" > "$streams/$case.meta.json"
done
rm -f "$streams/.stderr"
# A killed run: the passing stream cut in the middle of its final line.
size=$(wc -c < "$streams/pass.jsonl")
last=$(tail -n 1 "$streams/pass.jsonl" | wc -c)
keep=$((size - last / 2))
head -c "$keep" "$streams/pass.jsonl" > "$streams/truncated.jsonl"
printf '{"case":"truncated","derived_from":"pass.jsonl","bytes_kept":%d,"bytes_original":%d,"stream_sha256":"%s"}\n' \
  "$keep" "$size" "$(digest "$streams/truncated.jsonl")" > "$streams/truncated.meta.json"
