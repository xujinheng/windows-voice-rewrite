#!/usr/bin/env bash
# Regression checks against a real Xcode-produced ad-hoc-signed app.
set -euo pipefail
if [[ $# -ne 3 ]]; then
  echo "usage: $0 VoiceRewrite.app arm64|x86_64 VERSION" >&2
  exit 2
fi
app="$1"
arch="$2"
version="$3"
verify="scripts/verify-macos-package.sh"
bash "$verify" app "$app" "$arch" "$version" dev

if bash "$verify" app "$app" "$arch" "999.999.999" dev >/dev/null 2>&1; then
  echo "verifier accepted the wrong version" >&2; exit 1
fi
other_arch=arm64
[[ "$arch" == arm64 ]] && other_arch=x86_64
if bash "$verify" app "$app" "$other_arch" "$version" dev >/dev/null 2>&1; then
  echo "verifier accepted a mismatched architecture" >&2; exit 1
fi

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
/usr/bin/ditto "$app" "$stage/VoiceRewrite.app"
rm "$stage/VoiceRewrite.app/Contents/MacOS/VoiceRewrite"
if bash "$verify" app "$stage/VoiceRewrite.app" "$arch" "$version" dev >/dev/null 2>&1; then
  echo "verifier accepted an app without the native Go-linked executable" >&2; exit 1
fi
echo "macOS package verifier negative cases passed"
