#!/bin/bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 VoiceRewrite.app output.dmg arm64|x86_64" >&2
  exit 2
fi

app="$1"
output="$2"
architecture="$3"
case "$architecture" in
  arm64|x86_64) ;;
  *) echo "unsupported architecture: $architecture" >&2; exit 2 ;;
esac

test -x "$app/Contents/MacOS/VoiceRewrite"
test -x "$app/Contents/MacOS/voicebridge"
for executable in VoiceRewrite voicebridge; do
  actual="$(lipo -archs "$app/Contents/MacOS/$executable")"
  if [[ "$actual" != "$architecture" ]]; then
    echo "$executable: expected $architecture, found $actual" >&2
    exit 1
  fi
done

codesign --verify --deep --strict "$app"
mkdir -p "$(dirname "$output")"
output="$(cd "$(dirname "$output")" && pwd)/$(basename "$output")"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
/usr/bin/ditto "$app" "$stage/VoiceRewrite.app"
codesign --verify --deep --strict "$stage/VoiceRewrite.app"
ln -s /Applications "$stage/Applications"

/usr/bin/hdiutil create -volname "VoiceRewrite" -srcfolder "$stage" \
  -format UDZO -ov "$output" >/dev/null
/usr/bin/hdiutil verify "$output" >/dev/null
(
  cd "$(dirname "$output")"
  shasum -a 256 "$(basename "$output")" > "$(basename "$output").sha256"
)
echo "Verified $architecture DMG: $output"
