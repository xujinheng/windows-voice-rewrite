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

## Signed distribution

The `macOS signed distribution` workflow builds separate Apple Silicon and Intel
apps, signs the nested Go helper and native app with Developer ID Application,
submits the signed ZIP for Apple notarization, staples the returned ticket, and
uploads a checksum alongside each verified ZIP. Configure the repository secret
`MACOS_DEVELOPER_ID_P12_BASE64` (base64-encoded P12 certificate),
`MACOS_DEVELOPER_ID_P12_PASSWORD`, `APPLE_NOTARY_APPLE_ID`,
`APPLE_NOTARY_APP_PASSWORD`, and repository variables
`MACOS_DEVELOPER_ID_IDENTITY` and `APPLE_TEAM_ID` before invoking that workflow.
Without those credentials only the ad-hoc signed CI developer builds are available.
A signed/notarized build keeps a stable app identity for macOS permission prompts;
it does not bypass microphone or Accessibility authorization.

## Menu bar and window lifecycle

VoiceRewrite remains active in the macOS menu bar after its main window is closed.
Closing the main window does **not** quit the app or clear the current-session history.
Use the waveform menu bar icon to view voice/meeting status, pause or resume ordinary
voice shortcuts, cancel an active voice task, reopen the workspace, open Settings,
restore a recent result, clear the current-session history, inspect recent errors
or the diagnostic log, check GitHub releases, stop a running Meeting session,
or quit VoiceRewrite. Reopening the window does not reconnect the helper or
restart shortcut listeners. Quit explicitly from the menu bar or app menu to
stop background operations and discard in-memory history.

Recent result menu labels show only the result type and time, never dictated
or rewritten words. Restoring a result opens the existing result card. The
Meeting entry opens the workspace; choose its Meeting tab to start or configure
capture. Pausing ordinary voice shortcuts does not pause Meeting capture: use
the explicit Stop Meeting action when a meeting is running. Update checks only
show the release page when available; they do not install software.

## Use

1. In Settings, enter **base** URLs for ASR and LLM services, e.g. `http://127.0.0.1:8080/v1` and `http://127.0.0.1:1234/v1`, and their model names. API keys are optional for local services that support unauthenticated HTTP requests. If required by your provider, enter its ASR or LLM key in the corresponding secure field and click Save. Keys are persisted only in Keychain; nonsecret settings are stored in a private Application Support JSON file. Each operation accesses only its own provider's key. A missing key is sent as an empty string (no Bearer header); a locked or inaccessible Keychain produces a visible error rather than an unprotected fallback.
2. At startup and after saving settings, the app probes ASR and LLM through the bundled Go Helper. The ASR probe sends one second of locally generated **silence** through the real transcription endpoint; the LLM probe requests `/models`. These probes never send a user's voice recording, selected text or prompts. The Settings page also provides separate **Test ASR connection** and **Test LLM connection** buttons. A passing probe does not prove actual dictation or generation works.\n3. Use Start Microphone Recording / Stop and Transcribe to capture the default physical microphone. The app requests microphone permission when needed, encodes 16 kHz mono signed PCM16, calls the bundled Go Helper's `transcribe` operation, then displays the actual ASR result. There are no sample transcripts or simulated microphone devices.
4. For dictation into another application, focus an editable text field and place the caret or select the text to replace. **Hold Mouse4** to speak, then release to transcribe and insert. A short Mouse4 click is replayed for normal navigation. While the button is down, a floating pill at the bottom of the screen shows the same recording, transcribing, replacement and result phases as Windows. The microphone starts only after the hold, so a short click does not open it. **Ctrl+Option+R** can also start/stop dictation. The app attempts insertion when the original foreground application remains active; Accessibility text-field support is optional for normal dictation. The app verifies the original application and any available focused AX element before sending a single keyboard paste; it cannot detect every focus change within an application when AX information is unavailable.
5. For text editing, enter an explicit instruction in the VoiceRewrite window, switch to a target application, select text, and press **Ctrl+Option+T**. Go Core decides whether to allow an attempted replacement. A valid Go decision is *not sufficient*: the native Accessibility adapter must also verify the unchanged focused element, selection and task generation. Preview-only results are never inserted.
6. To modify selected text with **spoken instructions**, select text in another application, **hold Mouse5** while speaking a rewrite/translation/explanation request and release it to process. A short Mouse5 click is replayed for normal navigation. **Ctrl+Option+E** is an alternative start/stop shortcut. Go Core runs ASR and text-edit routing as one cancelable task and returns the recognized instruction plus edit/preview decision. The original selection is captured before recording; only a still-valid Accessibility target can be changed. If focus or selection changes, the result remains a preview.\n7. Click Request Accessibility Permission to request the macOS prompt; grant VoiceRewrite access under System Settings → Privacy & Security → Accessibility. Password fields and permission failures are denied. For supported editable targets, the app validates the original AX focus and selection before a native write or a guarded foreground paste; clipboard state is restored only when VoiceRewrite still owns its temporary content. Unsupported or unverifiable editors retain the transcript for manual copying.

