#!/bin/sh
# Renders the app icon: resources/icon.png (1024, the updater window's too)
# and design/icon/Talos.icns, whose 16 and 32 point sizes use the
# simplified drawings.
set -e
cd "$(dirname "$0")"
set_dir=$(mktemp -d)/Talos.iconset
mkdir -p "$set_dir"
render() { sips -s format png "$1" -z "$2" "$2" --out "$3" >/dev/null; }
render talos-mech-16.svg 16 "$set_dir/icon_16x16.png"
render talos-mech-32.svg 32 "$set_dir/icon_16x16@2x.png"
render talos-mech-32.svg 32 "$set_dir/icon_32x32.png"
render talos-mech.svg 64 "$set_dir/icon_32x32@2x.png"
for s in 128 256 512; do
	render talos-mech.svg $s "$set_dir/icon_${s}x${s}.png"
	render talos-mech.svg $((s * 2)) "$set_dir/icon_${s}x${s}@2x.png"
done
iconutil -c icns "$set_dir" -o Talos.icns
mkdir -p ../../resources
render talos-mech.svg 1024 ../../resources/icon.png
