#!/bin/sh
# Fetch the offline entry (lz-offline) that CI runs every check in, verify it
# against the pinned digests, and install it outside this clone.
#
#   tools/scripts/fetch-offline-entry.sh [--dest DIR]
#
# It does what the "Fetch the runtime layer by digest" step of
# .github/workflows/foundation.yml does, and reads the five pins (LZ_IMAGE,
# LZ_MANIFEST, LZ_LAYER, LZ_ENTRY_SHA256, LZ_BWRAP_SHA256) from that file's
# own env block, so a new runtime revision needs no edit here. The workflow is
# rendered from pipelines/github/foundation-source.json, and
# `task test:foundation-ci` keeps the two equal.
#
# The image is pulled anonymously from ghcr.io. Every download is checked
# against its digest before use; any mismatch stops the script and leaves
# nothing installed. The default install directory is
# ${XDG_DATA_HOME:-~/.local/share}/landingzone-for-ovhcloud/runtime/<first 12
# hex of the entry digest>; a verified install there is reused.
#
# The entry runs its sandbox through /usr/bin/bwrap, which must be the pinned
# build shipped in the layer. If it is not, the script prints the one sudo
# command that installs it and exits 3; it never runs sudo itself.
# LZ_HOST_BWRAP replaces /usr/bin/bwrap for the tests only: the entry always
# uses /usr/bin/bwrap.
#
# stdout carries only the entry's path, so `E=$(tools/scripts/fetch-offline-entry.sh)`
# works; everything else goes to stderr.
# Exit: 0 ready, 1 refused, 2 usage, 3 installed but /usr/bin/bwrap not pinned
# (follow the printed command, then run the script again).
# Needs: curl, sha256sum, tar with gzip, sed, grep.
set -eu
# pipefail where the shell has it (bash, busybox ash, newer dash); older dash
# lacks it. No pipeline below needs it to fail closed: a failed stage leaves
# an empty or short value, which the checks after it refuse.
# shellcheck disable=SC3040
if (set -o pipefail) 2>/dev/null; then set -o pipefail; fi

me=fetch-offline-entry
registry=https://ghcr.io

say() { printf '%s: %s\n' "$me" "$*" >&2; }
fail() {
	code=$1
	shift
	say "$*"
	exit "$code"
}
# quote prints $1 as one single-quoted shell word.
quote() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }

usage="usage: $0 [--dest DIR]"
dest=
while [ $# -gt 0 ]; do
	case $1 in
	--dest)
		[ $# -ge 2 ] || fail 2 "USAGE: --dest needs a directory; $usage"
		dest=$2
		shift 2
		;;
	-h | --help)
		printf '%s\n' "$usage"
		exit 0
		;;
	*) fail 2 "USAGE: unknown argument $1; $usage" ;;
	esac
done

repo=$(cd "$(dirname "$0")/../.." && pwd -P)
workflow=$repo/.github/workflows/foundation.yml
[ -f "$workflow" ] || fail 1 "PIN_INVALID: $workflow not found"

# pin prints the value of one line of the workflow's own env block, which
# every job shares. The line must occur exactly once and its value must match
# the extended regular expression $2.
pin() {
	count=$(sed -n "/^  $1: /p" "$workflow" | wc -l | tr -d ' ')
	[ "$count" -eq 1 ] || fail 1 "PIN_INVALID: $1 occurs $count times in $workflow, want once"
	value=$(sed -n "s/^  $1: //p" "$workflow")
	printf '%s\n' "$value" | grep -Eqx "$2" || fail 1 "PIN_INVALID: $1 is not a well-formed value: $value"
	printf '%s' "$value"
}
image=$(pin LZ_IMAGE '[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*')
manifest=$(pin LZ_MANIFEST 'sha256:[0-9a-f]{64}')
layer=$(pin LZ_LAYER 'sha256:[0-9a-f]{64}')
entry_sha=$(pin LZ_ENTRY_SHA256 '[0-9a-f]{64}')
bwrap_sha=$(pin LZ_BWRAP_SHA256 '[0-9a-f]{64}')

if [ -z "$dest" ]; then
	dest=${XDG_DATA_HOME:-${HOME:?HOME is not set}/.local/share}/landingzone-for-ovhcloud/runtime/$(printf '%s' "$entry_sha" | cut -c1-12)
fi

