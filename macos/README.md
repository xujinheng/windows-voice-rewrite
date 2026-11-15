# VoiceRewrite for macOS (native host)

This Xcode project builds a real SwiftUI/AppKit macOS application (`VoiceRewrite.app`) for macOS 14.0 and later. The application starts its **bundled**, architecture-matched Go `voicebridge` directly using Foundation `Process`/`Pipe` and shows the actual `hello` JSONL v1 handshake result. It never invokes a shell to start the helper or searches the user's `PATH` at runtime.

## Build and launch on macOS

Requirements: Xcode 15 or later, Xcode Command Line Tools, and `go` available to the **build machine**. The build phase compiles `../voice-rewrite/cmd/voicebridge` for `ARCHS`, combines slices with `lipo` when needed, copies it to `Contents/MacOS/voicebridge`, and ad-hoc signs the helper before Xcode signs the app. No prebuilt Go binary is checked in.

From the repository root:

```sh
xcodebuild -project macos/VoiceRewrite.xcodeproj \
  -scheme VoiceRewrite -configuration Debug -destination 'platform=macOS' \
  -derivedDataPath "$PWD/.voice-rewrite-derived" build

APP="$PWD/.voice-rewrite-derived/Build/Products/Debug/VoiceRewrite.app"
test -d "$APP" && test -x "$APP/Contents/MacOS/VoiceRewrite" \
  && test -x "$APP/Contents/MacOS/voicebridge"
file "$APP/Contents/MacOS/voicebridge"
lipo -info "$APP/Contents/MacOS/voicebridge"
/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Print :NSMicrophoneUsageDescription' "$APP/Contents/Info.plist"
codesign --verify --deep --strict --verbose=2 "$APP"
open -R "$APP"  # Finder: double-click VoiceRewrite.app
```

The app's workspace should transition from *connecting* to **connected: VoiceRewrite / JSONL v1** only when the bundled helper actually responds to `hello`; otherwise it displays the error. Try **Reconnect** after a failure. The **Settings** tab and `⌘,` preferences window contain editable ASR URL, LLM URL and model name fields.

## Tests (macOS)

```sh
xcodebuild -project macos/VoiceRewrite.xcodeproj \
  -scheme VoiceRewrite -configuration Debug -destination 'platform=macOS' \
  -derivedDataPath "$PWD/.voice-rewrite-derived" test
```

`VoiceRewriteTests` exercises split and multi-line JSONL frames, invalid frames, error replies, duplicate IDs, cancellation, request-generation filtering, real subprocess startup/hello, and forced child-process exit. The real-process tests use the bundled `voicebridge` in the Debug `.app`, not a mock. Debug builds intentionally include the Go build step. No macOS desktop screenshot is supplied by this repository.

## Current limitations and security

- **Handshake-only integration:** `transcribe`, `selected_task` and Go-side cancellation are specified by issue #42 but not present in the fixed baseline; this app does not yet connect recording or text editing to them. Recording and result panels show **unavailable/no results**, never invented ASR/LLM text. In-flight cancel transport is in place for eventual integration.
- ASR URL, LLM URL and model name are editable **in memory only**; no network call or persistence occurs. Later lead-owned integration will connect B (PCM16), D (settings/Keychain) and E (hotkeys/safe text operations). No API keys are written to plaintext.
- `NSMicrophoneUsageDescription` is included, but microphone capture and accessibility permission prompts are not implemented yet. When integrating global shortcuts or system-wide text operations, instruct users to grant Accessibility permission in System Settings > Privacy & Security > Accessibility; never assume the permission exists.
- This Debug project uses **ad-hoc signing** and **no App Sandbox**. It is suitable for local Finder launch from the build directory, **not** notarized distribution. Gatekeeper may reject copied/quarantined builds. Distribution requires Developer ID signing of both the helper and outer app, hardened runtime, notarization, and verification of nested-code signatures. Enabling App Sandbox later requires explicit consideration of child-process inheritance, sandboxed networking, microphone and accessibility restrictions; do not turn it on without retesting the helper.
- During development the **Go toolchain** is required at build time, not at application runtime. The helper executable is bundled at `VoiceRewrite.app/Contents/MacOS/voicebridge` and runs without shell or global `PATH` resolution. stdout is reserved for JSONL; stderr is drained separately and discarded. The bridge limits individual frames to 8 MiB and drops late, canceled or unrecognized response IDs.
