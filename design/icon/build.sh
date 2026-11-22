#!/bin/sh
# Renders the Stratum app icon: resources/icon.png (1024, white default),
# design/icon/Stratum.icns (white default), and design/icon/Stratum-dark.icns.
set -e
cd "$(dirname "$0")"
python3 src/stratum.py

build_icns() {
	prefix=$1
	out=$2
	set_dir=$(mktemp -d)/Stratum.iconset
	mkdir -p "$set_dir"
	render() { sips -s format png "$1" -z "$2" "$2" --out "$3" >/dev/null; }
	render "${prefix}-16.svg" 16 "$set_dir/icon_16x16.png"
	render "${prefix}-32.svg" 32 "$set_dir/icon_16x16@2x.png"
	render "${prefix}-32.svg" 32 "$set_dir/icon_32x32.png"
	render "${prefix}.svg" 64 "$set_dir/icon_32x32@2x.png"
	for s in 128 256 512; do
		render "${prefix}.svg" $s "$set_dir/icon_${s}x${s}.png"
		render "${prefix}.svg" $((s * 2)) "$set_dir/icon_${s}x${s}@2x.png"
	done
	iconutil -c icns "$set_dir" -o "$out"
}

build_icns stratum-icon-light Stratum.icns
build_icns stratum-icon-dark Stratum-dark.icns
mkdir -p ../../resources
sips -s format png stratum-icon-light.svg -z 1024 1024 --out ../../resources/icon.png >/dev/null