To dictate into another app, focus its editable text field and trigger Mouse4 or Ctrl+Option+R **without returning to VoiceRewrite**. A preselected range is replaced by the transcript. The VoiceRewrite window's Start Microphone Recording button records for preview only because clicking it changes the foreground target. Cross-app insertion requires the original foreground application to remain active. Ordinary dictation does not require readable AX text values, caret ranges or text-change notifications: it sends one native paste even to browser editors with incomplete Accessibility support. AX is retained for selected-text editing and helps reject known secure fields or changed focused elements when available. With AX unavailable, a change of focus *within* the same application cannot always be detected; dictation is deliberately optimized for input compatibility rather than exact caret verification. Before sending keyboard events, the app checks macOS event-posting permission and reports denial rather than claiming an insert. Sent events remain unverified until the target editor provides evidence. A paste event can be accepted without the target editor consuming it; session history retains the transcript.

## Microphone permission recovery

When **开始麦克风录音** reports microphone access denied, the recording has not reached ASR. Quit VoiceRewrite, open **System Settings → Privacy & Security → Microphone**, enable the **current VoiceRewrite app**, then relaunch it. The dashboard now links to that settings pane and refreshes authorization when the app becomes active. A previously denied request cannot show another system prompt without changing the system authorization.

CI macOS developer ZIPs are signed **ad hoc** rather than with a stable Developer ID identity. Replacing, moving or launching a different copy of the app can require fresh authorization; use one stable app location. If no VoiceRewrite entry appears, quit the app, run `tccutil reset Microphone com.xujinheng.VoiceRewrite` in Terminal **only if you intend to reset its microphone decision**, then reopen the app to request access again. Resetting does not grant permission automatically. If system restrictions prevent enabling the toggle, do not try to bypass macOS controls.

## Safety and limitations

- The child Helper receives ephemeral Keychain credentials only through a bounded local stdin request. No keys are supplied in argv, environment variables or persistent model request logs.
- Go HTTP requests obey context cancellation; late replies and previous UI generations are discarded. Native AX writes are single-use and revalidate the target before applying.
- Side-button input uses a native Quartz event tap for auxiliary buttons 3/4 (Mouse4/Mouse5). A press lasting at least 280 ms starts recording and is suppressed as a navigation click; a shorter press is replayed once with the original click event and no voice action. Accessibility and, depending on the OS, Input Monitoring permissions are required; device drivers that remap side buttons to keyboard keys may prevent detection. If the event tap cannot be installed, normal mouse navigation remains unaffected and keyboard shortcuts remain available. Only the system-default microphone is supported. User-visible errors are expected for unsupported devices/editors or denied permissions.
The dashboard reports the side-button listener separately from keyboard shortcuts. After granting Accessibility in System Settings, return to VoiceRewrite to retry registration automatically, or click **重新检测鼠标侧键**. Hold a side button longer than 280 ms; a brief click is navigation only. When an auxiliary mouse event reaches the app, the dashboard shows its button number. If no event is observed despite an enabled listener, check mouse-driver remapping to keyboard/browser shortcuts, Input Monitoring authorization, and whether the tested app copy is the authorized copy.

- A successful hosted CI build does **not** prove microphone hardware input, signed Keychain access or actual cross-app Accessibility behavior. Follow the physical Mac acceptance checklist in GitHub Issue #73 before shipping.
- Debug uses ad-hoc signing, no App Sandbox and no notarization. Public distribution requires Developer ID signing of the Helper and app, hardened runtime, notarization and separate sandbox/permission validation. Gatekeeper may block ZIP copies downloaded from CI.
- macOS system/application Meeting audio capture via ScreenCaptureKit is outside the current client and tracked separately in Issue #48.

## Meeting audio (macOS)

The **会议** tab offers a system-audio source or a specific running application via ScreenCaptureKit. Choose a source and start capture explicitly. The first attempt requests macOS Screen & System Audio Recording permission; after granting it, restart the app if required. VoiceRewrite excludes its own process audio. The captured PCM enters the shared Go Meeting segmentation, transcription and translation pipeline; the two caption panes can be resized by dragging the divider. Configure your ASR and LLM base URLs and model IDs in Settings. Meeting currently uses the compatible multipart ASR mode; user microphone selection, spoken expression and reply controls still belong to the remaining platform-parity backlog. Hosted CI checks the native API build and mock audio pipeline, not actual app audio or permission prompts; record those on physical hardware in issue #73 / #75.
