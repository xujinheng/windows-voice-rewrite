# VoiceRewrite for macOS

Native SwiftUI/AppKit application for macOS 14 or later. Uses a bundled Go `voicebridge` subprocess over private JSON Lines stdin/stdout; speech transcription and selected-text routing are implemented only by the existing Go Core. Apple platform access lives in three local Swift packages under `macos/Modules`.

## Build, launch and test

Requires Xcode 15+ and Go on the **build machine** (Go is not required at runtime). From the repository root:

```sh
xcodebuild -project macos/VoiceRewrite.xcodeproj -scheme VoiceRewrite \
  -configuration Debug -destination 'platform=macOS' \
  -derivedDataPath "$PWD/.voice-rewrite-derived" test

APP="$PWD/.voice-rewrite-derived/Build/Products/Debug/VoiceRewrite.app"
test -x "$APP/Contents/MacOS/voicebridge"
codesign --verify --deep --strict "$APP"
open -R "$APP"
```

Double-click VoiceRewrite.app in Finder, or use `open "$APP"`. The bundled Helper is built to the active Debug architecture and signed ad hoc. It launches directly via Foundation `Process` and `Pipe`, never a shell or an external network port.

GitHub Actions runs `swift test` in Microphone, Storage and Interaction and `xcodebuild test` in the application project on macos-15. When the tests pass, CI uploads **two native developer builds**: `VoiceRewrite-macOS-arm64-Dev` for Apple Silicon (M1/M2/M3/M4 and newer M-series) and `VoiceRewrite-macOS-x64-Dev` for Intel Mac (x86_64). Each artifact contains a ZIP of `VoiceRewrite.app` plus its SHA-256 checksum. The app and bundled `voicebridge` are verified for the matching architecture on their respective macOS 15 runners. Choose the ZIP that matches your Mac's CPU; both are **ad-hoc signed developer builds**, not notarized releases.

## Use

1. In Settings, enter **base** URLs for ASR and LLM services, e.g. `http://127.0.0.1:8080/v1` and `http://127.0.0.1:1234/v1`, and their model names. API keys are optional for local services that support unauthenticated HTTP requests. If required by your provider, enter its ASR or LLM key in the corresponding secure field and click Save. Keys are persisted only in Keychain; nonsecret settings are stored in a private Application Support JSON file. Each operation accesses only its own provider's key. A missing key is sent as an empty string (no Bearer header); a locked or inaccessible Keychain produces a visible error rather than an unprotected fallback.
2. At startup and after saving settings, the app probes ASR and LLM through the bundled Go Helper. The ASR probe sends one second of locally generated **silence** through the real transcription endpoint; the LLM probe requests `/models`. These probes never send a user's voice recording, selected text or prompts. The Settings page also provides separate **Test ASR connection** and **Test LLM connection** buttons. A passing probe does not prove actual dictation or generation works.\n3. Use Start Microphone Recording / Stop and Transcribe to capture the default physical microphone. The app requests microphone permission when needed, encodes 16 kHz mono signed PCM16, calls the bundled Go Helper's `transcribe` operation, then displays the actual ASR result. There are no sample transcripts or simulated microphone devices.
4. For dictation into another application, focus an editable text field with the caret placed at the intended position. Press **Ctrl+Option+R** to start and press again to stop. The app attempts insertion only if an Accessibility snapshot of the original target remains valid. If focus/selection/target changes, it retains the transcript in VoiceRewrite instead of writing to an unsafe location.
5. For text editing, enter an explicit instruction in the VoiceRewrite window, switch to a target application, select text, and press **Ctrl+Option+T**. Go Core decides whether to allow an attempted replacement. A valid Go decision is *not sufficient*: the native Accessibility adapter must also verify the unchanged focused element, selection and task generation. Preview-only results are never inserted.
6. To modify selected text with **spoken instructions**, select text in another application, press **Ctrl+Option+E** to start recording, speak a rewrite/translation/explanation request and press the same shortcut again to stop. Go Core runs ASR and text-edit routing as one cancelable task and returns the recognized instruction plus edit/preview decision. The original selection is captured before recording; only a still-valid Accessibility target can be changed. If focus or selection changes, the result remains a preview.\n7. Click Request Accessibility Permission to request the macOS prompt; grant VoiceRewrite access under System Settings → Privacy & Security → Accessibility. Unsupported AX editors, password fields and permission failures are denied without blind clipboard paste or synthetic keystrokes.

The app can show a recognized or edited result without changing external text. The original application must remain the valid target during the asynchronous task. A normal window button cannot authorize writing to some previously focused app.

## Safety and limitations

- The child Helper receives ephemeral Keychain credentials only through a bounded local stdin request. No keys are supplied in argv, environment variables or persistent model request logs.
- Go HTTP requests obey context cancellation; late replies and previous UI generations are discarded. Native AX writes are single-use and revalidate the target before applying.
- Native hotkeys may be unavailable if claimed by the OS or another app. Only the system-default microphone is supported. User-visible errors are expected for unsupported devices/editors or denied permissions.
- A successful hosted CI build does **not** prove microphone hardware input, signed Keychain access or actual cross-app Accessibility behavior. Follow the physical Mac acceptance checklist in GitHub Issue #57 before shipping.
- Debug uses ad-hoc signing, no App Sandbox and no notarization. Public distribution requires Developer ID signing of the Helper and app, hardened runtime, notarization and separate sandbox/permission validation. Gatekeeper may block ZIP copies downloaded from CI.
- macOS system/application Meeting audio capture via ScreenCaptureKit is outside the current client and tracked separately in Issue #48.