# The entry refuses to run from inside the candidate it checks, and admits
# only a clean absolute path, so the install goes to such a path outside the
# clone. Nothing is created before the location is accepted.
case $dest in
/*) ;;
*) fail 1 "INSTALL_LOCATION: $dest is not an absolute path" ;;
esac
case $dest in
*//* | */./* | */../* | */. | */.. | */) fail 1 "INSTALL_LOCATION: $dest is not a clean path (no //, . or .. and no trailing /)" ;;
esac
# Resolve the deepest existing ancestor physically, so a symlink cannot hide
# the clone; the part below it does not exist yet.
existing=$dest
rest=
while [ ! -d "$existing" ]; do
	rest=/${existing##*/}$rest
	existing=${existing%/*}
	[ -n "$existing" ] || existing=/
done
base=$(cd "$existing" && pwd -P)
[ "$base" != / ] || base=
dest=$base$rest
case $dest/ in
"$repo"/*) fail 1 "INSTALL_LOCATION: $dest is inside the clone $repo; the entry must be installed outside it" ;;
esac

# matches succeeds when $1 is a regular file whose sha256 is $2.
matches() { [ -f "$1" ] && [ ! -L "$1" ] && [ "$(sha256sum <"$1" | cut -d' ' -f1)" = "$2" ]; }
verify() {
	matches "$1" "$2" || fail 1 "DIGEST_MISMATCH: $3 is not the pinned sha256 $2"
}

entry=$dest/runtime/lz-offline
if [ -e "$dest" ]; then
	if [ -f "$dest/.layer-digest" ] && [ "$(cat "$dest/.layer-digest")" = "$layer" ] &&
		matches "$entry" "$entry_sha" && [ -x "$entry" ] && matches "$dest/host/bwrap" "$bwrap_sha"; then
		say "reusing the verified install in $dest"
	else
		fail 1 "INSTALL_LOCATION: $dest exists and is not a verified install of $layer; remove it or pass --dest"
	fi
else
	parent=${dest%/*}
	mkdir -p "${parent:-/}"
	work=$(mktemp -d "${parent}/.fetch-XXXXXX")
	trap 'rm -rf "$work"' EXIT
	trap 'exit 1' HUP INT TERM

	get() {
		what=$1
		shift
		curl -fsSL --proto '=https' --proto-redir '=https' "$@" || fail 1 "FETCH_FAILED: $what"
	}
	say "fetching $registry/$image@$manifest"
	response=$(curl -fsS --proto '=https' "$registry/token?scope=repository:$image:pull") ||
		fail 1 "FETCH_FAILED: no anonymous pull token for $image"
	token=$(printf '%s' "$response" | sed -n 's/^.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*$/\1/p')
	[ -n "$token" ] || fail 1 "FETCH_FAILED: no anonymous pull token for $image"

	get "manifest" -H "Authorization: Bearer $token" -H "Accept: application/vnd.oci.image.manifest.v1+json" \
		-o "$work/manifest.json" "$registry/v2/$image/manifests/$manifest"
	verify "$work/manifest.json" "${manifest#sha256:}" "the manifest"
	# The manifest bytes are pinned above, so naming the layer is enough.
	grep -Eq "\"digest\"[[:space:]]*:[[:space:]]*\"$layer\"" "$work/manifest.json" ||
		fail 1 "DIGEST_MISMATCH: the pinned manifest does not name the layer $layer"

	get "layer" -H "Authorization: Bearer $token" -o "$work/layer.tgz" "$registry/v2/$image/blobs/$layer"
	verify "$work/layer.tgz" "${layer#sha256:}" "the layer"

	mkdir "$work/root"
	# The entry admits runtime/resources/ against the exact modes recorded in
	# resources.json, so the layer's modes must survive whatever the caller's
	# umask is. GNU tar -p keeps them; busybox tar applies the umask even with
	# -p. Unpacking under umask 0 keeps them with both. $work is mode 0700
	# (mktemp -d), so nobody else can reach the files meanwhile.
	(umask 0 && tar -xzpf "$work/layer.tgz" -C "$work/root") || fail 1 "FETCH_FAILED: the layer does not unpack"
	verify "$work/root/runtime/lz-offline" "$entry_sha" "the entry runtime/lz-offline"
	verify "$work/root/host/bwrap" "$bwrap_sha" "the launcher host/bwrap"
	# The entry and the launcher sit outside resources/ and are not admitted by
	# mode: make them executable and writable by their owner only.
	chmod 0755 "$work/root/runtime/lz-offline" "$work/root/host/bwrap"
	printf '%s\n' "$layer" >"$work/root/.layer-digest"

	[ ! -e "$dest" ] || fail 1 "INSTALL_LOCATION: $dest appeared during the fetch"
	# -T: never move into a directory that appeared meanwhile.
	mv -T "$work/root" "$dest" || fail 1 "INSTALL_LOCATION: could not move the install to $dest"
	verify "$entry" "$entry_sha" "the installed entry"
	[ -x "$entry" ] || fail 1 "INSTALL_LOCATION: the installed entry $entry is not executable"
	say "installed in $dest"
fi

host_bwrap=${LZ_HOST_BWRAP:-/usr/bin/bwrap}
if ! matches "$host_bwrap" "$bwrap_sha"; then
	say "BWRAP_UNPINNED: $host_bwrap is missing or is not the pinned build (sha256 $bwrap_sha)."
	say "The entry needs it. Install the verified build from the layer, then run this script again:"
	printf '  sudo install -m 0755 %s %s\n' "$(quote "$dest/host/bwrap")" "$(quote "$host_bwrap")" >&2
	exit 3
fi

say "ready. Run a check from the root of your clone:"
# shellcheck disable=SC2016 # $PWD is for the reader's shell, not this one.
printf '  %s --candidate "$PWD" -- task test:slice\n' "$(quote "$entry")" >&2
printf '%s\n' "$entry"
