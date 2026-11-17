#!/usr/bin/env bash
# Native developer build; release signing/notarization belongs to the distribution workflow.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
arch="${1:-$(uname -m)}"
case "$arch" in
  arm64) go_arch=arm64 ;;
  x86_64) go_arch=amd64 ;;
  *) echo "Unsupported Mac architecture: $arch" >&2; exit 2 ;;
esac
destination="${2:-$root/.oneclick-build/$arch}"
app="$destination/VoiceRewrite-OneClick.app"
mkdir -p "$app/Contents/MacOS"
export GOOS=darwin GOARCH="$go_arch" CGO_ENABLED=0
(
  cd "$root/oneclick"
  go build -trimpath -o "$app/Contents/MacOS/qwen3asr-oneclick" .
)
sdk="$(xcrun --sdk macosx --show-sdk-path)"
swiftc -O -parse-as-library -target "$arch-apple-macos14.0" -sdk "$sdk" \
  "$root/macos/OneClickLauncher/OneClickApp.swift" \
  -o "$app/Contents/MacOS/VoiceRewrite-OneClick"
cat > "$app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key><string>zh_CN</string>
  <key>CFBundleExecutable</key><string>VoiceRewrite-OneClick</string>
  <key>CFBundleIdentifier</key><string>com.xujinheng.VoiceRewrite.OneClick</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleName</key><string>VoiceRewrite OneClick</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>14.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSAppTransportSecurity</key>
  <dict><key>NSAllowsLocalNetworking</key><true/></dict>
</dict>
</plist>
PLIST
# Signing the nested helper first prevents unsigned-code errors when launching a
# development app. K owns stable Developer ID signing, notarization and artifacts.
codesign --force --sign - --timestamp=none "$app/Contents/MacOS/qwen3asr-oneclick"
codesign --force --sign - --timestamp=none "$app"
test "$(lipo -archs "$app/Contents/MacOS/qwen3asr-oneclick")" = "$arch"
test "$(lipo -archs "$app/Contents/MacOS/VoiceRewrite-OneClick")" = "$arch"
codesign --verify --deep --strict "$app"
printf 'Built %s (%s)\n' "$app" "$arch"
