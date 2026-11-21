#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
arch="${1:-$(uname -m)}"
out="${2:-$PWD/dist}"
version="${3:-0.1.0}"
case "$arch" in
  arm64) goarch=arm64 ;;
  x86_64) goarch=amd64 ;;
  *) echo "unsupported macOS architecture: $arch" >&2; exit 2 ;;
esac
command -v swiftc >/dev/null
command -v go >/dev/null
app="$out/VoiceRewrite.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
# Link the same Go Core into the native status-bar host. Each HID/frame
# decision is a direct C ABI call: no per-event subprocess and no second Go runtime.
( cd clients
  MACOSX_DEPLOYMENT_TARGET=13.0 CGO_CFLAGS="-mmacosx-version-min=13.0" \
    CGO_LDFLAGS="-mmacosx-version-min=13.0" \
    CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" go build -trimpath -buildmode=c-archive \
    -o "$out/libvoicecore.a" ./macos/bridge
)
swiftc -O -target "${arch}-apple-macosx13.0" \
  -import-objc-header clients/macos/bridge/VoiceCore.h \
  -framework AppKit -framework AVFoundation -framework ApplicationServices \
  -framework CoreGraphics -framework Security -framework CoreFoundation \
  -o "$app/Contents/MacOS/VoiceRewrite" \
  clients/macos/app/main.swift clients/macos/app/RecordingPill.swift "$out/libvoicecore.a"
cat > "$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>VoiceRewrite</string>
<key>CFBundleIdentifier</key><string>com.xujinheng.VoiceRewrite</string>
<key>CFBundleName</key><string>VoiceRewrite</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>$version</string>
<key>CFBundleVersion</key><string>1</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>LSUIElement</key><true/>
<key>NSMicrophoneUsageDescription</key><string>VoiceRewrite records microphone audio while a configured shortcut is held.</string>
</dict></plist>
EOF
plutil -lint "$app/Contents/Info.plist"
test -x "$app/Contents/MacOS/VoiceRewrite"
test "$(lipo -archs "$app/Contents/MacOS/VoiceRewrite")" = "$arch"
if [[ "${CODESIGN_ADHOC:-1}" == 1 ]]; then
  codesign --force --sign - "$app"
  codesign --verify --deep --strict "$app"
fi
echo "$app"
