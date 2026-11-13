#!/bin/sh
# Builds Talos for release into build/darwin-universal/: the app, its disk
# image and the update files, signed with the key of updates.privateKey.
#
#   TALOS_SIGN="Developer ID Application: … (TEAMID)" scripts/release.sh -upload
#
# -upload puts them in a draft release of shqingda/talos-releases; publishing
# the draft is what installed apps update to. The version's notes are its
# section of CHANGELOG.md.
#
# With TALOS_SIGN the app is signed with that identity and notarized
# through the keychain profile "talos-notary", stored once with
#   xcrun notarytool store-credentials talos-notary
# Without it the app is signed ad hoc and runs only on this Mac.
set -e
cd "$(dirname "$0")/.."
if [ -n "$TALOS_SIGN" ]; then
	exec go tool mygo build -platform darwin/universal -sign "$TALOS_SIGN" "$@"
fi
echo "TALOS_SIGN not set: signing ad hoc, skipping notarization" >&2
exec go tool mygo build -platform darwin/universal -skip-notarize "$@"
