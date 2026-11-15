# VoiceRewrite for macOS (native host)

The Xcode project builds a SwiftUI/AppKit macOS application (`VoiceRewrite.app`) for macOS 14.0 and later. The application starts its **bundled**, architecture-matched Go `voicebridge` directly using Foundation `Process`/`Pipe` and shows the actual `hello` JSONL v1 handshake result. The Helper is launched from its absolute path in the application bundle.

## Build and launch on macOS

Requirements: Xcode 15 or later, Xcode Command Line Tools, and `go` available to the **build machine**. The build phase compiles `../voice-rewrite/cmd/voicebridge` for `ARCHS`, combines slices with `lipo` when needed, copies it to `Contents/MacOS/voicebridge`, and ad-hoc signs the helper before Xcode signs the app. The Go executable is generated during the app build.

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

`VoiceRewriteTests` exercises split and multi-line JSONL frames, invalid frames, error replies, duplicate IDs, cancellation, request-generation filtering, real subprocess startup/hello, and forced child-process exit. The real-process tests use the bundled `voicebridge` in the Debug `.app`, not a mock. Debug builds compile the Go helper as part of the application target.

## Current limitations and security

- **Handshake-only interface:** The UI checks the bundled helper with `hello`. Recording and text processing do not send `transcribe` or `selected_task` requests. Result panels remain empty until a service response is provided; the transport supports cancellation of in-flight requests.
- ASR and LLM base URLs default to `http://127.0.0.1:8080/v1` and `http://127.0.0.1:1234/v1`. The URLs and model name are editable **in memory only**; they are not sent to a service, saved to disk, or used for network requests. No API credentials are persisted.
- `NSMicrophoneUsageDescription` is included. This app does not capture audio or install global shortcuts. Accessibility permission for system-wide text operations is managed in System Settings > Privacy & Security > Accessibility.
- This Debug project uses **ad-hoc signing** and **no App Sandbox**. It is suitable for local Finder launch from the build directory, **not** notarized distribution. Gatekeeper may reject copied/quarantined builds. Distribution requires Developer ID signing of both the helper and outer app, hardened runtime, notarization, and verification of nested-code signatures. App Sandbox changes require retesting child-process execution, networking, microphone access, and accessibility permissions.
- During development the **Go toolchain** is required at build time, not at application runtime. The helper executable is bundled at `VoiceRewrite.app/Contents/MacOS/voicebridge` and runs without shell or global `PATH` resolution. stdout is reserved for JSONL; stderr is drained separately and discarded. The bridge limits individual frames to 8 MiB and drops late, canceled or unrecognized response IDs.
