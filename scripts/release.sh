#!/bin/sh
# Builds Stratum for Apple silicon into build/darwin-arm64/: the app, its disk
# image and the update files, signed with the key of updates.privateKey.
# The app is signed ad hoc and not notarized; the first launch of a
# downloaded copy is allowed in System Settings → Privacy & Security.
#
#   scripts/release.sh -upload
#
# -upload puts them in a draft release of shqingda/stratum-releases; publishing
# the draft is what installed apps update to. The version's notes are its
# section of CHANGELOG.md.
set -e
cd "$(dirname "$0")/.."
exec go tool mygo build -platform darwin/arm64 "$@"
