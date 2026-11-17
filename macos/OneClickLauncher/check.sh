#!/usr/bin/env bash
# Runnable on macos-15 (Apple Silicon) and macos-15-intel without models or GPU.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root/oneclick"
go test -race -count=1 ./...
go vet ./...
cd "$root"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
bash macos/OneClickLauncher/build.sh arm64 "$scratch/arm64"
bash macos/OneClickLauncher/build.sh x86_64 "$scratch/x86_64"
for arch in arm64 x86_64; do
  app="$scratch/$arch/VoiceRewrite-OneClick.app"
  test -x "$app/Contents/MacOS/VoiceRewrite-OneClick"
  test -x "$app/Contents/MacOS/qwen3asr-oneclick"
  test "$(lipo -archs "$app/Contents/MacOS/VoiceRewrite-OneClick")" = "$arch"
  test "$(lipo -archs "$app/Contents/MacOS/qwen3asr-oneclick")" = "$arch"
done
echo "Both macOS architectures compile; mock lifecycle tests passed (no GPU inference claimed)."
