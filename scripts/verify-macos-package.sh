#!/usr/bin/env bash
# Validate the *contents* of an app or downloadable archive, not merely its filename.
# Usage: verify-macos-package.sh app|zip|dmg PATH arm64|x86_64 VERSION dev|signed|notarized
set -euo pipefail

if [[ $# -ne 5 ]]; then
  echo "usage: $0 app|zip|dmg PATH arm64|x86_64 VERSION dev|signed|notarized" >&2
  exit 2
fi
kind="$1"
artifact="$2"
arch="$3"
version="$4"
trust="$5"
case "$kind" in app|zip|dmg) ;; *) echo "unknown artifact kind: $kind" >&2; exit 2 ;; esac
case "$arch" in arm64|x86_64) ;; *) echo "unknown architecture: $arch" >&2; exit 2 ;; esac
case "$trust" in dev|signed|notarized) ;; *) echo "unknown trust level: $trust" >&2; exit 2 ;; esac
[[ -e "$artifact" ]] || { echo "missing artifact: $artifact" >&2; exit 1; }

fail() { echo "macOS package verification failed: $*" >&2; exit 1; }

signature_info() { /usr/bin/codesign -dv --verbose=4 "$1" 2>&1; }

verify_app() {
  local app="$1" found info app_team
  [[ -d "$app/Contents" ]] || fail "invalid app directory: $app"
  [[ -f "$app/Contents/Info.plist" ]] || fail "missing Info.plist"
  [[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app/Contents/Info.plist")" == "com.xujinheng.VoiceRewrite" ]] ||
    fail "unexpected bundle identifier"
  found="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")"
  [[ "$found" == "$version" ]] || fail "expected version $version, found $found"

  local exe="$app/Contents/MacOS/VoiceRewrite"
  [[ -f "$exe" && -x "$exe" ]] || fail "missing or non-executable VoiceRewrite"
  found="$(/usr/bin/lipo -archs "$exe")" || fail "invalid Mach-O"
  [[ "$found" == "$arch" ]] || fail "expected only $arch, found $found"
  [[ ! -e "$app/Contents/MacOS/voicebridge" ]] || fail "obsolete Go subprocess included"
  /usr/bin/codesign --verify --deep --strict "$app" || fail "invalid app signature"
  info="$(signature_info "$app")"

  case "$trust" in
    dev)
      [[ "$info" == *"Signature=adhoc"* ]] ||
        fail "developer package must be explicitly ad-hoc signed"
      ;;
    signed|notarized)
      [[ "$info" == *"Authority=Developer ID Application:"* ]] ||
        fail "app lacks Developer ID Application signature"
      [[ "$info" == *"(runtime)"* ]] || fail "hardened runtime missing"
      app_team="$(printf '%s\n' "$info" | sed -n 's/^TeamIdentifier=//p' | head -1)"
      [[ -n "$app_team" && "$app_team" != "not set" ]] || fail "app has no Developer ID team"
      if [[ "$trust" == notarized ]]; then
        /usr/bin/xcrun stapler validate "$app" || fail "app notarization ticket missing"
        /usr/sbin/spctl --assess --type execute --verbose=2 "$app" ||
          fail "Gatekeeper rejects app"
      fi
      ;;
  esac
}

verify_checksum() {
  [[ -f "$artifact.sha256" ]] || fail "missing SHA-256 sidecar: $artifact.sha256"
  (cd "$(dirname "$artifact")" && /usr/bin/shasum -a 256 -c "$(basename "$artifact").sha256") ||
    fail "SHA-256 checksum mismatch"
}

case "$kind" in
  app)
    verify_app "$artifact"
    ;;
  zip)
    verify_checksum
    extracted="$(mktemp -d)"
    trap 'rm -rf "$extracted"' EXIT
    /usr/bin/ditto -x -k "$artifact" "$extracted"
    [[ "$(find "$extracted" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')" == 1 ]] ||
      fail "ZIP must contain only VoiceRewrite.app"
    verify_app "$extracted/VoiceRewrite.app"
    ;;
  dmg)
    verify_checksum
    if [[ "$trust" == notarized ]]; then
      /usr/bin/xcrun stapler validate "$artifact" || fail "DMG notarization ticket missing"
    fi
    mounted="$(mktemp -d)"
    attached=0
    cleanup_mount() {
      if [[ "$attached" == 1 ]]; then /usr/bin/hdiutil detach -quiet "$mounted" || true; fi
      rmdir "$mounted" 2>/dev/null || true
    }
    trap cleanup_mount EXIT
    /usr/bin/hdiutil attach -quiet -readonly -nobrowse -mountpoint "$mounted" "$artifact"
    attached=1
    [[ -L "$mounted/Applications" && "$(readlink "$mounted/Applications")" == /Applications ]] ||
      fail "DMG Applications drag target missing"
    verify_app "$mounted/VoiceRewrite.app"
    ;;
esac
echo "Verified $kind ($arch, $version, $trust): $artifact"
