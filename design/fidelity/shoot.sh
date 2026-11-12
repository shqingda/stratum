#!/bin/sh
# shoot.sh N W H OUT: renders the N-th window (1-based) of ../mockups.html
# alone, at twice its size, to OUT.
set -e
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
sed "s#</body>#<script>(function(){var w=document.querySelectorAll('.win')[$1-1];document.body.innerHTML='';document.body.appendChild(w);document.body.style.margin='0';document.body.style.background='transparent';w.style.borderRadius='0';w.style.boxShadow='none';})()</script></body>#" "$here/../mockups.html" > "$tmp/m.html"
cp -R "$here/../icon" "$tmp/icon"
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --hide-scrollbars \
  --force-device-scale-factor=2 --window-size="$2,$3" --virtual-time-budget=8000 --screenshot="$4" "file://$tmp/m.html" 2>/dev/null
rm -rf "$tmp"
