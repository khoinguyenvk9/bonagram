#!/bin/sh
# Draws the README screenshots from fake data: the test writes HTML, headless Chrome takes the picture,
# ImageMagick cuts away the page around the terminal. Needs Google Chrome and ImageMagick.
set -e
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
BONAGRAM_SHOTS="$tmp" go test -run TestScreenshots -count=1 . >/dev/null
chrome="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
for f in "$tmp"/*.html; do
	name=$(basename "$f" .html)
	"$chrome" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 \
		--window-size=1400,1000 --screenshot="$tmp/$name.png" "file://$f" >/dev/null 2>&1
	magick "$tmp/$name.png" -fuzz 2% -trim +repage "docs/$name.png"
	echo "docs/$name.png"
done
rm -rf "$tmp"
