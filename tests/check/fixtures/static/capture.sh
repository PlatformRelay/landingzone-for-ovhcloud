#!/bin/sh
# Capture the pinned tools' static-clause reports through the offline entry.
# usage: capture.sh <lz-offline> <absolute-checkout>
# Each report is written to reports/<case>.json and its exit status to
# reports/<case>.exit; stderr is checked for the exit marker the capture
# target prints.
set -eu
entry=$1
checkout=$2
out=$(dirname "$0")/reports
mkdir -p "$out"
for case in validate-valid validate-malformed tflint-valid tflint-lint tflint-malformed; do
	"$entry" --candidate "$checkout" -- task "capture:$case" > "$out/$case.json" 2> "$out/$case.stderr"
	sed -n 's/^LZ_EXIT=\([0-9]*\)$/\1/p' "$out/$case.stderr" > "$out/$case.exit"
	test -s "$out/$case.exit"
	rm "$out/$case.stderr"
done
